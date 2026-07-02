#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# acceptance.sh — the release tripwire for the AF Stack hero journey.
#
# It drives the §2 flow end to end against a fresh `compose up`:
#
#   scaffold  ->  run  ->  (tenant + API key)  ->  submit a coding task
#             ->  a REAL pull request           ->  a metered cost row
#
# Design: every phase reports PASS, FAIL, or SKIP. The script exits non-zero
# ONLY when an *enforced* phase fails. Phases that need external credentials
# (a GH_TOKEN + a throwaway repo, an operator token) SKIP with a clear reason
# until those are provided — so the skeleton is green today and each phase
# "flips on" as the underlying capability + credentials land. It is fully
# green only when the entire hero flow is real; that is the release gate (R2).
#
# Usage:
#   ./scripts/acceptance.sh
#
# Env knobs:
#   AF_STACK_PORT        (default 8080)  runtime host port
#   ACCEPTANCE_TIMEOUT_S (default 120)   readiness timeout
#   WITH_SHIPWRIGHT      (default 1)     also bring up the shipwright overlay
#   OPERATOR_TOKEN       (optional)      bearer token for /api/v1/admin/*
#   GH_TOKEN             (optional)      enables the real-PR phase
#   ACCEPT_REPO          (optional)      throwaway repo url for the real-PR phase
#   KEEP_STACK           (default 0)     leave the stack up on exit (for debugging)

set -euo pipefail
cd "$(dirname "$0")/.."

# ── pretty output ───────────────────────────────────────────────────────────
green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

# ── phase bookkeeping ───────────────────────────────────────────────────────
declare -a RESULTS=()
ENFORCED_FAILS=0

pass() { RESULTS+=("PASS  $1"); green "  PASS  $1"; }
skip() { RESULTS+=("SKIP  $1 — $2"); yellow "  SKIP  $1 — $2"; }
fail() {
    RESULTS+=("FAIL  $1 — $2")
    red "  FAIL  $1 — $2"
    ENFORCED_FAILS=$((ENFORCED_FAILS + 1))
}

# ── config ──────────────────────────────────────────────────────────────────
PORT="${AF_STACK_PORT:-8080}"
RUNTIME_URL="http://localhost:${PORT}"
TIMEOUT="${ACCEPTANCE_TIMEOUT_S:-120}"
WITH_SHIPWRIGHT="${WITH_SHIPWRIGHT:-1}"
KEEP_STACK="${KEEP_STACK:-0}"
OPERATOR_TOKEN="${OPERATOR_TOKEN:-}"
GH_TOKEN="${GH_TOKEN:-}"
ACCEPT_REPO="${ACCEPT_REPO:-}"

COMPOSE=(docker compose -f docker-compose.yml)
if [ "$WITH_SHIPWRIGHT" = "1" ]; then
    COMPOSE+=(-f examples/02-shipwright/docker-compose.yml)
fi

need_cmd() { command -v "$1" >/dev/null 2>&1 || { red "required command not found: $1"; exit 2; }; }
need_cmd docker
need_cmd curl
need_cmd jq

teardown() {
    if [ "$KEEP_STACK" = "1" ]; then
        yellow "KEEP_STACK=1 — leaving the stack running"
        return
    fi
    step "Tear down"
    "${COMPOSE[@]}" down -v >/dev/null 2>&1 || true
}
trap teardown EXIT

# authed curl helpers -------------------------------------------------------
op_curl() { curl -s -H "Authorization: Bearer ${OPERATOR_TOKEN}" "$@"; }

# ─────────────────────────────────────────────────────────────────────────────
# Phase 1 — bring the stack up
# ─────────────────────────────────────────────────────────────────────────────
step "Phase 1/8  Bring up a fresh stack"
"${COMPOSE[@]}" down -v >/dev/null 2>&1 || true
if [ -x scripts/preflight.mjs ] || command -v node >/dev/null 2>&1; then
    node scripts/preflight.mjs 2>/dev/null || true
fi
if "${COMPOSE[@]}" up -d --build 2>&1 | tail -5; then
    pass "compose up"
else
    fail "compose up" "docker compose up failed"
    # No stack -> the rest is meaningless; summarise and bail.
    printf '\n%s\n' "${RESULTS[@]}"
    exit 1
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 2 — runtime becomes ready
# ─────────────────────────────────────────────────────────────────────────────
step "Phase 2/8  Wait for runtime /ready (timeout ${TIMEOUT}s)"
DEADLINE=$(($(date +%s) + TIMEOUT))
READY=""
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    if curl -s --max-time 3 "${RUNTIME_URL}/ready" | grep -q '"status":"ready"'; then
        READY=1
        break
    fi
    sleep 2
done
if [ -n "$READY" ]; then
    pass "runtime ready"
