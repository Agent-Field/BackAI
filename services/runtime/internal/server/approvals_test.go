// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/approvals"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

type fakeApprovalsStore struct {
	requested approvals.RequestInput
	decided   approvals.DecideInput
	row       approvals.Approval
}

func newFakeApprovalsStore() *fakeApprovalsStore {
	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	return &fakeApprovalsStore{
		row: approvals.Approval{
			ID:        "appr_1",
			TenantID:  "00000000-0000-0000-0000-000000000000",
			Kind:      "deploy_to_prod",
			Payload:   map[string]any{"service": "api"},
			Status:    approvals.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func (f *fakeApprovalsStore) Request(_ context.Context, in approvals.RequestInput) (approvals.Approval, error) {
	f.requested = in
	f.row.Kind = in.Kind
	f.row.Payload = in.Payload
	return f.row, nil
}

func (f *fakeApprovalsStore) Get(_ context.Context, _ string) (approvals.Approval, error) {
	return f.row, nil
}

func (f *fakeApprovalsStore) List(_ context.Context, _ approvals.ListInput) (approvals.ListResult, error) {
	return approvals.ListResult{Approvals: []approvals.Approval{f.row}, Total: 1}, nil
}

func (f *fakeApprovalsStore) Decide(_ context.Context, in approvals.DecideInput) (approvals.Approval, error) {
	f.decided = in
	f.row.Status = in.Status
	note := in.DecisionNote
	f.row.DecisionNote = &note
	now := time.Now().UTC()
	f.row.DecidedAt = &now
	return f.row, nil
}

func TestApprovalsRequestAndDecide(t *testing.T) {
	store := newFakeApprovalsStore()
	srv := New(config.Default(), slog.Default(), Deps{Approvals: store})

	ctx := tenantctx.WithTenantAndUser(context.Background(),
		"00000000-0000-0000-0000-000000000000",
		"",
		"11111111-1111-1111-1111-111111111111")
	req := httptest.NewRequest("POST", "/api/v1/approvals", strings.NewReader(`{
		"kind":"deploy_to_prod",
		"payload":{"service":"api"}
	}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.requested.Kind != "deploy_to_prod" || store.requested.RequestedBy == "" {
		t.Fatalf("unexpected request input: %+v", store.requested)
	}

	req = httptest.NewRequest("POST", "/api/v1/approvals/appr_1/decide", strings.NewReader(`{
		"status":"approved",
		"decision_note":"looks good"
	}`)).WithContext(ctx)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got approvals.Approval
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != approvals.StatusApproved || store.decided.DecidedBy == "" {
		t.Fatalf("unexpected decide result: got=%+v input=%+v", got, store.decided)
	}
}

func TestApprovalsRequireStore(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/api/v1/approvals", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
