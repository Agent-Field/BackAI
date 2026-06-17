#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# demo-traffic.sh — drive real traffic against the backai runtime so the
# Home page (and every other admin surface) shows live data instead of
# zeros. Used throughout UI development to verify that what we render
# tracks what the backend actually emits.
#
# All requests go through the dashboard at localhost:DASHBOARD_PORT,
# which proxies to the runtime. This matches the same auth path the
# browser uses, so anything that works for this script will work for the
# real UI.
#
# Subcommands:
#   status         Print current Home tile values from /api/v1/home/overview.
#   events [N]     Print the latest N events from /api/v1/admin/events.
#   services       Print backing services and their status.
#   setup          Sign in + issue an LLM API key, cache both locally.
#   llm [PROMPT]   POST a chat completion -> req/min, cost, sparklines.
#   fail           Hit a bogus execute endpoint -> failed runs 24h.
#   job            Enqueue a background job -> queue depth + live runs.
#   webhook        Trigger a test webhook delivery -> activity feed.
#   activity [A]   Log a customer-facing activity entry A (default tenant.created).
#   budget [USD]   Set the default tenant budget to USD (default 5).
#   all            Run one of each write subcommand so every tile ticks.
#   loop [SECS]    Run `all` every SECS seconds (default 15) until Ctrl-C.
#   login          Force re-authentication and refresh the cached cookie.
#   reset          Wipe cached cookie + key files.
#
# Configuration (override via env):
#   RUNTIME_URL    Default http://localhost:8080 — direct runtime, used for
#                  the read-only /home/overview + /admin/events + /admin/services.
#   DASHBOARD_URL  Default http://localhost:33000 — dashboard proxy + auth.
#   OPERATOR_EMAIL Default operator@af-stack.local (from .env).
#   OPERATOR_PASSWORD Default changeme123 (from .env).
#   LLM_MODEL      Default qwen/qwen-plus — change to any LiteLLM-routable model.
#
# Cache files:
#   /tmp/backai-demo-cookie.txt   better-auth session cookie jar
#   /tmp/backai-demo-apikey.txt   Bearer token for LLM calls
#
# Examples:
#   ./scripts/demo-traffic.sh setup
#   ./scripts/demo-traffic.sh llm "Say hello"
#   ./scripts/demo-traffic.sh loop 10
#   watch -n 5 './scripts/demo-traffic.sh status'

set -euo pipefail

RUNTIME_URL="${RUNTIME_URL:-http://localhost:8080}"
DASHBOARD_URL="${DASHBOARD_URL:-http://localhost:33000}"
OPERATOR_EMAIL="${OPERATOR_EMAIL:-operator@af-stack.local}"
OPERATOR_PASSWORD="${OPERATOR_PASSWORD:-changeme123}"
LLM_MODEL="${LLM_MODEL:-openrouter/qwen/qwen-2.5-72b-instruct}"

COOKIE_JAR="/tmp/backai-demo-cookie.txt"
APIKEY_FILE="/tmp/backai-demo-apikey.txt"

# Pretty colours when stdout is a tty.
if [[ -t 1 ]]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'
  RED=$'\033[31m'; CYAN=$'\033[36m'; RESET=$'\033[0m'
else
  BOLD=""; DIM=""; GREEN=""; YELLOW=""; RED=""; CYAN=""; RESET=""
fi

log()  { printf '%s%s%s %s\n' "$CYAN" "[demo]" "$RESET" "$*" >&2; }
ok()   { printf '%s%s%s %s\n' "$GREEN" "[ok]"   "$RESET" "$*" >&2; }
warn() { printf '%s%s%s %s\n' "$YELLOW" "[warn]" "$RESET" "$*" >&2; }
err()  { printf '%s%s%s %s\n' "$RED"    "[err]"  "$RESET" "$*" >&2; }

# ─── Auth helpers ──────────────────────────────────────────────────────────

# is_signed_in: returns 0 when the cached cookie still resolves to a
# session at the dashboard. Cookie expiry is roughly 7 days, but we check
# every time to keep the demo idempotent.
is_signed_in() {
  [[ -f "$COOKIE_JAR" ]] || return 1
  local code
  code=$(/usr/bin/curl -sS -b "$COOKIE_JAR" -o /dev/null -w '%{http_code}' \
    "${DASHBOARD_URL}/api/auth/get-session" || echo "000")
  [[ "$code" == "200" ]]
}

sign_in() {
  log "signing in as $OPERATOR_EMAIL"
  : > "$COOKIE_JAR"
  local body
  body=$(/usr/bin/curl -sS -c "$COOKIE_JAR" -b "$COOKIE_JAR" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$OPERATOR_EMAIL\",\"password\":\"$OPERATOR_PASSWORD\"}" \
    "${DASHBOARD_URL}/api/auth/sign-in/email")
  if ! echo "$body" | grep -q '"token"\|"user"'; then
    err "sign-in failed: $body"
    return 1
  fi
  ok "session cached at $COOKIE_JAR"
}

