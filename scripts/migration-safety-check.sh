#!/usr/bin/env bash
# Static safety lint for BackAI goose migrations.
#
# Thin wrapper over the tested Go tool at services/runtime/cmd/migrationlint.
# Verifies every migration has an Up + Down section, has no destructive op
# (DROP TABLE / DROP COLUMN) in its Up section without a
# `-- backai:allow-destructive` marker, and has balanced goose
# StatementBegin/StatementEnd pairs.
#
# Usage:
#   ./scripts/migration-safety-check.sh [migrations-dir]
#
# Exits non-zero when any finding is reported. Run from the repo root.
set -euo pipefail

cd "$(dirname "$0")/.."

exec go run ./services/runtime/cmd/migrationlint "$@"
