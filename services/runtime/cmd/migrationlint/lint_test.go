// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"runtime"
	"testing"
)

func codesOf(findings []Finding) map[string]int {
	m := map[string]int{}
	for _, f := range findings {
		m[f.Code]++
	}
	return m
}

func TestLintGoodMigrationIsClean(t *testing.T) {
	good := `-- +goose Up
create table suite_widgets (
    id uuid primary key,
    tenant_id uuid not null
);
alter table suite_widgets enable row level security;
alter table suite_widgets force row level security;

-- +goose Down
drop table if exists suite_widgets;
`
	if got := LintContent("00099_good.sql", good); len(got) != 0 {
		t.Fatalf("expected clean, got %+v", got)
	}
}

func TestLintCatchesMissingDown(t *testing.T) {
	content := `-- +goose Up
create table t (id int);
`
	codes := codesOf(LintContent("bad.sql", content))
	if codes[CodeNoDown] != 1 {
		t.Errorf("expected one MIGRATION_NO_DOWN, got %v", codes)
	}
}

func TestLintCatchesDestructiveUp(t *testing.T) {
	content := `-- +goose Up
drop table suite_important;
alter table suite_other drop column secret;

-- +goose Down
select 1;
`
	codes := codesOf(LintContent("bad.sql", content))
	if codes[CodeDestructiveUp] != 2 {
		t.Errorf("expected two MIGRATION_DESTRUCTIVE_UP (drop table + drop column), got %v", codes)
	}
}

func TestLintAllowsMarkedDestructiveUp(t *testing.T) {
	// Same-line marker and previous-line marker both suppress the finding.
	content := `-- +goose Up
drop table suite_legacy; -- backai:allow-destructive
-- backai:allow-destructive
alter table suite_x drop column old_col;

-- +goose Down
select 1;
`
	if got := LintContent("marked.sql", content); len(got) != 0 {
		t.Fatalf("expected marked destructive ops to be allowed, got %+v", got)
	}
}

func TestLintIgnoresDropsInDownSection(t *testing.T) {
	// A DROP in the Down section is the normal rollback — not a finding.
	content := `-- +goose Up
create table t (id int);

-- +goose Down
drop table if exists t;
`
	if got := LintContent("rollback.sql", content); len(got) != 0 {
		t.Fatalf("expected no findings for Down-section drop, got %+v", got)
	}
}

func TestLintCatchesUnbalancedStatementBegin(t *testing.T) {
	content := `-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN NULL; END $$;

-- +goose Down
select 1;
`
	codes := codesOf(LintContent("unbalanced.sql", content))
	if codes[CodeUnbalancedStmt] != 1 {
		t.Errorf("expected one MIGRATION_UNBALANCED_STATEMENT (missing StatementEnd), got %v", codes)
	}
}

func TestLintCatchesOrphanStatementEnd(t *testing.T) {
	content := `-- +goose Up
select 1;
-- +goose StatementEnd

-- +goose Down
select 1;
`
	codes := codesOf(LintContent("orphan.sql", content))
	if codes[CodeUnbalancedStmt] != 1 {
		t.Errorf("expected one MIGRATION_UNBALANCED_STATEMENT (orphan StatementEnd), got %v", codes)
	}
}

func TestLintBalancedStatementBlockIsClean(t *testing.T) {
	content := `-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN NULL; END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN NULL; END $$;
-- +goose StatementEnd
`
	if got := LintContent("balanced.sql", content); len(got) != 0 {
		t.Fatalf("expected clean balanced statement blocks, got %+v", got)
	}
}

// Regression guard: the real runtime migration tree must lint clean, proving
// the checker does not false-positive on shipped migrations (whose Down
// sections are full of legitimate DROPs).
func TestRealMigrationsAreClean(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "internal", "db", "migrations")
	findings, err := lintDir(dir)
	if err != nil {
		t.Fatalf("lintDir(%s): %v", dir, err)
	}
	if len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("%s:%d [%s] %s", f.File, f.Line, f.Code, f.Message)
		}
		t.Fatalf("real migrations produced %d finding(s); either a migration regressed or the linter false-positives", len(findings))
	}
}
