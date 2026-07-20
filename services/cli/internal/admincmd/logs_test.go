// SPDX-License-Identifier: Apache-2.0

package admincmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/output"
)

func testClient(t *testing.T, h http.HandlerFunc) (*client.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	return &client.Client{BaseURL: srv.URL, HTTP: srv.Client()}, srv.Close
}

// Contract: `logs --json` emits {logs:[...]} in the stable envelope.
func TestLogs_JSONEnvelope(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/logs" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"logs": []map[string]any{
			{"ts": "2026-07-20T00:00:00Z", "level": "error", "service": "runtime", "msg": "boom"},
		}})
	})
	defer done()
	var out bytes.Buffer
	if err := RunLogs(context.Background(), c, []string{"--json"}, &out, io.Discard); err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	var got logsResponse
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json not valid JSON: %v (%s)", err, out.String())
	}
	if len(got.Logs) != 1 || got.Logs[0].Msg != "boom" {
		t.Fatalf("logs = %#v", got.Logs)
	}
}

// Contract: --tail overrides --limit, and --since/--level are forwarded as
// query params.
func TestLogs_TailAndSinceQuery(t *testing.T) {
	var gotQuery string
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"logs": []any{}})
	})
	defer done()
	err := RunLogs(context.Background(), c,
		[]string{"--tail", "10", "--limit", "500", "--since", "15m", "--level", "error"},
		io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	if !strings.Contains(gotQuery, "limit=10") {
		t.Errorf("--tail should win over --limit; query=%q", gotQuery)
	}
	if !strings.Contains(gotQuery, "since=15m") {
		t.Errorf("--since not forwarded; query=%q", gotQuery)
	}
	if !strings.Contains(gotQuery, "level=error") {
		t.Errorf("--level not forwarded; query=%q", gotQuery)
	}
}

// Contract: a runtime that lacks the logs route (404) degrades to the remote
// exit code, not the not-found code — the route, not a target, is missing.
func TestLogs_MissingRouteIsRemote(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer done()
	err := RunLogs(context.Background(), c, nil, io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitRemote {
		t.Fatalf("missing-route exit = %d, want %d (err=%v)", code, output.ExitRemote, err)
	}
}

// Contract: a 401 from the runtime maps to the auth exit code (operator key
// required).
func TestLogs_UnauthorizedIsAuthExit(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "UNAUTHENTICATED", "message": "no key"}})
	})
	defer done()
	err := RunLogs(context.Background(), c, nil, io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitAuth {
		t.Fatalf("401 exit = %d, want %d (err=%v)", code, output.ExitAuth, err)
	}
}
