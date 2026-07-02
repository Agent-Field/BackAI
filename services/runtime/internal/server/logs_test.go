// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

type stubLogsStore struct {
	caps logs.Capabilities
}

func (s stubLogsStore) Query(context.Context, logs.Filter) (logs.Page, error) {
	return logs.Page{Entries: []logs.Entry{{
		TS:      time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		Level:   "info",
		Service: "runtime",
		Msg:     "ok",
	}}}, nil
}

func (s stubLogsStore) Tail(context.Context, logs.Filter) (<-chan logs.Entry, error) {
	return nil, logs.ErrUnsupportedCapability
}

func (s stubLogsStore) Capabilities() logs.Capabilities { return s.caps }

func TestAdminLogsList(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{LogsStore: stubLogsStore{caps: logs.Capabilities{SupportsTail: true, MaxEntriesPerPage: 1000}}})
	withOperator(srv, "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs?limit=1", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out logsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Logs) != 1 || out.Logs[0].Msg != "ok" {
		t.Fatalf("out=%+v", out)
	}
}

func TestAdminLogsTailUnsupported(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{LogsStore: stubLogsStore{caps: logs.Capabilities{SupportsTail: false}}})
	withOperator(srv, "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/tail", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["error"]["code"] != "unsupported_capability" {
		t.Fatalf("error=%+v", out)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Contract: the /api/v1/admin/logs* surfaces must never serve without an
// operator session — they bypass the tenant resolver via publicPrefixes,
// so this gate is the only auth they have.
func TestAdminLogsRequireOperatorSession(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{LogsStore: stubLogsStore{caps: logs.Capabilities{SupportsTail: true, MaxEntriesPerPage: 1000}}})
	withOperator(srv, "") // unauthenticated
	for _, path := range []string{
		"/api/v1/admin/logs",
		"/api/v1/admin/logs/tail",
		"/api/v1/admin/logs/capabilities",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.srv.Handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status=%d body=%s, want 401", path, rr.Code, rr.Body.String())
		}
		var out map[string]map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
		if out["error"]["code"] != "OPERATOR_AUTH_REQUIRED" {
			t.Fatalf("%s error=%+v", path, out)
		}
	}
}
