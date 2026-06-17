// SPDX-License-Identifier: Apache-2.0

package toolstats

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const (
	TransportNative = "native"
	TransportMCP    = "mcp"

	StatusSuccess = "success"
	StatusError   = "error"
	StatusTimeout = "timeout"

	defaultTenant = "00000000-0000-0000-0000-000000000000"
	bufferSize    = 1024
)

type Event struct {
	TenantID   string
	AgentID    string
	ToolName   string
	Transport  string
	DurationMS int64
	Status     string
	ErrorCode  string
	CalledAt   time.Time
}

type Recorder struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	ch   chan Event
}

func NewRecorder(pool *pgxpool.Pool, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.Default()
	}
	r := &Recorder{pool: pool, log: log, ch: make(chan Event, bufferSize)}
	if pool != nil {
		go r.run()
	}
	return r
}

func (r *Recorder) Record(ctx context.Context, ev Event) {
	if r == nil || r.pool == nil {
		return
	}
	ev = normalizeEvent(ctx, ev)
	select {
	case r.ch <- ev:
	default:
		r.log.Warn("toolstats: dropping tool call event; buffer full",
			"tool", ev.ToolName,
			"transport", ev.Transport,
		)
	}
}

func (r *Recorder) RecordResult(ctx context.Context, tenantID, toolName, transport string, started time.Time, err error) {
	status := StatusSuccess
	code := ""
	if err != nil {
		status = StatusError
		code = errorCode(err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = StatusTimeout
		}
	}
	r.Record(ctx, Event{
		TenantID:   tenantID,
		AgentID:    AgentFromContext(ctx),
		ToolName:   toolName,
		Transport:  transport,
		DurationMS: time.Since(started).Milliseconds(),
		Status:     status,
		ErrorCode:  code,
		CalledAt:   time.Now().UTC(),
	})
}

func AgentFromContext(ctx context.Context) string {
	if v := strings.TrimSpace(tenantctx.UserID(ctx)); v != "" {
		return v
	}
	return "system"
}

func (r *Recorder) run() {
	for ev := range r.ch {
		r.insert(ev)
	}
}

func (r *Recorder) insert(ev Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		r.log.Warn("toolstats: begin insert failed", "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		r.log.Warn("toolstats: set bypass failed", "error", err)
		return
	}
	var errorCode *string
	if ev.ErrorCode != "" {
		errorCode = &ev.ErrorCode
	}
	if _, err := tx.Exec(ctx, `
        insert into suite_tool_calls
          (tenant_id, agent_id, tool_name, transport, duration_ms, status, error_code, called_at)
        values ($1, $2, $3, $4, $5, $6, $7, $8)
    `, ev.TenantID, ev.AgentID, ev.ToolName, ev.Transport, ev.DurationMS, ev.Status, errorCode, ev.CalledAt); err != nil {
		r.log.Warn("toolstats: insert failed", "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		r.log.Warn("toolstats: commit failed", "error", err)
	}
}

func normalizeEvent(ctx context.Context, ev Event) Event {
	if ev.TenantID == "" {
		ev.TenantID = tenantctx.TenantID(ctx)
	}
	if ev.TenantID == "" {
		ev.TenantID = defaultTenant
	}
	if ev.AgentID == "" {
		ev.AgentID = AgentFromContext(ctx)
	}
	if ev.AgentID == "" {
		ev.AgentID = "system"
	}
	if ev.Transport == "" {
		ev.Transport = TransportNative
	}
	if ev.Status == "" {
		ev.Status = StatusSuccess
	}
	if ev.CalledAt.IsZero() {
		ev.CalledAt = time.Now().UTC()
	}
	if ev.DurationMS < 0 {
		ev.DurationMS = 0
	}
	return ev
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	if code == "" {
		return "error"
	}
	if len(code) > 160 {
		code = code[:160]
	}
	return code
}
