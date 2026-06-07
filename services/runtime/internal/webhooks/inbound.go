// SPDX-License-Identifier: Apache-2.0

// inbound.go — InboundService: the verify -> dedup -> persist ->
// forward pipeline for /webhooks/in/<slug>.
//
// Lifecycle for one POST:
//
//   1. Load endpoint by slug. 404 if unknown; a missing slug is the
//      only response that surfaces the not-found state to callers
//      (everything else returns 200 so providers don't retry).
//   2. If the endpoint declares a secret, fetch it from the vault and
//      verify the HMAC signature. A mismatch persists a
//      'dropped_invalid_signature' delivery row and returns 200.
//   3. Compute the dedup token (provider header or sha256 body hash)
//      and attempt the insert. A duplicate persists a separate
//      'dropped_duplicate' row (without the token, so it can write)
//      and returns 200.
//   4. Forward to the endpoint's forward_to target. http(s):// targets
//      get a POST through a small fixed-timeout client; af://agents/...
//      targets invoke the AgentField client.
//   5. Update the delivery row with the response status / latency /
//      error string. Always reach a terminal state — the dashboard
//      never sees a row stuck at status='delivering'.
//
// Why everything returns 200: providers retry aggressively on 5xx and
// many ban a webhook destination that 4xx's too often. The dashboard
// is the audit trail; the wire reply is a probe of "did the runtime
// hear me", not "did the handler succeed".
package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
)

// AgentInvoker is the minimal contract the InboundService needs from
// the runtime's AgentField client. Stays an interface (not the
// concrete *agentfield.Client) so tests can stub it.
type AgentInvoker interface {
	Execute(ctx context.Context, path string, body []byte) (AgentResponse, error)
}

// AgentResponse mirrors the shape of agentfield.ExecuteResponse but
// kept here so this package doesn't import agentfield (which would
// create a cycle once the agentfield client itself wants to log
// webhook deliveries).
type AgentResponse struct {
	StatusCode int
	Body       []byte
}

// SecretLookup is the minimal contract the InboundService needs from
// the secrets vault. Stays an interface so tests don't need a real
// pgxpool. tenantID may be empty for cross-tenant endpoints — the
// vault treats that as "the default tenant" since endpoints can be
// declared cross-tenant in gateway.yaml.
type SecretLookup interface {
	Get(ctx context.Context, tenantID, key string) ([]byte, error)
}

// InboundService wires the endpoint store, delivery store, secret
// vault, and agent client into the single Handle entry point the HTTP
// handler calls.
type InboundService struct {
	endpoints *EndpointStore
	deliveries *DeliveryStore
	vault     SecretLookup
	agents    AgentInvoker
	client    *http.Client
	log       *slog.Logger
}

// InboundDeps is the dependency set for NewInboundService. Each
// dependency may be nil — when vault is nil, signature verification is
// skipped (and a signed endpoint will be treated as unauthenticated);
// when agents is nil, af:// forwards return ErrUnsupportedForward.
type InboundDeps struct {
	Endpoints  *EndpointStore
	Deliveries *DeliveryStore
	Vault      SecretLookup
	Agents     AgentInvoker
	Logger     *slog.Logger
}

// NewInboundService constructs an InboundService. Returns nil when
// Endpoints or Deliveries is missing — the HTTP handler treats nil as
// "module not configured" and surfaces 503.
func NewInboundService(deps InboundDeps) *InboundService {
	if deps.Endpoints == nil || deps.Deliveries == nil {
		return nil
	}
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	return &InboundService{
		endpoints:  deps.Endpoints,
		deliveries: deps.Deliveries,
		vault:      deps.Vault,
		agents:     deps.Agents,
		// SSRF defence: the forward target is user-supplied — endpoints
		// declare a forward_to URL the dashboard / provider configures.
		// safehttp refuses private CIDRs and cloud-metadata hosts so a
		// hostile endpoint config can't coerce the runtime into hitting
		// 169.254.169.254 / Postgres / internal services.
		client: safehttp.New(safehttp.Options{Timeout: 15 * time.Second}),
		log:    log,
	}
}

