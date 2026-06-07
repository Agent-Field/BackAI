#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 9 sandbox API.
#
# What this exercises:
#   1. Sign in as the operator via better-auth (cookie auth, same pattern
#      as test-db-studio.sh and test-memory.sh).
#   2. POST /api/v1/sandbox/run with a simple alpine echo command.
#      Asserts: exit_code=0, status=done, duration_s > 0, cpu_seconds >= 0.
#   3. GET /api/v1/sandbox/pool — assert the response shape matches
#      SandboxPoolStatsSchema (apps/dashboard/src/lib/api.ts) and that
#      total_runs_today >= 1.
#   4. GET /api/v1/sandbox/runs?limit=5 — verify the new run is in the list.
#   5. Run a deliberately-failing command (sh -c "exit 7") and assert
#      exit_code=7, status=failed.
#   6. Run a timeout case (sleep 60 with timeout_s=2) and assert
#      status=timeout.
#
# When the Phase 9.x sandbox endpoints are not yet wired or Docker is not
# reachable, the script SKIPs cleanly with exit 0. Markers:
#   - /api/v1/sandbox/run -> 404 or 503  => SKIP
#   - /api/v1/sandbox/run -> 502 OR error mentions "docker not available"
#     => SKIP (no docker socket reachable from the runtime)
#   - sign-in fails on the dashboard       => SKIP
#
# Usage:
#   ./scripts/test-sandbox.sh
#
# Env knobs:
#   AF_STACK_PORT           — runtime port  (default 38080)
#   AF_STACK_DASHBOARD_PORT — dashboard port (default 33000)
#   OP_EMAIL                — operator email (default operator@example.com)
#   OP_PASSWORD             — operator pwd   (default af-stack-demo-pwd)

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
    yellow "   (re-run once Phase 9.x sandbox endpoints + docker adapter are wired)"
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

COOKIE_JAR="$(mktemp -t af-sandbox-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR" /tmp/.sandbox_* 2>/dev/null || true' EXIT

PASSES=0
FAILS=0
pass_step() { green "  PASS  $1"; PASSES=$((PASSES + 1)); }
fail_step() { red   "  FAIL  $1"; FAILS=$((FAILS + 1)); }

# Detect a "docker not available" / 502 condition coming back from the
# runtime sandbox adapter. Used everywhere we POST /sandbox/run to keep
# the SKIP behaviour consistent.
is_docker_unavailable() {
    local http="$1" body_file="$2"
    case "$http" in
        502|503)
            return 0
            ;;
        500)
            # Some adapter wrappers return 500 with a typed message.
            if grep -qiE 'docker (not |un)available|connect: no such file|docker daemon' "$body_file" 2>/dev/null; then
                return 0
            fi
            ;;
    esac
    return 1
}

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
SIGNIN_HTTP="$(curl -s -o /tmp/.sandbox_signin -w '%{http_code}' \
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
        head -c 200 /tmp/.sandbox_signin 2>/dev/null || true
        fail "sign-in failed"
        ;;
esac

# Helper: cookie-bearing request via the dashboard proxy.
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
        --max-time 120 \
        -b "$COOKIE_JAR" \
        -X "$method" \
        "${extra[@]}" \
        "${DASH_URL}${path}" || echo "000"
}

# Probe the route so SKIP triggers cleanly when Phase 9.x isn't wired.
PROBE="$(curl_auth GET /api/v1/sandbox/pool /tmp/.sandbox_probe)"
case "$PROBE" in
    200|400|422)
        green "  /api/v1/sandbox/pool probe returned HTTP ${PROBE}"
        ;;
    404)
        skip "/api/v1/sandbox/pool returned HTTP 404 — Phase 9.x endpoints not wired yet"
        ;;
    503)
        # Either the module isn't enabled or no adapter is registered.
        # Both translate to "feature not available in this stack".
        skip "/api/v1/sandbox/pool returned HTTP 503 — sandbox module not active"
        ;;
    401|403)
        skip "/api/v1/sandbox/pool returned HTTP ${PROBE} — operator auth not accepted"
        ;;
    *)
        yellow "  /api/v1/sandbox/pool probe returned HTTP ${PROBE} — proceeding"
        ;;
esac

# ── 2. POST /api/v1/sandbox/run — happy path ──────────────────────────────
step "2/6  POST /api/v1/sandbox/run alpine echo (expect status=done exit_code=0)"
RUN_BODY="$(jq -nc '{
    image: "alpine",
    command: ["sh","-c","echo hello world; echo to-stderr >&2; exit 0"],
    timeout_s: 30
}')"
RUN_HTTP="$(curl_auth POST /api/v1/sandbox/run /tmp/.sandbox_run \
    -H 'Content-Type: application/json' \
    --data-binary "$RUN_BODY")"

