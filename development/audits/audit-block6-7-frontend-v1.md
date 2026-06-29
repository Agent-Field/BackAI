# Audit — Blocks 6, 7, and Frontend Pattern (v1)

Source-of-truth scan against `execution-blocks-v1.md`. All findings cite absolute paths + line numbers.

---

## Block 6 — Aggregation endpoints

### 6.1 `GET /api/v1/reasoners/analytics` — MISSING (ground-up)

- No `reasoners` route in `services/runtime/internal/server/`. `grep "/reasoners"` returns zero handlers. Only field-level `reasoner` strings exist in `runs.go:117,386,420` and `agentfield/client.go:96,155-190`.
- `suite_cost_events` schema (`services/runtime/internal/db/migrations/00005_cost.sql:20-34`) tags rows by `model`, `provider`, `agent` — **no `reasoner` column**. `00023_cost_events_modality.sql` and `00026_cost_event_request_id.sql` add `modality` and `request_id`; neither adds reasoner.
- `internal/cost/recorder.go` and `aggregate.go` contain zero `reasoner` references.
- **Implication**: Block 6.1 requires a migration to add `reasoner text` (nullable) to `suite_cost_events`, recorder plumbing through `internal/cost/hooks.go`, and a new aggregation handler. The block doc says "aggregated over `suite_cost_events` (where `agent` + `reasoner` are tagged)" but reasoner is not tagged today. Flag this as scope-hidden work in the block.

### 6.2 `GET /api/v1/tools/usage` — MISSING (ground-up)

- No `tools/usage` endpoint in `internal/server/tools_native.go` or `mcp.go`.
- No `suite_tool_calls` / `suite_tool_invocations` table in migrations. `grep "suite_tool"` returns only `suite_tool_adapters` (configuration, not call log; `00019_tool_adapters.sql`) and `suite_tenant_tools` (`00020_tenant_tools.sql`).
- `internal/tools/registry.go` and `mcp/store.go` issue inserts only against config tables; no per-call audit row.
- **Implication**: a new migration + write-path hook in `tools.Registry.Call` and `mcp.Bridge.Call` is required. Same scope-hidden problem as 6.1.

### 6.3 `GET /api/v1/search/indexes` — MISSING

- `internal/server/search.go:18-22` mounts only `POST /api/v1/search`, `PUT /api/v1/search/documents`, `DELETE /api/v1/search/documents/{ns}/{key}`. No `/indexes` handler.
- `00016_search.sql:32-37` defines a single FTS index `suite_search_documents_fts_idx`. Block 6.3 wants `pg_indexes` + `pg_stat_user_indexes` aggregation — straightforward read-only PG query, no schema changes.
- Note: Block 1.5 also lists this endpoint with the same file location (`internal/server/search.go`). Duplicate planning between Block 1 and Block 6.

### 6.4 Notification channels CRUD — MISSING (env-only today)

- `00009_notifications.sql:22-45` defines `suite_notifications` (outbox of attempts only) — **no `suite_notification_channels` table**.
- `internal/notifications/interface.go:28` calls "Kind" the channel concept; `internal/notifications/factory.go` (4.7K) wires adapters from env. `grep "channel"` returns one comment hit and zero CRUD.
- `apps/dashboard/src/lib/api.ts:1995-2034` exposes `api.notifications.{stats,list,get,send}` — **no `channels` accessor**.
- **Implication**: needs new migration, new package code, new api.ts entry. Matches the block's "replacing env-only display" framing.

### 6.5 `GET /api/v1/oauth/refresh-history` — MISSING

- `00021_oauth_tokens.sql:21-34` defines `suite_oauth_tokens` only (current snapshot). No refresh-history table.
- `internal/oauth/manager.go:11K` calls `Refresh` transparently; no row inserted per attempt.
- `apps/dashboard/src/lib/api.ts:2098-2115` exposes only `connections`, `providers`, `authorize`, `revoke`.
- **Implication**: needs migration + manager write-path hook on every refresh call.

