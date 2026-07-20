// SPDX-License-Identifier: Apache-2.0

// idempotency_mw.go — the inbound idempotency middleware hook (PRD R1).
//
// This is the single wiring point that turns the runtime-wide idempotency
// store (internal/idempotency) into an HTTP middleware. It sits INSIDE the
// tenant resolver (so the tenant is already bound) but wraps every handler,
// and only acts when ALL of these hold:
//
//   - the method is mutating (POST/PUT/PATCH/DELETE),
//   - the path is under /api/v1,
//   - the request carries an Idempotency-Key header,
//   - a tenant is resolved on the context,
//   - a store is configured, and
//   - the request body is small enough to fingerprint (<= 1MiB).
//
// Otherwise the request passes through untouched (GET/HEAD are never
// intercepted; a missing header is a plain passthrough).
//
// Semantics (validation contract):
//   - first request with a key: the handler runs once; its response is
//     captured and stored for replay (status < 500, non-SSE, <= 1MiB).
//   - retry with same key + same fingerprint: the handler does NOT run;
//     the stored response is replayed byte-identically with an
//     Idempotency-Replayed: true header.
//   - same key + different fingerprint: 422 IDEMPOTENCY_KEY_REUSED.
//   - concurrent duplicate still in flight: 409 IDEMPOTENCY_IN_FLIGHT
//     (we return immediately rather than block a goroutine; clients retry
//     with backoff).
//   - streaming (SSE) responses, responses > 1MiB, and 5xx responses are
//     NOT stored; the reservation is released so a retry can proceed.
//
// Failing open: any bookkeeping error (reserve/save/release) is logged and
// the request proceeds normally — idempotency must never take the runtime
// down.
package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/idempotency"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const (
	idempotencyHeader         = "Idempotency-Key"
	idempotencyReplayedHeader = "Idempotency-Replayed"
	// maxIdempotentRequestBody caps how much request body we buffer to
	// fingerprint. Larger requests (e.g. big storage uploads) pass through
	// without idempotency rather than pinning memory.
	maxIdempotentRequestBody = 1 << 20 // 1 MiB
	// maxIdempotentResponseBody caps the stored response. Larger responses
	// stream to the client but are not stored for replay.
	maxIdempotentResponseBody = 1 << 20 // 1 MiB
)

// replayHeaderAllow is the subset of response headers persisted and
// restored on replay. Kept deliberately small: replays reproduce the body
// and status; volatile/transport headers (Date, X-Request-ID, tracing) are
// re-derived per request by the surrounding middleware.
var replayHeaderAllow = []string{"Content-Type", "Location"}

func idempotentMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// idempotencyMiddleware returns the PRD R1 middleware. It is only installed
// when s.idem != nil (see New()).
func (s *Server) idempotencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.idem == nil || !idempotentMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
		if key == "" || !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		// No resolved tenant ⇒ we cannot scope the key; pass through.
		if tenantctx.TenantID(r.Context()) == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Response capture requires the statusWriter installed by
		// withLogging. In production it is always present; if not, degrade
		// to a plain passthrough rather than run without capture.
		sw, ok := w.(*statusWriter)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		// Skip fingerprinting for oversized request bodies (declared or
		// actual) — pass through, preserving the body for the handler.
		if r.ContentLength > maxIdempotentRequestBody {
			next.ServeHTTP(w, r)
			return
		}
		body, rest, tooBig, err := readCappedBody(r.Body, maxIdempotentRequestBody)
		if err != nil {
			// Body read failed — restore what we have and let the handler
			// surface the error normally.
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), rest))
			next.ServeHTTP(w, r)
			return
		}
		if tooBig {
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), rest))
			next.ServeHTTP(w, r)
			return
		}
		// Body fully buffered; replace it so the handler can read it again.
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))

		fingerprint := idempotency.Fingerprint(r.Method, r.URL.Path, r.URL.RawQuery, body)

		res, err := s.idem.Reserve(r.Context(), key, fingerprint)
		if err != nil {
			// Fail open: never let idempotency bookkeeping break the request.
			s.log.Warn("idempotency reserve failed",
				"error", err, "method", r.Method, "path", r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}

		switch res.Outcome {
		case idempotency.OutcomeMismatch:
			writeError(w, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED",
				"Idempotency-Key was already used for a different request", nil)
		case idempotency.OutcomeInFlight:
			writeError(w, http.StatusConflict, "IDEMPOTENCY_IN_FLIGHT",
				"a request with this Idempotency-Key is already in progress; retry shortly", nil)
		case idempotency.OutcomeReplay:
			replayStoredResponse(w, res.Response)
		case idempotency.OutcomeAcquired:
			s.runCapturedHandler(next, sw, r, key)
		}
	})
}

