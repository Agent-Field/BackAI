// Package notifications is AF Stack's outbox-style notification layer.
//
// A Notification is inserted into suite_notifications by Service.Send at
// status='queued'. A background worker drains the table every ~2s and
// dispatches each row through the configured Adapter (log, Resend, …).
// Status transitions are atomic so the dashboard always sees one of
// queued / sending / sent / failed / skipped.
//
// Contract notes
//
//	The wire schemas live in apps/dashboard/src/lib/api.ts
//	(NotificationSchema, NotificationListSchema, SendNotificationInputSchema,
//	NotificationStatsSchema). Field names, enum values, and nullable
//	semantics here MUST mirror those zod schemas — drift breaks the
//	dashboard's safeParse.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind enumerates the outbound channels.
type Kind string

const (
	KindEmail Kind = "email"
	KindSMS   Kind = "sms"
	KindPush  Kind = "push"
	KindLog   Kind = "log"
)

// Status is the lifecycle phase of a notification row.
type Status string

const (
	StatusQueued  Status = "queued"
	StatusSending Status = "sending"
	StatusSent    Status = "sent"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// IsTerminal reports whether the status represents a finished delivery.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSent, StatusFailed, StatusSkipped:
		return true
	default:
		return false
	}
}

// Notification mirrors the persisted suite_notifications row and the
// NotificationSchema in apps/dashboard/src/lib/api.ts. Nullable wire
// fields use pointer types so json.Marshal emits null rather than the
// zero value.
type Notification struct {
	ID                string         `json:"id"`
	TenantID          *string        `json:"tenant_id"`
	Kind              Kind           `json:"kind"`
	Adapter           *string        `json:"adapter"`
	Template          string         `json:"template"`
	To                string         `json:"to"`
	From              *string        `json:"from"`
	Subject           *string        `json:"subject"`
	Data              map[string]any `json:"data"`
	Status            Status         `json:"status"`
	ProviderMessageID *string        `json:"provider_message_id"`
	Attempts          int            `json:"attempts"`
	LastError         *string        `json:"last_error"`
	ScheduledAt       string         `json:"scheduled_at"`
	SentAt            *string        `json:"sent_at"`
	CreatedAt         string         `json:"created_at"`
}

// SendInput mirrors SendNotificationInputSchema. Pointer fields stand in
// for "omitted" so the Service can apply defaults vs treating "" as
// "blank on purpose".
type SendInput struct {
	TenantID    string
	Kind        Kind
	Template    string
	To          string
	From        string
	Subject     string
	Data        map[string]any
	ScheduledAt *time.Time
}

// ListFilters mirrors the query params on GET /api/v1/notifications.
type ListFilters struct {
	TenantID string
	Status   string
	Kind     string
	Limit    int
	Offset   int
}

// ListResult is one page of suite_notifications.
type ListResult struct {
	Notifications []Notification
	Total         int
	HasMore       bool
}

// Stats mirrors NotificationStatsSchema.
type Stats struct {
	ByStatus     map[Status]int `json:"by_status"`
	ByAdapter    []AdapterCount `json:"by_adapter"`
	SentToday    int            `json:"sent_today"`
	FailedToday  int            `json:"failed_today"`
}

// AdapterCount is one entry in Stats.ByAdapter.
type AdapterCount struct {
	Adapter string `json:"adapter"`
	Count   int    `json:"count"`
}

// Adapter is the wire-level contract every notification backend must
// implement. Implementations should be safe for concurrent use; the
// worker calls Send from up to 16 goroutines at once.
type Adapter interface {
	// Send dispatches the notification through this adapter. The returned
	// provider message ID becomes the row's provider_message_id column
	// (useful for deep-linking from the dashboard to e.g. Resend's UI).
	// A non-nil error marks the row as failed; the worker records the
	// error message verbatim in last_error.
	Send(ctx context.Context, n Notification) (providerMessageID string, err error)

	// Name returns the stable adapter identifier persisted in the row's
	// adapter column ("log", "resend", "sendgrid", …).
	Name() string
}

// Sentinel errors. The REST layer maps each to a code + status.
var (
	ErrInvalidInput  = errors.New("notifications: invalid input")
	ErrNotConfigured = errors.New("notifications: not configured")
	ErrNotFound      = errors.New("notifications: not found")
	ErrAdapter       = errors.New("notifications: adapter error")
)

// Validate normalises the input in-place and returns an error if any
// field is out of bounds.
func (in *SendInput) Validate() error {
	if in == nil {
		return fmt.Errorf("%w: nil input", ErrInvalidInput)
	}
	in.Template = strings.TrimSpace(in.Template)
	if in.Template == "" {
		// Default template is intentionally permissive — Resend adapter
		// will fall back to body=text(data) when no real template ships.
		in.Template = "default"
	}
	in.To = strings.TrimSpace(in.To)
	if in.To == "" {
		return fmt.Errorf("%w: to is required", ErrInvalidInput)
	}
	if len(in.To) > 320 {
		return fmt.Errorf("%w: to exceeds 320 chars", ErrInvalidInput)
	}
	in.Subject = strings.TrimSpace(in.Subject)
	in.From = strings.TrimSpace(in.From)
	switch in.Kind {
	case "":
		in.Kind = KindEmail
	case KindEmail, KindSMS, KindPush, KindLog:
		// ok
	default:
		return fmt.Errorf("%w: kind must be email|sms|push|log", ErrInvalidInput)
	}
	if in.Data == nil {
		in.Data = map[string]any{}
	}
	return nil
}

// MaxBatchSize caps how many queued rows the worker processes in a
// single loop iteration.
const MaxBatchSize = 16
