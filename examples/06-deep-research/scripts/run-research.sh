#!/usr/bin/env bash
# Kick off a research run and stream the agent's progress.
#
# Assumes:
#  - docker compose -f examples/06-deep-research/docker-compose.yml up is running
#  - OPENROUTER_API_KEY (or ANTHROPIC_API_KEY / OPENAI_API_KEY) is set in .env
#
# Cost: ~$0.001-$0.01 with the default Qwen 2.5 72B model.
#
# Usage:
#   ./scripts/run-research.sh
#   ./scripts/run-research.sh "What are open problems in retrieval-augmented generation?"

set -euo pipefail

# Default question is intentionally meta: it forces the agent to fan
# out across multiple sub-topics (architecture, evaluation, security,
# tooling, deployment), which is exactly the shape of a real research
# task an operator might hand it.
QUESTION="${1:-What are the latest advances in foundation model agent harnesses?}"

PORT="${AF_STACK_PORT:-8080}"
DASH_PORT="${AF_STACK_DASHBOARD_PORT:-3000}"

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

step "Streaming researcher logs in the background"
# Detached log tail so the operator can watch the four pipeline stages
# (decompose / investigate / accumulate / synthesise) as they happen.
docker compose -f "$(dirname "$0")/../docker-compose.yml" \
    logs --no-color --tail 0 -f researcher 2>&1 \
    | sed -u 's/^/[researcher] /' &
LOG_PID=$!
# Make sure the background tail exits cleanly when we do.
trap 'kill ${LOG_PID} 2>/dev/null || true' EXIT INT TERM

step "POST research question to the gateway"
echo "  question: $QUESTION"
echo "  endpoint: http://localhost:${PORT}/api/v1/agents/researcher.research"
echo

# AgentField REST shape: {"input": {<arg_name>: <arg_value>, ...}}.
# Our `research` reasoner takes `question` and optional `depth`.
RESP="$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "$(printf '{"input":{"question":%s,"depth":2}}' "$(printf '%s' "$QUESTION" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')")" \
    "http://localhost:${PORT}/api/v1/agents/researcher.research")"

echo
step "Response"
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP"

# Try to pull the run_id out so we can point the operator at the
# matching memory and cost views. The runtime returns it in the
# response envelope; if not present we just skip the deep links.
RUN_ID="$(printf '%s' "$RESP" | python3 -c 'import json,sys
try:
    d=json.load(sys.stdin)
    print(d.get("run_id") or d.get("workflow_id") or d.get("execution_id") or "")
except Exception:
    print("")' || true)"

echo
step "Where to look next"
if [ -n "$RUN_ID" ]; then
    green "  Run ID: $RUN_ID"
    echo "  Memory browser (findings accumulated at scope=run):"
    echo "    http://localhost:${DASH_PORT}/dashboard/memory?run_id=${RUN_ID}"
    echo "  Cost log (per-LLM-call spend for this run):"
    echo "    http://localhost:${DASH_PORT}/dashboard/cost?run_id=${RUN_ID}"
else
    yellow "  Could not parse run_id from response — open the dashboard manually:"
    echo "    http://localhost:${DASH_PORT}/dashboard/memory"
    echo "    http://localhost:${DASH_PORT}/dashboard/cost"
fi

echo
step "Cost summary from the gateway"
# Pull the last few cost events for this run. The gateway records one
# event per .ai() / .harness() call, with tokens, latency, and USD cost.
COST_JSON="$(curl -s "http://localhost:${PORT}/api/v1/cost/events?limit=10" || echo '{}')"
echo "$COST_JSON" | python3 -c 'import json,sys
try:
    d = json.load(sys.stdin)
    events = d.get("events", [])
    if not events:
        print("  (no cost events recorded yet — check that the LLM key is set)")
    else:
        total = sum(e.get("cost_usd", 0) for e in events)
        print(f"  total of last {len(events)} events: ${total:.5f}")
        for e in events[:10]:
            print(f"    {e.get(\"model\",\"?\")} : ${e.get(\"cost_usd\",0):.5f} ({e.get(\"total_tokens\",0)} tok)")
except Exception as exc:
    print(f"  cost events not available: {exc}")' || true

# Give the background log tail a beat to flush before we exit.
sleep 1
