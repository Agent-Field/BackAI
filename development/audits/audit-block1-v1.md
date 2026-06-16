# Block 1 Audit — execution-blocks-v1.md vs. actual codebase

Audited 2026-06-15 against `supportdesk-first-dx` branch.

---

## 1.1 `/api/v1/admin/adapters` mounting

**ALREADY DONE** — mounted at `services/runtime/internal/server/admin_adapters.go:13`, with OpenAPI registration at line 25 and full test coverage in `admin_adapters_test.go` (covers GET + OpenAPI presence). Doc instruction to "add to `cmd/af-stack/main.go`" is stale — it's wired through `Server.register*` not `main.go`.

## 1.2 `GET /api/v1/admin/services` (Connected Services synth)

**CLEAN — does not exist.** No `services.go` handler. `Server` package has no `/admin/services` mux entry (verified). The adapter registry (`internal/adapters/registry/registry.go:179-204`) already exposes per-slot status/version/admin-UI via `SlotView`/`ActiveView` — significant overlap with the proposed `/admin/services` shape. **Recommendation/hazard the doc misses**: the new endpoint should consume the registry's `Probe`/`StatusTTL` cache (`registry.go:240-274`) rather than reinventing fast health checks. Otherwise we end up with two parallel probe systems for the same OSS backends.

## 1.3 `GET /api/v1/admin/db/health`

**CLEAN — does not exist.** No `pg_stat*` queries anywhere in `services/runtime/`. **HAZARD the doc misses**: docker-compose Postgres image is `pgvector/pgvector:pg16` (`docker-compose.yml:16`) which does NOT preload `pg_stat_statements`. The extension binaries ship with the postgres image, but without `shared_preload_libraries=pg_stat_statements` in `postgresql.conf`, `CREATE EXTENSION` silently collects no stats. Block 1 must either (a) add a `command:` override or mount a `postgresql.conf` in compose, or (b) gracefully report `available: false` not just on missing extension but on missing shared-preload (which is what users will actually hit).

## 1.4 LLM provider availability poller

**CLEAN — does not exist.** No poller, no `suite_provider_health_log` migration (last migration is `00026_cost_event_request_id.sql`). No `health`/`Health` references in `internal/llmgateway/litellm_admin.go` or `litellm_provider.go`. **HAZARD**: LiteLLM's `/health` endpoint iterates every configured upstream provider sequentially and routinely takes 10-60 s; a naive 60 s poller can stack. Use `/health/readiness` (cheap) for liveness and `/health` only on a longer cadence or behind a separate goroutine with bounded concurrency.

## 1.5 `POST /api/v1/crons/{id}/trigger`

**CLEAN — does not exist.** `internal/server/crons.go:45-51` registers only list / create / get / set-active / delete. **HAZARD CONFIRMED**: scheduler uses `JobEnqueuer` interface (`internal/crons/interface.go:71-76`); the wiring already exists in `cmd/af-stack/main.go:86-100, 1188` (`cronJobEnqueuer.Enqueue → jobsManager.Enqueue`). The new handler must call this — NOT update `next_run_at` in `suite_crons`. The doc table at line 145 says only "Manual cron execution"; that needs an explicit cross-reference to the JobEnqueuer wiring.

## 1.5 `POST /api/v1/llm/cache/flush`

**CLEAN — does not exist.** `internal/server/llm.go:175` only registers `GET /api/v1/llm/cache/stats`. **HAZARD**: `llmcache.Cache` has `Evict(ctx)` (`internal/llmcache/cache.go:265-275`) but it only deletes rows where `expires_at < now`. There is NO existing API to flush ALL rows or to filter by `tenant` / `prompt_hash`. Implementing this endpoint requires extending the `Cache` interface (e.g., `Flush(ctx, FlushFilter)`) — not a pure wire-up.

## 1.5 `POST /api/v1/admin/keys/{id}/rotate`

**CLEAN — does not exist.** Routes registered in `internal/server/admin.go:66-69` are list / issue / delete / spend; no rotate. `tenancy.Manager` has `IssueKey` (`manager.go:1030`) and `RevokeKey` (`manager.go:1231`) but no `Rotate`. **HAZARD**: issuing a key also mirrors to LiteLLM (`manager.go:1020-1028`) and revoke does best-effort upstream cleanup (`manager.go:1267-1283`, async, non-atomic). A naive "revoke then issue" sequence inside one HTTP handler is NOT atomic — the LiteLLM mirror cleanup goroutine and the new key's LiteLLM mint can race. Either add a real `tenancy.Manager.Rotate` that serialises both, or document the eventual-consistency window.

## 1.5 `POST /api/v1/notifications/{id}/mute`

**CLEAN — does not exist; semantically broken.** No mute handler in `internal/server/notifications.go:64-68`; no mute concept in `internal/notifications/` (no matches for `Mute`/`silence` anywhere). **HAZARD**: notifications are an outbox (`internal/notifications/interface.go:1-17`) — each row is a one-shot delivery already passing through `queued → sending → sent`. "Mute a sent notification by id" makes no sense — there's nothing to mute. The doc text says "mute future notifications matching pattern" which means: new `suite_notification_mutes` table + filter logic in `Service.Send`. This is materially larger than a "single-shot endpoint."

