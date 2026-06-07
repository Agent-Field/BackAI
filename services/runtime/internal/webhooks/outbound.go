// SPDX-License-Identifier: Apache-2.0

// outbound.go — thin proxy in front of a Svix sidecar.
//
// Background. OSS-AUDIT item #2 replaced the hand-rolled outbox + retry
// worker (~600 lines) with Svix (https://www.svix.com/), the same
// webhook fabric Resend, Clerk, Lago, and others ship. Svix runs as a
// sidecar Docker container (see docker-compose.yml). This file is the
// glue that lets `webhooks.send(...)` keep its existing wire shape while
// delegating delivery + retry + signing to Svix.
//
// Mental model:
//
//   - Each AF Stack tenant maps to a Svix "application" (lazy-created
//     on first send with appUid=tenant-id).
//   - Each distinct destination URL maps to a Svix "endpoint" under that
//     application (lazy-created with a deterministic UID).
//   - One `webhooks.send(url, event_type, body)` call POSTs one message
//     to the tenant's application; Svix fans it out to every endpoint
//     attached to that app. With our 1-URL-1-endpoint policy that means
//     exactly one HTTP delivery (no broadcast), matching the legacy
//     contract.
//
// We translate Svix's `MessageOut` / `MessageAttempt` shapes back into
// our `Delivery` wire shape so the dashboard's /operate/webhook-activity
// view and the SDKs see no schema drift.
//
// What this file does NOT do:
//   - Schedule retries (Svix does it).
//   - Sign requests (Svix does it, with the per-endpoint secret).
//   - Persist anything to suite_webhook_deliveries on the outbound side.
//
// The legacy `DeliveryStore` is still used by the inbound pipeline
// (inbound.go); the outbound surface no longer touches it.
package webhooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SvixConfig captures the runtime-side knobs for talking to a Svix
// sidecar. All fields are read from environment variables in
// SvixConfigFromEnv(); tests construct one directly.
type SvixConfig struct {
	// BaseURL is the Svix server base, e.g. "http://svix-server:8071".
	BaseURL string
	// Token is the JWT used as `Authorization: Bearer <token>`. Can be
	// either:
	//   1. A long-lived admin JWT minted by `svix-server jwt generate`
	//      and stored in suite_secrets under key "svix_admin_token".
	//   2. The shared secret in SVIX_JWT_SECRET that the sidecar uses to
	//      sign its own JWTs (only valid in dev / single-tenant mode).
	// Empty disables the outbound proxy (handlers return 503).
	Token string
	// HTTPTimeout caps how long we wait for the Svix sidecar to respond.
	// Svix is local so we keep this small; the actual delivery to the
	// upstream destination is bounded by Svix's own per-attempt timeout.
	HTTPTimeout time.Duration
}

// SvixConfigFromEnv reads AF_STACK_SVIX_URL + AF_STACK_SVIX_TOKEN from
// the environment. BaseURL is normalised to drop any trailing slash so
// the path joiner stays predictable.
func SvixConfigFromEnv(get func(string) string) SvixConfig {
	if get == nil {
		return SvixConfig{}
	}
	cfg := SvixConfig{
		BaseURL:     strings.TrimRight(strings.TrimSpace(get("AF_STACK_SVIX_URL")), "/"),
		Token:       strings.TrimSpace(get("AF_STACK_SVIX_TOKEN")),
		HTTPTimeout: 15 * time.Second,
	}
	return cfg
}

// OutboundService is the Svix proxy. It satisfies the same surface the
// HTTP handlers + service facade already call into (Enqueue, Retry,
// List, Get, Configured) so nothing above this layer has to change.
//
// The store field is retained — but only the inbound pipeline writes to
// it now. We keep it on the struct so the existing constructor wiring
// in main.go stays untouched.
type OutboundService struct {
	store  *DeliveryStore // retained for legacy callers; unused outbound
	cfg    SvixConfig
	client *http.Client
	log    *slog.Logger

	// ensuredApps memoises which tenant -> application UIDs we have
	// already created on Svix, so a hot send loop doesn't repeat the
	// idempotent POST /app/. Keyed by tenant ID (or "default" when the
	// caller is single-tenant).
	mu          sync.RWMutex
	ensuredApps map[string]bool
	// ensuredEndpoints memoises (tenant, destination) -> endpointUID.
	// Each unique destination URL gets one Svix endpoint UID derived
	// deterministically from sha256(URL).
	ensuredEndpoints map[string]string
}

