// Package webhooks is AF Stack's webhook outbox + inbox layer.
//
// The package owns two surfaces that share a single `suite_webhook_deliveries`
// table (column `direction` distinguishes them):
//
//   - INBOUND  — endpoints declared in `gateway.yaml` (or via the SDK).
//     The runtime stores raw payloads, verifies HMAC, dedups by token,
//     and forwards to a handler. Phase 10.2 owns inbound.go / verify.go /
//     dedup.go.
//
//   - OUTBOUND — `webhooks.send()` enqueues a delivery row in the PG
//     outbox; a background worker drains the queue with retries and
//     records the upstream response. Phase 10.3 owns outbound.go /
//     worker.go.
//
// Contract notes
//
//	The wire schemas live in apps/dashboard/src/lib/api.ts
//	(WebhookDeliverySchema, WebhookDirectionSchema, SendWebhookInputSchema,
//	WebhookDeliveryListSchema). Field names, enum values, and nullable
//	semantics here MUST mirror the zod schemas — the dashboard's safeParse
//	will reject anything drifting.
//
// Status lifecycle:
//
//	queued         — row inserted, never attempted
//	delivering     — currently being POSTed (outbound) / handled (inbound)
//	succeeded      — 2xx response (outbound) / handler returned ok (inbound)
//	failed         — non-2xx or transport error; worker will retry until
//	                 attempts >= MaxAttempts
//	dropped_duplicate          — inbound only; dedup token already seen
//	dropped_invalid_signature  — inbound only; HMAC verify failed
package webhooks

import (
	"errors"
	"time"
)

// Direction tags whether a delivery row was emitted by us (outbound) or
// received from a third-party provider (inbound). Mirrors
// WebhookDirectionSchema in api.ts.
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// Status is the delivery row lifecycle. Mirrors the status enum in
// WebhookDeliverySchema.
type Status string

const (
	StatusQueued                  Status = "queued"
	StatusDelivering              Status = "delivering"
	StatusSucceeded               Status = "succeeded"
	StatusFailed                  Status = "failed"
	StatusDroppedDuplicate        Status = "dropped_duplicate"
	StatusDroppedInvalidSignature Status = "dropped_invalid_signature"
)

// IsTerminal reports whether the status represents a finished delivery
// (i.e. the worker will not pick it back up).
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusDroppedDuplicate, StatusDroppedInvalidSignature:
		return true
	default:
		return false
	}
}

// Endpoint is the persisted row shape returned to clients. Mirrors
// WebhookEndpointSchema. Pointer fields stand in for nullable wire
// columns so the dashboard's safeParse sees JSON `null` rather than the
// empty string when an optional field is unset.
type Endpoint struct {
	ID                 string  `json:"id"`
	Slug               string  `json:"slug"`
	TenantID           *string `json:"tenant_id"`
	Provider           string  `json:"provider"`
	SecretKeyRef       *string `json:"secret_key_ref"`
	SignatureAlgorithm *string `json:"signature_algorithm"`
	SignatureHeader    *string `json:"signature_header"`
	ForwardTo          string  `json:"forward_to"`
	DedupWindowS       int     `json:"dedup_window_s"`
	IsActive           bool    `json:"is_active"`
	CreatedAt          string  `json:"created_at"`
}

// CreateEndpointInput is the structured input to EndpointStore.Create.
// Mirrors CreateEndpointInputSchema in api.ts; the HTTP layer fills in
// TenantID from the request context.
type CreateEndpointInput struct {
	Slug               string
	Provider           string
	ForwardTo          string
	SecretKeyRef       *string
	SignatureAlgorithm string
	SignatureHeader    string
	DedupWindowS       int
	TenantID           string // empty -> NULL (cross-tenant inbound)
}

// ListEndpointsFilter scopes EndpointStore.List. Empty TenantID returns
// every endpoint (admin path).
type ListEndpointsFilter struct {
	TenantID string
}

