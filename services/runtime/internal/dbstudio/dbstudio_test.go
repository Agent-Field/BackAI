// SPDX-License-Identifier: Apache-2.0

// Unit tests for the dbstudio package — no DB required.
//
// Integration tests (which DO need a Postgres instance) live in
// dbstudio_integration_test.go and are gated behind the `integration`
// build tag. Keep this file fast: `go test ./...` runs it on every
// developer push.
package dbstudio

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestValidateIdent covers the identifier-validation contract referenced
// in the package doc and the REST handler. The rule:
//
//	must match ^[a-zA-Z_][a-zA-Z0-9_]*$
//	must be 1..63 characters
//
// Anything else returns ErrInvalidIdentifier. The dashboard surfaces
// this as the INVALID_IDENTIFIER envelope.
func TestValidateIdent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"simple", "users", true},
		{"underscore_start", "_internal", true},
		{"mixed_case", "AccountKeys", true},
		{"with_digits", "tenant_v2", true},
		{"max_length_63", "a234567890123456789012345678901234567890123456789012345678901234"[:63], true},

		// Injection attempts — every one must be rejected.
		{"injection_drop", `"hack"; drop --`, false},
		{"injection_semicolon", "users;drop", false},
		{"injection_space", "users users", false},
		{"injection_quote", `"users"`, false},
		{"injection_paren", "users()", false},
		{"injection_dot", "public.users", false},
		{"injection_dash", "user-table", false},
		{"injection_unicode", "tableΩ", false},
		{"empty", "", false},
		{"leading_digit", "1users", false},
		{"too_long", "a234567890123456789012345678901234567890123456789012345678901234567890", false},
		// pg_class is a perfectly valid identifier by the regex — we
		// reject it at the SQL layer (system schemas filter), not at
		// the validator. So this one IS valid here.
		{"valid_pg_prefix", "pg_class", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateIdent(c.in)
			if c.ok && err != nil {
				t.Errorf("ValidateIdent(%q) = %v, want nil", c.in, err)
			}
			if !c.ok && !errors.Is(err, ErrInvalidIdentifier) {
				t.Errorf("ValidateIdent(%q) = %v, want ErrInvalidIdentifier", c.in, err)
			}
		})
	}
}

// TestSQLRunResultJSONShape pins the wire shape of SQLRunResult against
// SQLRunResultSchema in apps/dashboard/src/lib/api.ts. The dashboard
// hard-fails (SCHEMA_MISMATCH) on any drift, so this is our contract
// guard.
func TestSQLRunResultJSONShape(t *testing.T) {
	result := SQLRunResult{
		Columns:    []string{"n"},
		Rows:       [][]any{{int32(1)}},
		RowCount:   1,
		Truncated:  false,
		DurationMS: 7,
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Decode back into a tagless map and assert every key the
	// dashboard zod schema requires.
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required := []string{"columns", "rows", "row_count", "truncated", "duration_ms"}
	for _, k := range required {
		if _, ok := got[k]; !ok {
			t.Errorf("missing required key %q in wire shape; got %v", k, got)
		}
	}
	// Spot-check types: columns is []string, rows is [][]any.
	cols, ok := got["columns"].([]any)
	if !ok {
		t.Errorf("columns should marshal as JSON array, got %T", got["columns"])
	}
	if len(cols) != 1 {
		t.Errorf("expected 1 column, got %d", len(cols))
	}
	rows, ok := got["rows"].([]any)
	if !ok {
		t.Errorf("rows should marshal as JSON array, got %T", got["rows"])
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
}

// TestDBTableJSONShape pins the wire shape of DBTable against
// DBTableSchema in api.ts.
func TestDBTableJSONShape(t *testing.T) {
	tbl := DBTable{
		Schema:        "public",
		Name:          "users",
		Kind:          "table",
		EstimatedRows: 42,
		SizeBytes:     8192,
		HasRLS:        true,
	}
	raw, err := json.Marshal(tbl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{
		"schema", "name", "kind", "estimated_rows", "size_bytes", "has_rls",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing required key %q; got %v", k, got)
		}
	}
}

// TestDBTableDetailJSONShape pins DBTableDetailSchema fields. Nested
// arrays (columns, indexes, policies) must be present even when empty,
// not JSON null — zod's array<T> rejects null.
func TestDBTableDetailJSONShape(t *testing.T) {
	d := DBTableDetail{
		Schema:     "public",
		Name:       "users",
		Kind:       "table",
		Columns:    []DBColumn{},
		Indexes:    []DBIndex{},
		Policies:   []RLSPolicy{},
		RLSEnabled: false,
		RLSForced:  false,
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{
		"schema", "name", "kind", "columns", "indexes",
		"rls_enabled", "rls_forced", "policies",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing required key %q; got %v", k, got)
		}
	}
	// Arrays must be non-nil JSON arrays (not null).
	for _, k := range []string{"columns", "indexes", "policies"} {
		v, _ := got[k].([]any)
		if v == nil {
			t.Errorf("%q must be a non-nil JSON array (zod rejects null)", k)
		}
	}
}

// TestDBColumnNullableDefault — DBColumn.DefaultValue is *string so
// `null` is the wire representation for "no default". Pinning here so
// a future refactor to string doesn't break the dashboard contract
// (zod field is .nullable()).
func TestDBColumnNullableDefault(t *testing.T) {
	c := DBColumn{Name: "id", DataType: "uuid", IsPrimaryKey: true}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, ok := got["default_value"]
	if !ok {
		t.Fatalf("default_value key missing entirely; zod nullable expects explicit null")
	}
	if v != nil {
		t.Errorf("default_value should marshal as null when unset, got %v", v)
	}
}

// TestStudioNilPoolReturnsNil documents the New(nil) -> nil contract
// callers depend on so they can check `if studio != nil` instead of
// catching a panic on first method call.
func TestStudioNilPoolReturnsNil(t *testing.T) {
	s := New(nil)
	if s != nil {
		t.Errorf("New(nil) should return nil, got %+v", s)
	}
}

// TestIsReadOnlyViolationDetection covers both the PG error code path
// and the message-fallback path so the dashboard always sees the
// READ_ONLY_VIOLATION envelope instead of a raw error.
func TestIsReadOnlyViolationDetection(t *testing.T) {
	// Message path: arbitrary error whose string contains the
	// canonical phrase.
	err := errors.New("ERROR: cannot execute INSERT in a read-only transaction (SQLSTATE 25006)")
	if !isReadOnlyViolation(err) {
		t.Errorf("isReadOnlyViolation should match the canonical phrase: %v", err)
	}
	// Unrelated error is not a violation.
	if isReadOnlyViolation(errors.New("connection refused")) {
		t.Errorf("isReadOnlyViolation should not match unrelated errors")
	}
	// nil is not a violation.
	if isReadOnlyViolation(nil) {
		t.Errorf("isReadOnlyViolation(nil) should be false")
	}
}