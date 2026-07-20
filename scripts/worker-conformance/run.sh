#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Worker-protocol conformance runner (PRD R3). Starts a reference pull-worker
# (Python or TypeScript), enqueues every vector in spec.json, and asserts each
# job reaches its expected terminal state via the jobs API. Deterministic +
# self-reporting: prints a JSON summary and exits non-zero on any FAIL/ERROR.
#
# Env:
#   BASE_URL   runtime base URL         (default http://localhost:8080)
#   API_KEY    tenant key w/ jobs:work  (required)
#   WORKER     py | ts                  (default py)
#
# PREREQUISITE: each vector `kind` in spec.json must be registered as a REMOTE
# job definition on the target stack — the runtime rejects `enqueue` of unknown
# kinds. See README.md.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
SPEC="${SPEC:-$HERE/spec.json}"
BASE_URL="${BASE_URL:-http://localhost:8080}"
BASE_URL="${BASE_URL%/}"
API_KEY="${API_KEY:-${AF_STACK_API_KEY:-}}"
WORKER="${WORKER:-py}"

die() {
  echo "worker-conformance: $*" >&2
  exit 2
}

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v jq >/dev/null 2>&1 || die "jq is required"
[ -f "$SPEC" ] || die "spec not found: $SPEC"
[ -n "$API_KEY" ] || die "API_KEY is required (a tenant key with the jobs:work scope)"

POLL_TIMEOUT="$(jq -r '.poll_timeout_seconds // 45' "$SPEC")"
WORKER_LOG="$(mktemp)"

# ── start the reference worker ────────────────────────────────────────────
start_worker() {
  if [ "$WORKER" = "ts" ]; then
    echo "==> building @af-stack/sdk for the TS reference worker" >&2
    (cd "$REPO_ROOT" && pnpm --filter @af-stack/sdk build >/dev/null) || die "SDK build failed"
    local runner
    if command -v tsx >/dev/null 2>&1; then runner="tsx"; else runner="node --experimental-strip-types"; fi
    SPEC="$SPEC" BASE_URL="$BASE_URL" API_KEY="$API_KEY" \
      $runner "$HERE/ref_worker.ts" >"$WORKER_LOG" 2>&1 &
  else
    SPEC="$SPEC" BASE_URL="$BASE_URL" API_KEY="$API_KEY" \
      python3 "$HERE/ref_worker.py" >"$WORKER_LOG" 2>&1 &
  fi
  WORKER_PID=$!
}

cleanup() {
  if [ -n "${WORKER_PID:-}" ]; then
    kill "$WORKER_PID" >/dev/null 2>&1
    wait "$WORKER_PID" 2>/dev/null
  fi
  rm -f "$WORKER_LOG"
}
trap cleanup EXIT

start_worker
sleep 2
if ! kill -0 "$WORKER_PID" >/dev/null 2>&1; then
  echo "worker-conformance: reference worker exited early; log:" >&2
  cat "$WORKER_LOG" >&2
  die "reference worker failed to start"
fi

# ── enqueue every vector ──────────────────────────────────────────────────
declare -a NAMES KINDS EXPECTS IDS
TERMINAL_RE='^(completed|discarded|cancelled)$'

enqueue() {
  local kind="$1" payload="$2" max_attempts="$3"
  local body
  body="$(jq -cn --arg name "$kind" --argjson args "$payload" --argjson ma "$max_attempts" \
    '{name: $name, args: $args, max_attempts: $ma}')"
  curl -sS -w '\n%{http_code}' -X POST "$BASE_URL/api/v1/jobs" \
    -H "authorization: Bearer $API_KEY" \
    -H "content-type: application/json" \
    -d "$body"
}

while IFS= read -r vec; do
  name="$(jq -r '.name' <<<"$vec")"
  kind="$(jq -r '.kind' <<<"$vec")"
  expect="$(jq -r '.expect_state' <<<"$vec")"
  payload="$(jq -c '.payload // {}' <<<"$vec")"
  max_attempts="$(jq -r '.max_attempts // 1' <<<"$vec")"

  resp="$(enqueue "$kind" "$payload" "$max_attempts")"
  code="$(tail -n1 <<<"$resp")"
  json="$(sed '$d' <<<"$resp")"

  NAMES+=("$name"); KINDS+=("$kind"); EXPECTS+=("$expect")
  if [ "$code" = "201" ] || [ "$code" = "200" ]; then
    IDS+=("$(jq -r '.id // empty' <<<"$json")")
  else
    ecode="$(jq -r '.error.code // "HTTP_'"$code"'"' <<<"$json" 2>/dev/null || echo "HTTP_$code")"
    IDS+=("ERR:$ecode")
  fi
done < <(jq -c '.vectors[]' "$SPEC")

# ── poll each job to a terminal state ─────────────────────────────────────
job_state() {
  curl -sS "$BASE_URL/api/v1/jobs/$1" -H "authorization: Bearer $API_KEY" \
    | jq -r '.state // "unknown"'
}

results="[]"
pass=0; fail=0; error=0
deadline=$(( $(date +%s) + POLL_TIMEOUT ))

for i in "${!NAMES[@]}"; do
  name="${NAMES[$i]}"; kind="${KINDS[$i]}"; expect="${EXPECTS[$i]}"; id="${IDS[$i]}"
  status="fail"; got=""; detail=""

  if [[ "$id" == ERR:* ]]; then
    status="error"; got="${id#ERR:}"
    detail="enqueue rejected — is kind '$kind' registered as a remote job definition?"
    error=$((error+1))
  else
    while :; do
      got="$(job_state "$id")"
      if [[ "$got" =~ $TERMINAL_RE ]]; then break; fi
      if [ "$(date +%s)" -ge "$deadline" ]; then detail="timed out (last state: $got)"; break; fi
      sleep 1
    done
    if [ "$got" = "$expect" ]; then status="pass"; pass=$((pass+1))
    else status="fail"; fail=$((fail+1)); [ -z "$detail" ] && detail="want=$expect got=$got"; fi
  fi

  results="$(jq -c \
    --arg name "$name" --arg kind "$kind" --arg id "$id" \
    --arg expect "$expect" --arg got "$got" --arg status "$status" --arg detail "$detail" \
    '. += [{name:$name, kind:$kind, job_id:$id, expect:$expect, got:$got, status:$status, detail:$detail}]' \
    <<<"$results")"
done

jq -n --arg worker "$WORKER" --argjson pass "$pass" --argjson fail "$fail" \
  --argjson error "$error" --argjson results "$results" \
  '{worker:$worker, pass:$pass, fail:$fail, error:$error, results:$results}'

if [ "$fail" -gt 0 ] || [ "$error" -gt 0 ]; then
  echo "worker-conformance: FAILED (fail=$fail error=$error). Worker log:" >&2
  cat "$WORKER_LOG" >&2
  exit 1
fi
echo "worker-conformance: OK ($pass vectors passed)" >&2
exit 0
