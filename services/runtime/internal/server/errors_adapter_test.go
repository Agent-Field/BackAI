// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
)

type stubErrorsStore struct {
	caps obserrors.Capabilities
	err  error
	last obserrors.Update
}

func (s *stubErrorsStore) ListGroups(context.Context, obserrors.ListFilter) (obserrors.Page, error) {
	if s.err != nil {
		return obserrors.Page{}, s.err
	}
	return obserrors.Page{Groups: []obserrors.Group{{
		ID:        "g1",
		Title:     "boom",
		Service:   "runtime",
		Status:    obserrors.StatusOpen,
		Count:     2,
		FirstSeen: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
	}}}, nil
}

func (s *stubErrorsStore) GetGroup(context.Context, string) (obserrors.Group, error) {
	if s.err != nil {
		return obserrors.Group{}, s.err
	}
	return obserrors.Group{ID: "g1", Title: "boom", Service: "runtime", Status: obserrors.StatusOpen, Count: 2}, nil
}

func (s *stubErrorsStore) UpdateGroup(_ context.Context, _ string, update obserrors.Update) (obserrors.Group, error) {
	if s.err != nil {
		return obserrors.Group{}, s.err
	}
	s.last = update
	return obserrors.Group{ID: "g1", Title: "boom", Service: "runtime", Status: update.Status, Count: 2}, nil
}

func (s *stubErrorsStore) Capabilities() obserrors.Capabilities { return s.caps }

func TestAdminErrorsListGetCapabilitiesAndMutation(t *testing.T) {
	store := &stubErrorsStore{caps: obserrors.Capabilities{SupportsList: true, SupportsGet: true, SupportsMute: true, SupportsResolve: true, Persistence: "volatile"}}
	srv := New(config.Default(), testLogger(), Deps{ErrorsStore: store})
	withOperator(srv, "owner") // routes are operator-gated (S1b)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/errors", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rr.Code, rr.Body.String())
	}
	var page errorListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 1 || page.Groups[0].ID != "g1" {
		t.Fatalf("page=%+v", page)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/errors/g1", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/errors/g1/resolve", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", rr.Code, rr.Body.String())
	}
	if store.last.Status != obserrors.StatusResolved {
		t.Fatalf("last update=%+v", store.last)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/errors/capabilities", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "volatile") {
		t.Fatalf("caps status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminErrorsUnsupportedCapabilityAndOpenAPI(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{ErrorsStore: &stubErrorsStore{caps: obserrors.Capabilities{SupportsList: true}}})
	withOperator(srv, "owner")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/errors/g1/resolve", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("openapi status=%d body=%s", rr.Code, rr.Body.String())
	}
	for _, path := range []string{
		"/api/v1/admin/errors",
		"/api/v1/admin/errors/{id}",
		"/api/v1/admin/errors/capabilities",
		"/api/v1/admin/errors/{id}/mute",
		"/api/v1/admin/errors/{id}/resolve",
		"/api/v1/admin/errors/{id}/reopen",
	} {
		if !strings.Contains(rr.Body.String(), path) {
			t.Fatalf("openapi missing %s", path)
		}
	}
}