// Configured reports whether the service has the minimum dependencies
// wired (endpoint store + delivery store). The HTTP layer uses this to
// translate "no service" into 503.
func (s *InboundService) Configured() bool {
	return s != nil && s.endpoints != nil && s.deliveries != nil
}

// Handle processes a single inbound POST. The returned Delivery is the
// terminal row (succeeded / failed / dropped_*); the bool indicates
// whether the slug was known (false => 404, true => 200 regardless of
// dropped/failed outcome).
//
// The handler ALWAYS returns the row even on internal error — the
// dashboard needs the audit trail.
func (s *InboundService) Handle(
	ctx context.Context,
	slug string,
	headers map[string]string,
	body []byte,
) (*Delivery, bool, error) {
	if !s.Configured() {
		return nil, false, ErrNotConfigured
	}

	ep, err := s.endpoints.GetBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, ErrEndpointNotFound) {
			return nil, false, ErrEndpointNotFound
		}
		return nil, false, err
	}
	if !ep.IsActive {
		// Inactive endpoint is reported as 200 with a dropped row so
		// the dashboard shows the audit but the provider stops
		// retrying.
		d, _ := s.recordDrop(ctx, ep, headers, body, "endpoint inactive",
			StatusDroppedInvalidSignature)
		return d, true, nil
	}

	// HMAC verify (when configured). A missing vault but a secret-ref
	// endpoint downgrades to "verification skipped" — we log a warning
	// because that's a misconfiguration, not a normal path.
	if ep.SecretKeyRef != nil && *ep.SecretKeyRef != "" {
		if s.vault == nil {
			s.log.Warn("webhooks: secret-ref endpoint hit with no vault",
				"slug", slug, "secret_key_ref", *ep.SecretKeyRef)
		} else if err := s.verifySignature(ctx, ep, headers, body); err != nil {
			d, _ := s.recordDrop(ctx, ep, headers, body, err.Error(),
				StatusDroppedInvalidSignature)
			return d, true, nil
		}
	}

	// Dedup: compute token, attempt insert at status='delivering'. A
	// duplicate insert returns ErrDuplicateDelivery; we then write a
	// SECOND row (without the token) at status='dropped_duplicate' so
	// the dashboard still shows the attempt.
	token := Dedup(ep.Provider, headers, body)
	preview := truncatePreview(body, BodyPreviewBytes)

	row, err := s.deliveries.Insert(ctx, InsertParams{
		EndpointID:  &ep.ID,
		TenantID:    ep.TenantID,
		Direction:   DirectionInbound,
		Destination: ep.ForwardTo,
		EventType:   eventTypeFromHeaders(ep.Provider, headers),
		Status:      StatusDelivering,
		Headers:     filterHeaders(headers),
		Body:        body,
		BodyPreview: ptrIfSet(preview),
		DedupToken:  &token,
		ScheduledAt: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateDelivery) {
			d, _ := s.recordDrop(ctx, ep, headers, body, "duplicate delivery",
				StatusDroppedDuplicate)
			return d, true, nil
		}
		return nil, true, fmt.Errorf("webhooks: insert delivery: %w", err)
	}

	// Forward.
	respStatus, respMS, forwardErr := s.forward(ctx, ep.ForwardTo, body, headers)

	now := time.Now().UTC()
	upd := UpdateParams{
		ResponseStatus: pointerOrNilInt(respStatus),
		ResponseMS:     pointerOrNilInt(respMS),
	}
	if forwardErr == nil && respStatus >= 200 && respStatus < 300 {
		upd.Status = StatusSucceeded
		t := now
		upd.DeliveredAt = &t
	} else {
		upd.Status = StatusFailed
		errStr := ""
		if forwardErr != nil {
			errStr = forwardErr.Error()
		} else {
			errStr = fmt.Sprintf("upstream returned %d", respStatus)
		}
		upd.LastError = &errStr
	}
	attempts := row.Attempts + 1
	upd.Attempts = &attempts

	if err := s.deliveries.UpdateStatus(ctx, row.ID, upd); err != nil {
		s.log.Warn("webhooks: update terminal status failed",
			"id", row.ID, "error", err)
	}
	// Re-read for the caller so it gets the canonical row.
	if fresh, getErr := s.deliveries.Get(ctx, row.ID); getErr == nil {
		return fresh, true, nil
	}
	// Mutate in-memory as fallback.
	row.Status = upd.Status
	row.Attempts = attempts
	row.ResponseStatus = upd.ResponseStatus
	row.ResponseMS = upd.ResponseMS
	row.LastError = upd.LastError
	if upd.DeliveredAt != nil {
		v := upd.DeliveredAt.UTC().Format(time.RFC3339Nano)
		row.DeliveredAt = &v
	}
	return row, true, nil
}

