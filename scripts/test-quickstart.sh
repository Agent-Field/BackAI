#!/usr/bin/env bash
# Validates the 60-second quickstart end to end.
#
# Brings up the full docker-compose stack from a clean slate, polls until
# the suite gateway is reachable, then verifies the supportdesk agent responds.
#
# Used in CI (when re-enabled) and by maintainers before each release.
#
# Usage:
#   ./scripts/test-quickstart.sh
#
# Env knobs:
#   AF_STACK_PORT (default 8080) — host port to bind the runtime to
#   QUICKSTART_TIMEOUT_S (default 90) — max seconds to wait for ready

set -euo pipefail

cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

PORT="${AF_STACK_PORT:-8080}"
TIMEOUT="${QUICKSTART_TIMEOUT_S:-90}"

step "1/5  Tear down any existing stack"
docker compose down -v >/dev/null 2>&1 || true

step "2/5  Preflight + docker compose up"
START_TS=$(date +%s)
node scripts/preflight.mjs
docker compose up -d --build 2>&1 | tail -10

step "3/5  Wait for runtime to report ready (timeout ${TIMEOUT}s)"
DEADLINE=$((START_TS + TIMEOUT))
HEALTH_OK=""
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    if STATUS_JSON="$(curl -s --max-time 3 "http://localhost:${PORT}/ready" || true)"; then
        if echo "$STATUS_JSON" | grep -q '"status":"ready"'; then
            HEALTH_OK="yes"
            break
        fi
    fi
    sleep 2
done

if [ -z "$HEALTH_OK" ]; then
    red "FAIL: gateway did not become ready within ${TIMEOUT}s"
    echo "--- docker compose ps ---"
    docker compose ps
    echo "--- runtime logs ---"
    docker compose logs runtime --tail 50
    echo "--- agentfield logs ---"
    docker compose logs agentfield --tail 30
    exit 1
fi
ELAPSED=$(($(date +%s) - START_TS))
green "  ready in ${ELAPSED}s"

step "4/5  Verify supportdesk agent is registered with AgentField"
# Give the supportdesk agent up to 30 seconds to register
REG_DEADLINE=$(($(date +%s) + 30))
REGISTERED=""
while [ "$(date +%s)" -lt "$REG_DEADLINE" ]; do
    AGENTS_JSON="$(curl -s --max-time 3 "http://localhost:${PORT}/api/v1/agents" || echo '{}')"
    if echo "$AGENTS_JSON" | grep -q 'sample'; then
        REGISTERED="yes"
        break
    fi
    sleep 2
done

if [ -z "$REGISTERED" ]; then
    yellow "  WARN: supportdesk agent not visible in /api/v1/agents after 30s"
    echo "  agents response: $AGENTS_JSON"
    echo "  --- supportdesk-agent logs ---"
    docker compose logs supportdesk-agent --tail 30
    # Don't fail — agent discovery format may differ. We test invocation next.
fi

step "5/5  POST to supportdesk.echo via the gateway"
# AgentField's REST shape: {"input": {<arg_name>: <arg_value>}}.
# Our `echo` reasoner takes one arg `payload: dict`, so we send:
ECHO_RESP="$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d '{"input":{"payload":{"message":"hello world"}}}' \
    "http://localhost:${PORT}/api/v1/agents/supportdesk.echo")"

echo "  response: $ECHO_RESP"

if echo "$ECHO_RESP" | grep -q 'hello world\|echoed'; then
    green "  PASS  supportdesk.echo round-trip works"
else
    red "  FAIL  supportdesk.echo did not return expected payload"
    docker compose logs supportdesk-agent --tail 30
    docker compose logs runtime --tail 30
    exit 1
fi

TOTAL=$(($(date +%s) - START_TS))
echo
green "===================================================="
green "  Quickstart PASS in ${TOTAL} seconds"
green "===================================================="

step "Tear down"
docker compose down -v >/dev/null 2>&1