// Delivery is the persisted row shape returned to clients. Mirrors
// WebhookDeliverySchema exactly. Nullable wire fields use pointer types
// so json.Marshal emits null rather than the zero value.
type Delivery struct {
	ID             string    `json:"id"`
	EndpointID     *string   `json:"endpoint_id"`
	Direction      Direction `json:"direction"`
	Destination    string    `json:"destination"`
	EventType      string    `json:"event_type"`
	Status         Status    `json:"status"`
	Attempts       int       `json:"attempts"`
	ResponseStatus *int      `json:"response_status"`
	ResponseMS     *int      `json:"response_ms"`
	BodyPreview    *string   `json:"body_preview"`
	BodyStorageURL *string   `json:"body_storage_url"`
	LastError      *string   `json:"last_error"`
	ScheduledAt    string    `json:"scheduled_at"`
	DeliveredAt    *string   `json:"delivered_at"`
	CreatedAt      string    `json:"created_at"`

	// TenantID is not part of the wire schema (the dashboard
	// derives it from the auth context) but the DB row carries it so
	// the list endpoint can filter.
	TenantID *string `json:"-"`

	// Headers + Body are not surfaced on the list endpoint but are
	// needed when the worker re-POSTs an outbound delivery. They are
	// populated by Insert/Get and consumed by Deliver.
	Headers map[string]string `json:"-"`
	Body    []byte            `json:"-"`
}

// SendInput is the public input shape for OutboundService.Enqueue.
// Mirrors SendWebhookInputSchema in api.ts (sans the JSON-encoded body
// payload, which we accept as []byte at the Go layer for flexibility).
type SendInput struct {
	URL       string
	EventType string
	// Body is the raw request body bytes. The HTTP layer encodes the
	// caller's `body` field with json.Marshal before invoking Enqueue.
	Body      []byte
	Headers   map[string]string
	Immediate bool

	// TenantID is filled in from the request context by the HTTP
	// handler; the SDK doesn't surface it.
	TenantID string
}

// ListFilters mirrors the query params on GET /api/v1/webhooks/deliveries.
type ListFilters struct {
	Direction  Direction
	EndpointID string
	TenantID   string
	Status     string
	Limit      int
	Offset     int
}

// ListResult is one page of suite_webhook_deliveries.
type ListResult struct {
	Deliveries []Delivery
	Total      int
	HasMore    bool
}

// Defaults applied by the worker + Enqueue.
const (
	// MaxAttempts caps outbound retries. After this many failures the
	// worker stops re-scheduling — the row stays at status='failed'
	// and the dashboard can trigger a manual retry.
	MaxAttempts = 8

	// PollInterval is how often the worker scans for ready rows.
	PollInterval = 2 * time.Second

	// BatchSize is how many rows the worker claims per scan.
	BatchSize = 32

	// BackoffStep is multiplied by the current attempt count to set
	// the next scheduled_at. Capped at MaxBackoff.
	BackoffStep = 2 * time.Second
	MaxBackoff  = 5 * time.Minute

	// DefaultTimeout bounds a single outbound POST. Worker reuses
	// this; the immediate path takes it as an upper bound too.
	DefaultTimeout = 30 * time.Second

	// BodyPreviewBytes is the cap on body_preview (truncated UTF-8
	// safe; the full body lives in Body / object storage).
	BodyPreviewBytes = 4096
)

// Sentinel errors. The HTTP handlers map each to a code + status.
var (
	ErrInvalidInput    = errors.New("webhooks: invalid input")
	ErrNotFound        = errors.New("webhooks: delivery not found")
	ErrNotConfigured   = errors.New("webhooks: store not configured")
	ErrTransport       = errors.New("webhooks: transport error")
	ErrUpstreamFailure = errors.New("webhooks: upstream returned non-2xx")

	// Inbound-specific.
	ErrEndpointNotFound   = errors.New("webhooks: endpoint not found")
	ErrEndpointInactive   = errors.New("webhooks: endpoint inactive")
	ErrSlugConflict       = errors.New("webhooks: slug already in use")
	ErrSignatureMismatch  = errors.New("webhooks: signature mismatch")
	ErrUnsupportedAlgo    = errors.New("webhooks: unsupported signature algorithm")
	ErrUnsupportedForward = errors.New("webhooks: unsupported forward target")
	ErrDuplicateDelivery  = errors.New("webhooks: duplicate delivery")
	ErrSecretLookupFailed = errors.New("webhooks: secret lookup failed")
	ErrForwardFailed      = errors.New("webhooks: forward failed")
)