// NewOutboundService constructs an OutboundService that proxies to Svix.
//
// The store parameter is kept for backwards compatibility with the
// runtime's constructor wiring (cmd/af-stack/main.go) but is NOT used by
// the outbound path — Svix owns the delivery state now. The inbound
// pipeline (inbound.go) still uses the store for its own rows.
//
// Configuration is read lazily so tests can mutate env after import.
func NewOutboundService(store *DeliveryStore, log *slog.Logger) *OutboundService {
	if log == nil {
		log = slog.Default()
	}
	return &OutboundService{
		store: store,
		cfg:   SvixConfigFromEnv(envFromOS),
		client: &http.Client{
			// Svix is co-located (sidecar); no SSRF concern, no need for
			// safehttp here. The destination URL is validated by Svix
			// itself before it forwards.
			Timeout: 15 * time.Second,
		},
		log:              log,
		ensuredApps:      make(map[string]bool),
		ensuredEndpoints: make(map[string]string),
	}
}

// WithConfig overrides the Svix endpoint + token. Used by tests to
// point at an httptest server without touching the environment.
func (s *OutboundService) WithConfig(cfg SvixConfig) *OutboundService {
	if s == nil {
		return nil
	}
	s.cfg = cfg
	if cfg.HTTPTimeout > 0 {
		s.client.Timeout = cfg.HTTPTimeout
	}
	// Reset memo caches: a fresh Svix server has no apps/endpoints yet.
	s.mu.Lock()
	s.ensuredApps = make(map[string]bool)
	s.ensuredEndpoints = make(map[string]string)
	s.mu.Unlock()
	return s
}

// Configured reports whether the proxy can reach a Svix sidecar.
// Without a URL the handlers surface 503 — same contract as before.
func (s *OutboundService) Configured() bool {
	return s != nil && s.cfg.BaseURL != ""
}

// Enqueue posts a message to Svix. The flow is:
//
//  1. Validate the input (same checks as the legacy version).
//  2. Ensure the tenant's application exists (idempotent POST).
//  3. Ensure an endpoint exists for the destination URL.
//  4. POST the message to /app/{app}/msg/ — Svix queues + signs +
//     retries + delivers asynchronously.
//  5. Translate Svix's response into our Delivery wire shape.
//
// The Immediate flag is honoured cosmetically only: Svix is already
// async-by-default. We return whatever Svix gives us (the message ID +
// next attempt time). The dashboard's polling refreshes the row as Svix
// progresses through retries.
func (s *OutboundService) Enqueue(ctx context.Context, in SendInput) (*Delivery, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	if err := validateSendInput(&in); err != nil {
		return nil, err
	}

	tenantKey := tenantKeyOrDefault(in.TenantID)
	appUID := tenantToAppUID(tenantKey)

	if err := s.ensureApp(ctx, appUID, tenantKey); err != nil {
		return nil, fmt.Errorf("webhooks: ensure svix application: %w", err)
	}
	endpointUID, err := s.ensureEndpoint(ctx, appUID, in.URL, in.EventType, in.Headers)
	if err != nil {
		return nil, fmt.Errorf("webhooks: ensure svix endpoint: %w", err)
	}

	// Build the message payload. Svix expects `payload` to be a JSON
	// value — we keep the caller's body as raw JSON so the upstream
	// sees exactly the bytes that the caller wrote.
	var payload json.RawMessage
	if json.Valid(in.Body) {
		payload = json.RawMessage(in.Body)
	} else {
		// Wrap non-JSON bytes so Svix accepts them. The upstream sees
		// {"raw_body":"<bytes>"} — same fallback we used on the inbound
		// agent forward.
		wrap, _ := json.Marshal(map[string]string{"raw_body": string(in.Body)})
		payload = wrap
	}

	body := map[string]any{
		"eventType": in.EventType,
		"payload":   payload,
	}
	// Svix supports an idempotency hint via `eventId`; we don't compute
	// one here (the caller is the one with replay semantics) but the
	// field stays open for a future bump.

	var resp svixMessageOut
	if err := s.svixDo(ctx, http.MethodPost, "/api/v1/app/"+appUID+"/msg/", body, &resp); err != nil {
		return nil, fmt.Errorf("webhooks: svix create message: %w", err)
	}

	d := s.messageToDelivery(resp, in, endpointUID)
	return d, nil
}

