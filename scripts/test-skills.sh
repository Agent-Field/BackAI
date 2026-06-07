#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 11.3 skills API.
#
# Exercises:
#   1. GET /api/v1/skills — start state.
#   2. POST /api/v1/skills with source="af-skill://acme/test@1.0.0".
#   3. GET — verify the row appears.
#   4. POST /api/v1/skills/attach binding the skill to a fake agent.
#   5. DELETE /api/v1/skills/{id} — uninstall.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; yellow "   (re-run once Phase 11.3 skills endpoints are wired)"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1"; }
need curl; need jq

DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-skills-cookies.XXXXXX)"
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

step "Probe GET /api/v1/skills"
PROBE=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/skills" -w "\n%{http_code}")
CODE=$(echo "$PROBE" | tail -1)
[ "$CODE" != "404" ] && [ "$CODE" != "503" ] || skip "skills not wired (HTTP $CODE)"

VERSION="$(date +%s)"
SOURCE="af-skill://acme/test@${VERSION}"

step "Install skill ($SOURCE)"
INS=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/skills" \
    -H "Content-Type: application/json" \
    -d "{\"source\":\"$SOURCE\"}")
SKILL_ID=$(echo "$INS" | jq -r '.id // empty')
if [ -n "$SKILL_ID" ]; then
    pass "installed: id=$SKILL_ID"
else
    fail_step "install failed: $(echo "$INS" | head -c 200)"
fi

step "List"
LIST=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/skills")
COUNT=$(echo "$LIST" | jq -r '.skills | length // 0')
if [ "$COUNT" -ge 1 ]; then
    pass "list: $COUNT skill(s)"
else
    fail_step "list empty"
fi

if [ -n "$SKILL_ID" ]; then
    step "Attach to agent"
    ATT=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/skills/attach" \
        -H "Content-Type: application/json" \
        -d "{\"skill_id\":\"$SKILL_ID\",\"agent\":\"smoke-test-agent\"}")
    ATTACHED=$(echo "$ATT" | jq -r '.attached // empty')
    if [ "$ATTACHED" = "true" ]; then
        pass "attached"
    else
        fail_step "attach failed: $(echo "$ATT" | head -c 200)"
    fi

    step "Uninstall"
    DEL=$(curl -s -b "$COOKIE_JAR" -X DELETE "$DASH_URL/api/v1/skills/$SKILL_ID")
    DELETED=$(echo "$DEL" | jq -r '.deleted // empty')
    if [ "$DELETED" = "true" ]; then
        pass "uninstalled"
    else
        fail_step "uninstall failed: $(echo "$DEL" | head -c 200)"
    fi
fi

echo
echo "=================================="
[ "$FAILS" -eq 0 ] && green "All skills checks passed ($PASSES/$((PASSES + FAILS)))" || { red "FAILED ($FAILS failures)"; exit 1; }