else
    fail "runtime ready" "not ready within ${TIMEOUT}s"
    "${COMPOSE[@]}" logs runtime --tail 40 || true
    printf '\n%s\n' "${RESULTS[@]}"
    exit 1
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 3 — multi-tenancy is ON (the scaffold turns it on)
# ─────────────────────────────────────────────────────────────────────────────
step "Phase 3/8  Multi-tenancy enabled"
MODULES="$(curl -s --max-time 5 "${RUNTIME_URL}/api/v1/modules" || echo '{}')"
if [ "$(echo "$MODULES" | jq -r '.multi_tenancy_enabled // false')" = "true" ]; then
    pass "multi-tenancy on"
else
    # Enforced: the hero flow is multi-tenant. A single-tenant stack is a real
    # miss, not a skip.
    fail "multi-tenancy on" "modules.multi_tenancy_enabled is not true"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 4 — the coding-agent scaffold produces the expected app
# ─────────────────────────────────────────────────────────────────────────────
step "Phase 4/8  Scaffold: af-stack init --template coding-agent"
SCAFFOLD_DIR="$(mktemp -d)"
CLI_BIN=""
if command -v af-stack >/dev/null 2>&1; then
    CLI_BIN="af-stack"
elif command -v go >/dev/null 2>&1; then
    if go build -o "${SCAFFOLD_DIR}/af-stack" ./services/cli/cmd/af-stack 2>/dev/null; then
        CLI_BIN="${SCAFFOLD_DIR}/af-stack"
    fi
fi
if [ -z "$CLI_BIN" ]; then
    skip "scaffold" "no af-stack binary and no go toolchain to build one"
else
    # `af-stack init` rebrands in place inside an AF Stack checkout (it looks
    # upward for package.json + apps/dashboard + apps/customer-app), taking the
    # project name via --name. Give it a clean checkout snapshot — tracked files
    # only, so it's fast and carries no .git/node_modules — and run init there.
    # The coding-agent template writes under <root>/apps/backend/agents/coding-agent/.
    FORK_DIR="${SCAFFOLD_DIR}/fork"
    mkdir -p "$FORK_DIR"
    if git archive HEAD | tar -x -C "$FORK_DIR" 2>/dev/null; then
        (cd "$FORK_DIR" && "$CLI_BIN" init --name acme-coder --template coding-agent >/dev/null 2>&1) || true
        AGENT_MAIN="${FORK_DIR}/apps/backend/agents/coding-agent/main.py"
        ENV_FILE="${FORK_DIR}/.env"
        if [ -f "$AGENT_MAIN" ] && grep -q 'node_id' "$AGENT_MAIN" 2>/dev/null; then
            if [ -f "$ENV_FILE" ] && grep -qi 'MULTI_TENANCY=true' "$ENV_FILE" 2>/dev/null; then
                pass "scaffold (coding-agent + multi-tenancy on)"
            else
                fail "scaffold" "coding-agent scaffolded but .env does not enable multi-tenancy"
            fi
        else
            fail "scaffold" "coding-agent main.py not produced at expected path"
        fi
    else
        skip "scaffold" "git archive unavailable (not a git checkout)"
    fi
fi
rm -rf "$SCAFFOLD_DIR"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 5 — the coding agent registers with AgentField
# ─────────────────────────────────────────────────────────────────────────────
step "Phase 5/8  Coding agent registered (shipwright.build)"
if [ "$WITH_SHIPWRIGHT" != "1" ]; then
    skip "agent registered" "shipwright overlay not requested (WITH_SHIPWRIGHT=0)"
else
    REG_DEADLINE=$(($(date +%s) + 45))
    AGENT_SEEN=""
    while [ "$(date +%s)" -lt "$REG_DEADLINE" ]; do
        if curl -s --max-time 3 "${RUNTIME_URL}/api/v1/agents" | grep -q 'shipwright'; then
            AGENT_SEEN=1
            break
        fi
        sleep 3
    done
    if [ -n "$AGENT_SEEN" ]; then
        pass "agent registered"
    else
        fail "agent registered" "shipwright not visible in /api/v1/agents after 45s"
    fi
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 6 — a tenant + a working API key
# ─────────────────────────────────────────────────────────────────────────────
# The hero flow mints these via the customer-app web signup. Scripting the
# better-auth session is a later flip; for now we provision through the
# operator admin API when an OPERATOR_TOKEN is available.
step "Phase 6/8  Provision a tenant + API key"
API_KEY=""
TENANT_ID=""
ADMIN_CODE="$(op_curl -o /dev/null -w '%{http_code}' --max-time 5 "${RUNTIME_URL}/api/v1/admin/tenants" || echo 000)"
if [ -z "$OPERATOR_TOKEN" ] || [ "$ADMIN_CODE" = "401" ] || [ "$ADMIN_CODE" = "403" ]; then
    skip "tenant + key" "admin API needs an OPERATOR_TOKEN (got HTTP ${ADMIN_CODE})"
elif [ "$ADMIN_CODE" = "000" ] || [ "$ADMIN_CODE" = "404" ]; then
    skip "tenant + key" "admin API not reachable (HTTP ${ADMIN_CODE})"
