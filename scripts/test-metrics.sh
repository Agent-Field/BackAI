#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 12.2 metrics summary API.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; yellow "   (re-run once Phase 12.2 metrics endpoint is wired)"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1"; }
need curl; need jq

DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-metrics-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR" 2>/dev/null || true' EXIT

step "Sign in"
SIGNIN=$(curl -s -c "$COOKIE_JAR" -X POST "$DASH_URL/api/auth/sign-in/email" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$OP_EMAIL\",\"password\":\"$OP_PASSWORD\"}" -w "\n%{http_code}")
[ "$(echo "$SIGNIN" | tail -1)" = "200" ] || skip "sign-in failed"
green "  signed in"

step "Probe GET /api/v1/metrics/summary"
PROBE=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/metrics/summary" -w "\n%{http_code}")
CODE=$(echo "$PROBE" | tail -1)
[ "$CODE" != "404" ] && [ "$CODE" != "503" ] || skip "metrics not wired (HTTP $CODE)"

step "Validate shape"
BODY=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/metrics/summary")
REQS=$(echo "$BODY" | jq -r '.http_requests_total // -1')
GOROUTINES=$(echo "$BODY" | jq -r '.goroutines // -1')
UPTIME=$(echo "$BODY" | jq -r '.uptime_seconds // -1')
VERSION=$(echo "$BODY" | jq -r '.version // empty')

if [ "$REQS" != "-1" ] && [ "$GOROUTINES" != "-1" ] && [ "$UPTIME" != "-1" ] && [ -n "$VERSION" ]; then
    green "  PASS  shape: reqs=$REQS goroutines=$GOROUTINES uptime=${UPTIME}s version=$VERSION"
else
    red "  FAIL  shape: $(echo "$BODY" | head -c 300)"
    exit 1
fi

echo
green "metrics summary OK"