---

## Block 7 — Polish

### 7.1 Adapter pill — ALREADY EXISTS

`apps/dashboard/src/components/new-admin/operator-page.tsx:95-100` already renders `model.adapter` as a `<Badge>` with "via {adapter}" + external-link icon. Block 7.1 is just *populating* `model.adapter` on more page-model entries — no new component.

### 7.2 Home Connected Services strip — page-model edit only

- Home (`page-model.ts:159-179`) already reads `snapshot.services` (line 168) into a "Service status" card. The "strip" is just restyling this card on the Home model — no structural change.
- `snapshot.services: ServiceVital[]` (`data.ts:96`) is currently sourced from `seededServices` (`data.ts:437`) — **no `api.admin.services()` call** in the 44 `settle(...)` calls (`data.ts:501-544`).
- Block 1.2 plans `GET /api/v1/admin/services`. Until that lands, Home strip stays seeded.

### 7.3 Capability-honest degradation — NEW PATTERN

- `grep -n "capability"` in `page-model.ts` returns 15 hits, but all are *labels* (`"capability gated"`, `"adapter capability"`) — no runtime checking. `data.ts:987` reads `slot.active.capabilities` only for counting.
- No `if (capabilities.supportsRange) ...` style guards anywhere. This is genuinely new infrastructure: each adapter-backed page (Logs, Traces, Errors, Cost, Metrics) needs a `capabilities` snapshot field + a per-section conditional in `page-model.ts`.

---

## Cross-cutting frontend pattern

### Data flow confirmed

- `apps/dashboard/src/lib/new-admin/data.ts:500-545` calls **44 endpoints** via `settle(() => api.X())` (line 109 wraps each in try/catch returning null). Falls through to `seededSnapshot` if all null (`data.ts:547-548`).
- Catch-all route is `apps/dashboard/src/app/(admin)/[...slug]/page.tsx:15-21` — calls `getOperatorSnapshot()` + `buildPageModel(slug, snapshot)` + renders `<OperatorPage model={model} />`.
- Home route `apps/dashboard/src/app/(admin)/page.tsx:1-15` — same shape with hard-coded `"/"`.
- `apps/dashboard/src/app/old/` exists as archived previous Home (`page.tsx:1-21` — old `api.home()` consumer); confirmed archival.

### Per-endpoint integration cost (constant across blocks)

For each new endpoint, the work is exactly:

1. Add zod schema + `api.<group>.<verb>()` in `apps/dashboard/src/lib/api.ts` (single object literal in `export const api = { ... }` at line 1810).
2. Add `settle(() => api.<group>.<verb>())` in the `Promise.all` at `data.ts:500`.
3. Map response into a `ConsoleRow[]` or named snapshot field (`Snapshot` interface at `data.ts:~73-107`).
4. Update relevant `page-model.ts` entry (route -> `PageDefinition`) to read `snapshot.<field>` instead of seeded fallback.
5. Flip the matching `kpi("...", "missing"|"gap"|"deferred", ...)` to live.

**No new React components required** for Blocks 1–6 except 7.3 capability-honest degradation (which can also be expressed inside `OperatorPage` via a new optional `model.capabilities` field — still page-model-only, not a new component file).

### Static gap-indicator -> block mapping (page-model.ts)

