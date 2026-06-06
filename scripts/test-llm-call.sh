#!/usr/bin/env bash
# Smoke-test a real LLM call through the suite gateway.
#
# Requires:
#  - docker compose up running
#  - OPENROUTER_API_KEY (or ANTHROPIC_API_KEY / OPENAI_API_KEY) set on host
#  - sample-agent container has registered the `summarize` reasoner
#
# Cost:  ~$0.0001 with the default Qwen 2.5 72B model.
#
# Usage:  AF_STACK_PORT=38080 ./scripts/test-llm-call.sh

set -euo pipefail

PORT="${AF_STACK_PORT:-8080}"

red()    { printf "\033[31m%s\033[0m\n" "$1"; }
green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }

if [ -z "${OPENROUTER_API_KEY:-${ANTHROPIC_API_KEY:-${OPENAI_API_KEY:-}}}" ]; then
    yellow "WARN: no LLM provider key found in env (OPENROUTER_API_KEY / ANTHROPIC_API_KEY / OPENAI_API_KEY)."
    yellow "      summarize reasoner won't have been registered."
    yellow "      Falling back to echo round-trip."
    payload='{"input":{"payload":{"message":"hi from test-llm-call"}}}'
    endpoint="sample.echo"
else
    echo "Using cheap model defaults (Qwen 2.5 72B via OpenRouter where possible)."
    payload='{"input":{"payload":{"text":"AgentField is an open-source backend platform for AI applications. It provides cryptographic identity for every agent, verifiable credentials per execution, and a runtime that scales agents like APIs."}}}'
    endpoint="sample.summarize"
fi

echo "POST http://localhost:${PORT}/api/v1/agents/${endpoint}"
echo "payload: $payload"
echo

RESP="$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "http://localhost:${PORT}/api/v1/agents/${endpoint}")"

echo "response:"
echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP"

if echo "$RESP" | grep -q '"status":"succeeded"'; then
    green "PASS  end-to-end LLM call succeeded"
else
    red "FAIL  LLM call did not return success"
    exit 1
fi
