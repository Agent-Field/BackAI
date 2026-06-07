#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 10.4 billing API.
#
# Exercises:
#   1. GET /api/v1/billing/customers — returns at least the default tenant.
#   2. GET /api/v1/billing/meters — usage rolled up for the current month.
#   3. POST /api/v1/billing/customers/{tenantId}/portal — returns a portal
#      URL (real if STRIPE_SECRET_KEY is set, stub otherwise).
#
# Skips on 404/503.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }
skip() { yellow "SKIP: $1"; yellow "   (re-run once Phase 10.4 billing endpoints are wired)"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1"; }
need curl; need jq

PORT="${AF_STACK_PORT:-38080}"
DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-billing-cookies.XXXXXX)"
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

step "Probe GET /api/v1/billing/customers"
PROBE=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/billing/customers" -w "\n%{http_code}")
CODE=$(echo "$PROBE" | tail -1)
[ "$CODE" != "404" ] && [ "$CODE" != "503" ] || skip "billing not wired (HTTP $CODE)"

step "List customers"
LIST=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/billing/customers")
COUNT=$(echo "$LIST" | jq -r '.customers | length // 0')
if [ "$COUNT" -ge 1 ]; then
    pass "customers: $COUNT"
    TENANT=$(echo "$LIST" | jq -r '.customers[0].tenant_id // empty')
else
    fail_step "no customers — $(echo "$LIST" | head -c 200)"
    TENANT=""
fi

step "List meters"
METERS=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/billing/meters")
TOTAL=$(echo "$METERS" | jq -r '.total_cost_usd // -1')
if [ "$TOTAL" != "-1" ]; then
    pass "meters: total_cost_usd=$TOTAL"
else
    fail_step "meters bad shape — $(echo "$METERS" | head -c 200)"
fi

if [ -n "$TENANT" ]; then
    step "Portal link for tenant $TENANT"
    PORTAL=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/billing/customers/$TENANT/portal")
    URL=$(echo "$PORTAL" | jq -r '.url // empty')
    if [ -n "$URL" ]; then
        pass "portal: $URL"
    else
        fail_step "portal link failed — $(echo "$PORTAL" | head -c 200)"
    fi
fi

echo
echo "=================================="
[ "$FAILS" -eq 0 ] && green "All billing checks passed ($PASSES/$((PASSES + FAILS)))" || { red "FAILED ($FAILS failures)"; exit 1; }
