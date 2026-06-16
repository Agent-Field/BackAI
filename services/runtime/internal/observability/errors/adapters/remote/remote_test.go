// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
)

func TestRemoteErrorsStore(t *testing.T) {
	var sawList bool
	var sawPatch bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":             "errors-echo",
				"slot":             "errors",
				"protocol_version": "errors-v1",
				"capabilities": map[string]any{
					"supports_list":       true,
					"supports_get":        true,
					"supports_mute":       true,
					"supports_resolve":    true,
					"native_query_lang":   "echo",
					"persistence":         "remote",
					"max_groups_per_page": 50,
				},
			})
		case "/v1/errors/list":
			if r.Method != http.MethodPost {
				t.Fatalf("method=%s want POST", r.Method)
			}
			sawList = true
			_ = json.NewEncoder(w).Encode(obserrors.Page{Groups: []obserrors.Group{{ID: "g1", Title: "boom", Status: obserrors.StatusOpen}}})
		case "/v1/errors/g1":
			if r.Method == http.MethodPatch {
				sawPatch = true
				_ = json.NewEncoder(w).Encode(obserrors.Group{ID: "g1", Title: "boom", Status: obserrors.StatusResolved})
				return
			}
			_ = json.NewEncoder(w).Encode(obserrors.Group{ID: "g1", Title: "boom", Status: obserrors.StatusOpen})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if store.AdapterName() != "errors-echo" {
		t.Fatalf("name=%q", store.AdapterName())
	}
	page, err := store.ListGroups(context.Background(), obserrors.ListFilter{Status: obserrors.StatusOpen})
	if err != nil || !sawList || len(page.Groups) != 1 {
		t.Fatalf("ListGroups page=%+v saw=%v err=%v", page, sawList, err)
	}
	group, err := store.GetGroup(context.Background(), "g1")
	if err != nil || group.ID != "g1" {
		t.Fatalf("GetGroup group=%+v err=%v", group, err)
	}
	group, err = store.UpdateGroup(context.Background(), "g1", obserrors.Update{Status: obserrors.StatusResolved})
	if err != nil || !sawPatch || group.Status != obserrors.StatusResolved {
		t.Fatalf("UpdateGroup group=%+v saw=%v err=%v", group, sawPatch, err)
	}
}
