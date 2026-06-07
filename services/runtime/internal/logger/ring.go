// ring.go — in-memory ring buffer of recent log lines.
//
// Powers the dashboard's /operate/logs tab. The runtime owns one of
// these buffers, the structured logger writes every line through it,
// and the /api/v1/logs endpoint reads from it. Strictly process-local
// (multi-process deployments need a real log aggregator like Loki).
//
// We capture only the data the dashboard renders (ts, level, msg,
// service, plus a handful of well-known attributes); everything else
// goes into the structured "fields" map so the JSON tail still
// surfaces full context when the operator expands a row.
package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// RingSize is the cap on remembered log lines. Each line is small
// (well under 1 KiB at p99) so 2k entries fits comfortably in memory.
const RingSize = 2048

// LineLevel is the dashboard's narrowed level set. We don't expose
// slog.Level directly because the dashboard schema requires the
// lowercase strings below — anything outside the set collapses to
// "info" so the buffer stays parsable.
type LineLevel string

const (
	LevelDebug LineLevel = "debug"
	LevelInfo  LineLevel = "info"
	LevelWarn  LineLevel = "warn"
	LevelError LineLevel = "error"
)

// Line is one captured record. Field tags mirror LogLineSchema in
// apps/dashboard/src/lib/api.ts. Optional fields use `omitempty` so
// absent values stay absent (zod treats undefined as fine for
// .optional()).
type Line struct {
	TS        string         `json:"ts"`
	Level     LineLevel      `json:"level"`
	Service   string         `json:"service"`
	Msg       string         `json:"msg"`
	RequestID string         `json:"request_id,omitempty"`
	TenantID  string         `json:"tenant_id,omitempty"`
	Agent     string         `json:"agent,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Ring is a fixed-capacity circular buffer of Lines. Safe for
// concurrent writes (the structured logger writes from every
// request goroutine) and concurrent reads from the HTTP handler.
type Ring struct {
	mu      sync.Mutex
	lines   []Line
	idx     int
	full    bool
	service string
}

// NewRing constructs a Ring of the given capacity. service is stamped
// onto every line as the dashboard renders it next to the badge.
func NewRing(capacity int, service string) *Ring {
	if capacity <= 0 {
		capacity = RingSize
	}
	if service == "" {
		service = "af-stack"
	}
	return &Ring{
		lines:   make([]Line, capacity),
		service: service,
	}
}

// Append writes one Line into the buffer.
func (r *Ring) Append(line Line) {
	if r == nil {
		return
	}
	if line.TS == "" {
		line.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if line.Service == "" {
		line.Service = r.service
	}
	r.mu.Lock()
	r.lines[r.idx] = line
	r.idx = (r.idx + 1) % len(r.lines)
	if r.idx == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Recent returns up to `limit` of the most-recent lines, ordered
// oldest-first so the dashboard renders top-to-bottom chronologically.
// When the buffer hasn't filled the requested limit, the actual size
// is returned.
func (r *Ring) Recent(limit int) []Line {
	if r == nil {
		return nil
	}
	if limit <= 0 {
		limit = 200
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.idx
	if r.full {
		n = len(r.lines)
	}
	if n == 0 {
		return []Line{}
	}
	if limit > n {
		limit = n
	}
	out := make([]Line, 0, limit)
	// Walk backwards from the most-recent index `limit` steps, then
	// emit forwards so callers see chronological order.
	start := r.idx - limit
	if start < 0 {
		start += len(r.lines)
	}
	for i := 0; i < limit; i++ {
		out = append(out, r.lines[(start+i)%len(r.lines)])
	}
	return out
}

// ringHandler wraps an underlying slog.Handler and tees every record
// into the Ring as a Line. The wrapping is transparent: callers (and
// children created via WithAttrs / WithGroup) see the same Handler
// surface as the underlying one.
//
// We use a json.Encoder + bytes.Buffer to walk the record's attributes
// — slog.Handler doesn't expose them directly without writing them out.
type ringHandler struct {
	inner slog.Handler
	ring  *Ring
	attrs []slog.Attr
}

// NewRingHandler wraps `inner` so every log record is appended to ring
// in addition to inner.Handle(). Pass the same level filter you'd use
// on the underlying handler.
//
// When ring is nil this returns the inner handler unchanged so callers
// can switch tee-mode on and off without conditional plumbing.
func NewRingHandler(inner slog.Handler, ring *Ring) slog.Handler {
	if ring == nil {
		return inner
	}
	return &ringHandler{inner: inner, ring: ring}
}

func (h *ringHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *ringHandler) Handle(ctx context.Context, rec slog.Record) error {
	// Collect well-known fields + a generic map.
	line := Line{
		TS:    rec.Time.UTC().Format(time.RFC3339Nano),
		Level: lineLevelFromSlog(rec.Level),
		Msg:   rec.Message,
	}
	fields := map[string]any{}
	for _, a := range h.attrs {
		populateLineField(&line, fields, a)
	}
	rec.Attrs(func(a slog.Attr) bool {
		populateLineField(&line, fields, a)
		return true
	})
	if len(fields) > 0 {
		line.Fields = fields
	}
	h.ring.Append(line)
	return h.inner.Handle(ctx, rec)
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &ringHandler{
		inner: h.inner.WithAttrs(attrs),
		ring:  h.ring,
		attrs: merged,
	}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{
		inner: h.inner.WithGroup(name),
		ring:  h.ring,
		attrs: h.attrs,
	}
}

func lineLevelFromSlog(l slog.Level) LineLevel {
	switch {
	case l <= slog.LevelDebug:
		return LevelDebug
	case l <= slog.LevelInfo:
		return LevelInfo
	case l <= slog.LevelWarn:
		return LevelWarn
	default:
		return LevelError
	}
}

// populateLineField inspects one attribute and routes it into the
// dashboard's well-known columns when the key matches; everything
// else lands in the generic fields map. We resolve before reading so
// LogValuer attrs (lazy values) materialise.
func populateLineField(line *Line, fields map[string]any, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	v := a.Value.Resolve()
	switch a.Key {
	case "request_id", "requestID":
		line.RequestID = asString(v)
	case "tenant_id", "tenantID":
		line.TenantID = asString(v)
	case "agent":
		line.Agent = asString(v)
	case "service":
		// service is a header on Line, not a generic field; mirror.
		s := asString(v)
		if s != "" {
			line.Service = s
		}
	default:
		fields[a.Key] = anyFromValue(v)
	}
}

// asString flattens an slog.Value into a plain string for our
// well-known columns. Everything else (numbers, slices) gets a default
// JSON-style representation.
func asString(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindGroup:
		// Groups don't make sense as a single string; fall back to
		// encoding so we never panic.
		buf := &bytes.Buffer{}
		_ = json.NewEncoder(buf).Encode(anyFromValue(v))
		return buf.String()
	default:
		return v.String()
	}
}

func anyFromValue(v slog.Value) any {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindGroup:
		m := map[string]any{}
		for _, sub := range v.Group() {
			m[sub.Key] = anyFromValue(sub.Value.Resolve())
		}
		return m
	default:
		return v.Any()
	}
}
