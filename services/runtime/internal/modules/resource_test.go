// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"strings"
	"testing"
)

func TestTableName(t *testing.T) {
	cases := map[string]struct{ module, resource, want string }{
		"simple":     {"notes", "notes", "notes_notes"},
		"hyphenated": {"agent-commerce", "orders", "agent_commerce_orders"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := TableName(c.module, c.resource); got != c.want {
				t.Fatalf("TableName(%q,%q) = %q, want %q", c.module, c.resource, got, c.want)
			}
		})
	}
}

// TestBuildersAlwaysFilterTenant is the core cross-tenant guarantee: every
// generated statement scopes to tenant_id = $1 (or, for INSERT, binds
// tenant_id as column $1). A tenant value can therefore never be omitted or
// supplied by the client — it is always the resolver-bound $1.
func TestBuildersAlwaysFilterTenant(t *testing.T) {
	table := "notes_notes"
	fields := []string{"title", "body", "done"}

	insert := buildInsertSQL(table, fields)
	if !strings.Contains(insert, "(tenant_id,") {
		t.Fatalf("insert must bind tenant_id as the first column: %s", insert)
	}
	if !strings.Contains(insert, "VALUES ($1,") && !strings.Contains(insert, "VALUES ($1)") {
		t.Fatalf("insert must bind $1 to tenant_id: %s", insert)
	}

	filtered := map[string]string{
		"list":   buildListSQL(table, fields),
		"count":  buildCountSQL(table),
		"get":    buildGetSQL(table, fields),
		"update": buildUpdateSQL(table, []string{"title"}, fields),
		"delete": buildDeleteSQL(table),
	}
	for name, sql := range filtered {
		if !strings.Contains(sql, "tenant_id = $1") {
			t.Fatalf("%s must filter tenant_id = $1: %s", name, sql)
		}
	}

	// Update must never move a row across tenants: tenant_id is not settable.
	upd := buildUpdateSQL(table, []string{"title", "done"}, fields)
	if strings.Contains(upd, "tenant_id =") && !strings.Contains(upd, "WHERE tenant_id = $1") {
		t.Fatalf("update unexpectedly sets tenant_id: %s", upd)
	}
	if strings.Contains(upd, "SET \"tenant_id\"") || strings.Contains(upd, "SET tenant_id") {
		t.Fatalf("update SET clause must not include tenant_id: %s", upd)
	}
}

func TestBuildUpdateSQL_PlaceholderSequence(t *testing.T) {
	// tenant=$1, first present field=$2, ... id is the final placeholder.
	got := buildUpdateSQL("notes_notes", []string{"title", "done"}, []string{"title", "body", "done"})
	if !strings.Contains(got, `"title" = $2`) || !strings.Contains(got, `"done" = $3`) {
		t.Fatalf("present fields should bind $2, $3: %s", got)
	}
	if !strings.Contains(got, "AND id = $4") {
		t.Fatalf("id should be the final placeholder $4: %s", got)
	}
	if !strings.Contains(got, "updated_at = now()") {
		t.Fatalf("update should bump updated_at: %s", got)
	}
}

func TestSQLColumnType(t *testing.T) {
	cases := map[string]string{
		FieldTypeString:    "text",
		FieldTypeInt:       "bigint",
		FieldTypeBool:      "boolean",
		FieldTypeTimestamp: "timestamptz",
		FieldTypeJSON:      "jsonb",
	}
	for in, want := range cases {
		if got := SQLColumnType(in); got != want {
			t.Fatalf("SQLColumnType(%q) = %q, want %q", in, got, want)
		}
	}
}
