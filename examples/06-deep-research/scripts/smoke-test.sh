#!/usr/bin/env bash
# End-to-end smoke test for the deep-research example.
#
# Walks through:
#   1. Skip with warning if no LLM key is configured
#   2. POST a small research question to researcher.research
#   3. Wait up to 2 minutes for completion
#   4. Assert the Report shape: thesis non-empty, >=1 finding,
#      confidence in [0, 1]
#
# Cost: ~$0.001-$0.01 with the default Qwen 2.5 72B model.

set -euo pipefail

PORT="${AF_STACK_PORT:-8080}"
TIMEOUT_S="${SMOKE_TIMEOUT_S:-120}"

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }
fail()   { red "FAIL: $1"; exit 1; }

# ---- 0. Preflight ------------------------------------------------------
step "Preflight"
if [ -z "${OPENROUTER_API_KEY:-${ANTHROPIC_API_KEY:-${OPENAI_API_KEY:-}}}" ]; then
    yellow "SKIP: no LLM provider key (OPENROUTER_API_KEY / ANTHROPIC_API_KEY / OPENAI_API_KEY)."
    yellow "      The researcher agent needs at least one to be registered."
    exit 0
fi
command -v curl >/dev/null 2>&1 || fail "missing curl"
command -v python3 >/dev/null 2>&1 || fail "missing python3"
green "  preflight ok"

# ---- 1. Verify the runtime is up --------------------------------------
step "Check runtime is reachable"
if ! curl -fsS --max-time 5 "http://localhost:${PORT}/ready" >/dev/null 2>&1; then
    fail "runtime not ready at http://localhost:${PORT} — is docker compose up?"
fi
green "  runtime is up"

# ---- 2. Verify the researcher agent registered -------------------------
step "Check researcher agent is registered"
REG_DEADLINE=$(($(date +%s) + 30))
REGISTERED=""
while [ "$(date +%s)" -lt "$REG_DEADLINE" ]; do
    AGENTS_JSON="$(curl -s --max-time 3 "http://localhost:${PORT}/api/v1/agents" || echo '{}')"
    if echo "$AGENTS_JSON" | grep -q 'researcher'; then
        REGISTERED="yes"
        break
    fi
    sleep 2
done
[ -n "$REGISTERED" ] || fail "researcher not registered after 30s — check docker compose logs researcher"
green "  researcher registered"

# ---- 3. POST a tiny research question ---------------------------------
step "POST researcher.research"
# Deliberately small/concrete question — keeps the pipeline cost-bounded.
QUESTION="What is the difference between agent orchestration and agent harnesses?"
PAYLOAD="$(python3 -c "
import json
print(json.dumps({'input': {'question': '$QUESTION', 'depth': 1}}))
")"

START_TS=$(date +%s)
# `--max-time` gives the curl call enough budget to wait through the
# full fan-out. The reasoner runs synchronously over the gateway.
RESP="$(curl -s --max-time "$TIMEOUT_S" \
    -X POST \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" \
    "http://localhost:${PORT}/api/v1/agents/researcher.research" || echo '{}')"
ELAPSED=$(($(date +%s) - START_TS))
echo "  done in ${ELAPSED}s"

# ---- 4. Assert Report shape -------------------------------------------
step "Validate Report shape"
echo "$RESP" | python3 -c "
import json, sys

raw = sys.stdin.read()
try:
    env = json.loads(raw)
except Exception as exc:
    print(f'FAIL: response is not JSON: {exc}')
    print(raw[:400])
    sys.exit(1)

# AgentField wraps the reasoner return value. Different runtime versions
# return slightly different envelopes; handle both shapes.
report = env.get('result') or env.get('output') or env.get('data') or env
if isinstance(report, dict) and 'body' in report:
    report = report['body']
if not isinstance(report, dict):
    print(f'FAIL: report is not an object: {type(report).__name__}')
    print(json.dumps(env, indent=2)[:400])
    sys.exit(1)
if 'error' in report:
    print(f'FAIL: reasoner returned error: {report[\"error\"]}')
    sys.exit(1)

errors = []
thesis = report.get('thesis')
if not isinstance(thesis, str) or not thesis.strip():
    errors.append('thesis is empty or not a string')

findings = report.get('key_findings')
if not isinstance(findings, list) or len(findings) < 1:
    errors.append(f'key_findings must be a non-empty list, got {findings!r}')

conf = report.get('confidence')
if not isinstance(conf, (int, float)) or not (0.0 <= conf <= 1.0):
    errors.append(f'confidence must be in [0,1], got {conf!r}')

if errors:
    print('FAIL: Report shape invalid')
    for e in errors:
        print(f'  - {e}')
    print(json.dumps(report, indent=2)[:800])
    sys.exit(1)

print(f'  thesis: {thesis[:100]}...')
print(f'  key_findings: {len(findings)}')
print(f'  confidence: {conf:.2f}')
print(f'  sources_consulted: {report.get(\"sources_consulted\")}')
"

green ""
green "===================================================="
green "  Deep-research smoke test PASS in ${ELAPSED}s"
green "===================================================="
