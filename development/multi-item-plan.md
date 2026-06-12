# Multi-Item Execution Plan (8 items)

Tracking doc for the 8 items in the active goal. Each section has the
detailed plan; items in flight reference this doc.

## Order

```
✅ #31  internal/memory dedup audit         DONE — suite_memory stays canonical
✅ #22  LiteLLM virtual keys                DONE — admin client + mirror; unblocks #14 + #32
✅ #16  Agent tool adapters                 DONE — internal/tools/ shipped with native: MCP namespace
✅ #18  OAuth-on-behalf-of-user             DONE — GitHub + Google shipped; Notion/Slack/Linear stubbed
✅ #15  Realtime run subscriptions          DONE — `suite.runs.subscribe`, WS at `/api/v1/realtime/runs`
✅ #25  AgentField data in dashboard        DONE — link-out + inline summary
✅ #14  Multimodal API                      DONE — audio/speech, transcriptions, translations + images/generations, edits, variations; adapters: litellm, elevenlabs, cartesia, flux, fal; modality cost tracking
✅ #32  Kill internal/ratelimit             DONE — ratelimit package deleted; LiteLLM 429 + Retry-After + X-RateLimit-* proxied through
```

5 sub-agents running concurrently. They edit non-overlapping file scopes;
the two overlaps (`internal/agentfield/client.go` between #15 and #25;
`packages/sdk-py/af_stack/__init__.py` between #15/#16/#18) are
additive-append-only and explicitly noted in the briefs.

## #31 — internal/memory dedup audit (RESOLVED)

**Verdict**: Don't deprecate `suite_memory`. Keep as canonical store.

- af-stack `suite_memory` has 5 scopes (Global / Tenant / Agent / Session / Run)
- AgentField has 4 (Global / Actor / Session / Workflow)
- af-stack-unique: **Tenant** (multi-tenant) + **Agent** (per-agent)
- AgentField memory is NOT currently exposed to af-stack runtime
  (`agentfield.Client` only has `Execute()` — no memory routes)
- All examples (Notable verified) use `suite.memory.*` → `suite_memory`
- Bridging AgentField memory requires AgentField repo changes — punt

**Implication for #25**: dashboard memory tab already reads `suite_memory`
which IS the de facto canonical store. #25 scope reduces to runs / spans
/ DAG view only.

## #22 — LiteLLM virtual keys (Wave 1) — DONE ✅

**Landed:**
- Migration `00020_api_keys_litellm.sql` adds `litellm_key_alias`,
  `litellm_key_hash`, `budget_max_usd`, `rate_limit_rpm`, `rate_limit_tpm`
  to `suite_api_keys`.
- `services/runtime/internal/llmgateway/litellm_admin.go` is the admin
  client (`/key/generate`, `/key/update`, `/key/delete`, `/key/info`,
  `/spend/keys`, `/spend/users`) — fully unit-tested with a fake
  upstream.
- `services/runtime/internal/tenancy/litellm_mirror.go` introduces a
  `LiteLLMMirror` + `SecretSink` interface so the manager can mint a
  LiteLLM virtual key per AF Stack api key without taking a hard
  dep on `llmgateway` / `secrets`. The LiteLLM secret is stored in
  the vault at `litellm/key/{api_key_id}`; only alias + SHA-256 hash
  live on the row.
- `IssueAPIKey` accepts `BudgetMaxUSD`, `RateLimitRPM`, `RateLimitTPM`.
  Failure of the LiteLLM call rolls back the `suite_api_keys` insert
  atomically. `RevokeAPIKey` deletes the upstream key (best-effort).
- LLM gateway threads the per-tenant LiteLLM secret on `context.Context`
  (`llmgateway.WithLiteLLMKey`). The provider HTTP client uses it as
  the auth header so LiteLLM enforces budget + rate limit upstream and
  attributes spend to the right virtual key.
- `cost.Aggregate.FromLiteLLM` reads live spend; `suite_cost_events`
  is now documented as write-through audit.
- `apps/dashboard` adds Budget / RPM / Spent columns to the API keys
  view + the issue dialog has Budget USD, RPM, TPM fields. `api.ts`
  exposes `api.admin.keys.spend(id)` and an `APIKeySpend` schema.
- Backward compat: legacy rows without LiteLLM mapping continue to
  work (provider falls back to `LITELLM_MASTER_KEY`, dashboard shows
  em-dashes).
- New tests: `litellm_admin_test.go`, `litellm_mirror_test.go`,
  `auth_context_test.go`. Existing 700+ tests still pass.

Original brief follows.



**Goal**: Per-user budgets + per-key rate limits enforced upstream by
LiteLLM. Drop the runtime's in-memory limiter.

**File touch list**:
- `services/runtime/internal/tenancy/manager.go` — IssueAPIKey adds POST to LiteLLM `/key/new`
- `services/runtime/internal/llmgateway/litellm_admin.go` (new) — admin client for `/key/*`, `/spend/*`
- `services/runtime/internal/cost/aggregate.go` — pull from LiteLLM `/spend/keys/{key}` + `/spend/users/{user}`
- `apps/dashboard/src/app/(admin)/customers/api-keys/_components/api-keys-view.tsx` — show budget + rate limit columns
- `apps/dashboard/src/app/(admin)/customers/api-keys/_components/issue-api-key-dialog.tsx` — budget + rate limit fields
- `services/runtime/internal/server/admin.go` — wire issue dialog to new fields
- DB migration: `services/runtime/internal/db/migrations/NNNN_api_keys_litellm.sql` — add `litellm_key_id`, `budget_max_usd`, `rate_limit_rpm` columns

**Acceptance**:
- Issuing a new API key creates a corresponding LiteLLM virtual key
- Dashboard shows "Budget remaining: $X / $Y" and "Rate limit: N rpm"
  per key, pulled live from LiteLLM
- LLM gateway requests use the customer's LiteLLM key
- `suite_cost_events` table becomes write-through audit (LiteLLM is source of truth)
- Existing keys without LiteLLM mapping continue to work (backward compat)

**Dependencies**: LiteLLM sidecar must be running (already in docker-compose) ✅

## #16 — Agent tool adapters (Wave 1)

**Goal**: First-party tool adapter set in `services/runtime/internal/tools/`,
MCP-callable, per-tenant enable.

**File layout**:
```
services/runtime/internal/tools/
  interface.go              # Tool interface
  registry.go               # tool registry + per-tenant enable
  adapters/
    browser/
      interface.go          # BrowserAdapter
      browseruse/           # primary impl
      steel/                # alt
      playwright/           # alt
    search/
      interface.go          # SearchAdapter
      searxng/              # primary
      tavily/               # paid
      brave/                # paid
      exa/                  # paid
      duckduckgo/           # zero-key
    fs/
      sandbox.go            # wraps suite.sandbox + storage
    exec/
      sandbox.go            # wraps suite.sandbox
    http/
      safehttp.go           # wraps internal/safehttp
    sql/
      readonly.go           # read-only over PG
```

**Routes added**:
- `GET /api/v1/tools/adapters` — list available adapters per tool type
- `POST /api/v1/tools/adapters/{type}/configure` — set adapter for tenant
- `POST /api/v1/tools/call` (already exists for MCP — extend to native tools)

**SDK**:
- `suite.tools.list_adapters()` 
- `suite.tools.configure(tool_type, adapter, config)`
- `suite.tools.call_native(tool, args)`

**Dashboard**:
- `/build/integrations` becomes the tool adapter management page
- Per-tenant toggle for each tool

**Acceptance**:
- At least 1 adapter shipped per tool type (browser-use, SearXNG, sandbox-fs, sandbox-exec, safehttp, readonly-SQL)
- MCP-callable via `app.mcp.call("native:browser", "navigate", {url: "..."})` from inside agent
- Per-tenant enable wired in dashboard

**Dependencies**: None — new directory, no conflicts.

## #18 — OAuth-on-behalf-of-user (Wave 1)

**Goal**: Composio-shape OAuth for agents to act as the user in 3rd-party APIs.

**File layout**:
```
services/runtime/internal/oauth/
  interface.go              # OAuthProvider interface
  manager.go                # token storage + refresh + revoke
  adapters/
    google/                 # Calendar, Drive, Gmail
    github/                 # repos, issues, PRs
    notion/                 # pages
    slack/                  # messaging
    linear/                 # issues
DB migration: services/runtime/internal/db/migrations/NNNN_oauth_tokens.sql
  - suite_oauth_tokens (tenant_id, user_id, provider, access_token, refresh_token, expires_at, scopes)
```

**Routes**:
- `GET /api/v1/oauth/{provider}/authorize` — redirect to provider
- `GET /api/v1/oauth/{provider}/callback` — exchange code, store tokens
- `GET /api/v1/oauth/connected` — which providers does this user have connected
- `DELETE /api/v1/oauth/{provider}` — disconnect

**SDK**:
- `suite.oauth.authorize_url(provider, scopes, return_url)`
- `suite.oauth.token(provider, user_id)` — returns valid access token (auto-refreshes)
- `suite.oauth.disconnect(provider, user_id)`

**Customer-app pages**:
- `apps/customer-app/src/app/(app)/integrations/page.tsx` — list connected apps + connect buttons

**Acceptance**:
- 2 providers minimum (Google + GitHub)
- OAuth flow works end-to-end
- Tokens stored encrypted via secrets vault
- Auto-refresh on expiry
- `suite.oauth.token()` from a workload module returns a fresh access token

**Dependencies**: None — new directory, no conflicts.

## #14 — Multimodal API (Wave 2 — after #22)

**Goal**: OpenAI-compatible `POST /api/v1/audio/speech`, `/audio/transcriptions`,
`/images/generations`. LiteLLM-routed where it covers; first-party adapters
for ElevenLabs / Cartesia / Flux / fal.ai where LiteLLM doesn't.

**Why post-#22**: both touch `internal/llmgateway/`. Wait for #22 to land
its files first, then add multimodal as parallel handlers.

**Detailed implementation plan**:

### Routes (OpenAI-compatible, 1:1 with the OpenAI API)

```
POST /api/v1/audio/speech              ← TTS; body: {model, input, voice, response_format}
POST /api/v1/audio/transcriptions       ← STT; multipart form: file, model, language
POST /api/v1/audio/translations         ← STT + translate; same as above
POST /api/v1/images/generations         ← image gen; body: {model, prompt, n, size}
POST /api/v1/images/edits              ← image edit; multipart: image, mask, prompt
POST /api/v1/images/variations         ← image variation; multipart: image, n
```

All routes mirror the OpenAI shape exactly so `openai.OpenAI(base_url=AF_STACK_URL)` works without modification.

### Provider routing

The runtime inspects the `model` parameter to pick the provider:
- `openai/tts-1`, `openai/tts-1-hd`, `openai/whisper-1`, `openai/dall-e-3`, `openai/gpt-image-1` → LiteLLM (which routes to OpenAI)
- `elevenlabs/<voice-id>`, `elevenlabs/turbo`, `elevenlabs/multilingual` → first-party ElevenLabs adapter
- `cartesia/sonic`, `cartesia/sonic-english` → first-party Cartesia adapter
- `flux/schnell`, `flux/pro`, `flux/dev` → first-party Flux adapter (via fal.ai or Replicate)
- `fal/<model-name>` → first-party fal.ai adapter (catch-all for image + video)

If LiteLLM supports it directly (Whisper, TTS-1, DALL-E), prefer LiteLLM. Otherwise use the first-party adapter. The boundary is: **the more provider-key we save the operator from managing themselves, the better.**

### File layout

```
services/runtime/internal/llmgateway/
├── multimodal.go             # the three POST handlers
├── multimodal_routes.go      # route registration
└── adapters/
    ├── interface.go          # MultimodalAdapter interface
    ├── litellm/
    │   ├── tts.go            # delegates to LiteLLM /audio/speech
    │   ├── stt.go            # delegates to LiteLLM /audio/transcriptions
    │   └── image.go          # delegates to LiteLLM /images/generations
    ├── elevenlabs/
    │   └── tts.go            # POST https://api.elevenlabs.io/v1/text-to-speech/{voice_id}
    ├── cartesia/
    │   └── tts.go            # POST https://api.cartesia.ai/tts/bytes
    ├── flux/
    │   └── image.go          # via fal.ai or Replicate, depending on env
    └── fal/
        └── image.go          # POST https://fal.run/{model}
```

### Adapter interface

```go
type MultimodalAdapter interface {
    Name() string
    SupportsTTS() bool
    SupportsSTT() bool
    SupportsImage() bool
    Speech(ctx, req SpeechRequest) (SpeechResponse, error)        // TTS
    Transcribe(ctx, req TranscribeRequest) (TranscribeResponse, error)  // STT
    Image(ctx, req ImageRequest) (ImageResponse, error)            // image gen
}
```

Adapters that don't support a verb return `ErrUnsupported`.

### env (operator picks providers via key presence)

- `OPENAI_API_KEY` (already set) — enables OpenAI multimodal via LiteLLM
- `ELEVENLABS_API_KEY` — enables ElevenLabs
- `CARTESIA_API_KEY` — enables Cartesia
- `FAL_API_KEY` — enables fal.ai
- `REPLICATE_API_KEY` — enables Replicate (alternative for Flux)

Each adapter checks its key at construction; missing key → adapter excluded from registry → models routed to it return clear 503 with "configure ELEVENLABS_API_KEY".

### LiteLLM config additions

`apps/backend/litellm-config.yaml` — add `model_list` entries:

```yaml
model_list:
  # ... existing entries ...

  # Audio — TTS
  - model_name: openai/tts-1
    litellm_params:
      model: tts-1
      api_key: os.environ/OPENAI_API_KEY
  - model_name: openai/tts-1-hd
    litellm_params:
      model: tts-1-hd
      api_key: os.environ/OPENAI_API_KEY

  # Audio — STT
  - model_name: openai/whisper-1
    litellm_params:
      model: whisper-1
      api_key: os.environ/OPENAI_API_KEY

  # Images
  - model_name: openai/dall-e-3
    litellm_params:
      model: dall-e-3
      api_key: os.environ/OPENAI_API_KEY
  - model_name: openai/gpt-image-1
    litellm_params:
      model: gpt-image-1
      api_key: os.environ/OPENAI_API_KEY
```

### Cost tracking

Extend `suite_cost_events` with a `modality` column (`'text' | 'audio_speech' | 'audio_transcription' | 'image' | 'video'`).

Add migration `services/runtime/internal/db/migrations/NNNN_cost_events_modality.sql`:

```sql
ALTER TABLE suite_cost_events
    ADD COLUMN IF NOT EXISTS modality text NOT NULL DEFAULT 'text';
CREATE INDEX IF NOT EXISTS suite_cost_events_modality_idx
    ON suite_cost_events (tenant_id, modality, created_at DESC);
```

Cost calculation per modality:
- TTS: `cost_per_char × characters` (provider-specific rate)
- STT: `cost_per_minute × audio_seconds / 60`
- Image: `cost_per_image × n` (varies by model and size)

Add per-provider pricing to `services/runtime/internal/pricing/pricing.go`.

### Python SDK — `suite.audio.*` and `suite.images.*`

`packages/sdk-py/af_stack/audio.py`:

```python
async def speech(model: str, input: str, voice: str = "alloy",
                 response_format: str = "mp3") -> bytes:
    """Generate speech from text. Returns audio bytes."""

async def transcribe(model: str, file: bytes | IO[bytes],
                     language: str | None = None) -> TranscriptionResult:
    """Transcribe audio to text."""

async def translate(model: str, file: bytes | IO[bytes]) -> TranscriptionResult:
    """Transcribe + translate to English."""
```

`packages/sdk-py/af_stack/images.py`:

```python
async def generate(model: str, prompt: str, n: int = 1,
                   size: str = "1024x1024",
                   response_format: str = "url") -> ImageGenerationResult:
    """Generate images from prompt."""

async def edit(model: str, image: bytes, prompt: str,
               mask: bytes | None = None,
               n: int = 1, size: str = "1024x1024") -> ImageGenerationResult:
    """Edit an image with a prompt."""

async def variations(model: str, image: bytes, n: int = 1,
                     size: str = "1024x1024") -> ImageGenerationResult:
    """Generate variations of an image."""
```

Models match OpenAI's Pydantic shape (so `openai` SDK works against our endpoint).

### TS SDK

`packages/sdk-ts/src/audio.ts` + `images.ts` — same surface as Python.

### Tests

- Adapter unit tests for each provider (HTTP fakes)
- Integration test: `openai.OpenAI(base_url=AF_STACK_URL).audio.speech.create(...)` works
- Cost test: TTS call → cost_event with modality='audio_speech'
- Missing-key test: model points to adapter without key → 503 with clear message

### Docs

- Update `development/strategy.md` Phase 2 / docs/extensibility.md §4.2 #7 → ✅
- Update `skills/af-stack/SKILL.md` primitives table — Multimodal from "Yet to ship" → Tier 1 ✅
- Update `development/multi-item-plan.md` #14 status → ✅
- Add `docs/multimodal.md` with provider matrix + how to add a new adapter
- Update `docs/oss-audit.md` — add ElevenLabs / Cartesia / Flux / fal as vendored choices

### Quality gates

- `go build ./...`, `go vet ./...`, tests pass
- Python + TS SDK lint clean
- LiteLLM proxy starts cleanly with the expanded model_list

### Don't

- Don't break existing `suite.llm.chat()` or `suite.llm.embed()` — multimodal is additive
- Don't ship adapters that require docker-compose changes (no new services). Operator pays for ElevenLabs etc., we just speak HTTP
- Don't ship video gen yet — it's a different cost shape, defer to v1.2
- Don't commit. Leave staged.

## #25 — AgentField data in dashboard (Wave 2) — ✅ DONE

**Status**: shipped via link-out + inline summary (Wave 1 execution).

**Goal**: Surface AgentField run / span / tool-call data in the operator dashboard.

**Scope (post-#31 audit + AgentField inspection)**: AgentField's control
plane already ships its own UI at `:8081/agentic/run/{run_id}` and
`/executions/{execution_id}/details` with full DAG + step inspector. Per
`docs/architecture.md`'s "don't rebuild what's already excellent" rule, we
**link out** to AgentField's UI for deep inspection and **inline the
summary** (status, duration, cost, agent name) in af-stack's runs list.

Available AgentField endpoints (verified in
`agentfield/.../control-plane/internal/server/routes_*.go`):
- `GET /agentic/run/:run_id` — run overview
- `GET /executions/:execution_id` — execution status
- `GET /executions/:execution_id/details` — full details (their UI uses this)
- `GET /executions/:execution_id/notes` — execution notes
- `GET /executions/:execution_id/approval-status` — approval flow
- `POST /executions/:execution_id/cancel|pause|resume|request-approval`

Memory: NOT needed per #31 audit — `suite_memory` is the canonical store.

**File touch list** (smaller scope after AgentField inspection):
- `services/runtime/internal/agentfield/client.go` — add:
  - `GetRunOverview(ctx, run_id)` — proxies `GET /agentic/run/:run_id`
  - `GetExecutionDetails(ctx, execution_id)` — proxies `GET /executions/:execution_id/details`
  - `CancelExecution(ctx, execution_id)` — proxies cancel
  - `RequestApproval(ctx, execution_id)` — proxies approval request
- `services/runtime/internal/server/runs.go` (extend) — new routes:
  - `GET /api/v1/runs/{id}/agentfield` — returns AgentField's run overview + a `agentfield_url` for the deep-view link
  - `POST /api/v1/runs/{id}/cancel` — delegates to AgentField cancel
- `apps/dashboard/src/app/(admin)/operate/runs/[id]/_components/`:
  - `run-detail-view.tsx` (extend existing) — add "View in AgentField" button that opens `${AF_URL}/executions/{execution_id}/details` in a new tab
  - `run-summary-card.tsx` (new) — status / duration / cost / agent / approval-status (from AgentField summary)
  - `run-actions.tsx` (new) — Cancel / Pause / Resume buttons (proxy to AgentField)

**Acceptance**:
- Click a run in af-stack dashboard → see summary card with AgentField data inline
- "View in AgentField" button opens the deep view in AgentField's UI
- Cancel / pause / resume work via af-stack routes that proxy to AgentField
- If AgentField is unreachable, fail gracefully (gray out buttons, show "AgentField unavailable" badge)

## #15 — Realtime run subscriptions (DONE)

**Status**: ✅ shipped.

**Approach**: bridge AgentField's existing SSE streams — verified that
AgentField emits run events on `GET /api/v1/executions/:execution_id/events`
(per-execution) and `GET /api/ui/v1/executions/events` (global). The
runtime adds a WebSocket at `/api/v1/realtime/runs` that fans the right
upstream into a wire-protocol envelope (`run.started` / `run.step` /
`run.completed` / `run.error`). Polled snapshot fallback (`PollExecutionSnapshot`)
covers the case where a run finished before the subscriber connected,
or AgentField's UI module is disabled.

**Files touched**:
- `services/runtime/internal/server/runs.go` (new) — `handleRunsSubscribe`,
  filter, SSE→WS relay, wire-protocol mapping
- `services/runtime/internal/server/runs_test.go` (new) — unit tests
  for filter parsing, payload mapping, tenant scoping; integration
  test against a fake AgentField SSE server
- `services/runtime/internal/agentfield/client.go` — added
  `StreamExecutionEventsByID`, `StreamAllExecutionEvents`,
  `PollExecutionSnapshot`, `ExecutionSnapshot` (additive; existing
  `StreamRunEvents` kept untouched)
- `services/runtime/internal/server/server.go` — route + OpenAPI registration
- `packages/sdk-py/af_stack/runs.py` (new) — `subscribe()` async iterator
- `packages/sdk-py/af_stack/__init__.py` — re-export
- `packages/sdk-py/tests/test_runs.py` (new)
- `packages/sdk-ts/src/runs.ts` — added `subscribe(filter, opts)` + types;
  renamed legacy per-run helper to `subscribeById` (kept the
  `subscribeRunEvents` export alias so old callers still resolve)
- `packages/sdk-ts/src/index.ts` — re-exports
- `packages/sdk-ts/tests/runs.test.ts` — covers both legacy and new shape
- `apps/dashboard/src/app/(admin)/operate/runs/_components/live-runs-strip.tsx`
  (new) — operator demo
- `apps/dashboard/src/app/(admin)/operate/runs/page.tsx` — mounts demo
- `docs/realtime-runs.md` (new), `docs/extensibility.md`, `skills/af-stack/SKILL.md`

## #32 — Kill internal/ratelimit (Wave 3 — post-#22)

**Goal**: Drop the runtime's in-memory token-bucket limiter. LiteLLM enforces
per-key rate limits upstream (via virtual keys' RPM/TPM limits from #22).

**Detailed plan**:

### Step 1 — Verify #22 actually enforces

Before deleting anything, verify that an LLM call from a client with a
LiteLLM virtual key (issued via #22) DOES get a 429 when the RPM limit
is exceeded. Run a small smoke test:

```bash
# Issue a key with rpm=2
curl -X POST localhost:8080/api/v1/admin/api-keys \
  -H "Content-Type: application/json" \
  -d '{"name":"smoke-test","budget_max_usd":1,"rate_limit_rpm":2}'

# Hit the LLM endpoint 5 times rapidly
for i in {1..5}; do
  curl -X POST localhost:8080/api/v1/llm/chat/completions \
    -H "Authorization: Bearer <key>" \
    -d '{"model":"qwen/...","messages":[{"role":"user","content":"hi"}]}'
done
```

Expect: first 2 succeed, next 3 return 429. If not, abort #32 — #22 isn't fully working.

### Step 2 — File deletions

```
services/runtime/internal/ratelimit/
  ├── ratelimit.go               DELETE
  ├── ratelimit_test.go          DELETE
  └── (any sub-files)            DELETE
```

### Step 3 — Reference removals

```
services/runtime/cmd/af-stack/main.go
  - Remove ratelimit import
  - Remove ratelimit.New(...) construction
  - Remove ratelimit from any deps struct passed downstream

services/runtime/internal/server/server.go
  - Remove ratelimit import
  - Remove the rate-limit middleware from the request chain
  - Remove ratelimit field from Server struct
  - Remove the constructor parameter

services/runtime/internal/server/llm.go
  - Remove the in-process token-bucket check on POST /api/v1/llm/*
  - Surface 429s from LiteLLM with the X-RateLimit-* headers proxied through

services/runtime/internal/hooks/  (if any hook handler used ratelimit)
  - Remove llm.pre_call handler that called ratelimit.Check
```

### Step 4 — Surface upstream 429s nicely

When LiteLLM returns 429 (because virtual-key RPM limit hit):
- Proxy `Retry-After` header through
- Proxy `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` headers through (LiteLLM emits these)
- The error envelope returns `{"error":{"code":"RATE_LIMIT_EXCEEDED","message":"...","retry_after":N}}`

This is **better** than what the local limiter did (it returned `429 too many requests` with no headers).

### Step 5 — Tests

- Delete `ratelimit_test.go`
- Add an integration test in `internal/server/llm_test.go`: configure LiteLLM with a low RPM key → hit limit → assert 429 with `Retry-After` header passed through
- Run all tests: `go test ./...` should pass

### Step 6 — Docs

- Update `development/strategy.md` Tier 1 #1 — note that the in-memory limiter is removed
- Update `docs/oss-audit.md` — note that rate limiting is now upstream (LiteLLM owns it)
- Update `skills/af-stack/rules/sdk.md` — note 429 responses now carry full rate-limit headers
- Update `development/multi-item-plan.md` #32 status → ✅

### Quality gates

- `go build ./...` passes
- `go vet ./...` passes
- All tests pass
- 429 smoke test from Step 1 still produces 429s after the deletion

### Don't

- Don't delete ratelimit if #22 isn't actually enforcing. Verify first.
- Don't add a new local rate limiter. The whole point is upstream enforcement.
- Don't change `suite.llm.chat()` SDK behavior — the wire format is unchanged.
- Don't commit. Leave staged.

**Dependencies**: hard-blocked on #22 completion. The Step 1 smoke test gates this.

## Status updates

This doc tracks status. Each subagent / wave updates the order block at top.
