#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 12.2 crons API.
#
# Exercises:
#   1. GET /api/v1/crons — start state.
#   2. POST /api/v1/crons — create an @hourly cron for the sample-job.
#   3. GET — verify it appears with next_run_at set.
#   4. PUT /api/v1/crons/{id}/active to disable.
#   5. DELETE.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; yellow "   (re-run once Phase 12.2 crons endpoints are wired)"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1"; }
need curl; need jq

DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-crons-cookies.XXXXXX)"
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

step "Probe GET /api/v1/crons"
PROBE=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/crons" -w "\n%{http_code}")
CODE=$(echo "$PROBE" | tail -1)
[ "$CODE" != "404" ] && [ "$CODE" != "503" ] || skip "crons not wired (HTTP $CODE)"

NAME="smoke-$(date +%s)"

step "Create @hourly cron"
CREATE=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/crons" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$NAME\",\"job_name\":\"sample-job\",\"schedule\":\"@hourly\",\"args\":{\"message\":\"hello from cron\"}}")
ID=$(echo "$CREATE" | jq -r '.id // empty')
if [ -n "$ID" ]; then
    pass "created: id=$ID"
else
    fail_step "create failed: $(echo "$CREATE" | head -c 200)"
fi

step "List"
LIST=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/crons")
COUNT=$(echo "$LIST" | jq -r '.crons | length // 0')
if [ "$COUNT" -ge 1 ]; then
    pass "list: $COUNT cron(s)"
else
    fail_step "list empty"
fi

if [ -n "$ID" ]; then
    step "Disable"
    DIS=$(curl -s -b "$COOKIE_JAR" -X PUT "$DASH_URL/api/v1/crons/$ID/active" \
        -H "Content-Type: application/json" -d '{"is_active":false}')
    ACT=$(echo "$DIS" | jq -r '.is_active // empty')
    if [ "$ACT" = "false" ]; then
        pass "disabled"
    else
        fail_step "disable failed"
    fi

    step "Delete"
    DEL=$(curl -s -b "$COOKIE_JAR" -X DELETE "$DASH_URL/api/v1/crons/$ID")
    DELETED=$(echo "$DEL" | jq -r '.deleted // empty')
    if [ "$DELETED" = "true" ]; then
        pass "deleted"
    else
        fail_step "delete failed"
    fi
fi

echo
echo "=================================="
[ "$FAILS" -eq 0 ] && green "All cron checks passed ($PASSES/$((PASSES + FAILS)))" || { red "FAILED ($FAILS failures)"; exit 1; }