ensure_session() {
  if is_signed_in; then return 0; fi
  sign_in
}

# Issue a tenant-scoped API key for LLM calls. The runtime's gateway
# requires `Authorization: Bearer af_…` for /api/v1/llm/* even in the
# default-tenant single-tenant mode.
ensure_apikey() {
  if [[ -s "$APIKEY_FILE" ]]; then return 0; fi
  ensure_session
  log "issuing demo api key"
  local body code
  body=$(/usr/bin/curl -sS -b "$COOKIE_JAR" \
    -H 'Content-Type: application/json' \
    -d '{"name":"demo-traffic","tenant_id":"00000000-0000-0000-0000-000000000000"}' \
    -w '\n%{http_code}' \
    "${DASHBOARD_URL}/api/v1/admin/keys")
  code=$(echo "$body" | /usr/bin/tail -n1)
  body=$(echo "$body" | /usr/bin/sed '$d')
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    err "key issue failed ($code): $body"
    return 1
  fi
  local secret
  secret=$(echo "$body" | jq -r '.value // .secret // .api_key // .key // empty')
  if [[ -z "$secret" ]]; then
    err "no secret returned: $body"
    return 1
  fi
  printf '%s' "$secret" > "$APIKEY_FILE"
  ok "api key cached at $APIKEY_FILE"
}

# ─── Read subcommands ─────────────────────────────────────────────────────

cmd_status() {
  local body
  body=$(/usr/bin/curl -sS "${RUNTIME_URL}/api/v1/home/overview")
  printf '%sHome KPI snapshot%s\n' "$BOLD" "$RESET"
  printf '  %s%-22s%s %s\n' "$DIM" "requests/min:"           "$RESET" "$(echo "$body" | jq -r '.requests_per_minute')"
  printf '  %s%-22s%s %s\n' "$DIM" "error rate (60m):"       "$RESET" "$(echo "$body" | jq -r '.error_rate')"
  printf '  %s%-22s%s %s\n' "$DIM" "cost today (USD):"       "$RESET" "$(echo "$body" | jq -r '.cost_today_usd')"
  printf '  %s%-22s%s %s\n' "$DIM" "queue depth:"            "$RESET" "$(echo "$body" | jq -r '.queue_depth')"
  printf '  %s%-22s%s %s\n' "$DIM" "live runs:"              "$RESET" "$(echo "$body" | jq -r '.live_runs')"
  printf '  %s%-22s%s %s\n' "$DIM" "failed 24h:"             "$RESET" "$(echo "$body" | jq -r '.failed_runs_last_24h')"
  printf '  %s%-22s%s %s\n' "$DIM" "tenants at risk:"        "$RESET" "$(echo "$body" | jq -r '.budgets_aggregate.tenants_at_risk')"
  printf '  %s%-22s%s %s\n' "$DIM" "avg consumed %:"         "$RESET" "$(echo "$body" | jq -r '.budgets_aggregate.avg_consumed_pct')"
  printf '  %s%-22s%s %s\n' "$DIM" "tenant count (budgets):" "$RESET" "$(echo "$body" | jq -r '.budgets_aggregate.tenant_count')"
  printf '  %s%-22s%s %s\n' "$DIM" "recent runs (rows):"     "$RESET" "$(echo "$body" | jq -r '.recent_runs | length')"
  printf '  %s%-22s%s %s\n' "$DIM" "recent webhooks:"        "$RESET" "$(echo "$body" | jq -r '.recent_webhook_deliveries | length')"
  printf '  %s%-22s%s %s\n' "$DIM" "alerts:"                 "$RESET" "$(echo "$body" | jq -r '.alerts | length')"
}

cmd_events() {
  local limit="${1:-10}"
  /usr/bin/curl -sS "${RUNTIME_URL}/api/v1/admin/events?limit=${limit}" \
    | jq -r '.events[] | "\(.occurred_at) \(.severity)\t\(.kind)\t\(.title)"' \
    | /usr/bin/sort -r
}

cmd_services() {
  /usr/bin/curl -sS "${RUNTIME_URL}/api/v1/admin/services" \
    | jq -r '.services[] | "\(.status)\t\(.name)\tv\(.version // "-")\t\(.admin_url // "-")"' \
    | column -t -s $'\t'
}

# ─── Setup ────────────────────────────────────────────────────────────────

cmd_setup() {
  ensure_session
  ensure_apikey
  ok "setup complete; cookie + api key cached"
}

cmd_login() {
  rm -f "$COOKIE_JAR"
  sign_in
}

cmd_reset() {
  rm -f "$COOKIE_JAR" "$APIKEY_FILE"
  ok "wiped $COOKIE_JAR and $APIKEY_FILE"
}

# ─── Write subcommands ───────────────────────────────────────────────────

