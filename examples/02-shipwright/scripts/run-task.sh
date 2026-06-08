#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [ -f "$ROOT/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env"
  set +a
fi

BASE="http://localhost:${AF_STACK_PORT:-8080}"

if [ -z "${OPENROUTER_API_KEY:-}${OPENAI_API_KEY:-}${ANTHROPIC_API_KEY:-}" ]; then
  echo "SKIP: set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY in .env"
  exit 0
fi

payload="$(mktemp)"
cat >"$payload" <<'JSON'
{
  "title": "Add a CSV export button to the billing table",
  "description": "Find the customer billing table and add a small export action that copies CSV to the clipboard. Keep the change scoped and include tests or a build command if available.",
  "repo_url": "https://github.com/Agent-Field/backai",
  "harness_provider": "codex"
}
JSON

echo "Creating Shipwright task..."
curl -fsS -X POST "$BASE/api/v1/shipwright/tasks" \
  -H 'content-type: application/json' \
  -d @"$payload" | jq .