else
    SUFFIX="$(date +%s)-$RANDOM"
    TBODY="$(jq -nc --arg s "acme-${SUFFIX}" --arg n "Acme Coder" '{slug:$s,name:$n}')"
    TENANT_ID="$(op_curl -X POST -H 'Content-Type: application/json' -d "$TBODY" \
        "${RUNTIME_URL}/api/v1/admin/tenants" | jq -r '.id // empty')"
    if [ -n "$TENANT_ID" ]; then
        KBODY="$(jq -nc --arg t "$TENANT_ID" '{tenant_id:$t,name:"acceptance",scopes:["agents:invoke","secrets:read","secrets:write"]}')"
        API_KEY="$(op_curl -X POST -H 'Content-Type: application/json' -d "$KBODY" \
            "${RUNTIME_URL}/api/v1/admin/keys" | jq -r '.value // empty')"
    fi
    if [ -n "$API_KEY" ]; then
        pass "tenant + key (key minted, not MT_DISABLED)"
    else
        fail "tenant + key" "admin API reachable but no key value returned"
    fi
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 7 — submit a coding task and get a REAL pull request
# ─────────────────────────────────────────────────────────────────────────────
step "Phase 7/8  Submit a coding task -> real PR"
if [ -z "$API_KEY" ]; then
    skip "real PR" "no API key from phase 6"
elif [ -z "$GH_TOKEN" ] || [ -z "$ACCEPT_REPO" ]; then
    skip "real PR" "set GH_TOKEN + ACCEPT_REPO (a throwaway repo) to exercise this"
else
    RBODY="$(jq -nc --arg r "$ACCEPT_REPO" \
        '{title:"acceptance: add a hello file",description:"Create ACCEPTANCE.md at the repo root.",repo_url:$r}')"
    CREATE="$(curl -s -X POST -H "Authorization: Bearer ${API_KEY}" \
        -H 'Content-Type: application/json' -d "$RBODY" \
        "${RUNTIME_URL}/api/v1/shipwright/tasks")"
    TASK_ID="$(echo "$CREATE" | jq -r '.task.id // empty')"
    if [ -z "$TASK_ID" ]; then
        fail "real PR" "task not created: $(echo "$CREATE" | head -c 200)"
    else
        PR_URL=""
        PR_DEADLINE=$(($(date +%s) + 600)) # real builds take minutes
        while [ "$(date +%s)" -lt "$PR_DEADLINE" ]; do
            DETAIL="$(curl -s -H "Authorization: Bearer ${API_KEY}" \
                "${RUNTIME_URL}/api/v1/shipwright/tasks/${TASK_ID}")"
            ST="$(echo "$DETAIL" | jq -r '.task.status // empty')"
            PR_URL="$(echo "$DETAIL" | jq -r '.task.ref // .patches[0].diff_url // empty')"
            case "$ST" in
                completed|succeeded) break ;;
                failed|cancelled) break ;;
            esac
            sleep 10
        done
        if echo "$PR_URL" | grep -qE 'github\.com/.+/(pull|compare)/'; then
            pass "real PR ($PR_URL)"
        else
            fail "real PR" "no github PR url after build (status=${ST:-?})"
        fi
    fi
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 8 — usage is metered to a per-tenant cost row
# ─────────────────────────────────────────────────────────────────────────────
step "Phase 8/8  Metered cost row for the tenant"
if [ -z "$TENANT_ID" ]; then
    skip "metered cost" "no tenant provisioned in phase 6"
elif [ -z "$API_KEY" ] || [ -z "$GH_TOKEN" ]; then
    skip "metered cost" "no real run to meter (needs the phase-7 build)"
else
    COST="$(op_curl --max-time 5 "${RUNTIME_URL}/api/v1/cost?tenant=${TENANT_ID}" || echo '{}')"
    ROWS="$(echo "$COST" | jq -r '[.. | objects | select(has("cost_usd") or has("total_usd"))] | length' 2>/dev/null || echo 0)"
    if [ "${ROWS:-0}" -gt 0 ]; then
        pass "metered cost (${ROWS} row(s))"
    else
        fail "metered cost" "no cost row found for tenant ${TENANT_ID}"
    fi
fi

# ── summary ──────────────────────────────────────────────────────────────────
step "Acceptance summary"
for line in "${RESULTS[@]}"; do
    case "$line" in
        PASS*) green "  $line" ;;
        SKIP*) yellow "  $line" ;;
        FAIL*) red "  $line" ;;
    esac
done

echo
if [ "$ENFORCED_FAILS" -gt 0 ]; then
    red "ACCEPTANCE FAILED — ${ENFORCED_FAILS} enforced phase(s) failed."
    exit 1
fi
SKIPS="$(printf '%s\n' "${RESULTS[@]}" | grep -c '^SKIP' || true)"
if [ "${SKIPS:-0}" -gt 0 ]; then
    yellow "ACCEPTANCE PASSED with ${SKIPS} skipped phase(s) — not yet the full release gate."
    yellow "Provide OPERATOR_TOKEN + GH_TOKEN + ACCEPT_REPO to flip the remaining phases on."
    exit 0
fi
green "ACCEPTANCE PASSED — the whole hero flow is real. Release tripwire green."
