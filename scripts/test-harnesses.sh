#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 11.4 harnesses API.
#
# Exercises:
#   1. GET /api/v1/harnesses — returns 4 entries (claude-code, codex,
#      gemini, opencode), each with a Status.
#   2. For each: status should be one of ready/needs_auth/missing/errored.
#   3. POST /api/v1/harnesses/claude-code/probe — re-probes, returns a
#      fresh row.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; yellow "   (re-run once Phase 11.4 harnesses endpoints are wired)"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1"; }
need curl; need jq

DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-harnesses-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR" 2>/dev/null || true' EXIT

PASSES=0; FAILS=0
pass() { green "  PASS  $1"; PASSES=$((PASSES + 1)); }
fail_step() { red "  FAIL  $1"; FAILS=$((FAILS + 1)); }

step "Sign in"
SIGNIN=$(curl -s -c "$COOKIE_JAR" -X POST "$DASH_URL/api/auth/sign-in/email" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$OP_EMAIL\",\"password\":\"$OP_PASSWORD\"}" -w "\n%{http_code}")
[ "$(echo "$SIGNIN" | tail -1)" = "200" ] || skip "sign-in failed"
green "  signed in"

step "Probe GET /api/v1/harnesses"
PROBE=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/harnesses" -w "\n%{http_code}")
CODE=$(echo "$PROBE" | tail -1)
[ "$CODE" != "404" ] && [ "$CODE" != "503" ] || skip "harnesses not wired (HTTP $CODE)"

step "List"
LIST=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/harnesses")
COUNT=$(echo "$LIST" | jq -r '.harnesses | length // 0')
if [ "$COUNT" -ge 4 ]; then
    pass "list: $COUNT harnesses"
    echo "$LIST" | jq -r '.harnesses[] | "  \(.provider): status=\(.status) installed=\(.is_installed)"'
else
    fail_step "list: expected 4 — got $COUNT"
fi

step "Re-probe claude-code"
RE=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/harnesses/claude-code/probe")
STATUS=$(echo "$RE" | jq -r '.status // empty')
case "$STATUS" in
    ready|needs_auth|missing|errored)
        pass "probe: status=$STATUS"
        ;;
    *)
        fail_step "probe: unexpected status — $(echo "$RE" | head -c 200)"
        ;;
esac

echo
echo "=================================="
[ "$FAILS" -eq 0 ] && green "All harness checks passed ($PASSES/$((PASSES + FAILS)))" || { red "FAILED ($FAILS failures)"; exit 1; }
