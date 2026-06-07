#!/usr/bin/env bash
# Smoke test for the Notable example.
#
# Asserts the three agent contracts hold and that the billing meter
# fires on a successful POST. Exits non-zero on any failure so CI can
# wire it up directly.

set -euo pipefail

API_URL="${NOTABLE_API_URL:-http://localhost:8090}"
RUNTIME_URL="${AF_STACK_URL:-http://localhost:8080}"
TENANT="${SMOKE_TENANT:-smoketest}"
USER="${SMOKE_USER:-smokeuser}"

ok()   { printf "\033[1;32m[ok]\033[0m %s\n" "$*"; }
fail() { printf "\033[1;31m[fail]\033[0m %s\n" "$*" >&2; exit 1; }
info() { printf "\033[1;34m[smoke]\033[0m %s\n" "$*"; }

require() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required tool: $1"
}
require jq
require curl

# ─── 1. API is healthy ───────────────────────────────────────────────────
info "checking ${API_URL}/healthz"
curl -fsS "${API_URL}/healthz" >/dev/null || fail "notable-api healthz failed"
ok "notable-api healthz"

# Provision the smoke tenant (idempotent).
info "provisioning tenant=${TENANT}"
if command -v psql >/dev/null 2>&1; then
  PSQL=(psql "${NOTABLE_DATABASE_URL:-postgres://afstack:afstack@localhost:5432/afstack?sslmode=disable}")
else
  PSQL=(docker compose -f "$(dirname "$0")/../docker-compose.yml" exec -T postgres psql -U afstack -d afstack)
fi
"${PSQL[@]}" -v ON_ERROR_STOP=1 -c "
SELECT set_config('app.bypass_rls', 'on', true);
INSERT INTO suite_tenants (slug, name, plan, settings, quota)
VALUES ('${TENANT}', 'Smoke Test Tenant', 'pro', '{}'::jsonb, '{}'::jsonb)
ON CONFLICT (slug) DO NOTHING;
" >/dev/null
ok "tenant ${TENANT} ready"

# ─── 2. POST creates a note + meters notable_notes_created ───────────────
info "fetching pre-test meter value for tenant=${TENANT}"
PRE_QTY=$(curl -fsS "${RUNTIME_URL}/api/v1/billing/meters?tenant=${TENANT}" \
  | jq '[.meters[] | select(.meter == "notable_notes_created") | .quantity] | add // 0')

info "POST /notable/notes (with TODOs in the body so todo_completer has work)"
BODY=$(cat <<'JSON'
{
  "title": "Smoke note",
  "body": "Plan the launch.\n\n- [ ] confirm budget with finance\n- [ ] book the venue\n- [ ] send save-the-date emails\n\nThe pricing page draft is locked.",
  "tags": []
}
JSON
)

NOTE_JSON=$(curl -fsS -X POST "${API_URL}/notable/notes" \
  -H "content-type: application/json" \
  -H "x-af-stack-tenant-id: ${TENANT}" \
  -H "x-af-stack-user-id: ${USER}" \
  -d "$BODY")
NOTE_ID=$(echo "$NOTE_JSON" | jq -r '.id')
[[ -n "$NOTE_ID" && "$NOTE_ID" != "null" ]] || fail "POST /notable/notes returned no id"
ok "note created id=${NOTE_ID}"

# Give the meter a moment to commit. The billing service writes the
# increment async; we tolerate a small skew.
sleep 1
POST_QTY=$(curl -fsS "${RUNTIME_URL}/api/v1/billing/meters?tenant=${TENANT}" \
  | jq '[.meters[] | select(.meter == "notable_notes_created") | .quantity] | add // 0')

if awk -v pre="$PRE_QTY" -v post="$POST_QTY" 'BEGIN { exit !(post > pre) }'; then
  ok "billing meter notable_notes_created incremented (${PRE_QTY} -> ${POST_QTY})"
else
  fail "billing meter did not increment (still ${POST_QTY}); is the billing module on?"
fi

# ─── 3. summarize returns a non-empty tldr ───────────────────────────────
info "PATCH /notable/notes/${NOTE_ID}/summarize"
SUM=$(curl -fsS -X PATCH "${API_URL}/notable/notes/${NOTE_ID}/summarize" \
  -H "x-af-stack-tenant-id: ${TENANT}" \
  -H "x-af-stack-user-id: ${USER}")
TLDR=$(echo "$SUM" | jq -r '.tldr // ""')
[[ -n "$TLDR" ]] || fail "summarize returned an empty tldr"
ok "summarize returned tldr ($(echo "$TLDR" | wc -c) chars)"

# ─── 4. suggest_tags returns >= 1 tag ────────────────────────────────────
info "PATCH /notable/notes/${NOTE_ID}/suggest-tags"
TAGS_JSON=$(curl -fsS -X PATCH "${API_URL}/notable/notes/${NOTE_ID}/suggest-tags" \
  -H "x-af-stack-tenant-id: ${TENANT}" \
  -H "x-af-stack-user-id: ${USER}")
TAG_COUNT=$(echo "$TAGS_JSON" | jq '.tags | length')
[[ "$TAG_COUNT" -ge 1 ]] || fail "suggest_tags returned no tags"
ok "suggest_tags returned ${TAG_COUNT} tags"

# ─── 5. todo_completer extracts >= 1 TODO ────────────────────────────────
info "calling notable-ai.todo_completer directly"
TODO_JSON=$(curl -fsS -X POST "${RUNTIME_URL}/agents/notable-ai.todo_completer" \
  -H "content-type: application/json" \
  -H "x-af-stack-tenant-id: ${TENANT}" \
  -d "$(jq -c -n --arg b "$(echo "$BODY" | jq -r .body)" '{input:{body:$b}}')")
COMPLETION_COUNT=$(echo "$TODO_JSON" | jq '.output.completions | length // 0')
[[ "$COMPLETION_COUNT" -ge 1 ]] || fail "todo_completer found no TODOs (got ${COMPLETION_COUNT})"
ok "todo_completer returned ${COMPLETION_COUNT} completions"

ok "all smoke tests passed."
