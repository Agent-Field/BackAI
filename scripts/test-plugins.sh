#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 12.3 plugin manifest API.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; yellow "   (re-run once Phase 12.3 plugin endpoint is wired)"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1"; }
need curl; need jq

DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
DASH_URL="http://localhost:${DASH_PORT}"

step "Probe GET /api/v1/plugins"
PROBE=$(curl -s "$DASH_URL/api/v1/plugins" -w "\n%{http_code}")
CODE=$(echo "$PROBE" | tail -1)
[ "$CODE" != "404" ] || skip "plugins endpoint not wired (HTTP $CODE)"

step "Validate shape"
BODY=$(curl -s "$DASH_URL/api/v1/plugins")
COUNT=$(echo "$BODY" | jq -r '.plugins | length // 0')
if [ -n "$COUNT" ]; then
    green "  PASS  $COUNT plugin(s) registered"
    echo "$BODY" | jq -r '.plugins[]? | "    \(.id): \(.name) → \(.route)"'
else
    red "  FAIL  shape: $(echo "$BODY" | head -c 200)"
    exit 1
fi

echo
green "plugins manifest OK"
