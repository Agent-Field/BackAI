#!/usr/bin/env bash
# Seed Notable with two tenants + sample notes + run all three agents.
#
# Why this is a shell script: it deliberately uses only psql + curl, the
# two tools you'd reach for if you'd never seen this codebase before. No
# language runtime, no SDK install — just "here is what is happening on
# the wire."
#
# Idempotent: re-running drops + recreates tenants and notes. The
# billing customer rows survive because Stripe customer ids are
# expensive to invalidate; if you want a clean slate, drop the table
# manually.

set -euo pipefail

# ─── Config ───────────────────────────────────────────────────────────────

API_URL="${NOTABLE_API_URL:-http://localhost:8090}"
RUNTIME_URL="${AF_STACK_URL:-http://localhost:8080}"
DB_URL="${NOTABLE_DATABASE_URL:-postgres://afstack:afstack@localhost:5432/afstack?sslmode=disable}"

# Pull psql out of PATH or fall back to the dockerized one. macOS users
# often don't have psql installed; the postgres container in the compose
# stack has it.
if command -v psql >/dev/null 2>&1; then
  PSQL=(psql "$DB_URL")
else
  PSQL=(docker compose -f "$(dirname "$0")/../docker-compose.yml" exec -T postgres psql -U afstack -d afstack)
fi

log() { printf "\033[1;34m[seed]\033[0m %s\n" "$*"; }
fail() { printf "\033[1;31m[seed]\033[0m %s\n" "$*" >&2; exit 1; }

# ─── 1. Wait for the API to be ready ─────────────────────────────────────

log "waiting for ${API_URL}/healthz ..."
for i in $(seq 1 60); do
  if curl -fsS "${API_URL}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
  [[ "$i" == "60" ]] && fail "notable-api never came up at ${API_URL}"
done
log "notable-api is up"

# ─── 2. Provision tenants + billing customers ────────────────────────────

log "creating tenants in suite_tenants (bypass RLS for setup)"
"${PSQL[@]}" -v ON_ERROR_STOP=1 <<'SQL'
SELECT set_config('app.bypass_rls', 'on', true);

INSERT INTO suite_tenants (slug, name, plan, settings, quota)
VALUES
  ('acme', 'Acme Inc.', 'pro', '{}'::jsonb, '{}'::jsonb),
  ('globex', 'Globex Corp.', 'pro', '{}'::jsonb, '{}'::jsonb)
ON CONFLICT (slug) DO UPDATE SET plan = EXCLUDED.plan, name = EXCLUDED.name;
SQL

# Seed billing customers on the pro plan with stub Stripe ids. The
# billing module's stub mode treats any cus_* string as a deterministic
# fake — we never call Stripe here. If STRIPE_SECRET_KEY is set on the
# runtime, the runtime will reconcile these against real Stripe on the
# next portal-link call.
log "seeding suite_billing_customers (stub Stripe ids)"
"${PSQL[@]}" -v ON_ERROR_STOP=1 <<'SQL'
SELECT set_config('app.bypass_rls', 'on', true);

INSERT INTO suite_billing_customers (
  tenant_id, stripe_customer_id, email, plan, created_at, updated_at
)
SELECT slug, 'cus_stub_' || slug, slug || '@example.com', 'pro', now(), now()
FROM suite_tenants
WHERE slug IN ('acme', 'globex')
ON CONFLICT (tenant_id) DO UPDATE
  SET plan = 'pro',
      stripe_customer_id = EXCLUDED.stripe_customer_id,
      email = EXCLUDED.email,
      updated_at = now();
SQL

# ─── 3. Create notes via the public API ──────────────────────────────────
#
# We deliberately go through the HTTP API (not psql) so this exercises
# the full code path: tenant resolver -> handler -> RLS -> billing meter.

make_note() {
  local tenant="$1" user="$2" title="$3" body="$4"
  curl -fsS -X POST "${API_URL}/notable/notes" \
    -H "content-type: application/json" \
    -H "x-af-stack-tenant-id: ${tenant}" \
    -H "x-af-stack-user-id: ${user}" \
    -d "$(jq -c -n --arg t "$title" --arg b "$body" '{title:$t, body:$b, tags:[]}')" \
    | jq -r '.id'
}

log "creating sample notes for acme + globex"
ACME_NOTES=()
GLOBEX_NOTES=()

ACME_NOTES+=("$(make_note acme alice 'Q3 launch plan' \
  $'We are launching the new pricing on Sept 5.\n\n- [ ] finalize pricing page copy\n- [ ] schedule announce email\n- [x] update homepage hero')")
ACME_NOTES+=("$(make_note acme alice 'Eng standup notes' \
  $'Mike pushed the rate limiter. Sasha is still on the data migration.\n\n- [ ] book demo room for Friday\n- [ ] follow up with infra on Postgres bump')")
ACME_NOTES+=("$(make_note acme alice 'Customer call: Initech' \
  'They want SSO. Their security team requires SAML, not OIDC. They are willing to pay for it as a separate line item.')")
ACME_NOTES+=("$(make_note acme alice 'Hiring loop ideas' \
  $'For the staff PM role.\n\n- [ ] draft loop questions\n- [ ] pick interviewers')")
ACME_NOTES+=("$(make_note acme bob 'Personal: read list' \
  'Designing Data-Intensive Applications, Staff Engineer, The Manager Path.')")

GLOBEX_NOTES+=("$(make_note globex carol 'Vendor evaluation: warehouses' \
  'Snowflake is winning on price for our shape. BigQuery wins on ergonomics.')")
GLOBEX_NOTES+=("$(make_note globex carol 'Board prep: Q3' \
  $'Topline: ARR up 28%. Net retention 117%.\n\n- [ ] export the latest funnel chart\n- [ ] write the narrative section')")
GLOBEX_NOTES+=("$(make_note globex carol 'Incident: payment webhook' \
  'Stripe webhook deliveries lagged ~6 min around 14:30 UTC. No customer impact. Add a Svix alert if p95 > 60s.')")
GLOBEX_NOTES+=("$(make_note globex dave 'Hiring: senior backend' \
  $'Pipeline is thin. Need to source more aggressively.\n\n- [ ] reach out to 10 candidates on LinkedIn this week')")
GLOBEX_NOTES+=("$(make_note globex dave 'Trip notes: ATX offsite' \
  'Team really liked the structured brainstorming sessions. Next time: bigger breakout rooms.')")

log "created ${#ACME_NOTES[@]} notes for acme, ${#GLOBEX_NOTES[@]} for globex"

# ─── 4. Run the agents over the seed data ────────────────────────────────

run_agent_on_note() {
  local tenant="$1" user="$2" note_id="$3" action="$4"
  curl -fsS -X PATCH "${API_URL}/notable/notes/${note_id}/${action}" \
    -H "x-af-stack-tenant-id: ${tenant}" \
    -H "x-af-stack-user-id: ${user}" \
    >/dev/null || log "agent ${action} on ${note_id} failed (skipping)"
}

log "running summarize + suggest_tags on each acme note"
for id in "${ACME_NOTES[@]}"; do
  run_agent_on_note acme alice "$id" summarize
  run_agent_on_note acme alice "$id" suggest-tags
done

log "running summarize + suggest_tags on each globex note"
for id in "${GLOBEX_NOTES[@]}"; do
  run_agent_on_note globex carol "$id" summarize
  run_agent_on_note globex carol "$id" suggest-tags
done

log "done. dashboard plugin should now show 2 tenants with real data."
log "  -> http://localhost:${AF_STACK_DASHBOARD_PORT:-3000}/plugins/notable"
