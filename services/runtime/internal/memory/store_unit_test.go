// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// ─── Scope validation ────────────────────────────────────────────────────

func TestScopeValid(t *testing.T) {
	good := []Scope{ScopeGlobal, ScopeTenant, ScopeAgent, ScopeSession, ScopeRun}
	for _, s := range good {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}
	bad := []Scope{"", "world", "TENANT", "Global"}
	for _, s := range bad {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestResolveScopeGlobal(t *testing.T) {
	ctx := context.Background()
	scope, id, err := resolveScope(ctx, ScopeGlobal, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if scope != ScopeGlobal || id != "" {
		t.Errorf("got scope=%q id=%q", scope, id)
	}

	// Non-empty scope_id with global must be rejected.
	if _, _, err := resolveScope(ctx, ScopeGlobal, "anything"); err == nil {
		t.Errorf("expected error for global + scope_id")
	}
}

func TestResolveScopeTenantFromCtx(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-123", "")
	scope, id, err := resolveScope(ctx, ScopeTenant, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if scope != ScopeTenant || id != "tenant-123" {
		t.Errorf("expected auto-fill, got scope=%q id=%q", scope, id)
	}
}

func TestResolveScopeTenantMissing(t *testing.T) {
	if _, _, err := resolveScope(context.Background(), ScopeTenant, ""); err == nil {
		t.Errorf("expected error when tenant scope has no scope_id and no ctx tenant")
	}
}

// A caller must not be able to name a *different* tenant via scope_id on
// scope=tenant — that was the cross-tenant hole. resolveScope forces the
// scope_id to the caller's own tenant and rejects a mismatch.
func TestResolveScopeTenantRejectsForeignScopeID(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-me", "")
	if _, _, err := resolveScope(ctx, ScopeTenant, "tenant-victim"); err == nil {
		t.Errorf("expected rejection when scope_id names a different tenant than the caller")
	}
	// The caller naming its OWN tenant explicitly is fine.
	scope, id, err := resolveScope(ctx, ScopeTenant, "tenant-me")
	if err != nil || scope != ScopeTenant || id != "tenant-me" {
		t.Errorf("own-tenant scope_id should be accepted, got scope=%q id=%q err=%v", scope, id, err)
	}
}

func TestResolveScopeAgentSessionRun(t *testing.T) {
	cases := []Scope{ScopeAgent, ScopeSession, ScopeRun}
	for _, s := range cases {
		if _, _, err := resolveScope(context.Background(), s, ""); err == nil {
			t.Errorf("expected error for scope=%q without scope_id", s)
		}
		scope, id, err := resolveScope(context.Background(), s, "x")
		if err != nil || scope != s || id != "x" {
			t.Errorf("scope=%q got scope=%q id=%q err=%v", s, scope, id, err)
		}
	}
}

func TestResolveScopeInvalid(t *testing.T) {
	if _, _, err := resolveScope(context.Background(), Scope("foo"), ""); err == nil {
		t.Errorf("expected invalid-scope error")
	}
}

// ─── Validation ──────────────────────────────────────────────────────────

func TestValidateKey(t *testing.T) {
	if err := validateKey(""); err == nil {
		t.Errorf("empty key should be invalid")
	}
	if err := validateKey(strings.Repeat("k", 513)); err == nil {
		t.Errorf("oversized key should be invalid")
	}
	if err := validateKey("user/123/profile"); err != nil {
		t.Errorf("normal key should be valid: %v", err)
	}
}

// ─── Vector encoding ─────────────────────────────────────────────────────

func TestEncodeVector(t *testing.T) {
	vec := []float32{0.1, -0.25, 0, 1}
	got := encodeVector(vec)
	if got[0] != '[' || got[len(got)-1] != ']' {
		t.Errorf("expected brackets, got %q", got)
	}
	if !strings.Contains(got, "0.1") || !strings.Contains(got, "-0.25") {
		t.Errorf("expected component values in output, got %q", got)
	}
}

func TestEncodeVectorEmpty(t *testing.T) {
	got := encodeVector(nil)
	if got != "[]" {
		t.Errorf("expected '[]', got %q", got)
	}
}

// ─── Entry JSON shape matches MemoryEntrySchema ──────────────────────────

// TestEntryJSONShape verifies the JSON keys we emit exactly match the
// zod schema fields. The dashboard's safeParse will reject any drift
// here.
func TestEntryJSONShape(t *testing.T) {
	scopeID := "tenant-42"
	entry := Entry{
		Scope:        ScopeTenant,
		ScopeID:      &scopeID,
		Key:          "prefs/lang",
		Value:        json.RawMessage(`"en-US"`),
		Metadata:     map[string]any{"source": "test"},
		HasEmbedding: true,
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-02T00:00:00Z",
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Round-trip through a generic map so we can assert exact keys.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	wantKeys := []string{
		"scope", "scope_id", "key", "value", "metadata",
		"has_embedding", "created_at", "updated_at",
	}
	for _, k := range wantKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in entry JSON", k)
		}
	}
	if got := len(m); got != len(wantKeys) {
		t.Errorf("expected %d keys, got %d (%v)", len(wantKeys), got, m)
	}

	// scope_id when set must be a string, not null.
	if v, _ := m["scope_id"].(string); v != "tenant-42" {
		t.Errorf("scope_id should be 'tenant-42', got %v", m["scope_id"])
	}
	if v, _ := m["has_embedding"].(bool); !v {
		t.Errorf("has_embedding should be true")
	}
}

// TestEntryJSONNullScopeID verifies a nil ScopeID encodes as JSON null
// (matches the zod `scope_id: z.string().nullable()` contract).
func TestEntryJSONNullScopeID(t *testing.T) {
	entry := Entry{
		Scope:     ScopeGlobal,
		ScopeID:   nil,
		Key:       "k",
		Value:     json.RawMessage(`null`),
		Metadata:  map[string]any{},
		CreatedAt: "t",
		UpdatedAt: "t",
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"scope_id":null`) {
		t.Errorf("expected scope_id:null in %q", raw)
	}
}

// TestPutInputJSONShape verifies the wire-side input keys match
// MemoryPutInputSchema. We don't enforce optional-vs-required here —
// that's a zod concern — but the field names must match.
func TestPutInputJSONShape(t *testing.T) {
	in := PutInput{
		Scope:    ScopeAgent,
		ScopeID:  "demo.summarise",
		Key:      "context/last",
		Value:    json.RawMessage(`{"x":1}`),
		Metadata: map[string]any{"tag": "v1"},
		Embed:    true,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"scope"`, `"scope_id"`, `"key"`, `"value"`, `"metadata"`, `"embed"`} {
		if !strings.Contains(string(raw), k) {
			t.Errorf("missing %s in %q", k, raw)
		}
	}
}

// TestEmbedText extracts the text used for embedding from the stored
// JSON value. Strings are unquoted; other JSON types pass through.
func TestEmbedText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"hello world"`, "hello world"},
		{`{"x":1}`, `{"x":1}`},
		{`null`, `null`},
		{``, ``},
	}
	for _, c := range cases {
		got := embedText(json.RawMessage(c.in))
		if got != c.want {
			t.Errorf("embedText(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
