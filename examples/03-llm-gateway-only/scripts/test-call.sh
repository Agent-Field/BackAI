#!/usr/bin/env bash
# Smoke-test the LLM gateway in two ways and then read the cost ledger.
#
# 1. Plain curl       — POST /api/v1/llm/chat/completions
# 2. OpenAI Python    — same endpoint, official SDK
# 3. GET /api/v1/cost — show the operator the money tick up
#
# Requires:
#   - docker compose up running in this example
#   - OPENROUTER_API_KEY set in .env (the runtime reads it)
#   - python3 + pip install openai  (for step 2; skipped if missing)
#
# Cost: ~$0.0002 total (two Qwen 2.5 72B calls).
#
# Usage:
#   ./scripts/test-call.sh
#   AF_STACK_PORT=18080 ./scripts/test-call.sh

set -euo pipefail

PORT="${AF_STACK_PORT:-8080}"
BASE="http://localhost:${PORT}"
MODEL="${AF_STACK_MODEL:-qwen/qwen-2.5-72b-instruct}"

red()    { printf "\033[31m%s\033[0m\n" "$1"; }
green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
bold()   { printf "\033[1m%s\033[0m\n" "$1"; }

bold "==> 1/3  curl -> POST /api/v1/llm/chat/completions"

PAYLOAD=$(cat <<EOF
{
  "model": "${MODEL}",
  "messages": [
    {"role": "system", "content": "You are a terse assistant. Reply in one short sentence."},
    {"role": "user", "content": "Say hi from a plain curl request."}
  ],
  "max_tokens": 60
}
EOF
)

RESP="$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer not-needed-mt-off" \
    -d "$PAYLOAD" \
    "${BASE}/api/v1/llm/chat/completions")"

echo "$RESP" | python3 -m json.tool 2>/dev/null || echo "$RESP"

# Token-count assertion. The OpenAI envelope returns usage.prompt_tokens
# and usage.completion_tokens; both must be > 0 for a real upstream call.
P_TOK=$(echo "$RESP" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('usage',{}).get('prompt_tokens',0))" 2>/dev/null || echo 0)
C_TOK=$(echo "$RESP" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('usage',{}).get('completion_tokens',0))" 2>/dev/null || echo 0)

if [ "${P_TOK:-0}" -gt 0 ] && [ "${C_TOK:-0}" -gt 0 ]; then
    green "PASS  prompt_tokens=${P_TOK} completion_tokens=${C_TOK}"
else
    red "FAIL  no token usage returned — check OPENROUTER_API_KEY"
    exit 1
fi

echo
bold "==> 2/3  openai SDK -> POST /api/v1/llm/chat/completions"

DEMO="$(dirname "$0")/openai-demo.py"
if python3 -c "import openai" 2>/dev/null; then
    AF_STACK_LLM_URL="${BASE}/api/v1/llm" \
    AF_STACK_MODEL="${MODEL}" \
    python3 "$DEMO" "Say hi from the OpenAI SDK." || {
        red "FAIL  openai-demo.py exited non-zero"
        exit 1
    }
    green "PASS  openai SDK call succeeded"
else
    yellow "SKIP  openai package not installed. Run: pip install openai"
fi

echo
bold "==> 3/3  GET /api/v1/cost — period spend so far"

# Give the post-call recorder a beat to flush to Postgres.
sleep 1
COST="$(curl -s -H "Authorization: Bearer not-needed-mt-off" \
    "${BASE}/api/v1/cost")"

echo "$COST" | python3 -m json.tool 2>/dev/null || echo "$COST"

PERIOD=$(echo "$COST" | python3 -c "import sys, json; d=json.load(sys.stdin); print(d.get('period_total_usd', 0))" 2>/dev/null || echo "0")
green "PASS  period_total_usd=\$${PERIOD}"
echo
yellow "Open the dashboard at http://localhost:3000/operate/cost to watch this number live."