| line | route | gap kpi | block that addresses |
|------|-------|---------|----------------------|
| 218 | `/operate/errors` | `"Endpoint gap", "1", "admin errors endpoint", "missing"` | Block 5 |
| 218 | `/operate/errors` | `"Open groups", "derived", "from error logs"` | Block 5 |
| 231 | `/operate/traces` | `"Trace endpoint", "missing", "runtime query"` | Block 3 |
| 231 | `/operate/traces` | `"Span tree", "thin", "from run context"` | Block 3 |
| 257 | `/operate/cache` | `"Flush", "hidden", "no endpoint", "gap"` | Block 1.5 (`POST /llm/cache/flush`) |
| 296 | `/operate/notifications` | `"Mute", "gap", "policy endpoint", "missing"` | Block 1.5 (`POST /notifications/{id}/mute`) |
| 296 | `/operate/notifications` | `"Channels", "env", "thin"` | Block 6.4 |
| 335 | `/operate/health` | `"Deep stats", "missing", "PG/certs/workers"` | Block 1.3 (`/admin/db/health`) + Block 4 |
| 385 | `/build/reasoners` | `"Analytics", "deferred", "cost/latency", "gap"` | Block 6.1 |
| 398 | `/build/tools` | `"Usage", "deferred", "analytics", "gap"` | Block 6.2 |
| 421 | `/build/harnesses` | `"Disable", "deferred", "per agent", "gap"` | **NOT addressed by any block** |
| 431 | `/build/crons` | `"Trigger now", "missing", "endpoint", "gap"` | Block 1.5 (`POST /crons/{id}/trigger`) |
| 454 | `/build/modules` | `"Migrations", "visible", "status", "thin"` | NOT addressed |
| 474 | `/build/data/sql` | `"History", "local", "thin"` | NOT addressed |
| 507 | `/build/data/search` | `"Stats", "missing", "index stats", "gap"` | Block 6.3 (= Block 1.5 duplicate) |
| 520 | `/build/feature-flags` | `"Overrides", "deferred", "tenant history", "gap"` | Out-of-scope (per block doc) |
| 561 | `/customers/api-keys` | `"Rotate", "revoke+issue", "gap"` | Block 1.5 (`POST /admin/keys/{id}/rotate`) |
| 581 | `/customers/sessions` | `"Sessions", "degraded", "adapter capability"` + `"Logout", "hidden", "gap"` | Out-of-scope (per block doc) |
| 614 | `/customers/oauth` | `"Refresh history", "missing", "endpoint", "gap"` | Block 6.5 |
| 644 | `/setup/auth` | `"Capabilities", "pending", "runtime endpoint", "gap"` | Out-of-scope (per block doc) |
| 654 | `/setup/llm` | `"Gateway", "adapter", "moving", "caveat"` | Partly Block 4 (provider-health) |
| 684 | `/setup/notifications` | `"Channels", "env", "thin"` + `"CRUD", "deferred", "endpoint", "gap"` | Block 6.4 |
| 704 | `/setup/observability` | `"Traces", "missing", "backend query", "thin"` | Block 3 |
| 724 | `/setup/deploy-targets` | `"Provisioning", "missing"` + `"Provider", "missing"` | Out-of-scope (per block doc) |
| 734 | `/setup/brand` | `"Endpoint", "missing", "admin brand", "gap"` | Block 1.5 (`/admin/brand`) |

**Gaps not addressed by any block** (un-tracked work):

- `/build/harnesses` row 421 — "Disable per agent" deferred.
- `/build/modules` row 454 — Migrations status thin.
- `/build/data/sql` row 474 — Query history local/thin.

---

## Anomalies / cleanup items

1. **Block 6.3 duplicates Block 1.5** — both list `GET /api/v1/search/indexes` in `internal/server/search.go`. Pick one block.
2. **Block 6.1 and 6.2 are not "small"** — both require a migration + write-path hook + handler, not just aggregation. The block doc claims "None require new OSS; all aggregate from durable tables" but the durable columns/tables don't exist yet.
3. **Block 1.2 (`/admin/services`) is a prerequisite for Block 7.2** — Home strip remains seeded until 1.2 lands. Block 7 should not be sequenced before Block 1.
4. **Adapter pill (7.1)** is already a rendered component; the block is purely a wiring task — set `model.adapter` on Logs/Traces/Errors/Cost/Health page entries in `page-model.ts`.
