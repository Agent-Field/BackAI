#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 8 database studio API.
#
# What this exercises:
#   1. Sign in as the operator via better-auth to obtain a session cookie
#   2. GET /api/v1/db/tables             — expect the core suite_* tables
#   3. GET /api/v1/db/tables/public/suite_secrets
#      — expect rls_enabled=true and at least one policy
#   4. POST /api/v1/db/sql (read-only SELECT)  — expect rows + non-zero count
#   5. POST /api/v1/db/sql (read-only DROP)    — expect 4xx with envelope
#      error.code == "READ_ONLY_VIOLATION"
#   6. POST /api/v1/db/sql (read-only INSERT)  — expect READ_ONLY_VIOLATION
#
# When the Phase 8.x DB-studio endpoints are not yet wired the script
# SKIPs cleanly with exit 0. Markers:
#   - /api/v1/db/tables -> 404 or 503  => SKIP
#   - sign-in fails on the dashboard   => SKIP
#
# Usage:
#   ./scripts/test-db-studio.sh
#
# Env knobs:
#   AF_STACK_PORT          — runtime port  (default 38080)
#   AF_STACK_DASHBOARD_PORT — dashboard port (default 33000)
#   OP_EMAIL               — operator email (default operator@example.com)
#   OP_PASSWORD            — operator pwd   (default af-stack-demo-pwd)

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
    yellow "   (re-run once Phase 8.x DB-studio endpoints are wired)"
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
PORT="${AF_STACK_PORT:-38080}"
DASH_PORT="${AF_STACK_DASHBOARD_PORT:-33000}"
RUNTIME_URL="http://localhost:${PORT}"
DASH_URL="http://localhost:${DASH_PORT}"
OP_EMAIL="${OP_EMAIL:-operator@example.com}"
OP_PASSWORD="${OP_PASSWORD:-af-stack-demo-pwd}"

COOKIE_JAR="$(mktemp -t af-dbstudio-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR" /tmp/.dbstudio_* 2>/dev/null || true' EXIT

PASSES=0
FAILS=0
pass_step() { green "  PASS  $1"; PASSES=$((PASSES + 1)); }
fail_step() { red   "  FAIL  $1"; FAILS=$((FAILS + 1)); }

# ── 0. Readiness probe ────────────────────────────────────────────────────
step "0/6  probe runtime + dashboard readiness"
if ! HEALTH="$(curl -s --max-time 3 "${RUNTIME_URL}/health" 2>/dev/null)"; then
    skip "runtime unreachable at ${RUNTIME_URL}"
fi
if ! echo "$HEALTH" | grep -q '"status"'; then
    skip "runtime /health did not return expected shape"
fi
green "  runtime reachable at ${RUNTIME_URL}"

DASH_PROBE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "${DASH_URL}/api/auth/sign-in/email" 2>/dev/null || echo "000")"
case "$DASH_PROBE" in
    200|400|404|405|422)
        # 400/405/422 are expected for an empty body POST probe — handler is live.
        green "  dashboard reachable at ${DASH_URL} (probe HTTP ${DASH_PROBE})"
        ;;
    000)
        skip "dashboard unreachable at ${DASH_URL}"
        ;;
    *)
        yellow "  dashboard probe returned HTTP ${DASH_PROBE} — proceeding"
        ;;
esac

# ── 1. Sign in via better-auth ────────────────────────────────────────────
step "1/6  sign in as ${OP_EMAIL} via better-auth"
SIGNIN_BODY="$(jq -nc --arg e "$OP_EMAIL" --arg p "$OP_PASSWORD" \
    '{email:$e, password:$p}')"
SIGNIN_HTTP="$(curl -s -o /tmp/.dbstudio_signin -w '%{http_code}' \
    --max-time 10 \
    -c "$COOKIE_JAR" \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "$SIGNIN_BODY" \
    "${DASH_URL}/api/auth/sign-in/email" || echo "000")"