// verifySignature loads the secret + checks the HMAC. Returns nil on
// success, an error (ErrSignatureMismatch / ErrUnsupportedAlgo / wrap
// of vault error) otherwise.
func (s *InboundService) verifySignature(
	ctx context.Context,
	ep *Endpoint,
	headers map[string]string,
	body []byte,
) error {
	tenantID := ""
	if ep.TenantID != nil {
		tenantID = *ep.TenantID
	}
	plaintext, err := s.vault.Get(ctx, tenantID, *ep.SecretKeyRef)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSecretLookupFailed, err)
	}
	header := ""
	if ep.SignatureHeader != nil {
		header = *ep.SignatureHeader
	}
	if header == "" {
		header = signatureHeaderForProvider(ep.Provider)
	}
	signature := caseInsensitiveHeader(headers, header)
	algo := "sha256"
	if ep.SignatureAlgorithm != nil && *ep.SignatureAlgorithm != "" {
		algo = *ep.SignatureAlgorithm
	}
	return VerifyHMAC(string(plaintext), body, signature, algo)
}

// forward dispatches the payload to the endpoint's forward_to target.
// Returns (response status, latency ms, error). HTTP forwards always
// return a non-zero status when the upstream replies, even on 5xx; an
// error is reserved for transport failures.
//
// For af://agents/<name> targets we call the AgentField client's
// Execute method with the body. AF's response shape is a JSON envelope
// — we treat AF status 2xx as success.
func (s *InboundService) forward(
	ctx context.Context,
	target string,
	body []byte,
	incomingHeaders map[string]string,
) (int, int, error) {
	target = strings.TrimSpace(target)
	switch {
	case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"):
		return s.forwardHTTP(ctx, target, body, incomingHeaders)
	case strings.HasPrefix(target, "af://agents/"):
		return s.forwardAgent(ctx, strings.TrimPrefix(target, "af://agents/"), body)
	default:
		return 0, 0, fmt.Errorf("%w: %s", ErrUnsupportedForward, target)
	}
}

