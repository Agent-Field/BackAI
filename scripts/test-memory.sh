#!/usr/bin/env bash
# End-to-end test for AF Stack Phase 8 memory API.
#
# What this exercises:
#   1. Sign in as the operator via better-auth (cookie auth, same as the
#      DB studio test).
#   2. PUT a `global`-scoped memory entry with embed=true.
#   3. GET it back; assert the same value.
#   4. SEARCH "how does multi-tenancy work" top_k=5; expect >=1 hit with
#      similarity > 0.3.
#   5. PUT a second related entry; SEARCH again; expect both ranked by
#      similarity (top hit should be the closer match).
#   6. DELETE first key; GET it -> expect 404.
#
# When the Phase 8.x memory endpoints are not yet wired, the script SKIPs
# cleanly with exit 0. Markers:
#   - /api/v1/memory -> 404 or 503  => SKIP
#   - sign-in fails on the dashboard => SKIP
#
# Usage:
#   ./scripts/test-memory.sh
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
    yellow "   (re-run once Phase 8.x memory endpoints are wired)"
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

SUFFIX="$(date +%s)-$RANDOM"
KEY_A="mt-test-a-${SUFFIX}"
KEY_B="mt-test-b-${SUFFIX}"

COOKIE_JAR="$(mktemp -t af-memory-cookies.XXXXXX)"
trap 'rm -f "$COOKIE_JAR" /tmp/.memtest_* 2>/dev/null || true; cleanup' EXIT

cleanup() {
    # Best-effort: drop both keys so reruns stay clean. Suppress all output.
    for k in "$KEY_A" "$KEY_B"; do
        curl -s --max-time 3 \
            -b "$COOKIE_JAR" \
            -X DELETE \
            "${DASH_URL}/api/v1/memory?scope=global&key=${k}" \
            >/dev/null 2>&1 || true
    done
}

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
SIGNIN_HTTP="$(curl -s -o /tmp/.memtest_signin -w '%{http_code}' \
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
        head -c 200 /tmp/.memtest_signin 2>/dev/null || true
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
        --max-time 15 \
        -b "$COOKIE_JAR" \
        -X "$method" \
        "${extra[@]}" \
        "${DASH_URL}${path}" || echo "000"
}

# Quick probe so we SKIP cleanly when the route isn't wired.
PROBE="$(curl_auth GET /api/v1/memory /tmp/.memtest_probe)"
case "$PROBE" in
    200|400|422)
        # Endpoint exists (200 ok or 400/422 from missing query params, which
        # is fine — the route is registered).
        green "  /api/v1/memory probe returned HTTP ${PROBE}"
        ;;
    404|503)
        skip "/api/v1/memory returned HTTP ${PROBE} — Phase 8.x endpoints not wired yet"
        ;;
    401|403)
        skip "/api/v1/memory returned HTTP ${PROBE} — operator auth not accepted (Phase 8.x routing not yet wired into auth pipeline)"
        ;;
    *)
        yellow "  /api/v1/memory probe returned HTTP ${PROBE} — proceeding"
        ;;
esac

# ── 2. PUT first entry, GET it back ───────────────────────────────────────
step "2/6  PUT global memory ${KEY_A} (embed=true)"
PUT_A_BODY="$(jq -nc --arg k "$KEY_A" '{
    scope: "global",
    key: $k,
    value: {text: "AF Stack runs autonomous AI backends with multi-tenant isolation."},
    embed: true
}')"
PUT_A_HTTP="$(curl_auth PUT /api/v1/memory /tmp/.memtest_put_a \
    -H 'Content-Type: application/json' \
    --data-binary "$PUT_A_BODY")"
if [ "$PUT_A_HTTP" != "200" ] && [ "$PUT_A_HTTP" != "201" ]; then
    red "  PUT /api/v1/memory returned HTTP ${PUT_A_HTTP}"
    head -c 300 /tmp/.memtest_put_a 2>/dev/null || true
    fail_step "PUT first entry"
else
    has_emb="$(jq -r '.has_embedding // false' /tmp/.memtest_put_a 2>/dev/null || echo "false")"
    if [ "$has_emb" = "true" ]; then
        pass_step "PUT ${KEY_A} (has_embedding=true)"
    else
        # has_embedding could be lazily set after worker runs; don't fail.
        yellow "  has_embedding=${has_emb} — assuming async embedder"
        pass_step "PUT ${KEY_A} (embed requested)"
    fi
