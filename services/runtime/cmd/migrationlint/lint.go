// SPDX-License-Identifier: Apache-2.0

// Command migrationlint statically checks BackAI goose migrations for
// production-safety hazards:
//
//   - every migration has both a `-- +goose Up` and a `-- +goose Down` section
//     (migrations must be reversible),
//   - no obviously destructive op (DROP TABLE / DROP COLUMN) runs on
//     migrate-*up* without an explicit `-- backai:allow-destructive` marker
//     (DROPs in the Down section are the normal rollback and are ignored),
//   - every `-- +goose StatementBegin` is matched by a `-- +goose StatementEnd`
//     (an unbalanced pair silently breaks DO-block migrations — the exact class
//     of bug that only surfaces at apply time; see the CI-no-DB memory).
//
// Exit code is non-zero when any finding is reported. Wire it via
// `make lint-migrations` or scripts/migration-safety-check.sh.
package main

import (
	"regexp"
	"strings"
)

// Stable finding codes.
const (
	CodeNoUp           = "MIGRATION_NO_UP"
	CodeNoDown         = "MIGRATION_NO_DOWN"
	CodeDestructiveUp  = "MIGRATION_DESTRUCTIVE_UP"
	CodeUnbalancedStmt = "MIGRATION_UNBALANCED_STATEMENT"
)

// Finding is a single lint hit.
type Finding struct {
	File    string
	Line    int
	Code    string
	Message string
}

var (
	reUp         = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+up\b`)
	reDown       = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+down\b`)
	reBegin      = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+statementbegin\b`)
	reEnd        = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+statementend\b`)
	reDropTable  = regexp.MustCompile(`(?i)\bdrop\s+table\b`)
	reDropColumn = regexp.MustCompile(`(?i)\bdrop\s+column\b`)
	reAllow      = regexp.MustCompile(`(?i)backai:allow-destructive`)
)

// LintContent lints a single migration file's content and returns findings in
// line order.
func LintContent(file, content string) []Finding {
	lines := strings.Split(content, "\n")
	var findings []Finding

	section := "" // "", "up", "down"
	hasUp, hasDown := false, false
	stmtDepth := 0
	prevLine := ""

	for i, line := range lines {
		lineNo := i + 1
		switch {
		case reUp.MatchString(line):
			hasUp = true
			section = "up"
		case reDown.MatchString(line):
			hasDown = true
			section = "down"
		case reBegin.MatchString(line):
			stmtDepth++
		case reEnd.MatchString(line):
			stmtDepth--
			if stmtDepth < 0 {
				findings = append(findings, Finding{
					File: file, Line: lineNo, Code: CodeUnbalancedStmt,
					Message: "goose StatementEnd without a matching StatementBegin",
				})
				stmtDepth = 0
			}
		}

		// Destructive-op detection: only in the Up section, and only on real
		// SQL lines (skip pure comments so a `-- drop the table` note or the
		// goose directives themselves don't false-positive).
		if section == "up" && !isCommentLine(line) {
			if reDropTable.MatchString(line) || reDropColumn.MatchString(line) {
				if !reAllow.MatchString(line) && !reAllow.MatchString(prevLine) {
					op := "DROP TABLE"
					if reDropColumn.MatchString(line) {
						op = "DROP COLUMN"
					}
					findings = append(findings, Finding{
						File: file, Line: lineNo, Code: CodeDestructiveUp,
						Message: op + " in the Up section without a `-- backai:allow-destructive` marker (destroys data on migrate-up)",
					})
				}
			}
		}
		prevLine = line
	}

	if !hasUp {
		findings = append(findings, Finding{File: file, Line: 0, Code: CodeNoUp, Message: "missing `-- +goose Up` section"})
	}
	if !hasDown {
		findings = append(findings, Finding{File: file, Line: 0, Code: CodeNoDown,
			Message: "missing `-- +goose Down` section (migrations must be reversible; use an explicit empty Down if truly irreversible)"})
	}
	if stmtDepth > 0 {
		findings = append(findings, Finding{File: file, Line: len(lines), Code: CodeUnbalancedStmt,
			Message: "unclosed goose StatementBegin (missing StatementEnd)"})
	}
	return findings
}

// isCommentLine reports whether the trimmed line is a pure SQL comment.
func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "--")
}
