#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

runtime_url="${BACKAI_RUNTIME_URL:-${AF_STACK_RUNTIME_URL:-http://localhost:8080}}"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

curl -fsS "${runtime_url}/api/v1/llm/chat/completions" \
  -H "content-type: application/json" \
  -d '{
    "model": "demo/supportdesk",
    "messages": [
      {
        "role": "user",
        "content": "A customer says their invoice is wrong and wants a refund."
      }
    ]
  }' >"$tmp"

if ! grep -q "Draft reply" "$tmp"; then
  echo "supportdesk demo smoke failed: response did not include demo draft" >&2
  cat "$tmp" >&2
  exit 1
fi

if ! grep -q "demo-supportdesk" "$tmp"; then
  echo "supportdesk demo smoke failed: response did not include demo fingerprint" >&2
  cat "$tmp" >&2
  exit 1
fi

echo "supportdesk demo smoke passed: ${runtime_url}"