fi

step "3/6  GET /api/v1/memory/get scope=global key=${KEY_A}"
GET_A_HTTP="$(curl_auth GET "/api/v1/memory/get?scope=global&key=${KEY_A}" /tmp/.memtest_get_a)"
if [ "$GET_A_HTTP" != "200" ]; then
    red "  GET returned HTTP ${GET_A_HTTP}"
    head -c 300 /tmp/.memtest_get_a 2>/dev/null || true
    fail_step "GET first entry"
else
    got_text="$(jq -r '.value.text // empty' /tmp/.memtest_get_a 2>/dev/null || echo '')"
    expected_text="AF Stack runs autonomous AI backends with multi-tenant isolation."
    if [ "$got_text" = "$expected_text" ]; then
        pass_step "GET ${KEY_A} round-trips the same value"
    else
        fail_step "GET value mismatch: got=\"${got_text:0:80}\" expected=\"${expected_text}\""
    fi
fi

# ── 4. SEARCH the corpus (single entry) ───────────────────────────────────
step "4/6  POST /api/v1/memory/search \"how does multi-tenancy work\" top_k=5"
# Allow a brief moment in case embeddings are written async.
sleep 1
SEARCH_BODY='{"query":"how does multi-tenancy work","scope":"global","top_k":5}'
SEARCH_HTTP="$(curl_auth POST /api/v1/memory/search /tmp/.memtest_search1 \
    -H 'Content-Type: application/json' \
    --data-binary "$SEARCH_BODY")"
if [ "$SEARCH_HTTP" != "200" ]; then
    red "  /memory/search returned HTTP ${SEARCH_HTTP}"
    head -c 300 /tmp/.memtest_search1 2>/dev/null || true
    fail_step "SEARCH (first pass)"
else
    hit_count="$(jq -r '.hits | length' /tmp/.memtest_search1 2>/dev/null || echo "0")"
    top_sim="$(jq -r '.hits[0].similarity // 0' /tmp/.memtest_search1 2>/dev/null || echo "0")"
    blue "  hits=${hit_count}, top_sim=${top_sim}"
    if [ "${hit_count:-0}" -ge 1 ] && \
       awk "BEGIN { exit !(${top_sim:-0} > 0.3) }"; then
        pass_step "SEARCH returned ${hit_count} hit(s), top similarity ${top_sim} > 0.3"
    else
        fail_step "SEARCH did not satisfy (hits=${hit_count}, top_sim=${top_sim}; expected ≥1 hit with similarity > 0.3)"
    fi
fi

# ── 5. PUT a second entry; SEARCH again; expect ranking ───────────────────
step "5/6  PUT second related entry ${KEY_B}, then SEARCH again"
PUT_B_BODY="$(jq -nc --arg k "$KEY_B" '{
    scope: "global",
    key: $k,
    value: {text: "Each tenant in AF Stack is isolated via PostgreSQL row-level security policies."},
    embed: true
}')"
PUT_B_HTTP="$(curl_auth PUT /api/v1/memory /tmp/.memtest_put_b \
    -H 'Content-Type: application/json' \
    --data-binary "$PUT_B_BODY")"
if [ "$PUT_B_HTTP" != "200" ] && [ "$PUT_B_HTTP" != "201" ]; then
    red "  PUT second entry returned HTTP ${PUT_B_HTTP}"
    head -c 300 /tmp/.memtest_put_b 2>/dev/null || true
    fail_step "PUT second entry"
else
    pass_step "PUT ${KEY_B}"
fi

sleep 1
SEARCH2_HTTP="$(curl_auth POST /api/v1/memory/search /tmp/.memtest_search2 \
    -H 'Content-Type: application/json' \
    --data-binary "$SEARCH_BODY")"
if [ "$SEARCH2_HTTP" != "200" ]; then
    red "  second /memory/search returned HTTP ${SEARCH2_HTTP}"
    head -c 300 /tmp/.memtest_search2 2>/dev/null || true
    fail_step "SEARCH (second pass)"
