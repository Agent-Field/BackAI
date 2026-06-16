// SPDX-License-Identifier: Apache-2.0

package glitchtip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
)

func TestGlitchTipOrgScopedPathsAndStatusTranslation(t *testing.T) {
	var seen []string
	ts := "2026-06-16T12:00:00Z"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/issues/":
			if got := r.URL.Query().Get("query"); got != "is:ignored logger:runtime" {
				t.Fatalf("query=%q", got)
			}
			w.Header().Set("Link", `<http://example.test>; rel="next"; results="true"; cursor="next-cursor"`)
			_, _ = w.Write([]byte(`[{"id":"42","title":"boom","logger":"runtime","status":"ignored","count":"3","firstSeen":"` + ts + `","lastSeen":"` + ts + `"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/issues/42/":
			_, _ = w.Write([]byte(`{"id":"42","title":"boom","logger":"runtime","status":"resolved","count":"3","firstSeen":"` + ts + `","lastSeen":"` + ts + `"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/organizations/acme/issues/42/":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != "unresolved" {
				t.Fatalf("status body=%q want unresolved", body["status"])
			}
			_, _ = w.Write([]byte(`{"id":"42","title":"boom","logger":"runtime","status":"unresolved","count":"3","firstSeen":"` + ts + `","lastSeen":"` + ts + `"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL, Org: "acme", Token: "token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	page, err := store.ListGroups(context.Background(), obserrors.ListFilter{Status: obserrors.StatusMuted, Service: "runtime"})
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if !page.HasMore || page.NextCursor != "next-cursor" {
		t.Fatalf("cursor=%q hasMore=%v", page.NextCursor, page.HasMore)
	}
	if page.Groups[0].Status != obserrors.StatusMuted {
		t.Fatalf("status=%q want muted", page.Groups[0].Status)
	}
	group, err := store.GetGroup(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if group.Status != obserrors.StatusResolved {
		t.Fatalf("get status=%q want resolved", group.Status)
	}
	group, err = store.UpdateGroup(context.Background(), "42", obserrors.Update{Status: obserrors.StatusOpen})
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if group.Status != obserrors.StatusOpen {
		t.Fatalf("update status=%q want open", group.Status)
	}
	if len(seen) != 3 {
		t.Fatalf("seen=%v", seen)
	}
}

func TestGlitchTipGetGroupFallsBackToOrgScopedListOn405(t *testing.T) {
	ts := "2026-06-16T12:00:00Z"
	var sawDetail bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/issues/42/":
			sawDetail = true
			w.Header().Set("Allow", "PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/issues/":
			if got := r.URL.Query().Get("query"); got != "is:unresolved" {
				t.Fatalf("query=%q", got)
			}
			_, _ = w.Write([]byte(`[{"id":"42","title":"boom","logger":"runtime","status":"unresolved","count":4,"firstSeen":"` + ts + `","lastSeen":"` + ts + `"}]`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL, Org: "acme", Token: "token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	group, err := store.GetGroup(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if !sawDetail {
		t.Fatal("expected org-scoped detail request before fallback")
	}
	if group.ID != "42" || group.Count != 4 {
		t.Fatalf("group=%+v", group)
	}
}

func TestStatusTranslation(t *testing.T) {
	tests := []struct {
		internal string
		sentry   string
	}{
		{obserrors.StatusOpen, "unresolved"},
		{obserrors.StatusMuted, "ignored"},
		{obserrors.StatusResolved, "resolved"},
	}
	for _, tt := range tests {
		if got := toSentryStatus(tt.internal); got != tt.sentry {
			t.Fatalf("toSentryStatus(%q)=%q want %q", tt.internal, got, tt.sentry)
		}
		if got := fromSentryStatus(tt.sentry); got != tt.internal {
			t.Fatalf("fromSentryStatus(%q)=%q want %q", tt.sentry, got, tt.internal)
		}
	}
}

func TestIssueWireCountAcceptsStringAndNumber(t *testing.T) {
	ts := "2026-06-16T12:00:00Z"
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "string", body: `{"id":"1","title":"boom","status":"unresolved","count":"7","firstSeen":"` + ts + `","lastSeen":"` + ts + `"}`, want: 7},
		{name: "number", body: `{"id":"1","title":"boom","status":"unresolved","count":8,"firstSeen":"` + ts + `","lastSeen":"` + ts + `"}`, want: 8},
		{name: "empty", body: `{"id":"1","title":"boom","status":"unresolved","count":"","firstSeen":"` + ts + `","lastSeen":"` + ts + `"}`, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw issueWire
			if err := json.Unmarshal([]byte(tt.body), &raw); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got := raw.toGroup().Count; got != tt.want {
				t.Fatalf("count=%d want %d", got, tt.want)
			}
		})
	}
}
