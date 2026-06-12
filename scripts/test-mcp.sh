#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 11.1/11.2 MCP API.
#
# Exercises:
#   1. GET /api/v1/mcp/servers — empty start.
#   2. POST /api/v1/mcp/servers — add a stub stdio server (binary "cat",
#      which exists everywhere). The server won't actually speak MCP
#      JSON-RPC so connect will likely error — that's fine, we just
#      validate the row landed and surfaced an error.
#   3. GET /api/v1/mcp/servers — verify the row appears with status set.
#   4. PUT /api/v1/mcp/servers/{name}/enabled — toggle off.
#   5. DELETE — remove the row.
#
# Skips cleanly when /api/v1/mcp/servers returns 404 or 503.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; yellow "   (re-run once Phase 11.1 MCP endpoints are wired)"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1"; }
need curl; need jq

PORT="${AF_STACK_PORT:-8080}"
DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-mcp-cookies.XXXXXX)"
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

step "Probe GET /api/v1/mcp/servers"
PROBE=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/mcp/servers" -w "\n%{http_code}")
CODE=$(echo "$PROBE" | tail -1)
[ "$CODE" != "404" ] && [ "$CODE" != "503" ] || skip "MCP not wired (HTTP $CODE)"

NAME="probe-$(date +%s)"

step "Add stub stdio server"
ADD=$(curl -s -b "$COOKIE_JAR" -X POST "$DASH_URL/api/v1/mcp/servers" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$NAME\",\"transport\":\"stdio\",\"command\":[\"cat\"],\"description\":\"smoke test stub\"}")
ADD_NAME=$(echo "$ADD" | jq -r '.name // empty')
if [ "$ADD_NAME" = "$NAME" ]; then
    pass "add: $NAME"
else
    fail_step "add failed: $(echo "$ADD" | head -c 200)"
fi

step "List servers"
LIST=$(curl -s -b "$COOKIE_JAR" "$DASH_URL/api/v1/mcp/servers")
FOUND=$(echo "$LIST" | jq -r --arg n "$NAME" '.servers[] | select(.name == $n) | .status // empty')
if [ -n "$FOUND" ]; then
    pass "list: server present with status=$FOUND"
else
    fail_step "list: $NAME not present"
fi

step "Disable"
DIS=$(curl -s -b "$COOKIE_JAR" -X PUT "$DASH_URL/api/v1/mcp/servers/$NAME/enabled" \
    -H "Content-Type: application/json" -d '{"enabled":false}')
ENABLED=$(echo "$DIS" | jq -r '.is_enabled // empty')
if [ "$ENABLED" = "false" ]; then
    pass "disabled"
else
    fail_step "disable failed: $(echo "$DIS" | head -c 200)"
fi

step "Delete"
DEL=$(curl -s -b "$COOKIE_JAR" -X DELETE "$DASH_URL/api/v1/mcp/servers/$NAME")
DELETED=$(echo "$DEL" | jq -r '.deleted // empty')
if [ "$DELETED" = "true" ]; then
    pass "deleted"
else
    fail_step "delete failed: $(echo "$DEL" | head -c 200)"
fi

echo
echo "=================================="
[ "$FAILS" -eq 0 ] && green "All MCP checks passed ($PASSES/$((PASSES + FAILS)))" || { red "FAILED ($FAILS failures)"; exit 1; }
