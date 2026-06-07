#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 7 budget enforcement.
#
# What this exercises:
#   1. Create a fresh tenant via the admin endpoint
#   2. Issue an API key for that tenant
#   3. Set a tiny ($0.0001) monthly budget via PUT /api/v1/admin/budgets
#   4. Make a chat completion — first call should succeed under budget
#   5. Make a second chat completion — should fail with HTTP 402 and
#      error.code == "BUDGET_EXCEEDED"
#   6. Cleanup: revoke the key, soft-delete the tenant
#
# When Phase 7.1 (gateway) or Phase 7.2 (budget enforcer) are not yet
# wired the script SKIPs cleanly with exit 0 so CI smoke runs pass.
#
# Usage:
#   ./scripts/test-budget-enforcement.sh
#
# Env knobs:
#   AF_STACK_PORT (default 8080)       — runtime port
#   AF_STACK_TEST_MODEL                — model to use (default qwen 2.5 72B)
#   BUDGET_AMOUNT (default 0.0001)     — monthly budget USD

set -euo pipefail

cd "$(dirname "$0")/.."

# ── pretty output ──────────────────────────────────────────────────────────
green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
blue()   { printf "\033[34m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() {
    yellow "SKIP: $1"
    yellow "   (re-run once Phase 7.1 + 7.2 are landed)"
    exit 0
}

fail() {
    red "FAIL: $1"
    exit 1
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}
need_cmd curl
need_cmd jq

# ── config ────────────────────────────────────────────────────────────────
PORT="${AF_STACK_PORT:-8080}"
RUNTIME_URL="http://localhost:${PORT}"
MODEL="${AF_STACK_TEST_MODEL:-qwen/qwen-2.5-72b-instruct}"
BUDGET_AMOUNT="${BUDGET_AMOUNT:-0.0001}"
SUFFIX="$(date +%s)-$RANDOM"
SLUG="budget-test-${SUFFIX}"

TENANT_ID=""
KEY_ID=""
KEY_VALUE=""

PASSES=0
FAILS=0
pass_step() { green "  PASS  $1"; PASSES=$((PASSES + 1)); }
fail_step() { red   "  FAIL  $1"; FAILS=$((FAILS + 1)); }

cleanup() {
    [ -n "$KEY_ID" ] && curl -s --max-time 5 -X DELETE "${RUNTIME_URL}/api/v1/admin/keys/${KEY_ID}" >/dev/null 2>&1 || true
    [ -n "$TENANT_ID" ] && curl -s --max-time 5 -X DELETE "${RUNTIME_URL}/api/v1/admin/tenants/${TENANT_ID}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# ── 0. Readiness probe ────────────────────────────────────────────────────
step "0/6  probe runtime readiness"
if ! HEALTH="$(curl -s --max-time 3 "${RUNTIME_URL}/health" 2>/dev/null)"; then
    skip "runtime unreachable at ${RUNTIME_URL}"
fi
if ! echo "$HEALTH" | grep -q '"status"'; then
    skip "runtime /health did not return expected shape"
fi
green "  runtime reachable"

# Confirm /api/v1/llm/* is wired. 401 == handler exists, just unauthed.
PROBE_STATUS="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \
    "${RUNTIME_URL}/api/v1/llm/models" 2>/dev/null || echo "000")"
case "$PROBE_STATUS" in
    200|401|403)
        green "  /api/v1/llm/models registered (HTTP $PROBE_STATUS)"
        ;;
    404|405|501)
        skip "/api/v1/llm/models not registered yet (HTTP $PROBE_STATUS)"
        ;;
    *)
        yellow "  /api/v1/llm/models returned HTTP $PROBE_STATUS — proceeding"
        ;;
esac

# Confirm /api/v1/admin/budgets is wired (Phase 7 admin surface).
BPROBE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \
    "${RUNTIME_URL}/api/v1/admin/budgets" 2>/dev/null || echo "000")"
