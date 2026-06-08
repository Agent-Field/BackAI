#!/usr/bin/env bash
# Credential-gated production docker-compose smoke test.
#
# This uses docker-compose.prod.yml with real external Postgres/S3-style
# configuration. It skips unless AF_STACK_PROD_COMPOSE_SMOKE=true and the
# required production env vars are present.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }

[ "${AF_STACK_PROD_COMPOSE_SMOKE:-false}" = "true" ] || skip "AF_STACK_PROD_COMPOSE_SMOKE is not true"
command -v docker >/dev/null 2>&1 || skip "docker not installed"
docker info >/dev/null 2>&1 || skip "docker daemon not reachable"

required=(
    AF_STACK_DOMAIN
    ACME_EMAIL
    AF_STACK_DATABASE_URL
    AGENTFIELD_STORAGE_POSTGRES_URL
    AF_STACK_KMS_KEY
    AF_STACK_AUTH_SECRET
    AF_STACK_S3_ENDPOINT
    AF_STACK_S3_BUCKET
    AF_STACK_S3_ACCESS_KEY
    AF_STACK_S3_SECRET_KEY
)

missing=()
for name in "${required[@]}"; do
    if [ -z "${!name:-}" ]; then
        missing+=("$name")
    fi
done
if [ "${#missing[@]}" -gt 0 ]; then
    skip "missing required env vars: ${missing[*]}"
fi

PROJECT="${AF_STACK_PROD_COMPOSE_PROJECT:-af-stack-prod-smoke}"
RUNTIME_PORT="${AF_STACK_PROD_COMPOSE_RUNTIME_PORT:-18080}"
DASHBOARD_PORT="${AF_STACK_PROD_COMPOSE_DASHBOARD_PORT:-13000}"
TIMEOUT="${AF_STACK_PROD_COMPOSE_TIMEOUT_S:-180}"
TMPDIR="$(mktemp -d)"
OVERRIDE="$TMPDIR/docker-compose.prod.smoke.yml"

cleanup() {
    docker compose -p "$PROJECT" -f docker-compose.prod.yml -f "$OVERRIDE" down -v >/dev/null 2>&1 || true
    rm -rf "$TMPDIR"
}
trap cleanup EXIT

cat > "$OVERRIDE" <<YAML
services:
  runtime:
    ports:
      - "${RUNTIME_PORT}:8080"
  dashboard:
    ports:
      - "${DASHBOARD_PORT}:3000"
YAML

step "Validate production compose config"
docker compose -p "$PROJECT" -f docker-compose.prod.yml -f "$OVERRIDE" config --quiet

step "Bring up production compose services"
docker compose -p "$PROJECT" -f docker-compose.prod.yml -f "$OVERRIDE" up -d

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
    docker compose -p "$PROJECT" -f docker-compose.prod.yml -f "$OVERRIDE" ps
    docker compose -p "$PROJECT" -f docker-compose.prod.yml -f "$OVERRIDE" logs runtime --tail 80 || true
    fail "$label did not become healthy: $url"
}

step "Probe services"
probe "http://127.0.0.1:${RUNTIME_PORT}/health" "runtime /health"
probe "http://127.0.0.1:${RUNTIME_PORT}/ready" "runtime /ready"
probe "http://127.0.0.1:${DASHBOARD_PORT}/" "dashboard /"

step "Verify containers are running"
docker compose -p "$PROJECT" -f docker-compose.prod.yml -f "$OVERRIDE" ps --status running

green ""
green "==> Production compose smoke passed"
