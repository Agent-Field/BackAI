// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// LintMigrationSQL statically enforces the multi-tenancy invariant on a
// module's migration SQL: every table the migration CREATEs must carry a
// tenant_id column AND have row-level security enabled, forced, and at
// least one policy. A module whose SQL fails this check is refused before
// any DDL runs — the module is disabled while the runtime keeps serving.
//
// The check is intentionally a lexical scan, not a full SQL parser: it is
// a guard rail, so it errs toward demanding the explicit tenant-isolation
// statements the codebase's own migrations use (see 00004_rls.sql,
// 00032_memory_tenant_rls.sql) rather than trying to prove isolation from
// arbitrary DDL.
func LintMigrationSQL(sql string) error {
	stripped := stripSQLComments(sql)

	created := createdTables(stripped)
	if len(created) == 0 {
		// No tables created — nothing to isolate (e.g. an index-only or
		// data-only migration). Nothing to enforce.
		return nil
	}

	enableRLS := tablesWith(stripped, reEnableRLS)
	forceRLS := tablesWith(stripped, reForceRLS)
	policyOn := tablesWith(stripped, rePolicyOn)

	var problems []string
	names := make([]string, 0, len(created))
	for name := range created {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := created[name]
		if !hasTenantColumn(body) {
			problems = append(problems, fmt.Sprintf("table %q is missing a tenant_id column", name))
		}
		if _, ok := enableRLS[name]; !ok {
			problems = append(problems, fmt.Sprintf("table %q is missing ENABLE ROW LEVEL SECURITY", name))
		}
		if _, ok := forceRLS[name]; !ok {
			problems = append(problems, fmt.Sprintf("table %q is missing FORCE ROW LEVEL SECURITY", name))
		}
		if _, ok := policyOn[name]; !ok {
			problems = append(problems, fmt.Sprintf("table %q has no CREATE POLICY", name))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("tenant-isolation lint failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

var (
	// reCreateTable captures the table name and the parenthesised column
	// body of a CREATE TABLE statement. [\s\S] so it spans newlines; the
	// body match is non-greedy to the matching-ish close paren of the DDL.
	reCreateTable = regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?([a-z_][a-z0-9_."]*)\s*\(([\s\S]*?)\)\s*;`)
	reEnableRLS   = regexp.MustCompile(`(?is)alter\s+table\s+(?:if\s+exists\s+)?([a-z_][a-z0-9_."]*)\s+enable\s+row\s+level\s+security`)
	reForceRLS    = regexp.MustCompile(`(?is)alter\s+table\s+(?:if\s+exists\s+)?([a-z_][a-z0-9_."]*)\s+force\s+row\s+level\s+security`)
	rePolicyOn    = regexp.MustCompile(`(?is)create\s+policy\s+[a-z0-9_"]+\s+on\s+([a-z_][a-z0-9_."]*)`)
	reTenantCol   = regexp.MustCompile(`(?im)^\s*"?tenant_id"?\s`)
)

// createdTables returns a map of normalised table name -> CREATE TABLE
// column body.
func createdTables(sql string) map[string]string {
	out := map[string]string{}
	for _, m := range reCreateTable.FindAllStringSubmatch(sql, -1) {
		out[normalizeTableName(m[1])] = m[2]
	}
	return out
}

// tablesWith returns the set of table names referenced by statements
// matching re (its first capture group is the table name).
func tablesWith(sql string, re *regexp.Regexp) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range re.FindAllStringSubmatch(sql, -1) {
		out[normalizeTableName(m[1])] = struct{}{}
	}
	return out
}

func hasTenantColumn(createBody string) bool {
	return reTenantCol.MatchString(createBody)
}

// normalizeTableName lowercases and strips schema qualifier + quotes so
// `public."Foo"` and `foo` compare equal.
func normalizeTableName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.ReplaceAll(name, `"`, "")
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// stripSQLComments removes -- line comments and /* */ block comments so
// commented-out DDL never trips (or satisfies) the lint. goose module
// annotations, if present, are line comments and drop out here too.
func stripSQLComments(sql string) string {
	// Block comments first.
	var b strings.Builder
	for {
		start := strings.Index(sql, "/*")
		if start < 0 {
			b.WriteString(sql)
			break
		}
		b.WriteString(sql[:start])
		end := strings.Index(sql[start:], "*/")
		if end < 0 {
			// Unterminated block comment — drop the rest.
			break
		}
		sql = sql[start+end+2:]
	}
	out := b.String()
	// Line comments.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "--"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}