// Retry asks Svix to resend the message to its endpoint. Svix calls
// this "resend"; we keep the legacy "Retry" name.
//
// The delivery ID we accept is the Svix message id. The caller must
// supply tenant context via SendInput-style metadata, but for the
// retry endpoint we expand from the id alone — Svix lets us look up
// the app from the message via the messages-by-id endpoint.
func (s *OutboundService) Retry(ctx context.Context, deliveryID string) (*Delivery, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return nil, fmt.Errorf("%w: id required", ErrInvalidInput)
	}

	// The delivery ID embeds the tenant + endpoint when it came from
	// Enqueue (we render the composite ID below). Parse it back out.
	tenantKey, msgID, endpointUID := parseDeliveryID(deliveryID)
	if msgID == "" {
		return nil, ErrNotFound
	}
	appUID := tenantToAppUID(tenantKey)

	endpoint := "/api/v1/app/" + appUID + "/msg/" + msgID + "/endpoint/" + endpointUID + "/resend/"
	if err := s.svixDo(ctx, http.MethodPost, endpoint, struct{}{}, nil); err != nil {
		return nil, fmt.Errorf("webhooks: svix resend: %w", err)
	}
	return s.Get(ctx, deliveryID)
}

// List returns Svix's view of recent messages. We translate it to a
// page of Delivery so the dashboard's existing list renderer sees no
// drift. Filters that don't map to Svix (status, endpoint_id) are
// applied client-side after we fetch.
func (s *OutboundService) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if s == nil || !s.Configured() {
		return &ListResult{Deliveries: []Delivery{}}, nil
	}
	// Outbound-only filter? Honour it; an explicit inbound filter
	// returns an empty page since Svix doesn't manage inbound.
	if f.Direction == DirectionInbound {
		return &ListResult{Deliveries: []Delivery{}}, nil
	}

	tenantKey := tenantKeyOrDefault(f.TenantID)
	appUID := tenantToAppUID(tenantKey)

	if exists := s.appExists(appUID); !exists {
		// Lazy-list path: a brand-new tenant with no sends yet. The
		// app may legitimately not exist on Svix — return empty page
		// rather than letting Svix 404 propagate.
		return &ListResult{Deliveries: []Delivery{}}, nil
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	path := "/api/v1/app/" + appUID + "/msg/?" + q.Encode()

	var resp svixMessageList
	if err := s.svixDo(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("webhooks: svix list messages: %w", err)
	}

	out := make([]Delivery, 0, len(resp.Data))
	for _, m := range resp.Data {
		d := s.messageToDelivery(m, SendInput{TenantID: tenantKey, URL: ""}, "")
		// Client-side filter for fields Svix doesn't take as query args.
		if f.Status != "" && string(d.Status) != f.Status {
			continue
		}
		if f.EndpointID != "" && (d.EndpointID == nil || *d.EndpointID != f.EndpointID) {
			continue
		}
		out = append(out, *d)
	}

	// Apply offset/limit after filtering so the dashboard's paging
	// stays sane even with a coarse-grained filter pass.
	if f.Offset > 0 && f.Offset < len(out) {
		out = out[f.Offset:]
	} else if f.Offset >= len(out) {
		out = out[:0]
	}

	return &ListResult{
		Deliveries: out,
		Total:      len(resp.Data),
		HasMore:    resp.Done == false,
	}, nil
}

