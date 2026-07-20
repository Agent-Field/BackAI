// SPDX-License-Identifier: Apache-2.0

package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The correlation contract: the id a downstream handler reads from the request
// (X-Request-ID) MUST equal the id on the response header (and thus the log
// trace_id + error-envelope request_id). Before the fix, downstream handlers
// like the LLM gateway derived a *different* id, so cost_events.request_id
// couldn't be joined back to the request.
func TestWithLoggingStampsSameRequestIDOnRequestAndResponse(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var seenByHandler string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenByHandler = r.Header.Get("X-Request-ID")
	})
	h := withLogging(log, nil, next)

	t.Run("no correlation headers -> generated id shared", func(t *testing.T) {
		seenByHandler = ""
		req := httptest.NewRequest(http.MethodGet, "/api/v1/llm/chat/completions", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		respID := rec.Header().Get("X-Request-ID")
		if respID == "" {
			t.Fatal("response X-Request-ID must be set")
		}
		if seenByHandler != respID {
			t.Errorf("handler saw request id %q, response header is %q — must match", seenByHandler, respID)
		}
	})

	t.Run("traceparent -> trace-id shared across request and response", func(t *testing.T) {
		seenByHandler = ""
		req := httptest.NewRequest(http.MethodGet, "/api/v1/llm/chat/completions", nil)
		const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Header().Get("X-Request-ID") != traceID {
			t.Errorf("response X-Request-ID = %q, want traceparent trace-id %q", rec.Header().Get("X-Request-ID"), traceID)
		}
		if seenByHandler != traceID {
			t.Errorf("handler saw %q, want traceparent trace-id %q on the request", seenByHandler, traceID)
		}
	})

	// llmRequestID (the LLM gateway's source for cost_events.request_id) reads
	// X-Request-ID from the request — so post-fix it returns the shared id.
	t.Run("llmRequestID picks up the stamped id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat/completions", nil)
		captured := ""
		inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			captured = llmRequestID(r)
		})
		withLogging(log, nil, inner).ServeHTTP(httptest.NewRecorder(), req)
		if captured == "" || captured == "req-" {
			t.Fatalf("llmRequestID returned %q; expected the stamped correlation id", captured)
		}
	})
}