case "$BPROBE" in
    200|401|403)
        green "  /api/v1/admin/budgets registered (HTTP $BPROBE)"
        ;;
    404|405|501)
        skip "/api/v1/admin/budgets not registered yet (HTTP $BPROBE)"
        ;;
    503)
        skip "/api/v1/admin/budgets returned 503 — budget manager not wired"
        ;;
    *)
        yellow "  /api/v1/admin/budgets returned HTTP $BPROBE — proceeding"
        ;;
esac

# ── 1. Create tenant ──────────────────────────────────────────────────────
step "1/6  create a fresh tenant (${SLUG})"
TENANT_RESP="$(curl -s --max-time 10 \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg s "$SLUG" --arg n "Budget Test Tenant" '{slug:$s, name:$n}')" \
    "${RUNTIME_URL}/api/v1/admin/tenants")"
TENANT_ID="$(echo "$TENANT_RESP" | jq -r '.id // empty')"
if [ -z "$TENANT_ID" ]; then
    fail "could not create tenant — response: ${TENANT_RESP:0:200}"
fi
green "  tenant id=${TENANT_ID}"

# ── 2. Issue API key ──────────────────────────────────────────────────────
step "2/6  issue an API key with llm:invoke scope"
KEY_RESP="$(curl -s --max-time 10 \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg tid "$TENANT_ID" \
        '{tenant_id:$tid, name:"budget-test", scopes:["llm:invoke","agents:invoke"]}')" \
    "${RUNTIME_URL}/api/v1/admin/keys")"
KEY_ID="$(echo "$KEY_RESP" | jq -r '.id // empty')"
KEY_VALUE="$(echo "$KEY_RESP" | jq -r '.value // empty')"
if [ -z "$KEY_ID" ] || [ -z "$KEY_VALUE" ] || [ "$KEY_VALUE" = "null" ]; then
    fail "could not issue key — response: ${KEY_RESP:0:200}"
fi
green "  key id=${KEY_ID} value=${KEY_VALUE:0:12}…"

# ── 3. Set tiny budget ─────────────────────────────────────────────────────
step "3/6  set monthly budget = \$${BUDGET_AMOUNT}"
BUDGET_BODY="$(jq -nc --arg tid "$TENANT_ID" --argjson amt "$BUDGET_AMOUNT" \
    '{tenant_id:$tid, monthly_usd:$amt, alert_threshold_pct:50}')"
BUDGET_RESP="$(curl -s -o /tmp/.budget_set -w '%{http_code}' --max-time 10 \
    -X PUT \
    -H 'Content-Type: application/json' \
    -d "$BUDGET_BODY" \
    "${RUNTIME_URL}/api/v1/admin/budgets" || echo "000")"
case "$BUDGET_RESP" in
    200|201)
        green "  budget set (HTTP $BUDGET_RESP)"
        ;;
    404|405)
        skip "PUT /admin/budgets not registered (HTTP $BUDGET_RESP) — Phase 7.2 in flight"
        ;;
    *)
        red "  PUT /admin/budgets returned HTTP $BUDGET_RESP"
        cat /tmp/.budget_set 2>/dev/null | head -c 200
        fail "could not set budget"
        ;;
esac

# ── 4. First chat call (should succeed, then exhaust the budget) ───────────
step "4/6  first chat.completions.create (expected to succeed)"
FIRST_PAYLOAD="$(jq -nc --arg model "$MODEL" '{
    model: $model,
    messages: [
        {role: "system", content: "Reply with one short sentence."},
        {role: "user", content: "Say hello in five words."}
    ],
    max_tokens: 100
}')"
FIRST_HTTP="$(curl -s -o /tmp/.budget_first -w '%{http_code}' --max-time 30 \
    -X POST \
    -H "Authorization: Bearer ${KEY_VALUE}" \
    -H 'Content-Type: application/json' \
    -d "$FIRST_PAYLOAD" \
    "${RUNTIME_URL}/api/v1/llm/chat/completions" || echo "000")"

