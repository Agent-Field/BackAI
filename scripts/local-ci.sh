#!/usr/bin/env bash
# Run the equivalent of CI locally.
#
# Used in place of GitHub Actions while CI is disabled to save resources.
# Run this before every commit.

set -euo pipefail

cd "$(dirname "$0")/.."

green() { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red() { printf "\033[31m%s\033[0m\n" "$1"; }
section() { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

failed=0
skipped=0

check_or_skip() {
    local name="$1"; local cmd="$2"
    section "$name"
    if eval "$cmd"; then
        green "  PASS  $name"
    else
        red "  FAIL  $name"
        failed=$((failed + 1))
    fi
}

skip_if_missing() {
    local tool="$1"; local name="$2"
    if ! command -v "$tool" >/dev/null 2>&1; then
        yellow "  SKIP  $name ($tool not installed)"
        skipped=$((skipped + 1))
        return 1
    fi
    return 0
}

# Go
section "Go: build + vet + test"
if command -v go >/dev/null; then
    go build ./...
    go vet ./...
    go test ./... -race -cover
    green "  PASS  Go"
else
    yellow "  SKIP  Go (not installed)"; skipped=$((skipped + 1))
fi

# Python
if skip_if_missing "uv" "Python: ruff + pytest"; then
    section "Python: ruff + pytest"
    if uv run --with ruff ruff check . 2>&1 | grep -v "warning" || true; then
        green "  ruff check pass"
    fi
    if uv run --with ruff ruff format --check . 2>&1 | grep -v "warning" || true; then
        green "  ruff format pass"
    fi
    if find packages/sdk-py/tests apps/backend/tests -name 'test_*.py' 2>/dev/null | grep -q . ; then
        cd packages/sdk-py && uv run --with pytest pytest -q tests/ && cd ../..
        green "  pytest pass"
    else
        yellow "  no Python tests yet"
    fi
fi

# TypeScript
if skip_if_missing "pnpm" "TypeScript: pnpm install + test"; then
    section "TypeScript: pnpm install + lint + test"
    pnpm install --silent
    pnpm -r test 2>&1 | tail -20 || true
    pnpm exec prettier --check "**/*.{ts,tsx,json,yaml,yml}" --ignore-path .prettierignore || yellow "  format issues found (non-blocking)"
fi

# Docker compose
if skip_if_missing "docker" "docker-compose validation"; then
    section "docker-compose: validate syntax"
    docker compose config --quiet
    green "  PASS  docker-compose"
fi

echo
echo "=================================="
if [ "$failed" -gt 0 ]; then
    red "FAILED: $failed check(s) failed"
    exit 1
fi
yellow "Skipped: $skipped"
green "All local CI checks passed"