case "$RUN_HTTP" in
    200|201)
        : # fall through
        ;;
    404)
        skip "POST /api/v1/sandbox/run returned 404 — endpoint not wired yet"
        ;;
    503)
        skip "POST /api/v1/sandbox/run returned 503 — sandbox module not active"
        ;;
    *)
        if is_docker_unavailable "$RUN_HTTP" /tmp/.sandbox_run; then
            skip "sandbox adapter cannot reach docker (HTTP ${RUN_HTTP}) — no docker socket from runtime"
        fi
        red "  POST /api/v1/sandbox/run returned HTTP ${RUN_HTTP}"
        head -c 400 /tmp/.sandbox_run 2>/dev/null || true
        fail_step "unexpected status from sandbox run"
        ;;
esac

if [ "$RUN_HTTP" = "200" ] || [ "$RUN_HTTP" = "201" ]; then
    RUN_ID="$(jq -r '.id // empty' /tmp/.sandbox_run 2>/dev/null || echo '')"
    EXIT_CODE="$(jq -r '.exit_code // empty' /tmp/.sandbox_run 2>/dev/null || echo '')"
    STATUS="$(jq -r '.status // empty' /tmp/.sandbox_run 2>/dev/null || echo '')"
    DURATION_S="$(jq -r '.duration_s // 0' /tmp/.sandbox_run 2>/dev/null || echo '0')"
    CPU_SECONDS="$(jq -r '.cpu_seconds // 0' /tmp/.sandbox_run 2>/dev/null || echo '0')"
    ADAPTER="$(jq -r '.adapter // empty' /tmp/.sandbox_run 2>/dev/null || echo '')"

    blue "  id=${RUN_ID} adapter=${ADAPTER} status=${STATUS} exit_code=${EXIT_CODE} duration_s=${DURATION_S} cpu_seconds=${CPU_SECONDS}"

    if [ -z "$RUN_ID" ]; then
        fail_step "sandbox run returned no id"
    else
        pass_step "sandbox run accepted (id=${RUN_ID})"
    fi

    if [ "$STATUS" = "done" ] && [ "$EXIT_CODE" = "0" ]; then
        pass_step "happy-path: status=done exit_code=0"
    else
        fail_step "happy-path expected status=done exit_code=0, got status=${STATUS} exit_code=${EXIT_CODE}"
    fi

    # duration_s > 0
    if awk "BEGIN { exit !(${DURATION_S:-0} > 0) }"; then
        pass_step "duration_s=${DURATION_S} > 0"
    else
        fail_step "duration_s=${DURATION_S} (expected > 0)"
    fi

    # cpu_seconds >= 0
    if awk "BEGIN { exit !(${CPU_SECONDS:-0} >= 0) }"; then
        pass_step "cpu_seconds=${CPU_SECONDS} >= 0"
    else
        fail_step "cpu_seconds=${CPU_SECONDS} (expected >= 0)"
    fi
fi

# ── 3. GET /api/v1/sandbox/pool — shape + total_runs_today >= 1 ──────────
step "3/6  GET /api/v1/sandbox/pool — assert SandboxPoolStatsSchema shape"
POOL_HTTP="$(curl_auth GET /api/v1/sandbox/pool /tmp/.sandbox_pool)"
if [ "$POOL_HTTP" != "200" ]; then
    if is_docker_unavailable "$POOL_HTTP" /tmp/.sandbox_pool; then
        skip "pool query returned HTTP ${POOL_HTTP} — adapter unhealthy"
    fi
    red "  GET /api/v1/sandbox/pool returned HTTP ${POOL_HTTP}"
    head -c 300 /tmp/.sandbox_pool 2>/dev/null || true
    fail_step "GET pool"
else
    # SandboxPoolStatsSchema:
    #   adapter, warm, active, queued, total_runs_today, cpu_seconds_today,
    #   cost_usd_today, capabilities { max_timeout_s, supports_gpu,
    #   supports_network, supports_mounts, cold_start_ms, adapter }
    SHAPE_OK="$(jq -e '
        (.adapter           | type == "string") and
        (.warm              | type == "number") and
        (.active            | type == "number") and
        (.queued            | type == "number") and
        (.total_runs_today  | type == "number") and
        (.cpu_seconds_today | type == "number") and
        (.cost_usd_today    | type == "number") and
        (.capabilities.max_timeout_s    | type == "number") and
        (.capabilities.supports_gpu     | type == "boolean") and
        (.capabilities.supports_network | type == "boolean") and
        (.capabilities.supports_mounts  | type == "boolean") and
        (.capabilities.cold_start_ms    | type == "number") and
        (.capabilities.adapter          | type == "string")
    ' /tmp/.sandbox_pool >/dev/null 2>&1 && echo yes || echo no)"

    if [ "$SHAPE_OK" = "yes" ]; then
        pass_step "/sandbox/pool matches SandboxPoolStatsSchema"
    else
        fail_step "/sandbox/pool response does not match SandboxPoolStatsSchema"
        head -c 400 /tmp/.sandbox_pool 2>/dev/null || true
    fi

    TOTAL_RUNS_TODAY="$(jq -r '.total_runs_today // 0' /tmp/.sandbox_pool 2>/dev/null || echo '0')"
    blue "  total_runs_today=${TOTAL_RUNS_TODAY}"
    if [ "${TOTAL_RUNS_TODAY:-0}" -ge 1 ] 2>/dev/null; then
        pass_step "total_runs_today=${TOTAL_RUNS_TODAY} >= 1"
    else
        fail_step "total_runs_today=${TOTAL_RUNS_TODAY} (expected >= 1 after the happy-path run)"
    fi