// Get fetches a single delivery by composite ID. The composite ID
// encodes (tenant, msg_id, endpoint_uid) so we can route the Svix call
// without an extra lookup table.
func (s *OutboundService) Get(ctx context.Context, deliveryID string) (*Delivery, error) {
	if s == nil || !s.Configured() {
		return nil, ErrNotFound
	}
	tenantKey, msgID, endpointUID := parseDeliveryID(deliveryID)
	if msgID == "" {
		return nil, ErrNotFound
	}
	appUID := tenantToAppUID(tenantKey)

	var msg svixMessageOut
	if err := s.svixDo(ctx, http.MethodGet,
		"/api/v1/app/"+appUID+"/msg/"+msgID+"/", nil, &msg); err != nil {
		if errors.Is(err, errSvixNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("webhooks: svix get message: %w", err)
	}

	// Pull the latest delivery attempt so the dashboard's row reflects
	// the current state (succeeded / failed / pending). A missing
	// attempt page just means Svix hasn't tried yet.
	var attempts svixAttemptList
	_ = s.svixDo(ctx, http.MethodGet,
		"/api/v1/app/"+appUID+"/msg/"+msgID+"/attempt/?limit=1", nil, &attempts)

	d := s.messageToDelivery(msg, SendInput{TenantID: tenantKey, URL: ""}, endpointUID)
	applyAttemptToDelivery(d, attempts)
	return d, nil
}

// ─── Svix HTTP plumbing ───────────────────────────────────────────────────

var errSvixNotFound = errors.New("svix: not found")

// svixDo is the one-stop HTTP shim. It marshals reqBody (when non-nil)
// as JSON, sets the Bearer token, decodes the response into out (when
// non-nil), and translates the response status into either an error or
// success. A 404 maps to errSvixNotFound so Get can surface ErrNotFound.
func (s *OutboundService) svixDo(ctx context.Context, method, path string, reqBody any, out any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.cfg.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("transport: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errSvixNotFound
	case resp.StatusCode == http.StatusConflict:
		// Idempotent creates return 409 when the entity already exists;
		// we treat that as success for the ensure-* paths and ignore the
		// body. Decoded `out` is left untouched.
		return nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out == nil || len(respBytes) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("decode: %w (body=%s)", err, truncateForError(respBytes))
		}
		return nil
	default:
		return fmt.Errorf("svix %d: %s", resp.StatusCode, truncateForError(respBytes))
	}
}

// truncateForError keeps error strings readable when Svix returns a big
// HTML page (very rare, but the Bearer-token check on the gateway can do
// it).
func truncateForError(b []byte) string {
	const max = 256
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}

// ─── Ensure application / endpoint ────────────────────────────────────────

// appExists reports whether we already issued an ensureApp for this UID.
// The check is best-effort: it only reflects what this process has
// done. Cold starts still hit ensureApp (which is idempotent on 409).
func (s *OutboundService) appExists(appUID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ensuredApps[appUID]
}

