// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/Agent-Field/backai/services/runtime/internal/logger"
)

const sentryFlushTimeout = 5 * time.Second

var (
	bearerTokenRE = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	openAIKeyRE   = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)
	afKeyRE       = regexp.MustCompile(`\baf_[A-Za-z0-9]{4,}_[A-Za-z0-9_-]{12,}\b`)
)

type SentryConfig struct {
	DSN         string
	Environment string
	Release     string
	ServiceName string
}

type SentryIntegration struct {
	enabled bool
}

func SetupSentry(cfg SentryConfig) (*SentryIntegration, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return &SentryIntegration{}, nil
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "af-stack"
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		AttachStacktrace: true,
		SendDefaultPII:   false,
		EnableTracing:    false,
		TracesSampleRate: 0,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			return scrubSentryEvent(event)
		},
	})
	if err != nil {
		return nil, err
	}
	return &SentryIntegration{enabled: true}, nil
}

func (s *SentryIntegration) Enabled() bool {
	return s != nil && s.enabled
}

func (s *SentryIntegration) WrapHandler(base slog.Handler) slog.Handler {
	if !s.Enabled() {
		return base
	}
	return logger.NewTeeHandler(base, &sentrySlogHandler{})
}

func (s *SentryIntegration) Flush() bool {
	if !s.Enabled() {
		return true
	}
	return sentry.Flush(sentryFlushTimeout)
}

type sentrySlogHandler struct {
	attrs []slog.Attr
}

func (h *sentrySlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelError
}

func (h *sentrySlogHandler) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level < slog.LevelError {
		return nil
	}
	extra := map[string]any{}
	tags := map[string]string{}
	for _, attr := range h.attrs {
		addSentryAttr(extra, tags, attr)
	}
	rec.Attrs(func(attr slog.Attr) bool {
		addSentryAttr(extra, tags, attr)
		return true
	})
	event := sentry.NewEvent()
	event.Level = sentryLevel(rec.Level)
	event.Message = scrubString(rec.Message)
	event.Timestamp = rec.Time.UTC()
	event.Platform = "go"
	event.Logger = "slog"
	event.Contexts = map[string]sentry.Context{"slog": extra}
	event.Tags = tags
	sentry.CaptureEvent(event)
	return nil
}

func (h *sentrySlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &sentrySlogHandler{attrs: make([]slog.Attr, 0, len(h.attrs)+len(attrs))}
	next.attrs = append(next.attrs, h.attrs...)
	next.attrs = append(next.attrs, attrs...)
	return next
}

func (h *sentrySlogHandler) WithGroup(name string) slog.Handler {
	return h
}

func addSentryAttr(extra map[string]any, tags map[string]string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	key := attr.Key
	if key == "" {
		return
	}
	value := attr.Value.Any()
	if sensitiveKey(key) {
		extra[key] = "[Filtered]"
		return
	}
	switch key {
	case "service", "component", "adapter", "tenant_id", "request_id", "trace_id":
		if s := strings.TrimSpace(scrubString(stringValue(value))); s != "" {
			tags[key] = s
		}
	default:
		extra[key] = scrubAny(value)
	}
}

func scrubSentryEvent(event *sentry.Event) *sentry.Event {
	if event == nil {
		return nil
	}
	event.Message = scrubString(event.Message)
	for contextName, contextValue := range event.Contexts {
		for k, v := range contextValue {
			if sensitiveKey(k) {
				contextValue[k] = "[Filtered]"
				continue
			}
			contextValue[k] = scrubAny(v)
		}
		event.Contexts[contextName] = contextValue
	}
	for k, v := range event.Tags {
		event.Tags[k] = scrubString(v)
	}
	return event
}

func scrubAny(v any) any {
	switch t := v.(type) {
	case string:
		return scrubString(t)
	case []byte:
		return scrubString(string(t))
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			if sensitiveKey(k) {
				out[k] = "[Filtered]"
			} else {
				out[k] = scrubAny(v)
			}
		}
		return out
	default:
		return v
	}
}

func scrubString(value string) string {
	value = bearerTokenRE.ReplaceAllString(value, "Bearer [Filtered]")
	value = openAIKeyRE.ReplaceAllString(value, "[Filtered]")
	value = afKeyRE.ReplaceAllString(value, "[Filtered]")
	return value
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, token := range []string{"authorization", "password", "secret", "token", "api_key", "apikey", "cookie"} {
		if strings.Contains(key, token) {
			return true
		}
	}
	return false
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func sentryLevel(level slog.Level) sentry.Level {
	if level >= slog.Level(12) {
		return sentry.LevelFatal
	}
	return sentry.LevelError
}