fi

# ── 4. GET /api/v1/sandbox/runs?limit=5 — verify our run is listed ───────
step "4/6  GET /api/v1/sandbox/runs?limit=5 — expect our run id in list"
LIST_HTTP="$(curl_auth GET '/api/v1/sandbox/runs?limit=5' /tmp/.sandbox_list)"
if [ "$LIST_HTTP" != "200" ]; then
    red "  GET /api/v1/sandbox/runs returned HTTP ${LIST_HTTP}"
    head -c 300 /tmp/.sandbox_list 2>/dev/null || true
    fail_step "GET runs list"
else
    LIST_COUNT="$(jq -r '.runs | length' /tmp/.sandbox_list 2>/dev/null || echo '0')"
    blue "  list returned ${LIST_COUNT} runs"
    if [ -n "${RUN_ID:-}" ]; then
        FOUND="$(jq -r --arg id "$RUN_ID" \
            '[.runs[] | select(.id == $id)] | length' /tmp/.sandbox_list 2>/dev/null || echo '0')"
        if [ "${FOUND:-0}" -ge 1 ]; then
            pass_step "happy-path run (${RUN_ID}) is in /sandbox/runs?limit=5"
        else
            fail_step "happy-path run (${RUN_ID}) not in the first 5 entries"
        fi
    else
        yellow "  skip: no RUN_ID captured from step 2"
    fi
fi

# ── 5. Failing command (exit 7) ──────────────────────────────────────────
step "5/6  POST /api/v1/sandbox/run sh -c 'exit 7' — expect status=failed exit_code=7"
FAIL_BODY="$(jq -nc '{
    image: "alpine",
    command: ["sh","-c","exit 7"]
}')"
FAIL_HTTP="$(curl_auth POST /api/v1/sandbox/run /tmp/.sandbox_fail \
    -H 'Content-Type: application/json' \
    --data-binary "$FAIL_BODY")"
if [ "$FAIL_HTTP" != "200" ] && [ "$FAIL_HTTP" != "201" ]; then
    if is_docker_unavailable "$FAIL_HTTP" /tmp/.sandbox_fail; then
        skip "failing-command run could not reach docker (HTTP ${FAIL_HTTP})"
    fi
    red "  POST /sandbox/run (fail case) returned HTTP ${FAIL_HTTP}"
    head -c 300 /tmp/.sandbox_fail 2>/dev/null || true
    fail_step "failing-command POST"
else
    F_STATUS="$(jq -r '.status // empty' /tmp/.sandbox_fail 2>/dev/null || echo '')"
    F_EXIT="$(jq -r '.exit_code // empty' /tmp/.sandbox_fail 2>/dev/null || echo '')"
    blue "  status=${F_STATUS} exit_code=${F_EXIT}"
    if [ "$F_STATUS" = "failed" ] && [ "$F_EXIT" = "7" ]; then
        pass_step "failing command: status=failed exit_code=7"
    else
        fail_step "failing command expected status=failed exit_code=7, got status=${F_STATUS} exit_code=${F_EXIT}"
    fi
fi

# ── 6. Timeout command (sleep 60, timeout_s=2) ───────────────────────────
step "6/6  POST /api/v1/sandbox/run sleep 60 timeout_s=2 — expect status=timeout"
TIMEOUT_BODY="$(jq -nc '{
    image: "alpine",
    command: ["sh","-c","sleep 60"],
    timeout_s: 2
}')"
TIMEOUT_HTTP="$(curl_auth POST /api/v1/sandbox/run /tmp/.sandbox_timeout \
    -H 'Content-Type: application/json' \
    --data-binary "$TIMEOUT_BODY")"
if [ "$TIMEOUT_HTTP" != "200" ] && [ "$TIMEOUT_HTTP" != "201" ]; then
    if is_docker_unavailable "$TIMEOUT_HTTP" /tmp/.sandbox_timeout; then
        skip "timeout-case run could not reach docker (HTTP ${TIMEOUT_HTTP})"
    fi
    red "  POST /sandbox/run (timeout case) returned HTTP ${TIMEOUT_HTTP}"
    head -c 300 /tmp/.sandbox_timeout 2>/dev/null || true
    fail_step "timeout-case POST"
else
    T_STATUS="$(jq -r '.status // empty' /tmp/.sandbox_timeout 2>/dev/null || echo '')"
    blue "  status=${T_STATUS}"
    if [ "$T_STATUS" = "timeout" ]; then
        pass_step "timeout command: status=timeout"
    else
        fail_step "timeout command expected status=timeout, got status=${T_STATUS}"
    fi
fi

# ── Summary ───────────────────────────────────────────────────────────────
echo
if [ "$FAILS" -eq 0 ]; then
    green "===================================================="
    green "  Sandbox PASS — ${PASSES} checks passed"
    green "===================================================="
    exit 0
else
    red "===================================================="
    red "  Sandbox FAIL — ${PASSES} passed, ${FAILS} failed"
    red "===================================================="
    exit 1
fi