func (s *InboundService) forwardHTTP(
	ctx context.Context,
	target string,
	body []byte,
	incomingHeaders map[string]string,
) (int, int, error) {
	postCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", ErrForwardFailed, err)
	}
	if ct := caseInsensitiveHeader(incomingHeaders, "Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "AF-Stack-Webhooks/0.1")
	if evt := caseInsensitiveHeader(incomingHeaders, "X-Event-Type"); evt != "" {
		req.Header.Set("X-Event-Type", evt)
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latency, fmt.Errorf("%w: %v", ErrForwardFailed, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, latency, nil
}

func (s *InboundService) forwardAgent(
	ctx context.Context,
	agentName string,
	body []byte,
) (int, int, error) {
	if s.agents == nil {
		return 0, 0, fmt.Errorf("%w: af://agents/ target but no agentfield client wired", ErrUnsupportedForward)
	}
	agentName = strings.Trim(agentName, "/")
	if agentName == "" {
		return 0, 0, fmt.Errorf("%w: empty agent name", ErrUnsupportedForward)
	}
	// AF's execute endpoint expects JSON; wrap the raw body if it
	// isn't already a JSON value.
	payload := body
	if !looksLikeJSON(body) {
		envelope := map[string]any{
			"raw_body": string(body),
		}
		payload, _ = json.Marshal(envelope)
	}
	start := time.Now()
	resp, err := s.agents.Execute(ctx, "/api/v1/execute/"+agentName, payload)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latency, fmt.Errorf("%w: %v", ErrForwardFailed, err)
	}
	return resp.StatusCode, latency, nil
}

// recordDrop persists a delivery row for a request we intentionally
// dropped (signature mismatch, duplicate, inactive endpoint). The
// dedup_token is intentionally omitted so the insert can't collide
// with the legitimate insert that triggered the drop.
func (s *InboundService) recordDrop(
	ctx context.Context,
	ep *Endpoint,
	headers map[string]string,
	body []byte,
	reason string,
	status Status,
) (*Delivery, error) {
	preview := truncatePreview(body, BodyPreviewBytes)
	errStr := reason
	now := time.Now().UTC()
	d, err := s.deliveries.Insert(ctx, InsertParams{
		EndpointID:  &ep.ID,
		TenantID:    ep.TenantID,
		Direction:   DirectionInbound,
		Destination: ep.ForwardTo,
		EventType:   eventTypeFromHeaders(ep.Provider, headers),
		Status:      status,
		Headers:     filterHeaders(headers),
		Body:        body,
		BodyPreview: ptrIfSet(preview),
		ScheduledAt: now,
	})
	if err != nil {
		s.log.Warn("webhooks: record drop failed",
			"slug", ep.Slug, "reason", reason, "error", err)
		return nil, err
	}
	// Stamp last_error so the dashboard surfaces the reason.
	_ = s.deliveries.UpdateStatus(ctx, d.ID, UpdateParams{
		Status:    status,
		LastError: &errStr,
	})
	d.LastError = &errStr
	return d, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────

// eventTypeFromHeaders picks an event-type label from provider-specific
// headers. Falls through to "webhook" for unknown providers.
func eventTypeFromHeaders(provider string, headers map[string]string) string {
	switch strings.ToLower(provider) {
	case "github":
		if v := caseInsensitiveHeader(headers, "X-GitHub-Event"); v != "" {
			return v
		}
	case "stripe":
		// Stripe sends the event type in the JSON body, not a header.
		// We leave the field blank here — a downstream consumer can
		// fill it in.
	case "shopify":
		if v := caseInsensitiveHeader(headers, "X-Shopify-Topic"); v != "" {
			return v
		}
	}
	if v := caseInsensitiveHeader(headers, "X-Event-Type"); v != "" {
		return v
	}
	return "webhook"
}

// filterHeaders strips request-bound headers (Host, Content-Length,
// Cookie, Authorization) before persisting. Keeping the curated map
// short stops the deliveries.headers jsonb from bloating + avoids
// surfacing the bearer token / session cookie in the dashboard.
func filterHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		lower := strings.ToLower(k)
		switch lower {
		case "host", "content-length", "cookie", "authorization":
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func caseInsensitiveHeader(headers map[string]string, name string) string {
	if len(headers) == 0 {
		return ""
	}
	if v, ok := headers[name]; ok && v != "" {
		return v
	}
	lower := strings.ToLower(name)
	for k, v := range headers {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return ""
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func pointerOrNilInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

func looksLikeJSON(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[', '"':
			return true
		case 't', 'f', 'n':
			return true
		default:
			if c == '-' || (c >= '0' && c <= '9') {
				return true
			}
			return false
		}
	}
	return false
}