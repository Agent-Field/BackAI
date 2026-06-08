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

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/shipwright"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

type fakeShipwrightStore struct {
	created shipwright.CreateTaskInput
	task    shipwright.Task
	patches []shipwright.Patch
}

func newFakeShipwrightStore() *fakeShipwrightStore {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	return &fakeShipwrightStore{
		task: shipwright.Task{
			ID:          "11111111-1111-1111-1111-111111111111",
			TenantID:    "00000000-0000-0000-0000-000000000000",
			Title:       "Add export button",
			Description: "Add CSV export to the billing page",
			RepoURL:     "https://github.com/acme/app",
			Status:      shipwright.StatusQueued,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
}

func (f *fakeShipwrightStore) CreateTask(_ context.Context, in shipwright.CreateTaskInput) (shipwright.Task, error) {
	f.created = in
	f.task.Title = in.Title
	f.task.Description = in.Description
	f.task.RepoURL = in.RepoURL
	if in.UserID != "" {
		f.task.UserID = &in.UserID
	}
	return f.task, nil
}

func (f *fakeShipwrightStore) StartTask(_ context.Context, _ string, runID string) (shipwright.Task, error) {
	f.task.Status = shipwright.StatusRunning
	f.task.RunID = &runID
	return f.task, nil
}

func (f *fakeShipwrightStore) MarkTaskFailed(_ context.Context, _ string) (shipwright.Task, error) {
	f.task.Status = shipwright.StatusFailed
	return f.task, nil
}

func (f *fakeShipwrightStore) GetTask(_ context.Context, _ string) (shipwright.Task, error) {
	return f.task, nil
}

func (f *fakeShipwrightStore) ListTasks(_ context.Context, _ shipwright.ListTasksInput) (shipwright.ListTasksResult, error) {
	return shipwright.ListTasksResult{Tasks: []shipwright.Task{f.task}, Total: 1}, nil
}

func (f *fakeShipwrightStore) CompleteTask(_ context.Context, in shipwright.CompleteTaskInput) (shipwright.Task, error) {
	f.task.Status = in.Status
	if f.task.Status == "" {
		f.task.Status = shipwright.StatusSucceeded
	}
	if in.Ref != "" {
		f.patches = []shipwright.Patch{{
			TaskID:    f.task.ID,
			Ref:       in.Ref,
			Summary:   in.Summary,
			DiffURL:   nilIfTestEmpty(in.DiffURL),
			CreatedAt: time.Date(2026, 6, 7, 12, 1, 0, 0, time.UTC),
		}}
	}
	return f.task, nil
}

func (f *fakeShipwrightStore) ListPatches(_ context.Context, _ string) ([]shipwright.Patch, error) {
	return f.patches, nil
}

func TestShipwrightCreateStartsAgentFieldExecution(t *testing.T) {
	store := newFakeShipwrightStore()
	var gotPath string
	var gotInput map[string]any
	afSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode AF request: %v", err)
		}
		gotInput = body.Input
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"execution_id":"exec_ship_123"}`))
	}))
	defer afSrv.Close()

	srv := New(config.Default(), slog.Default(), Deps{
		AF:         agentfield.New(agentfield.Config{URL: afSrv.URL}),
		Shipwright: store,
	})
	body := `{
		"title":"Add export button",
		"description":"Add CSV export to the billing page",
		"repo_url":"https://github.com/acme/app",
		"harness_provider":"codex",
		"model":"openrouter/google/gemini-2.5-flash"
	}`
	req := httptest.NewRequest("POST", "/api/v1/shipwright/tasks", strings.NewReader(body))
	req = req.WithContext(tenantctx.WithTenantAndUser(
		req.Context(),
		"00000000-0000-0000-0000-000000000000",
		"",
		"22222222-2222-2222-2222-222222222222",
	))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/api/v1/execute/async/shipwright.build" {
		t.Fatalf("AgentField path = %q", gotPath)
	}
	if gotInput["task_id"] != store.task.ID || gotInput["harness_provider"] != "codex" {
		t.Fatalf("unexpected AF input: %#v", gotInput)
	}
	if store.created.UserID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("user id not propagated: %q", store.created.UserID)
	}
	var resp shipwrightTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Task.RunID == nil || *resp.Task.RunID != "exec_ship_123" {
		t.Fatalf("run_id = %#v", resp.Task.RunID)
	}
	if !strings.Contains(resp.DetailsURL, "/agent-api/executions/exec_ship_123/details") {
		t.Fatalf("details URL = %q", resp.DetailsURL)
	}
}

func TestShipwrightCreateRequiresStoreAndAgentField(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})
	req := httptest.NewRequest("POST", "/api/v1/shipwright/tasks", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	srv = New(config.Default(), slog.Default(), Deps{Shipwright: newFakeShipwrightStore()})
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status without AF = %d body=%s", rec.Code, rec.Body.String())
	}
}

func nilIfTestEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
