// SPDX-License-Identifier: Apache-2.0

// Package audit writes append-only rows to suite_audit_log on admin
// mutations.
//
// The schema (tenant_id + user_id + api_key_id + action + resource_type
// + resource_id + metadata + ip + user_agent + occurred_at) was added
// in migration 00004_rls.sql. This package is the single chokepoint
// that produces those rows.
//
// All writes are best-effort and fire in a goroutine — a flaky DB
// must never crash the calling handler. Failures land in the structured
// logger at Warn level so they show up in /operate/logs without
// breaking the mutation that triggered them.

package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// Writer is the public façade. Cheap to construct (just holds a pool
// + a logger), safe to share across handlers.
type Writer struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New returns a Writer that will write into the given pool. Pass nil
// pool to get a no-op writer (boot-time use cases without DB).
func New(pool *pgxpool.Pool, log *slog.Logger) *Writer {
	if log == nil {
		log = slog.Default()
	}
	return &Writer{pool: pool, log: log}
}

// Event is the shape callers fill out. Action follows the dotted
// convention "<noun>.<verb>" (e.g. "api_key.create", "secret.put").
// ResourceID is the natural key of the thing acted on (uuid string,
// key name, etc.). Metadata is opaque per-action JSON.
type Event struct {
	Action       string
	ResourceType string
	ResourceID   string
	Metadata     map[string]any
}

// Write fires an audit row keyed off the principal in the request
// context. The default 2-second timeout is generous; if the pool
// can't accept the write in that window the row is dropped and a
// Warn line goes out.
func (w *Writer) Write(ctx context.Context, r *http.Request, ev Event) {
	if w == nil || w.pool == nil {
		return
	}
	tenantID := tenantctx.TenantID(ctx)
	apiKeyID := tenantctx.APIKeyID(ctx)
	userID := tenantctx.UserID(ctx)
	if tenantID == "" {
		// Admin paths (/api/v1/admin/*) bypass the tenant resolver, so
		// the resolved tenant id is empty even though there's a real
		// operator session. Either the caller provided a tenant id in
		// Event.Metadata["tenant_id"], OR the action is global —
		// either way we anchor the row at the default tenant so the
		// audit log surfaces it.
		if t, ok := ev.Metadata["tenant_id"].(string); ok && t != "" {
			tenantID = t
		} else {
			tenantID = "00000000-0000-0000-0000-000000000000"
		}
	}
	tenantPtr := &tenantID
	var userPtr, keyPtr *string
	if userID != "" {
		userPtr = &userID
	}
	if apiKeyID != "" {
		keyPtr = &apiKeyID
	}

	ip := clientIP(r)
	var ipPtr *string
	if ip != "" {
		ipPtr = &ip
	}
	ua := r.UserAgent()
	var uaPtr *string
	if ua != "" {
		uaPtr = &ua
	}

	metadata := ev.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if _, ok := metadata["path"]; !ok {
		metadata["path"] = r.URL.Path
	}
	if _, ok := metadata["method"]; !ok {
		metadata["method"] = r.Method
	}
	metaBytes, _ := json.Marshal(metadata)

	resourceType := ev.ResourceType
	if resourceType == "" {
		// Derive from action prefix if the caller didn't bother.
		if i := strings.IndexByte(ev.Action, '.'); i > 0 {
			resourceType = ev.Action[:i]
		}
	}
	var resourceTypePtr, resourceIDPtr *string
	if resourceType != "" {
		resourceTypePtr = &resourceType
	}
	if ev.ResourceID != "" {
		resourceIDPtr = &ev.ResourceID
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Bind the write connection to the row's tenant so the audit-log RLS
		// WITH CHECK passes (the pool's PrepareConn hook reads this from ctx).
		// Without it the goroutine's background context carries no tenant and
		// the insert is rejected.
		ctx = tenantctx.WithTenant(ctx, tenantID, apiKeyID)
		_, err := w.pool.Exec(ctx, `
			insert into suite_audit_log
				(tenant_id, user_id, api_key_id, action,
				 resource_type, resource_id, metadata, ip, user_agent)
			values
				($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, tenantPtr, userPtr, keyPtr, ev.Action,
			resourceTypePtr, resourceIDPtr, metaBytes, ipPtr, uaPtr)
		if err != nil {
			w.log.Warn("audit: insert failed",
				"action", ev.Action,
				"resource_id", ev.ResourceID,
				"tenant_id", tenantID,
				"error", err)
		}
	}()
}

// clientIP duplicates the helper in server/secrets.go so this package
// has no upward dependency. Prefers X-Forwarded-For (first), then
// X-Real-IP, then RemoteAddr (stripped of port).
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return strings.TrimSpace(v)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		return host[:i]
	}
	return host
}