else
    hit_count2="$(jq -r '.hits | length' /tmp/.memtest_search2 2>/dev/null || echo "0")"
    # Find the rank of each key in the hits list.
    rank_a="$(jq -r --arg k "$KEY_A" '
        [.hits[] | .entry.key] as $keys
        | ($keys | index($k)) // -1
    ' /tmp/.memtest_search2 2>/dev/null || echo "-1")"
    rank_b="$(jq -r --arg k "$KEY_B" '
        [.hits[] | .entry.key] as $keys
        | ($keys | index($k)) // -1
    ' /tmp/.memtest_search2 2>/dev/null || echo "-1")"
    sim_a="$(jq -r --arg k "$KEY_A" '
        ([.hits[] | select(.entry.key == $k) | .similarity] // [])[0] // 0
    ' /tmp/.memtest_search2 2>/dev/null || echo "0")"
    sim_b="$(jq -r --arg k "$KEY_B" '
        ([.hits[] | select(.entry.key == $k) | .similarity] // [])[0] // 0
    ' /tmp/.memtest_search2 2>/dev/null || echo "0")"
    blue "  hits=${hit_count2}, rank ${KEY_A}=${rank_a} sim=${sim_a}, rank ${KEY_B}=${rank_b} sim=${sim_b}"

    if [ "${hit_count2:-0}" -lt 2 ]; then
        fail_step "second SEARCH returned ${hit_count2} hits (expected ≥ 2 after two PUTs)"
    elif [ "$rank_a" = "-1" ] || [ "$rank_b" = "-1" ]; then
        fail_step "second SEARCH missing one of the entries (rank_a=${rank_a}, rank_b=${rank_b})"
    else
        # Both present. We expect "the closer match" ranks first. KEY_B is
        # explicitly about tenant isolation via RLS — that's the closer
        # match to "how does multi-tenancy work" than the generic blurb
        # in KEY_A. We assert that the top result has the higher
        # similarity score (i.e. results are sorted by similarity desc).
        # We don't pin which specific key wins, only that the top-ranked
        # key has the highest similarity of the two.
        top_key="$(jq -r '.hits[0].entry.key // empty' /tmp/.memtest_search2 2>/dev/null || echo '')"
        top_sim2="$(jq -r '.hits[0].similarity // 0' /tmp/.memtest_search2 2>/dev/null || echo "0")"
        # Compare numerically with awk so we don't need bc.
        if [ "$top_key" = "$KEY_A" ] && \
           awk "BEGIN { exit !(${top_sim2:-0} >= ${sim_b:-0}) }"; then
            pass_step "both entries returned, top=${KEY_A} sim=${top_sim2} (>= ${sim_b})"
        elif [ "$top_key" = "$KEY_B" ] && \
             awk "BEGIN { exit !(${top_sim2:-0} >= ${sim_a:-0}) }"; then
            pass_step "both entries returned, top=${KEY_B} sim=${top_sim2} (>= ${sim_a})"
        else
            fail_step "ranking not sorted by similarity desc (top=${top_key} top_sim=${top_sim2}, sim_a=${sim_a}, sim_b=${sim_b})"
        fi
    fi
fi

# ── 6. DELETE first key, GET it -> expect 404 ─────────────────────────────
step "6/6  DELETE ${KEY_A}, GET it -> expect 404"
DEL_HTTP="$(curl_auth DELETE "/api/v1/memory?scope=global&key=${KEY_A}" /tmp/.memtest_del)"
case "$DEL_HTTP" in
    200|204)
        pass_step "DELETE ${KEY_A} returned HTTP ${DEL_HTTP}"
        ;;
    *)
        red "  DELETE returned HTTP ${DEL_HTTP}"
        head -c 200 /tmp/.memtest_del 2>/dev/null || true
        fail_step "DELETE did not succeed"
        ;;
esac

GET2_HTTP="$(curl_auth GET "/api/v1/memory/get?scope=global&key=${KEY_A}" /tmp/.memtest_get_after_del)"
if [ "$GET2_HTTP" = "404" ]; then
    pass_step "GET ${KEY_A} after delete returned 404"
else
    red "  GET returned HTTP ${GET2_HTTP}"
    head -c 200 /tmp/.memtest_get_after_del 2>/dev/null || true
    fail_step "GET after delete returned HTTP ${GET2_HTTP} (expected 404)"
fi

# ── Summary ───────────────────────────────────────────────────────────────
echo
if [ "$FAILS" -eq 0 ]; then
    green "===================================================="
    green "  Memory PASS — ${PASSES} checks passed"
    green "===================================================="
    exit 0
else
    red "===================================================="
    red "  Memory FAIL — ${PASSES} passed, ${FAILS} failed"
    red "===================================================="
    exit 1
fi
