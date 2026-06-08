#!/usr/bin/env bash
# Credential-gated Fly.io staging deploy smoke test.
#
# Skips unless FLY_API_TOKEN plus staging app names are configured. When
# configured, deploys runtime + dashboard with templated Fly configs and
# probes the public health endpoints.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }

command -v flyctl >/dev/null 2>&1 || command -v fly >/dev/null 2>&1 || skip "flyctl not installed"
FLY_BIN="$(command -v flyctl || command -v fly)"

[ -n "${FLY_API_TOKEN:-}" ] || skip "FLY_API_TOKEN not set"
[ -n "${AF_STACK_FLY_STAGING_RUNTIME_APP:-}" ] || skip "AF_STACK_FLY_STAGING_RUNTIME_APP not set"
[ -n "${AF_STACK_FLY_STAGING_DASHBOARD_APP:-}" ] || skip "AF_STACK_FLY_STAGING_DASHBOARD_APP not set"

REGION="${AF_STACK_FLY_STAGING_REGION:-iad}"
RUNTIME_URL="${AF_STACK_FLY_STAGING_RUNTIME_URL:-https://${AF_STACK_FLY_STAGING_RUNTIME_APP}.fly.dev}"
DASHBOARD_URL="${AF_STACK_FLY_STAGING_DASHBOARD_URL:-https://${AF_STACK_FLY_STAGING_DASHBOARD_APP}.fly.dev}"
TIMEOUT="${AF_STACK_FLY_STAGING_TIMEOUT_S:-180}"

TMPDIR="$(mktemp -d)"
cleanup() {
    rm -rf "$TMPDIR"
}
trap cleanup EXIT

RUNTIME_CONFIG="$TMPDIR/fly.toml"
DASHBOARD_CONFIG="$TMPDIR/fly.dashboard.toml"

sed \
    -e "s/<app-name>/${AF_STACK_FLY_STAGING_RUNTIME_APP}/g" \
    -e "s/<primary-region>/${REGION}/g" \
    deploy/fly/fly.toml > "$RUNTIME_CONFIG"

sed \
    -e "s/<dashboard-app-name>/${AF_STACK_FLY_STAGING_DASHBOARD_APP}/g" \
    -e "s/<runtime-app-name>/${AF_STACK_FLY_STAGING_RUNTIME_APP}/g" \
    -e "s/<primary-region>/${REGION}/g" \
    deploy/fly/fly.dashboard.toml > "$DASHBOARD_CONFIG"

step "Validate Fly configs"
"$FLY_BIN" config validate --config "$RUNTIME_CONFIG"
"$FLY_BIN" config validate --config "$DASHBOARD_CONFIG"

step "Deploy runtime staging app"
"$FLY_BIN" deploy --config "$RUNTIME_CONFIG" --app "$AF_STACK_FLY_STAGING_RUNTIME_APP" --remote-only

step "Deploy dashboard staging app"
"$FLY_BIN" deploy --config "$DASHBOARD_CONFIG" --app "$AF_STACK_FLY_STAGING_DASHBOARD_APP" --remote-only

probe() {
    local url="$1"
    local label="$2"
    local deadline=$((SECONDS + TIMEOUT))
    while [ "$SECONDS" -lt "$deadline" ]; do
        if curl -fsS "$url" >/dev/null; then
            green "  $label ok"
            return 0
        fi
        sleep 5
    done
    fail "$label did not become healthy: $url"
}

step "Probe staging endpoints"
probe "$RUNTIME_URL/health" "runtime /health"
probe "$RUNTIME_URL/ready" "runtime /ready"
probe "$DASHBOARD_URL/" "dashboard /"

green ""
green "==> Fly staging smoke passed"
