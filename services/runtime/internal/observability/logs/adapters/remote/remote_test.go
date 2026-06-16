// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

func TestRemoteQueryAndTail(t *testing.T) {
	ts := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "healthy"})
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "test-logs", "version": "1", "slot": "logs", "protocol_version": "v1", "vendor": "test",
				"capabilities": map[string]any{
					"supports_tail": true, "supports_full_text": true, "supports_trace_id": true, "max_entries_per_page": 1000,
				},
			})
		case "/v1/logs/query":
			_ = json.NewEncoder(w).Encode(pageWire{Entries: []logs.Entry{{TS: ts, Level: "info", Service: "remote", Msg: "query ok"}}})
		case "/v1/logs/tail":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"ts":"2026-06-16T12:00:00Z","level":"info","service":"remote","msg":"tail ok"}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	page, err := store.Query(context.Background(), logs.Filter{Limit: 1})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Msg != "query ok" {
		t.Fatalf("page=%+v", page)
	}
	ch, err := store.Tail(context.Background(), logs.Filter{Limit: 1})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	entry := <-ch
	if entry.Msg != "tail ok" {
		t.Fatalf("entry=%+v", entry)
	}
}

func TestRemoteTailUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "test-logs", "version": "1", "slot": "logs", "protocol_version": "v1", "vendor": "test",
				"capabilities": map[string]any{"supports_tail": false, "max_entries_per_page": 1000},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := store.Tail(context.Background(), logs.Filter{}); err != logs.ErrUnsupportedCapability {
		t.Fatalf("err=%v", err)
	}
}
