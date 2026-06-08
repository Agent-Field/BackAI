// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/gorilla/websocket"
)

const realtimeChannel = "suite_realtime"

var realtimeTablePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`)

type realtimeNotifyPayload struct {
	Table    string         `json:"table"`
	TenantID string         `json:"tenant_id,omitempty"`
	Record   map[string]any `json:"record,omitempty"`
}

var realtimeUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

func (s *Server) handleRealtime(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "REALTIME_NOT_CONFIGURED",
			"realtime requires a database connection", nil)
		return
	}

	table := r.URL.Query().Get("table")
	if !validRealtimeTable(table) {
		writeError(w, http.StatusBadRequest, "INVALID_REALTIME_TABLE",
			"table must be a valid table identifier", nil)
		return
	}
	filter, err := parseRealtimeFilter(r.URL.Query().Get("filter"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REALTIME_FILTER",
			"filter must be a JSON object", nil)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	pgconn, err := s.db.Pool.Acquire(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "REALTIME_DB_UNAVAILABLE",
			"could not acquire realtime database connection", nil)
		return
	}
	defer pgconn.Release()

	if _, err := pgconn.Exec(ctx, `listen `+realtimeChannel); err != nil {
		writeError(w, http.StatusServiceUnavailable, "REALTIME_LISTEN_FAILED",
			"could not subscribe to realtime channel", nil)
		return
	}
	defer func() {
		unlistenCtx, unlistenCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer unlistenCancel()
		_, _ = pgconn.Exec(unlistenCtx, `unlisten `+realtimeChannel)
	}()

	conn, err := realtimeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	tenantID := tenantctx.TenantID(r.Context())
	requireTenant := s.multiTenancyEnabled()
	for {
		notification, err := pgconn.Conn().WaitForNotification(ctx)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			s.log.Warn("realtime wait failed", "error", err)
			return
		}
		if !realtimePayloadMatches([]byte(notification.Payload), table, tenantID, requireTenant, filter) {
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, []byte(notification.Payload)); err != nil {
			return
		}
	}
}

func validRealtimeTable(table string) bool {
	return table != "" && realtimeTablePattern.MatchString(table)
}

func parseRealtimeFilter(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}
	var filter map[string]any
	if err := json.Unmarshal([]byte(raw), &filter); err != nil {
		return nil, err
	}
	if filter == nil {
		return nil, nil
	}
	return filter, nil
}

func realtimePayloadMatches(raw []byte, table, tenantID string, requireTenant bool, filter map[string]any) bool {
	var payload realtimeNotifyPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	if payload.Table != table {
		return false
	}
	if requireTenant {
		if tenantID == "" || payload.TenantID != tenantID {
			return false
		}
	} else if payload.TenantID != "" && tenantID != "" && payload.TenantID != tenantID {
		return false
	}
	if len(filter) == 0 {
		return true
	}
	if payload.Record == nil {
		return false
	}
	for key, expected := range filter {
		actual, ok := payload.Record[key]
		if !ok || !jsonEqual(actual, expected) {
			return false
		}
	}
	return true
}

func jsonEqual(a, b any) bool {
	return reflect.DeepEqual(normalizeJSONValue(a), normalizeJSONValue(b))
}

func normalizeJSONValue(v any) any {
	switch n := v.(type) {
	case float64:
		if n == float64(int64(n)) {
			return int64(n)
		}
		return n
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, v := range n {
			out[k] = normalizeJSONValue(v)
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = normalizeJSONValue(v)
		}
		return out
	default:
		return v
	}
}
