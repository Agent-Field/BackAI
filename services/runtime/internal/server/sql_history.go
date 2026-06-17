// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const sqlHistoryMaxBytes = 64 * 1024

type sqlHistoryEntry struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Query      string `json:"query"`
	ExecutedAt string `json:"executed_at"`
}

type sqlHistoryResponse struct {
	History []sqlHistoryEntry `json:"history"`
}

func (s *Server) handleSQLHistory(w http.ResponseWriter, r *http.Request) {
	userID := tenantctx.UserID(r.Context())
	out := sqlHistoryResponse{History: []sqlHistoryEntry{}}
	if userID == "" || s.db == nil || s.db.Pool == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	limit := parseAnalyticsLimit(r.URL.Query().Get("limit"), 50)
	tx, err := s.db.Pool.Begin(r.Context())
	if err != nil {
		s.log.Error("sql history begin failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := tx.Exec(r.Context(), "set local app.bypass_rls = 'on'"); err != nil {
		s.log.Error("sql history bypass failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	rows, err := tx.Query(r.Context(), `
        select id::text, user_id::text, query, executed_at
        from suite_sql_history
        where user_id = $1
        order by executed_at desc
        limit $2
    `, userID, limit)
	if err != nil {
		s.log.Error("sql history list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var row sqlHistoryEntry
		var executed time.Time
		if err := rows.Scan(&row.ID, &row.UserID, &row.Query, &executed); err != nil {
			s.log.Error("sql history scan failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
			return
		}
		row.ExecutedAt = executed.UTC().Format(time.RFC3339Nano)
		out.History = append(out.History, row)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("sql history rows failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.log.Error("sql history commit failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSQLHistoryRecord(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "invalid JSON body: "+err.Error()))
		return
	}
	if in.Query == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "query is required"))
		return
	}
	if err := s.recordSQLHistory(r.Context(), in.Query); err != nil {
		s.log.Warn("sql history explicit record failed", "error", err)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) recordSQLHistory(ctx context.Context, query string) error {
	if s == nil || s.db == nil || s.db.Pool == nil {
		return nil
	}
	userID := tenantctx.UserID(ctx)
	if userID == "" {
		return nil
	}
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		tenantID = defaultTenantUUID
	}
	query = truncateSQLHistory(query)
	executedSecond := time.Now().UTC().Truncate(time.Second)
	sum := sha256.Sum256([]byte(query))
	hash := hex.EncodeToString(sum[:])

	writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := s.db.Pool.Begin(writeCtx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(writeCtx) }()
	if _, err := tx.Exec(writeCtx, "set local app.bypass_rls = 'on'"); err != nil {
		return err
	}
	_, err = tx.Exec(writeCtx, `
        insert into suite_sql_history
          (tenant_id, user_id, query, query_sha256, executed_second, executed_at)
        values ($1, $2, $3, $4, $5, now())
        on conflict (user_id, query_sha256, executed_second) do nothing
    `, tenantID, userID, query, hash, executedSecond)
	if err != nil {
		return err
	}
	return tx.Commit(writeCtx)
}

func truncateSQLHistory(query string) string {
	if len(query) <= sqlHistoryMaxBytes {
		return query
	}
	const suffix = "\n-- truncated by AF Stack SQL history"
	limit := sqlHistoryMaxBytes - len(suffix)
	if limit < 0 {
		limit = sqlHistoryMaxBytes
	}
	return query[:limit] + suffix
}
