// SPDX-License-Identifier: Apache-2.0

package loki

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

func TestFilterToLogQLLiteralAndLogfmt(t *testing.T) {
	got := filterToLogQL(logs.Filter{
		Services: []string{"runtime", "worker"},
		Levels:   []string{"warn", "error"},
		Search:   "timeout.*",
	})
	want := `{service=~"runtime|worker"} |= "timeout.*" | logfmt | level=~"warn|error"`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFilterToLogQLRegex(t *testing.T) {
	got := filterToLogQL(logs.Filter{Search: "timeout.*", SearchIsRegex: true})
	if !strings.Contains(got, `|~ "timeout.*"`) {
		t.Fatalf("expected regex filter, got %q", got)
	}
}

func TestQueryRangeDecodesEntries(t *testing.T) {
	ts := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/loki/api/v1/query_range" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "streams",
				"result": []any{
					map[string]any{
						"stream": map[string]string{"service": "runtime"},
						"values": [][]string{{"1781611200000000000", `level=warn msg="slow query" request_id=req_1`}},
					},
				},
			},
		})
	}))
	defer srv.Close()
	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	page, err := store.Query(context.Background(), logs.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("entries=%d want 1", len(page.Entries))
	}
	if page.Entries[0].TS != ts || page.Entries[0].Level != "warn" || page.Entries[0].Msg != "slow query" {
		t.Fatalf("decoded entry: %+v", page.Entries[0])
	}
}

func TestTailWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/loki/api/v1/tail" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"streams": []any{
				map[string]any{
					"stream": map[string]string{"service": "runtime"},
					"values": [][]string{{"1781611200000000000", `{"level":"info","msg":"tail ok","service":"runtime"}`}},
				},
			},
		})
	}))
	defer srv.Close()
	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := store.Tail(ctx, logs.Filter{})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	select {
	case entry := <-ch:
		if entry.Msg != "tail ok" {
			t.Fatalf("entry=%+v", entry)
		}
	case <-ctx.Done():
		t.Fatal("tail timed out")
	}
}

func TestRetentionDaysFromConfig(t *testing.T) {
	got := retentionDaysFromConfig([]byte("limits_config:\n  retention_period: 720h\n"))
	if got != 30 {
		t.Fatalf("got %d want 30", got)
	}
}
