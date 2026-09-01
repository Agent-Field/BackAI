// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func notesResource() Resource {
	return Resource{Name: "notes", Fields: []Field{
		{Name: "title", Type: FieldTypeString, Required: true},
		{Name: "body", Type: FieldTypeString},
		{Name: "done", Type: FieldTypeBool, Default: false},
	}}
}

func notesReturningRows() *fakeRows {
	now := time.Now().UTC()
	return &fakeRows{
		cols: selectColumnNames([]string{"title", "body", "done"}),
		data: [][]any{{"11111111-1111-1111-1111-111111111111", "hi", "", true, now, now}},
	}
}

// TestCreate_BindsResolverTenant_IgnoresBodyTenant is the cross-tenant
// guarantee at the handler level: the tenant argument is always the
// context-resolved tenant ($1), and a client-supplied tenant_id in the body
// is ignored (it is not a declared field).
func TestCreate_BindsResolverTenant_IgnoresBodyTenant(t *testing.T) {
	fake := &fakeQuerier{rows: notesReturningRows()}
	h := newResourceHandler("notes", notesResource(), fake, Responder{})

	req := newReq("POST", "/api/v1/workload/notes/notes",
		`{"title":"hi","tenant_id":"attacker-tenant","done":true}`, "tenant-a")
	rec := newRecorder()
	h.create(rec, req)

	if rec.status != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.status, rec.body.String())
	}
	if len(fake.calls) == 0 {
		t.Fatal("expected an INSERT to be issued")
	}
	args := fake.calls[0].args
	if len(args) != 4 {
		t.Fatalf("expected 4 args (tenant + 3 fields), got %d: %v", len(args), args)
	}
	if args[0] != "tenant-a" {
		t.Fatalf("arg $1 must be the resolver tenant, got %v", args[0])
	}
	for _, a := range args {
		if a == "attacker-tenant" {
			t.Fatalf("client-supplied tenant_id leaked into query args: %v", args)
		}
	}
	if args[1] != "hi" || args[3] != true {
		t.Fatalf("field args mismatch: %v", args)
	}
	if body := rec.decode(); body["title"] != "hi" {
		t.Fatalf("response missing created row: %v", body)
	}
}

func TestCreate_MissingTenant_401_NoDBCall(t *testing.T) {
	fake := &fakeQuerier{rows: notesReturningRows()}
	h := newResourceHandler("notes", notesResource(), fake, Responder{})

	req := newReq("POST", "/api/v1/workload/notes/notes", `{"title":"hi"}`, "") // no tenant
	rec := newRecorder()
	h.create(rec, req)

	if rec.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.status)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("no DB call should happen without a tenant, got %d", len(fake.calls))
	}
}

func TestCreate_MissingRequiredField_400(t *testing.T) {
	fake := &fakeQuerier{rows: notesReturningRows()}
	h := newResourceHandler("notes", notesResource(), fake, Responder{})

	req := newReq("POST", "/api/v1/workload/notes/notes", `{"body":"no title"}`, "tenant-a")
	rec := newRecorder()
	h.create(rec, req)

	if rec.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.status)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("validation failure must not touch the DB, got %d calls", len(fake.calls))
	}
}

func TestCreate_NoDBConfigured_503(t *testing.T) {
	h := newResourceHandler("notes", notesResource(), nil, Responder{})
	req := newReq("POST", "/api/v1/workload/notes/notes", `{"title":"hi"}`, "tenant-a")
	rec := newRecorder()
	h.create(rec, req)
	if rec.status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.status)
	}
}

func TestList_BindsTenantAsFirstArg(t *testing.T) {
	fake := &fakeQuerier{rows: &fakeRows{cols: selectColumnNames([]string{"title", "body", "done"})}}
	h := newResourceHandler("notes", notesResource(), fake, Responder{})

	req := newReq("GET", "/api/v1/workload/notes/notes?limit=10", "", "tenant-b")
	rec := newRecorder()
	h.list(rec, req)

	if rec.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.status, rec.body.String())
	}
	if len(fake.calls) == 0 {
		t.Fatal("expected a SELECT")
	}
	if fake.calls[0].args[0] != "tenant-b" {
		t.Fatalf("list must bind tenant as $1, got %v", fake.calls[0].args[0])
	}
}

func TestDelete_NotFound_404(t *testing.T) {
	fake := &fakeQuerier{tag: pgconn.NewCommandTag("DELETE 0")}
	h := newResourceHandler("notes", notesResource(), fake, Responder{})

	req := newReq("DELETE", "/api/v1/workload/notes/notes/abc", "", "tenant-a")
	req.SetPathValue("id", "abc")
	rec := newRecorder()
	h.delete(rec, req)

	if rec.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.status)
	}
	// The delete must still be scoped to the tenant.
	if len(fake.calls) == 0 || fake.calls[0].args[0] != "tenant-a" {
		t.Fatalf("delete must bind tenant as $1: %v", fake.calls)
	}
}

func TestDelete_Success_204(t *testing.T) {
	fake := &fakeQuerier{tag: pgconn.NewCommandTag("DELETE 1")}
	h := newResourceHandler("notes", notesResource(), fake, Responder{})

	req := newReq("DELETE", "/api/v1/workload/notes/notes/abc", "", "tenant-a")
	req.SetPathValue("id", "abc")
	rec := newRecorder()
	h.delete(rec, req)

	if rec.status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.status)
	}
}

func TestGet_NotFound_404(t *testing.T) {
	// Empty result set => 404.
	fake := &fakeQuerier{rows: &fakeRows{cols: selectColumnNames([]string{"title", "body", "done"})}}
	h := newResourceHandler("notes", notesResource(), fake, Responder{})

	req := newReq("GET", "/api/v1/workload/notes/notes/missing", "", "tenant-a")
	req.SetPathValue("id", "missing")
	rec := newRecorder()
	h.get(rec, req)

	if rec.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.status, rec.body.String())
	}
}