// ensureApp creates the Svix application for a tenant if we haven't yet
// in this process. POST /api/v1/app/ with appUid=<deterministic> is
// idempotent at the Svix side, so race-on-startup is safe.
func (s *OutboundService) ensureApp(ctx context.Context, appUID, displayName string) error {
	s.mu.RLock()
	if s.ensuredApps[appUID] {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	body := map[string]any{
		"uid":  appUID,
		"name": "af-stack:" + displayName,
	}
	if err := s.svixDo(ctx, http.MethodPost, "/api/v1/app/", body, nil); err != nil {
		return err
	}
	s.mu.Lock()
	s.ensuredApps[appUID] = true
	s.mu.Unlock()
	return nil
}

// ensureEndpoint creates a Svix endpoint that points at the given URL,
// keyed by sha256(url). One endpoint per destination URL means each
// message fans out to exactly one HTTP delivery — matching the legacy
// "one /send call -> one delivery" semantics.
//
// We register the event type as a filter so Svix only delivers
// messages matching the requested event_type to this endpoint. That
// way two callers using the same destination URL but different event
// types don't cross-pollinate.
func (s *OutboundService) ensureEndpoint(
	ctx context.Context,
	appUID, destURL, eventType string,
	headers map[string]string,
) (string, error) {
	cacheKey := appUID + "|" + destURL + "|" + eventType
	s.mu.RLock()
	if uid, ok := s.ensuredEndpoints[cacheKey]; ok {
		s.mu.RUnlock()
		return uid, nil
	}
	s.mu.RUnlock()

	endpointUID := endpointUIDFor(destURL, eventType)
	body := map[string]any{
		"uid":         endpointUID,
		"url":         destURL,
		"version":     1,
		"description": "af-stack:" + eventType,
		"filterTypes": []string{eventType},
	}
	if len(headers) > 0 {
		// Svix supports custom headers via a separate PATCH; for the
		// initial create we pass them inline if the build supports it
		// (newer Svix versions accept `headers`). Older versions
		// silently ignore the field, so the inline path is forward-
		// compatible without an extra round-trip.
		body["headers"] = headers
	}
	if err := s.svixDo(ctx, http.MethodPost,
		"/api/v1/app/"+appUID+"/endpoint/", body, nil); err != nil {
		return "", err
	}

	s.mu.Lock()
	s.ensuredEndpoints[cacheKey] = endpointUID
	s.mu.Unlock()
	return endpointUID, nil
}

// ─── ID helpers ───────────────────────────────────────────────────────────

// tenantKeyOrDefault collapses an empty tenant ID to "default" so the
// Svix application UID is always a valid identifier.
func tenantKeyOrDefault(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

// tenantToAppUID renders the deterministic Svix application UID for a
// tenant. We prefix with "afstack-" so an external operator scanning
// Svix can tell our apps apart from anything else hosted on the same
// sidecar.
func tenantToAppUID(tenantKey string) string {
	return "afstack-" + sanitiseUID(tenantKey)
}

// endpointUIDFor builds a deterministic endpoint UID from (URL, type).
// sha256 keeps the UID stable across runs and away from URL characters
// Svix would reject.
func endpointUIDFor(destURL, eventType string) string {
	h := sha256.Sum256([]byte(destURL + "|" + eventType))
	return "ep-" + hex.EncodeToString(h[:8])
}

// composeDeliveryID encodes (tenant, msg, endpoint) into a single
// opaque ID for the dashboard / API. Retry + Get unpack it.
func composeDeliveryID(tenantKey, msgID, endpointUID string) string {
	tk := sanitiseUID(tenantKey)
	if endpointUID == "" {
		return tk + ":" + msgID
	}
	return tk + ":" + msgID + ":" + endpointUID
}

// parseDeliveryID is the inverse of composeDeliveryID. Returns
// ("", "", "") when the ID is empty.
func parseDeliveryID(in string) (tenant, msg, endpoint string) {
	if in == "" {
		return "", "", ""
	}
	parts := strings.SplitN(in, ":", 3)
	switch len(parts) {
	case 1:
		// Caller passed a bare Svix message id — assume default tenant.
		return "default", parts[0], ""
	case 2:
		return parts[0], parts[1], ""
	default:
		return parts[0], parts[1], parts[2]
	}
}

// sanitiseUID strips characters Svix's UID validator rejects. UUIDs and
// "default" come through unchanged; only weird tenant slugs trigger the
// replacement path.
func sanitiseUID(in string) string {
	if in == "" {
		return "default"
	}
	var b strings.Builder
	b.Grow(len(in))
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ─── Svix <-> Delivery wire shape ─────────────────────────────────────────

// svixMessageOut mirrors Svix's MessageOut struct (subset). We don't
// import the Svix Go SDK because (a) it'd add a heavy dep for ~5 calls
// and (b) we already speak the wire format here.
type svixMessageOut struct {
	ID        string          `json:"id"`
	EventType string          `json:"eventType"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp string          `json:"timestamp"`
	NextAttempt string        `json:"nextAttempt,omitempty"`
}

type svixMessageList struct {
	Data []svixMessageOut `json:"data"`
	Done bool             `json:"done"`
}

type svixAttempt struct {
	ID           string `json:"id"`
	Status       int    `json:"status"`         // Svix delivery status enum
	ResponseStatus int  `json:"responseStatusCode"`
	Response     string `json:"response"`
	EndpointID   string `json:"endpointId"`
	Timestamp    string `json:"timestamp"`
}

type svixAttemptList struct {
	Data []svixAttempt `json:"data"`
}

// messageToDelivery translates a Svix message into the AF Stack
// Delivery wire shape. Where Svix doesn't have an exact equivalent (e.g.
// `body_preview`, `attempts`), we synthesise sensible defaults so the
// dashboard renders cleanly.
func (s *OutboundService) messageToDelivery(
	m svixMessageOut,
	in SendInput,
	endpointUID string,
) *Delivery {
	tenantKey := tenantKeyOrDefault(in.TenantID)

	// Body preview from payload — same UTF-8-safe truncation as before.
	var preview *string
	if len(m.Payload) > 0 {
		p := truncatePreview([]byte(m.Payload), BodyPreviewBytes)
		if p != "" {
			preview = &p
		}
	}

	id := composeDeliveryID(tenantKey, m.ID, endpointUID)
	createdAt := m.Timestamp
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	scheduledAt := createdAt
	if m.NextAttempt != "" {
		scheduledAt = m.NextAttempt
	}

	tenantPtr := tenantKey
	tenantPtrPtr := &tenantPtr

	var endpointIDPtr *string
	if endpointUID != "" {
		v := endpointUID
		endpointIDPtr = &v
	}

	dest := strings.TrimSpace(in.URL)

	d := &Delivery{
		ID:          id,
		EndpointID:  endpointIDPtr,
		Direction:   DirectionOutbound,
		Destination: dest,
		EventType:   m.EventType,
		// Svix queues immediately; until the first attempt lands we
		// report "queued" so the dashboard's badge matches what's
		// actually happening downstream.
		Status:      StatusQueued,
		Attempts:    0,
		BodyPreview: preview,
		ScheduledAt: scheduledAt,
		CreatedAt:   createdAt,
		TenantID:    tenantPtrPtr,
		Body:        []byte(m.Payload),
	}
	if dest == "" {
		// Best-effort hint when called from List (which doesn't know
		// the URL anymore) — Svix's payload doesn't include the
		// destination on the message, so we mark it as "svix" so the
		// dashboard doesn't render an empty cell.
		d.Destination = "(svix-managed)"
	}
	return d
}

// applyAttemptToDelivery overlays the latest delivery attempt onto a
// Delivery so the row reflects the current state (status / response
// status / response time). The attempt list is descending by timestamp,
// so we take the first entry.
func applyAttemptToDelivery(d *Delivery, attempts svixAttemptList) {
	if d == nil || len(attempts.Data) == 0 {
		return
	}
	a := attempts.Data[0]
	d.Attempts = len(attempts.Data)

	// Svix's status enum (MessageStatus): 0=Success, 1=Pending,
	// 2=Fail, 3=Sending. Map to our Status.
	switch a.Status {
	case 0:
		d.Status = StatusSucceeded
		t := a.Timestamp
		if t == "" {
			t = time.Now().UTC().Format(time.RFC3339Nano)
		}
		d.DeliveredAt = &t
	case 1, 3:
		d.Status = StatusDelivering
	case 2:
		d.Status = StatusFailed
		if a.Response != "" {
			r := a.Response
			d.LastError = &r
		}
	}
	if a.ResponseStatus > 0 {
		v := a.ResponseStatus
		d.ResponseStatus = &v
	}
	if a.EndpointID != "" {
		ep := a.EndpointID
		d.EndpointID = &ep
	}
}

// ─── Validation (unchanged from legacy) ───────────────────────────────────

// validateSendInput enforces the bare minimum: a parseable absolute URL
// with http/https scheme and a non-empty event type. The HTTP layer
// already validates the JSON shape; this is the last line.
func validateSendInput(in *SendInput) error {
	if in == nil {
		return fmt.Errorf("%w: nil input", ErrInvalidInput)
	}
	in.URL = strings.TrimSpace(in.URL)
	in.EventType = strings.TrimSpace(in.EventType)
	if in.URL == "" {
		return fmt.Errorf("%w: url is required", ErrInvalidInput)
	}
	if in.EventType == "" {
		return fmt.Errorf("%w: event_type is required", ErrInvalidInput)
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: url must be absolute http(s)", ErrInvalidInput)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("%w: url scheme must be http or https", ErrInvalidInput)
	}
	if in.Body == nil {
		in.Body = []byte("null")
	}
	return nil
}

// envFromOS is the default env lookup used by NewOutboundService. Tests
// override via WithConfig instead of mutating process state.
func envFromOS(k string) string {
	if k == "" {
		return ""
	}
	return os.Getenv(k)
}

// ensure errors.Is users continue to compile cleanly.
var _ = errors.Is