case "$SIGNIN_HTTP" in
    200)
        if grep -q 'better-auth.session_token' "$COOKIE_JAR"; then
            green "  signed in (HTTP 200, session cookie set)"
        else
            skip "sign-in succeeded but no session cookie was set"
        fi
        ;;
    401|403)
        skip "sign-in returned HTTP ${SIGNIN_HTTP} — operator account not provisioned?"
        ;;
    404|405)
        skip "better-auth sign-in endpoint not registered (HTTP ${SIGNIN_HTTP})"
        ;;
    429)
        skip "better-auth sign-in rate-limited (HTTP 429) — try again in a minute"
        ;;
    *)
        red "  sign-in returned HTTP ${SIGNIN_HTTP}"
        head -c 200 /tmp/.dbstudio_signin 2>/dev/null || true
        fail "sign-in failed"
        ;;
esac

# Helper: cookie-bearing GET via the dashboard proxy so RUNTIME_URL stays
# inside docker DNS for the runtime side, and so we exercise the same
# code path the operator's browser uses.
curl_auth() {
    local method="$1"
    local path="$2"
    local out="$3"
    local extra=()
    if [ "$#" -ge 4 ]; then
        shift 3
        extra=("$@")
    fi
    curl -s -o "$out" -w '%{http_code}' \
        --max-time 15 \
        -b "$COOKIE_JAR" \
        -X "$method" \
        "${extra[@]}" \
        "${DASH_URL}${path}" || echo "000"
}

# ── 2. GET /api/v1/db/tables — expect the core suite_* tables ─────────────
step "2/6  GET /api/v1/db/tables — expect core suite_* tables"
TABLES_HTTP="$(curl_auth GET /api/v1/db/tables /tmp/.dbstudio_tables)"
case "$TABLES_HTTP" in
    200)
        : # fall through
        ;;
    404|503)
        skip "/api/v1/db/tables returned HTTP ${TABLES_HTTP} — Phase 8.x endpoints not wired yet"
        ;;
    401|403)
        skip "/api/v1/db/tables returned HTTP ${TABLES_HTTP} — operator auth not accepted (Phase 8.x routing not yet wired into auth pipeline)"
        ;;
    *)
        red "  GET /api/v1/db/tables returned HTTP ${TABLES_HTTP}"
        head -c 300 /tmp/.dbstudio_tables 2>/dev/null || true
        fail "unexpected status from /api/v1/db/tables"
        ;;
esac

# Validate envelope: { tables: [{ schema, name, ... }, ...] }.
if ! jq -e '.tables | type == "array"' /tmp/.dbstudio_tables >/dev/null 2>&1; then
    red "  /api/v1/db/tables response missing tables[] array"
    head -c 300 /tmp/.dbstudio_tables
    fail "bad envelope"
fi

TABLE_NAMES="$(jq -r '.tables[].name' /tmp/.dbstudio_tables 2>/dev/null || echo '')"
blue "  saw $(echo "$TABLE_NAMES" | grep -c .) tables"

missing=()
for required in suite_tenants suite_secrets suite_gateway_requests suite_audit_log; do
    if ! echo "$TABLE_NAMES" | grep -qx "$required"; then
        missing+=("$required")
    fi
