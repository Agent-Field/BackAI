// Package log is the default zero-config notifications adapter.
//
// It writes a structured log line for each notification and returns a
// synthetic provider message id of the form "log_<uuid>". Useful for
// dev environments, CI, and the "default" sandbox so the dashboard's
// activity tab works out of the box.
//
// Production deployments should configure a real adapter (Resend,
// SendGrid, Twilio, …) and leave this one as a fallback.
package log

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"

	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
)

// Adapter writes notifications to a slog.Logger.
type Adapter struct {
	log *slog.Logger
}

// compile-time interface check.
var _ notifications.Adapter = (*Adapter)(nil)

// New constructs the log adapter. log may be nil; slog.Default() is
// used in that case.
func New(log *slog.Logger) *Adapter {
	if log == nil {
		log = slog.Default()
	}
	return &Adapter{log: log}
}

// Name returns the stable identifier persisted in the row.
func (a *Adapter) Name() string { return "log" }

// Send logs the notification at INFO and returns a synthetic message
// id. Never errors — the log adapter is intentionally infallible so
// CI and dev runs always succeed.
func (a *Adapter) Send(_ context.Context, n notifications.Notification) (string, error) {
	id := "log_" + randID(16)
	from := ""
	if n.From != nil {
		from = *n.From
	}
	subject := ""
	if n.Subject != nil {
		subject = *n.Subject
	}
	a.log.Info("notifications: log adapter delivery",
		"id", n.ID,
		"kind", string(n.Kind),
		"template", n.Template,
		"to", n.To,
		"from", from,
		"subject", subject,
		"data_keys", keysOf(n.Data),
		"provider_message_id", id,
	)
	return id, nil
}

// keysOf returns the sorted keys of a map for log readability. Avoids
// dumping arbitrary user data into the log line.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// randID returns a hex-encoded random id of length 2*n.
func randID(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failure is exceptional. Fall back to a constant
		// rather than panic; collisions are fine because the log
		// adapter is dev-only.
		return "deadbeef"
	}
	return hex.EncodeToString(buf)
}