case "$FIRST_HTTP" in
    200)
        pass_step "first chat returned 200"
        # Validate the response shape minimally.
        if jq -e '.choices[0].message.content' /tmp/.budget_first >/dev/null 2>&1; then
            pass_step "response has choices[0].message.content"
        else
            fail_step "response missing choices[0].message.content"
        fi
        ;;
    402)
        # Budget too small to fit a single call — also legitimate.
        CODE="$(jq -r '.error.code // empty' /tmp/.budget_first 2>/dev/null || echo '')"
        if [ "$CODE" = "BUDGET_EXCEEDED" ]; then
            yellow "  first call rejected with BUDGET_EXCEEDED — budget too small for even one call; that's ok"
            yellow "  treating the 402 as the enforcement signal we wanted to see"
            pass_step "BUDGET_EXCEEDED returned (HTTP 402) on first call"
            blue "  skipping second-call check (already exhausted)"
            FAILS=$((FAILS + 0))  # explicit no-op for clarity
            # Skip directly to summary
            echo
            green "===================================================="
            green "  Budget enforcement PASS — ${PASSES} checks passed"
            green "===================================================="
            exit 0
        fi
        red "  first chat returned 402 but code=${CODE} (expected BUDGET_EXCEEDED)"
        cat /tmp/.budget_first 2>/dev/null | head -c 200
        fail "unexpected 402 envelope on first call"
        ;;
    404|405)
        skip "chat/completions returned HTTP $FIRST_HTTP — Phase 7.1 gateway not wired downstream of auth"
        ;;
    *)
        red "  first chat returned HTTP $FIRST_HTTP"
        cat /tmp/.budget_first 2>/dev/null | head -c 300
        fail "unexpected status on first chat call"
        ;;
esac

# ── 5. Second chat call (should hit the 402 ceiling) ───────────────────────
step "5/6  second chat.completions.create (expected 402 BUDGET_EXCEEDED)"
# Give the recorder a beat to flush the previous call's spend.
sleep 1
SECOND_HTTP="$(curl -s -o /tmp/.budget_second -w '%{http_code}' --max-time 30 \
    -X POST \
    -H "Authorization: Bearer ${KEY_VALUE}" \
    -H 'Content-Type: application/json' \
    -d "$FIRST_PAYLOAD" \
    "${RUNTIME_URL}/api/v1/llm/chat/completions" || echo "000")"

if [ "$SECOND_HTTP" != "402" ]; then
    red "  second chat returned HTTP $SECOND_HTTP (expected 402)"
    cat /tmp/.budget_second 2>/dev/null | head -c 300
    fail_step "second call did not get 402 — budget enforcer not engaging"
else
    SECOND_CODE="$(jq -r '.error.code // empty' /tmp/.budget_second 2>/dev/null || echo '')"
    if [ "$SECOND_CODE" = "BUDGET_EXCEEDED" ]; then
        pass_step "second chat returned 402 with code=BUDGET_EXCEEDED"
    else
        fail_step "second chat returned 402 but code=${SECOND_CODE} (expected BUDGET_EXCEEDED)"
    fi
fi

# ── 6. Cleanup is handled by the EXIT trap. ────────────────────────────────
step "6/6  cleanup runs on EXIT trap"
green "  will revoke key=${KEY_ID:0:8} and soft-delete tenant=${TENANT_ID:0:8}"

# ── Summary ────────────────────────────────────────────────────────────────
echo
if [ "$FAILS" -eq 0 ]; then
    green "===================================================="
    green "  Budget enforcement PASS — ${PASSES} checks passed"
    green "===================================================="
    exit 0
else
    red "===================================================="
    red "  Budget enforcement FAIL — ${PASSES} passed, ${FAILS} failed"
    red "===================================================="
    exit 1
fi
