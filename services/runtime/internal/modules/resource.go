// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"
	"strings"
)

// TableName returns the backing table for a resource. The convention is
// <module>_<resource> with the module id's hyphens folded to
// underscores, so the `notes` module's `notes` resource is table
// `notes_notes` and an `agent-commerce` module's `orders` resource is
// `agent_commerce_orders`. A module author's migration MUST create this
// exact table (with id, tenant_id, created_at, updated_at + the declared
// fields).
func TableName(moduleID, resource string) string {
	return strings.ReplaceAll(moduleID, "-", "_") + "_" + resource
}

// quoteIdent wraps an already-validated identifier in double quotes.
// Field/resource/module names are validated against identPattern/idPattern
// at manifest parse time, so this is defence-in-depth, not the primary
// guard against injection (nothing user-supplied reaches these builders —
// only manifest-declared identifiers do).
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func selectColumns(fields []string) string {
	cols := make([]string, 0, len(fields)+3)
	cols = append(cols, "id")
	for _, f := range fields {
		cols = append(cols, quoteIdent(f))
	}
	cols = append(cols, "created_at", "updated_at")
	return strings.Join(cols, ", ")
}

// The returned column names, in select order, so a dynamic row scan can
// key values by name without depending on the driver's field descriptors.
func selectColumnNames(fields []string) []string {
	names := make([]string, 0, len(fields)+3)
	names = append(names, "id")
	names = append(names, fields...)
	names = append(names, "created_at", "updated_at")
	return names
}

// buildInsertSQL constructs a tenant-scoped INSERT. tenant_id is ALWAYS
// column $1 — the caller binds it from the request's resolved tenant, and
// no client-supplied tenant can override it because it is not among the
// resource fields.
func buildInsertSQL(table string, fields []string) string {
	cols := []string{"tenant_id"}
	placeholders := []string{"$1"}
	for i, f := range fields {
		cols = append(cols, quoteIdent(f))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+2))
	}
	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
		quoteIdent(table),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		selectColumns(fields),
	)
}

// buildListSQL constructs a tenant-scoped, paginated list. The WHERE
// clause ALWAYS filters tenant_id = $1.
func buildListSQL(table string, fields []string) string {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
		selectColumns(fields),
		quoteIdent(table),
	)
}

// buildCountSQL counts rows for the current tenant only.
func buildCountSQL(table string) string {
	return fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id = $1", quoteIdent(table))
}

// buildGetSQL fetches one row scoped to tenant_id = $1 AND id = $2.
func buildGetSQL(table string, fields []string) string {
	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE tenant_id = $1 AND id = $2",
		selectColumns(fields),
		quoteIdent(table),
	)
}

// buildUpdateSQL constructs a tenant-scoped UPDATE over the given present
// fields. tenant_id is $1 and id is the final placeholder; the WHERE
// clause ALWAYS pins both so a tenant can never mutate another tenant's
// row (nor can it move a row across tenants — tenant_id is never in SET).
func buildUpdateSQL(table string, present []string, allFields []string) string {
	sets := make([]string, 0, len(present)+1)
	for i, f := range present {
		sets = append(sets, fmt.Sprintf("%s = $%d", quoteIdent(f), i+2))
	}
	sets = append(sets, "updated_at = now()")
	idPlaceholder := fmt.Sprintf("$%d", len(present)+2)
	return fmt.Sprintf(
		"UPDATE %s SET %s WHERE tenant_id = $1 AND id = %s RETURNING %s",
		quoteIdent(table),
		strings.Join(sets, ", "),
		idPlaceholder,
		selectColumns(allFields),
	)
}

// buildDeleteSQL removes one row scoped to tenant_id = $1 AND id = $2.
func buildDeleteSQL(table string) string {
	return fmt.Sprintf("DELETE FROM %s WHERE tenant_id = $1 AND id = $2", quoteIdent(table))
}