## 1.5 `GET /api/v1/search/indexes`

**CLEAN — does not exist.** `internal/server/search.go:18-22` registers only `POST /api/v1/search`, `PUT /search/documents`, and `DELETE /search/documents/{ns}/{key}`. No index-stats handler anywhere. `page-model.ts:514` already references the missing endpoint by name ("`GET /api/v1/search/indexes`"). The page-model just needs flipping once it lands.

## 1.5 `GET /api/v1/admin/brand` + optional `PUT`

**CLEAN — does not exist.** No `brand`-named handler in `services/runtime/internal/server/`. `brand.yaml` lives at repo root (`/brand.yaml`). **HAZARD**: customer-app reads brand at BUILD TIME via `pnpm run generate:brand` (`apps/customer-app/package.json:6-8` runs as `predev`/`prebuild`) and writes generated `src/app/brand.css` + `src/lib/brand.ts`. Dockerfile copies `brand.yaml` into the image (`apps/customer-app/Dockerfile:27-28`). A `PUT /admin/brand` will NOT live-update the customer-app — it would only mutate the host file, and the customer-app would still serve the brand baked into its build. The doc treats this as runtime-mutable when it is currently build-time. Either de-scope to read-only, or land a regeneration/HUP path first.

---

## 1.6 Frontend wiring — page-model.ts vs. new components

The new admin is data-driven; once endpoints land, **all of these are one-line edits in `page-model.ts`** (flip `kpi("X", "missing", ...)` → `kpi("X", "backed", ...)` and drop the corresponding gap `card`), plus method additions to `lib/api.ts`, plus zod schemas, plus `data.ts:settle(() => api.X())` additions. No new React components needed for:

- **Trigger now** — `page-model.ts:431, 438` (`Build → Crons`).
- **Cache flush** — `page-model.ts:264` (Operate → Cache).
- **Search indexes stats** — `page-model.ts:507, 514` (Build → Data → Search).
- **DB deep stats** — `page-model.ts:335` (`Operate → Health` "Deep stats" kpi). Doc instead says "add a Health tab on `Build → Data → SQL`" — that IS new tab markup (a `tabs(...)` control plus row mapping), but still inside `page-model.ts` not a new component.
- **API key revoke** — already `backed`; rotate is a new row action (`page-model.ts:614` shows `Refresh history` as the current `missing` kpi, not rotate).
- **Notifications mute** — `page-model.ts:296` (Operate → Notifications).
- **Brand editable** — needs a new edit affordance; current page is read-only by design (`development/ui-plan-v1.md:108`). Possibly a new component if free-form YAML editing is desired.

`api.admin.adapters.list()` already exists (`lib/api.ts:2448-2450`); `api.crons.*`, `api.notifications.*`, `api.search.*`, `api.llm.cacheStats()` already exist. Block 1's frontend work is genuinely a thin layer — but only if the endpoints land first.

---

## Corrections needed in execution-blocks-v1.md

1. **§1.1**: delete the "needs mounting" instruction; the handler is mounted via `Server.registerAdminAdaptersRoutes`, not `main.go`. Update verification line to reference `admin_adapters.go`.
2. **§1.2**: add a "must consume adapter registry's `Probe`/`StatusTTL` cache" note; otherwise we duplicate probe infrastructure.
3. **§1.3**: add HAZARD on `pg_stat_statements`: requires `shared_preload_libraries=pg_stat_statements` in `postgresql.conf` (or `command:` override in `docker-compose.yml:13-30`). Bundled binary is not enough.
4. **§1.4**: add HAZARD on LiteLLM `/health` cost (10-60 s per call when iterating providers); recommend `/health/readiness` for the fast path.
5. **§1.5 crons**: add explicit "implementation MUST call `crons.JobEnqueuer.Enqueue(ctx, name, args, tenantID)` from `cmd/af-stack/main.go:86-100`'s `cronJobEnqueuer` — do NOT mutate `next_run_at` in `suite_crons`."
6. **§1.5 cache flush**: this is NOT single-shot. Requires extending `llmcache.Cache` with `Flush(ctx, FlushFilter{TenantID, PromptHash})` — Evict only handles expired rows.
7. **§1.5 keys rotate**: add HAZARD on LiteLLM mirror atomicity; spec a `tenancy.Manager.Rotate` instead of "revoke + re-issue" race.
8. **§1.5 notifications mute**: this is materially larger than other items in the table. Requires schema (`suite_notification_mutes`) + filter logic in `Service.Send`. Either upgrade to its own §1.5.x subsection or split into Block 6.
9. **§1.5 brand**: clarify scope. `brand.yaml` is build-time consumed by customer-app (`apps/customer-app/package.json:6-8`); a `PUT` does not live-update without a regeneration step.
10. **§1.6**: rewrite as "page-model + api.ts + data.ts + zod" rather than "row actions wire up" — all are data-driven edits except brand-editing (which is a new component).