// runCapturedHandler runs the downstream handler for a request that just
// claimed an idempotency key, capturing the response so it can be stored
// for replay. On a normal (<500, non-SSE, <=1MiB) response it saves; on a
// server error, oversized, or streaming response — or a panic — it releases
// the reservation so a retry can proceed.
func (s *Server) runCapturedHandler(next http.Handler, sw *statusWriter, r *http.Request, key string) {
	sw.beginCapture(maxIdempotentResponseBody)
	completed := false
	defer func() {
		if completed {
			return
		}
		// Handler panicked (propagating to withRecover). Release the
		// reservation so a retry can proceed. Use a detached-but-tenant
		// context so client disconnect doesn't abort the cleanup.
		if relErr := s.idem.Release(context.WithoutCancel(r.Context()), key); relErr != nil {
			s.log.Warn("idempotency release (panic) failed",
				"error", relErr, "path", r.URL.Path)
		}
	}()

	next.ServeHTTP(sw, r)
	completed = true

	capturedBody, overflowed := sw.captured()
	contentType := sw.Header().Get("Content-Type")
	storable := !overflowed &&
		sw.status < http.StatusInternalServerError &&
		!strings.Contains(contentType, "text/event-stream")

	// Detach from the request context's cancellation (the client may have
	// disconnected) while preserving the tenant binding for RLS.
	bgCtx := context.WithoutCancel(r.Context())
	if storable {
		resp := idempotency.StoredResponse{
			Status:  sw.status,
			Headers: capturedHeaderSubset(sw.Header()),
			Body:    append([]byte(nil), capturedBody...),
		}
		if saveErr := s.idem.Save(bgCtx, key, resp); saveErr != nil {
			s.log.Warn("idempotency save failed",
				"error", saveErr, "path", r.URL.Path)
		}
		return
	}
	if relErr := s.idem.Release(bgCtx, key); relErr != nil {
		s.log.Warn("idempotency release failed",
			"error", relErr, "path", r.URL.Path)
	}
}

// readCappedBody reads up to limit bytes from rc. If the body is at most
// limit bytes it returns (body, nil, false, nil). If it exceeds limit it
// returns the bytes read so far plus a reader for the remainder so the
// caller can reconstruct the full body for the handler.
func readCappedBody(rc io.ReadCloser, limit int) (body []byte, rest io.Reader, tooBig bool, err error) {
	if rc == nil {
		return nil, nil, false, nil
	}
	limited := io.LimitReader(rc, int64(limit)+1)
	buf, readErr := io.ReadAll(limited)
	if readErr != nil {
		return buf, rc, false, readErr
	}
	if len(buf) > limit {
		// One byte over the cap was read; stitch it back ahead of the rest.
		return buf, io.MultiReader(bytes.NewReader(buf[limit:]), rc), true, nil
	}
	return buf, nil, false, nil
}

// replayStoredResponse writes a previously captured response, marking it as
// a replay. Body is byte-identical to the original.
func replayStoredResponse(w http.ResponseWriter, resp *idempotency.StoredResponse) {
	if resp == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL",
			"idempotent replay missing stored response", nil)
		return
	}
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	w.Header().Set(idempotencyReplayedHeader, "true")
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

// capturedHeaderSubset extracts the replay-relevant response headers.
func capturedHeaderSubset(h http.Header) map[string]string {
	out := map[string]string{}
	for _, k := range replayHeaderAllow {
		if v := h.Get(k); v != "" {
			out[k] = v
		}
	}
	return out
}