cmd_llm() {
  ensure_apikey
  local prompt="${1:-Say hi in one short sentence.}"
  local key
  key=$(cat "$APIKEY_FILE")
  local body
  body=$(/usr/bin/curl -sS -H "Authorization: Bearer $key" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg m "$LLM_MODEL" --arg p "$prompt" \
        '{model:$m, messages:[{role:"user", content:$p}], max_tokens:64}')" \
    "${RUNTIME_URL}/api/v1/llm/chat/completions")
  local content
  content=$(echo "$body" | jq -r '.choices[0].message.content // .error.message // .' 2>/dev/null || echo "$body")
  ok "llm replied: $(echo "$content" | /usr/bin/head -c 120)"
}

cmd_fail() {
  ensure_apikey
  local key
  key=$(cat "$APIKEY_FILE")
  local code
  code=$(/usr/bin/curl -sS -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $key" \
    -H 'Content-Type: application/json' \
    -d '{"input":{}}' \
    "${RUNTIME_URL}/api/v1/execute/does-not-exist.fail-on-purpose")
  ok "forced execute failure with status $code (drives failed_runs_last_24h)"
}

cmd_job() {
  ensure_session
  # `sample-job` is the canonical demo job kind registered by the
  # runtime — always present even on fresh installs. Other kinds come
  # from integration modules.
  local body
  body=$(/usr/bin/curl -sS -b "$COOKIE_JAR" \
    -H 'Content-Type: application/json' \
    -d '{"name":"sample-job","args":{"note":"from demo-traffic.sh"}}' \
    "${DASHBOARD_URL}/api/v1/jobs")
  ok "enqueued job: $(echo "$body" | jq -c '{id, name, state}' 2>/dev/null || echo "$body")"
}

cmd_webhook() {
  ensure_session
  # POST /api/v1/webhooks/send queues an outbound delivery row in
  # suite_webhook_deliveries — which is exactly what Home's activity
  # feed pulls. Destination defaults to webhook.site so the worker gets
  # a 200; override DEST_URL to inspect the payload elsewhere.
  local dest_url="${DEST_URL:-https://webhook.site/demo-traffic-bin}"
  local body
  body=$(/usr/bin/curl -sS -b "$COOKIE_JAR" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg u "$dest_url" \
        '{url:$u, event_type:"demo.ping", body:{source:"demo-traffic.sh", ts:now}, immediate:true}')" \
    "${DASHBOARD_URL}/api/v1/webhooks/send")
  ok "webhook send: $(echo "$body" | jq -c '{id, status, direction, destination}' 2>/dev/null || echo "$body")"
}

cmd_activity() {
  ensure_session
  local action="${1:-tenant.created}"
  local body
  body=$(/usr/bin/curl -sS -b "$COOKIE_JAR" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg a "$action" \
        '{actor_type:"system", action:$a, resource_type:"demo", resource_id:"demo-1", metadata:{source:"demo-traffic.sh"}}')" \
    "${DASHBOARD_URL}/api/v1/activity")
  ok "logged activity: $(echo "$body" | jq -c '{id, action}' 2>/dev/null || echo "$body")"
}

cmd_budget() {
  ensure_session
  local cap="${1:-5}"
  local body
  body=$(/usr/bin/curl -sS -b "$COOKIE_JAR" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg c "$cap" \
        '{tenant_id:"00000000-0000-0000-0000-000000000000", monthly_usd:($c|tonumber), alert_threshold_pct:80}')" \
    -X PUT \
    "${DASHBOARD_URL}/api/v1/admin/budgets")
  ok "budget set to \$${cap}/mo: $(echo "$body" | jq -c '. | {tenant_id, monthly_usd, alert_threshold_pct}' 2>/dev/null || echo "$body")"
}

cmd_all() {
  cmd_llm "Reply with one short sentence about a koala." || warn "llm failed"
  cmd_fail        || warn "fail failed"
  cmd_job         || warn "job failed"
  cmd_webhook     || warn "webhook failed"
  cmd_activity    || warn "activity failed"
  ok "one of each fired; refresh the dashboard to see tiles tick"
}

cmd_loop() {
  local interval="${1:-15}"
  log "starting loop (every ${interval}s). Ctrl-C to stop."
  while true; do
    log "---- $(date '+%H:%M:%S') ----"
    cmd_all || true
    sleep "$interval"
  done
}

# ─── Dispatch ─────────────────────────────────────────────────────────────

usage() {
  /usr/bin/sed -n '4,40p' "$0"
}

case "${1:-}" in
  status)    shift; cmd_status "$@" ;;
  events)    shift; cmd_events "$@" ;;
  services)  shift; cmd_services "$@" ;;
  setup)     shift; cmd_setup ;;
  login)     shift; cmd_login ;;
  reset)     shift; cmd_reset ;;
  llm)       shift; cmd_llm "$@" ;;
  fail)      shift; cmd_fail ;;
  job)       shift; cmd_job ;;
  webhook)   shift; cmd_webhook ;;
  activity)  shift; cmd_activity "$@" ;;
  budget)    shift; cmd_budget "$@" ;;
  all)       shift; cmd_all ;;
  loop)      shift; cmd_loop "$@" ;;
  ""|-h|--help|help) usage ;;
  *)         err "unknown subcommand: $1"; usage; exit 2 ;;
esac