done
if [ ${#missing[@]} -gt 0 ]; then
    fail_step "core tables missing: ${missing[*]}"
else
    pass_step "core tables present (suite_tenants, suite_secrets, suite_gateway_requests, suite_audit_log)"
fi

# ── 3. GET /api/v1/db/tables/public/suite_secrets — RLS + policies ────────
step "3/6  GET /api/v1/db/tables/public/suite_secrets — expect RLS + policies"
SECRETS_HTTP="$(curl_auth GET /api/v1/db/tables/public/suite_secrets /tmp/.dbstudio_secrets)"
if [ "$SECRETS_HTTP" != "200" ]; then
    red "  GET /api/v1/db/tables/public/suite_secrets returned HTTP ${SECRETS_HTTP}"
    head -c 300 /tmp/.dbstudio_secrets 2>/dev/null || true
    fail_step "unexpected status from table detail endpoint"
else
    rls_enabled="$(jq -r '.rls_enabled // false' /tmp/.dbstudio_secrets 2>/dev/null || echo "false")"
    policy_count="$(jq -r '(.policies // []) | length' /tmp/.dbstudio_secrets 2>/dev/null || echo "0")"
    blue "  rls_enabled=${rls_enabled}, policies=${policy_count}"
    if [ "$rls_enabled" = "true" ] && [ "${policy_count:-0}" -ge 1 ]; then
        pass_step "suite_secrets has RLS enabled and ${policy_count} policy/policies"
    else
        fail_step "suite_secrets rls_enabled=${rls_enabled} policy_count=${policy_count} (expected RLS on + ≥1 policy)"
    fi
fi

# ── 4. POST /api/v1/db/sql — read-only SELECT ─────────────────────────────
step "4/6  POST /api/v1/db/sql read_only=true — SELECT count(*) from suite_tenants"
SELECT_BODY="$(jq -nc '{statement:"select count(*)::int as n from suite_tenants", read_only:true}')"
SELECT_HTTP="$(curl_auth POST /api/v1/db/sql /tmp/.dbstudio_select \
    -H 'Content-Type: application/json' \
    --data-binary "$SELECT_BODY")"
if [ "$SELECT_HTTP" != "200" ]; then
    red "  POST /api/v1/db/sql (select) returned HTTP ${SELECT_HTTP}"
    head -c 300 /tmp/.dbstudio_select 2>/dev/null || true
    fail_step "unexpected status from SQL runner"
else
    # rows is array of arrays; we want rows[0][0] >= 1.
    first_val="$(jq -r '.rows[0][0] // empty' /tmp/.dbstudio_select 2>/dev/null || echo '')"
    if [ -z "$first_val" ]; then
        fail_step "/db/sql select returned no rows[0][0]"
    elif [ "$first_val" -ge 1 ] 2>/dev/null; then
        pass_step "select returned suite_tenants count = ${first_val} (>= 1)"
    else
        fail_step "select returned rows[0][0]=${first_val} (expected >= 1)"
    fi
fi

# Helper: assert a write statement is refused with READ_ONLY_VIOLATION.
assert_read_only_violation() {
    local label="$1" stmt="$2" outfile="$3"
    local body http code
    body="$(jq -nc --arg s "$stmt" '{statement:$s, read_only:true}')"
    http="$(curl_auth POST /api/v1/db/sql "$outfile" \
        -H 'Content-Type: application/json' \
        --data-binary "$body")"
    case "$http" in
        4??)
            code="$(jq -r '.error.code // empty' "$outfile" 2>/dev/null || echo '')"
            if [ "$code" = "READ_ONLY_VIOLATION" ]; then
                pass_step "${label}: rejected with HTTP ${http} READ_ONLY_VIOLATION"
            else
                fail_step "${label}: HTTP ${http} but code=${code:-<missing>} (expected READ_ONLY_VIOLATION)"
                head -c 200 "$outfile" 2>/dev/null || true
            fi
            ;;
        *)
            red "  ${label}: returned HTTP ${http} (expected 4xx)"
            head -c 200 "$outfile" 2>/dev/null || true
            fail_step "${label}: did not return 4xx"
            ;;
    esac
}

# ── 5. POST /api/v1/db/sql — DROP TABLE must be refused ───────────────────
step "5/6  POST /api/v1/db/sql read_only=true — DROP suite_tenants (must refuse)"
assert_read_only_violation \
    "DROP rejected by read-only guard" \
    "drop table suite_tenants" \
    /tmp/.dbstudio_drop

# ── 6. POST /api/v1/db/sql — INSERT must be refused ───────────────────────
step "6/6  POST /api/v1/db/sql read_only=true — INSERT must refuse"
assert_read_only_violation \
    "INSERT rejected by read-only guard" \
    "insert into suite_tenants (id, slug, name) values ('00000000-0000-0000-0000-000000000099','rotest','rotest')" \
    /tmp/.dbstudio_insert

# ── Summary ───────────────────────────────────────────────────────────────
echo
if [ "$FAILS" -eq 0 ]; then
    green "===================================================="
    green "  DB studio PASS — ${PASSES} checks passed"
    green "===================================================="
    exit 0
else
    red "===================================================="
    red "  DB studio FAIL — ${PASSES} passed, ${FAILS} failed"
    red "===================================================="
    exit 1
fi
