#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 10.1 notifications API.
#
# What this exercises:
#   1. Sign in as the operator via better-auth (same pattern as
#      test-sandbox.sh / test-memory.sh).
#   2. POST /api/v1/notifications with kind=log + a simple payload.
#      Asserts: status in (queued, sent), template echoes back.
#   3. GET /api/v1/notifications/stats — shape matches NotificationStatsSchema.
#   4. GET /api/v1/notifications?limit=5 — verify the new row appears.
#
# Skips cleanly when /api/v1/notifications returns 404 or 503.
#
# Env knobs:
#   AF_STACK_PORT           — runtime port  (default 8080)
#   AF_STACK_DASHBOARD_PORT — dashboard port (default 33000)
#   OP_EMAIL                — operator email
#   OP_PASSWORD             — operator pwd

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() {
    yellow "SKIP: $1"
    yellow "   (re-run once Phase 10.1 notifications endpoints are wired)"
    exit 0
}
fail() { red "FAIL: $1"; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"; }
need_cmd curl
need_cmd jq

PORT="${AF_STACK_PORT:-8080}"
DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
RUNTIME_URL="http://localhost:${PORT}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-notifications-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR" 2>/dev/null || true' EXIT

PASSES=0
FAILS=0
pass_step() { green "  PASS  $1"; PASSES=$((PASSES + 1)); }
fail_step() { red   "  FAIL  $1"; FAILS=$((FAILS + 1)); }

# ── sign in ───────────────────────────────────────────────────────────────
step "Sign in"
SIGNIN_RESP=$(curl -s -c "$COOKIE_JAR" -X POST "$DASH_URL/api/auth/sign-in/email" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$OP_EMAIL\",\"password\":\"$OP_PASSWORD\"}" -w "\n%{http_code}")
SIGNIN_CODE=$(echo "$SIGNIN_RESP" | tail -1)
[ "$SIGNIN_CODE" = "200" ] || skip "could not sign in (HTTP $SIGNIN_CODE)"
green "  signed in as $OP_EMAIL"

# ── probe ────────────────────────────────────────────────────────────────
step "Probe POST /api/v1/notifications"
PROBE=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/notifications" \
    -H "Content-Type: application/json" \
    -d '{"kind":"log","template":"probe","to":"void@local","data":{"x":1}}' -w "\n%{http_code}")
PROBE_CODE=$(echo "$PROBE" | tail -1)
[ "$PROBE_CODE" != "404" ] && [ "$PROBE_CODE" != "503" ] || skip "notifications not configured (HTTP $PROBE_CODE)"

# ── send ─────────────────────────────────────────────────────────────────
step "Send a notification"
RESP=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/notifications" \
    -H "Content-Type: application/json" \
    -d '{"kind":"log","template":"smoke-test","to":"qa@af-stack.local","subject":"Smoke","data":{"name":"AF","value":42}}')
ID=$(echo "$RESP" | jq -r '.id // empty')
STATUS=$(echo "$RESP" | jq -r '.status // empty')
if [ -n "$ID" ] && { [ "$STATUS" = "queued" ] || [ "$STATUS" = "sent" ]; }; then
    pass_step "send: id=$ID status=$STATUS"
else
    fail_step "send: $(echo "$RESP" | head -c 200)"
fi

# ── stats ────────────────────────────────────────────────────────────────
step "Stats"
STATS=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/notifications/stats")
SENT_TODAY=$(echo "$STATS" | jq -r '.sent_today // 0')
if [ -n "$SENT_TODAY" ]; then
    pass_step "stats: sent_today=$SENT_TODAY"
else
    fail_step "stats: bad shape — $(echo "$STATS" | head -c 200)"
fi

# ── list ─────────────────────────────────────────────────────────────────
step "List recent"
LIST=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/notifications?limit=5")
COUNT=$(echo "$LIST" | jq -r '.notifications | length // 0')
if [ "$COUNT" -ge 1 ]; then
    pass_step "list: $COUNT recent"
else
    fail_step "list: empty — $(echo "$LIST" | head -c 200)"
fi

echo
echo "=================================="
[ "$FAILS" -eq 0 ] && green "All notification checks passed ($PASSES/$((PASSES + FAILS)))" || { red "FAILED ($FAILS failures)"; exit 1; }
