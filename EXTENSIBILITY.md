# Completeness — What's built-in, what isn't, what we add

> **Reframe**: We are **not** a plugin platform (WordPress, Strapi,
> Directus). We are a **backend-in-a-box** (Supabase, Firebase, Convex,
> Appwrite) — the AI version. Someone clones the repo, gets every
> primitive they need, attaches their frontend, and ships.
>
> That changes the question from "what extension model do we ship?" to
> **"what do we need to add so the box is actually complete for AI?"**

This doc audits that question.

---

## 1. What it means to be Supabase-shaped

The Supabase / Firebase / Convex / Appwrite mental model:

- **One repo, one deploy** — everything comes with it.
- **Customization is config + SDK calls** — not "install a plugin."
- **The platform ships the primitives**; user code composes them into
  apps. RAG, chat history, evals, doc uploads — those *compositions*
  are user code, not platform extensions.
- **Adapters are config-level** — swap MinIO → S3, log → Resend, Stripe
  → Lago via env. Not a marketplace, not a registry.
- **Examples are educational** — Notable, Shipwright, Deep Research
  *show* compositions; they're not "modules to install."

That's our model. Closing the loop:

| | WordPress model | Supabase model |
|---|---|---|
| Distribution unit | plugins (registry) | first-party features |
| User flow | install plugins | use SDK calls |
| Scope of platform | small core, big extension ecosystem | big core, no ecosystem |
| What we built | — | this |

We were briefly drifting toward the left column. Correcting now.

---

## 2. What Supabase / Firebase / Convex / Appwrite ship built-in

To know what "complete" means for us, here's what they consider
in-the-box for general backends. This is the bar.

| Capability | Supabase | Firebase | Convex | Appwrite |
|---|---|---|---|---|
| Relational DB | ✅ Postgres | (Firestore — doc DB) | ✅ Convex DB | ✅ MariaDB |
| Document DB | (Postgres JSONB) | ✅ Firestore | (Convex objects) | ✅ |
| Auth (email + OAuth) | ✅ GoTrue | ✅ Firebase Auth | ✅ Convex Auth | ✅ |
| Object storage | ✅ Storage | ✅ Cloud Storage | ✅ File storage | ✅ Storage |
| File previews / thumbnails | ✅ on Storage | ✅ Image transforms | ⚠️ | ✅ Image transforms |
| Realtime subscriptions | ✅ WebSocket | ✅ Realtime DB | ✅ Native | ✅ Realtime |
| Presence / multi-user | ⚠️ buildable on Realtime (no first-party API) | — | ⚠️ | ⚠️ |
| Serverless functions | ✅ Edge Functions (Deno) | ✅ Cloud Functions | ✅ Mutations/Actions | ✅ Functions |
| Scheduled / cron | ✅ pg_cron | ✅ Cloud Scheduler | ✅ Crons | ⚠️ |
| Queue / background jobs | ✅ pgmq | ✅ Cloud Tasks | ⚠️ | ⚠️ |
| Full-text search | ✅ PG FTS | (Algolia partnership) | ✅ Built-in | ⚠️ |
| Vector search | ✅ pgvector | ✅ Vertex AI | ✅ Native | ⚠️ |
| Email | ✅ via auth | ✅ Extensions | ⚠️ | ✅ Messaging |
| SMS | ⚠️ Twilio extension | ✅ Extensions | ⚠️ | ✅ Messaging |
| Push notifications | ⚠️ Extensions | ✅ FCM | ⚠️ | ✅ Messaging |
| Webhooks (in / out) | ✅ Database Webhooks | ✅ Extensions | ⚠️ | ✅ |
| Multi-tenancy | (RLS) | (data isolation) | (auth-based) | ⚠️ |
| Billing / metering | — | ✅ | — | — |
| Activity log / audit | ✅ pgaudit | ⚠️ | ⚠️ | ⚠️ |
| Analytics | ✅ Logflare | ✅ GA | ⚠️ | — |
| A/B testing / feature flags | — | ✅ Remote Config | — | — |

Then for **AI specifically** — what they've added more recently:

| AI capability | Supabase | Firebase | Convex | Appwrite |
|---|---|---|---|---|
| LLM provider gateway | ⚠️ (DIY) | ✅ Vertex AI | — | — |
| Embeddings API | ✅ AI Studio | ✅ Vertex AI | ⚠️ | — |
| Vector store | ✅ pgvector | ✅ Vertex AI | ✅ Native | — |
| Agent runtime / orchestration | — | ⚠️ Genkit | — | — |
| Tools / MCP | — | ⚠️ Genkit tools | — | — |
| Code sandboxes | — | — | — | — |
| Per-tenant LLM cost / budgets | — | — | — | — |
| Real-time agent runs / spans | — | — | — | — |

This second table is the AI-shaped gap they're not filling well. **That's
the seam we own.** Everything in this table is already in our stack
(STACK.md band ④ + ⑤). We're *ahead* of them on AI. We just need to be
*at parity* with them on the general-backend bits (especially Realtime).

---

## 3. What AF Stack ships built-in today

Map this against your repo state (per STACK.md + audit in PLAN-CLEAN.md):

| Capability | Status | Where |
|---|---|---|
| Postgres + pgvector | ✅ | docker-compose `postgres` |
| Auth (email + OAuth + magic links) | ✅ | better-auth |
| Multi-tenancy (PG RLS) | ✅ | `internal/tenancy/` |
| Object storage | ✅ | MinIO → S3 adapter |
| File previews / thumbnails | ❌ | — |
| **Realtime subscriptions (WebSocket)** | ✅ | Server: `GET /api/v1/realtime` (PG LISTEN/NOTIFY); Python `suite.realtime.subscribe(table, rt_filter)` lazy-loads optional `websockets` pkg; TS SDK ships `subscribe` |
| Presence / multi-user | ❌ | — |
| Serverless / inline functions | ⚠️ | We have Jobs + Crons; no "respond inline to a request" function shape |
| Scheduled / cron | ✅ | robfig/cron |
| Queue / background jobs | ✅ | River |
| Full-text search | ✅ | `POST /api/v1/search` (PG FTS + pgvector hybrid); SDK: `suite.search(q, mode)` (TS + Python) |
| Vector search | ✅ | AgentField memory.similarity_search |
| **Embeddings API** | ✅ | `POST /api/v1/llm/embeddings` (OpenAI-compat, LiteLLM-routed); SDK: `suite.llm.embed(model, input)` |
| Email | ✅ | log → Resend adapter |
| SMS | ❌ | — |
| Push notifications | ❌ | — |
| Webhooks in / out | ✅ | inbound (HMAC + dedup); outbound via Svix |
| Notifications outbox | ✅ | `internal/notifications/` |
| Billing | ✅ | Stripe; Lago adapter in STRATEGY |
| Audit log | ✅ | `suite_audit_log` |
| User activity log (user-facing) | ❌ | — (audit is admin-only) |
| Analytics | ⚠️ | Cost dashboard + metrics; no user-event analytics |
| A/B testing / feature flags | ⚠️ | Form exists, no-op (CLEANUP-PLAN hides it) |
| LLM provider gateway | ✅ | LiteLLM sidecar (incl. virtual keys, per-key budget + RPM/TPM upstream — item #22) |
| **Multimodal — TTS / STT / image gen** | ✅ | OpenAI catalog via LiteLLM; first-party adapters for ElevenLabs / Cartesia (TTS) + Flux / fal.ai (image). Routes: `/audio/{speech,transcriptions,translations}`, `/images/{generations,edits,variations}`. SDK: `suite.audio.*`, `suite.images.*`. See `docs/multimodal.md`. (item #14) |
| **Voice / real-time audio (WebRTC)** | ❌ | — |
| Agent runtime / orchestration | ✅ | AgentField. Dashboard pattern: link-out to AgentField UI (`:8081`) for DAG / step inspector; inline summary card + control actions (cancel / pause / resume / request-approval) live in af-stack via `GET /api/v1/runs/{id}/agentfield` + sibling POST routes. See `docs/agentfield-integration.md`. |
| Tools / MCP | ✅ | MCP host (stdio + SSE) |
| Agent tool adapters (browser / search / fs / exec / http / sql) | ✅ | First-party set in `services/runtime/internal/tools/`; MCP-callable as `native:<tool>`; per-tenant enable via `suite_tenant_tools`. |
| Code sandboxes | ✅ | docker / gVisor / Firecracker / e2b |
| Per-tenant LLM cost / budgets | ✅ | LiteLLM `/spend/keys` is source of truth; `suite_cost_events` is write-through audit (item #22) |
| Real-time agent runs / spans | ⚠️ | We have run records; not surfaced as a subscription |
| PII redaction / content moderation | ❌ | — (could be a gateway hook) |
| **OAuth-on-behalf-of-user** (for agent tools — Composio shape) | ✅ | GitHub + Google shipped; Notion / Slack / Linear stubbed |

The gaps cluster into two categories:

- **General-backend parity** (Supabase has them, we don't): Realtime,
  presence, file previews, SMS, push, FTS API, user activity log,
  feature flags.
- **AI-specific completeness** (no one ships these well today): embeddings
  shim, multimodal adapters, voice / real-time audio, agent tool
  adapters, PII redaction, OAuth-on-behalf, real-time run subscriptions.

---

## 4. What we'd add to be "complete" for AI

Honest take: there's a ~10-feature shortlist that takes us from "Phase
16 launch-ready" to "you can build any AI app on this without forking."

Grouped by what they unlock.

### 4.1 General-backend parity (catch up to Supabase)

These aren't AI-specific. They're table-stakes for a SaaS backend that
people will build product on top of.

| # | What | Why it matters | Build size |
|---|---|---|---|
| 1 | **Realtime** ✅ — Postgres LISTEN/NOTIFY → WebSocket bridge at `GET /api/v1/realtime`. SDK: `suite.realtime.subscribe(table, rt_filter)`. *Server shipped; Python SDK lazy-imports the optional `websockets` package.* | Live dashboards, live agent-run UIs, collab features, push-based UX. Every Supabase tutorial uses this. | shipped |
| 2 | **Search API** ✅ — REST shim over PG FTS + pgvector hybrid. `suite.search(q, mode: text\|vector\|hybrid)` in both TS and Python SDKs. | Built into the platform, not "extension." Hybrid search is what AI apps actually want. | shipped (1 week) |
| 3 | **User activity log** — `suite_user_activity` table + SDK. App-side equivalent of `suite_audit_log` for user-visible events. | Most apps want "X happened to your account" timelines. Currently they'd reinvent this. | ½ week |
| 4 | **Feature flags + remote config** — wire the existing form into a runtime endpoint (`/api/v1/config/flags`). | A working SaaS needs this. We have the dashboard form; just wire it. | ½ week |
| 5 | **File transforms** — thumbnail / resize / format-convert on storage GETs. `suite.storage.url(key, { width, height })`. | Supabase has this; we don't. Used by every app with avatars / image uploads. | 1 week |

### 4.2 AI-specific completeness (our differentiator)

The features that, missing them, force the user to write significant
code or install something external.

| # | What | Why it matters | Build size |
|---|---|---|---|
| 6 | **Embeddings API** ✅ — `POST /api/v1/llm/embeddings` shim, OpenAI-compatible, routed through LiteLLM. Exposed in the Python SDK as `suite.llm.embed(model, input)`. | Today the user has to go through AgentField memory to embed. A standalone embeddings call matches Supabase AI Studio and unlocks pure-RAG apps that don't want the memory abstraction. | shipped (½ week) |
| 7 | **Multimodal API** ✅ — `/api/v1/audio/{speech,transcriptions,translations}`, `/api/v1/images/{generations,edits,variations}`. OpenAI-compatible. LiteLLM handles the OpenAI catalog (tts-1, whisper-1, dall-e-{2,3}, gpt-image-1); first-party adapters cover ElevenLabs + Cartesia (TTS) and Flux + fal.ai (image). SDK: `suite.audio.{speech,transcribe,translate}`, `suite.images.{generate,edit,variations}`. Cost tracked per-modality in `suite_cost_events.modality`. See `docs/multimodal.md`. | Voice / image gen / podcasts — built-in. Today the user wires their own provider. | shipped (#14) |
| 8 | **Realtime run subscriptions** ✅ — `suite.runs.subscribe({tenant_id, user_id, agent, run_id, execution_id})` over WebSocket at `GET /api/v1/realtime/runs`. Bridges AgentField's per-execution + global SSE streams with server-side tenant/agent/user filtering; emits `run.started` / `run.step` / `run.completed` / `run.error` envelopes. Polled snapshot fallback for finished runs. SDK in Python + TS. See `docs/realtime-runs.md`. | Lets the customer-facing app render "agent is doing X" UIs without polling. Big for Shipwright. | shipped (#15) |
| 9 | **Agent tool adapters** (browser / search / fs / exec / HTTP / SQL) ✅ — first-party set in `services/runtime/internal/tools/`. Adapters: browser-use primary (+ Steel / Playwright stubs), SearXNG primary (+ Tavily / Brave / Exa / DuckDuckGo stubs), sandbox-fs, sandbox-exec, safehttp-http, read-only SQL. Per-tenant enable via `suite_tenant_tools`. MCP-callable via `app.mcp.call("native:<tool>", "<verb>", {...})`. | Agents that can act are 10× more valuable than agents that can talk. | shipped (2 weeks) |
| 10 | **PII redaction + moderation as gateway hooks** — built-in pre/post LLM hooks, config-driven. Adapter: regex (default) → AWS Comprehend / Presidio for production. | Without this, every app reinvents prompt sanitization. Make it free. | 1 week |
| 11 | **OAuth-on-behalf-of-user** ✅ — `services/runtime/internal/oauth/` ships GitHub + Google (working) and Notion / Slack / Linear (stubs). Tokens encrypted in the secrets vault, refresh-on-expiry, CSRF-signed state. SDK: `suite.oauth.{authorize_url, connected, token, disconnect}`. Customer-app `/integrations` page wired. See `docs/oauth.md`. | Unlocks the entire class of "personal agent" apps. Today not doable without big custom code. | shipped |

### 4.3 What stays user code (firm boundary)

These are *compositions* of the primitives. They're not built-in and
should not be — they're app-specific.

| Thing | Why it's user code | What we provide |
|---|---|---|
| RAG pipeline | Different per domain: legal vs support vs code. Parsers, chunkers, prompts all differ. | Storage + embeddings + vector search + agents + cron — they wire it. |
| Document upload UI | App-specific UX. | Storage SDK + signed URLs + file transforms. |
| Chat session / conversation history | AgentField Session scope memory already does this; the UI is app-specific. | AgentField memory. |
| Eval framework | What "good" means differs entirely by app. | Jobs + agents + storage — they wire it. |
| Voice agent app | Different latency / protocol / UI choices per domain. | Multimodal API + WebRTC adapter (we'd add an adapter, not a UI). |
| Knowledge base UI | App-specific UX. | Embeddings + search + storage. |
| Slack / Discord bot | Each one has different command shapes. | Webhooks + agents + tool adapters. |

### 4.4 What stays an adapter swap (not extensions)

Config-level choices. Operator picks one via env var. We ship the
adapters first-party so people don't have to write them.

| Primitive | Adapters we ship |
|---|---|
| Storage | minio, s3, r2, gcs, azure-blob |
| Sandbox | docker, gvisor, firecracker, e2b, modal (future) |
| Notifications email | log, resend, postmark, sendgrid, ses, mailgun |
| Notifications SMS | log, twilio |
| Notifications push | log, fcm, onesignal |
| Billing | stripe, lago, none |
| Auth providers | google, github, discord, microsoft (better-auth handles) |
| Vector backend | pgvector (built-in via AgentField) |
| Search backend | pg-fts (built-in) |
| PII redaction | regex (default), presidio, aws-comprehend |
| TTS | openai, elevenlabs, cartesia, deepgram |
| STT | openai (whisper), deepgram, assemblyai |
| Image gen | flux, dalle3, fal.ai, replicate |

Each adapter is a small implementation behind a single interface. *Not*
a plugin you install; a config choice you flip.

---

## 5. Completeness scorecard

Where AF Stack lands relative to Supabase for *general backends* and
relative to *no-one* for AI:

| Dimension | AF Stack today | After 4.1+4.2 (~10 features) | Supabase | Firebase |
|---|---|---|---|---|
| Multi-tenant SaaS backend | 8/10 | 10/10 | 9/10 | 6/10 |
| Real-time + collab apps | 3/10 | 9/10 | 9/10 | 9/10 |
| LLM app (chat / RAG) | 7/10 | 10/10 | 5/10 | 6/10 |
| Agent app (tools / sandboxes) | 6/10 | 10/10 | 1/10 | 4/10 |
| Voice / multimodal | 1/10 | 7/10 | 1/10 | 5/10 |
| Personal agent (OAuth on behalf) | 1/10 | 8/10 | 0/10 | 1/10 |
| Enterprise (SSO / RBAC / GDPR) | 5/10 | 8/10 (with Tier 3) | 7/10 | 8/10 |

The path to a 9-10 across the board: ~10 features, ~10 weeks of focused
work. That's STRATEGY.md's existing Tier 1 (LiteLLM virtual keys,
Billing, Shipwright, AgentField data, Approvals) **plus the 11 items
above**. Roughly an 8–10 week scope on top of what was already planned.

---

## 6. Five "any AI app" DX walkthroughs

How the developer builds these on the completed platform. Every example
uses *only* SDK calls — no extensions, no forks.

### 6.1 A RAG app for legal contracts

```python
# User uploads a contract via the customer-app frontend
async def upload_contract(file, tenant_id):
    # Storage primitive
    obj = await suite.storage.put(f"contracts/{file.name}", file.bytes)

    # Embeddings primitive (built-in, shipped — 4.2 #6 ✅)
    chunks = chunk_text(extract_text(file.bytes))  # user code
    vectors = await suite.llm.embed(
        model="openai/text-embedding-3-small",
        input=chunks,
    )

    # Memory primitive (AgentField)
    for chunk, vec in zip(chunks, vectors):
        await suite.memory.put(
            scope="actor", scope_id=tenant_id,
            key=f"contract:{obj.key}:chunk:{chunk.idx}",
            value=chunk, embedding=vec,
        )
    return obj.key

# Search uses the search primitive (shipped)
async def search_contracts(query, tenant_id):
    return await suite.search(
        query=query,
        mode="hybrid",
        scope="actor", scope_id=tenant_id,
        k=10,
    )
```

No "RAG framework." Three primitives, ~30 lines of user code.

### 6.2 A voice agent for customer support

```python
# Realtime audio in, agent reasons, audio out (needs Multimodal API — not yet)
@app.websocket("/voice")
async def voice_session(ws):
    async for audio_chunk in ws.iter_bytes():
        # STT primitive
        text = await suite.audio.transcribe(audio_chunk)
        # Agent (AgentField) — has tools (needs Agent tool adapters — not yet) for SQL, Slack, etc.
        reply = await suite.agents.call("support.handle", {"text": text})
        # TTS primitive
        audio = await suite.audio.speak(reply.text, voice="...")
        await ws.send_bytes(audio)
```

### 6.3 A live agent-run viewer for Shipwright

```tsx
// Realtime run subscription — `suite.runs.subscribe` returns a WebSocket
// streaming {"type": "run.started" | "run.step" | "run.completed" | ...}
// envelopes. See docs/realtime-runs.md.
import { suite, type RunEvent } from "@af-stack/sdk"

function ShipwrightTaskView({ runId }) {
  const [events, setEvents] = React.useState<RunEvent[]>([])
  React.useEffect(() => {
    const ws = suite.runs.subscribe({ run_id: runId })
    ws.addEventListener("message", (e) => {
      setEvents((prev) => [...prev, JSON.parse(e.data)])
    })
    return () => ws.close()
  }, [runId])
  return <DAGView events={events} />
}
```

### 6.4 A personal Notion agent

```python
# OAuth on behalf (needs OAuth-on-behalf — not yet)
@app.handler("/notion/connect")
async def connect_notion(user_id):
    return suite.oauth.authorize_url(
        provider="notion", user_id=user_id,
        scopes=["read_content", "update_content"],
    )

# Agent acts as the user
async def notion_agent(user_id, task):
    return await suite.agents.call(
        "notion.organize",
        input={"task": task},
        oauth_user_id=user_id,  # AgentField + tool adapter picks up the token
    )
```

### 6.5 A multi-user collab whiteboard with AI co-pilot

```ts
// Realtime presence + change feeds (uses Realtime — shipped)
const board = suite.realtime.channel(`board:${boardId}`)
board.on("presence", users => render(users))
board.on("change", change => applyChange(change))

// AI co-pilot writes back into the same channel
async function aiSuggest(prompt) {
  const result = await suite.agents.call("board.suggest", { prompt })
  board.broadcast({ kind: "ai-suggestion", ...result })
}
```

Five different app shapes. Zero forks. Zero plugin installs. Every
primitive is `suite.*`.

---

## 7. The frame in one paragraph

> **AF Stack is the Supabase for AI** — clone the repo, get every
> primitive a production AI app needs (Postgres / vector / auth /
> storage / realtime / search / functions / jobs / sandboxes / LLM
> gateway / agents / tools / multimodal / voice / cost ledger /
> billing / observability), attach a frontend, ship. Customization is
> SDK calls and one-env-var adapter swaps, not plugin installs. Domain
> compositions (RAG, eval, chat, knowledge bases, voice agents) live in
> user code; we ship reference example apps to show how. The boundary
> we hold: **anything that takes user code to compose stays user code;
> anything that's a platform service belongs in core.**

---

## 8. The decision asks

Three calls to make, then I'll write the spec / update STRATEGY.

### Q1. Adopt the 4.1 + 4.2 list as "v1.1 completeness"?

Eleven features, ~8–10 weeks on top of STRATEGY Tier 1. Get us from
"Phase 16 launch-ready" to "any AI app builds on this without forking."

- **Adopt all 11** ➡ aggressive but clean — we hit "Supabase for AI" by
  v1.1
- **Adopt 4.1 only (5 items)** ➡ catch up to Supabase on general
  backend; AI completeness stays for v1.2
- **Adopt 4.2 only (6 items)** ➡ bet on AI seam; risk being a worse
  Supabase for non-AI work
- **Pick a subset** ➡ I propose which (I'd say: skip #11 OAuth-on-behalf
  for v1.1 — it's two weeks alone and useful only for personal-agent
  apps; defer to v1.2)

### Q2. The "user code" boundary

I drew a firm line: RAG / eval / chat / voice-agent-app / knowledge-base
UI / Slack bot = user code. We provide primitives, ship example apps.
Confirm or push back.

- **Confirm** ➡ I add the boundary to STRATEGY.md as the non-goal list
- **Push back** ➡ specify which of these you want as first-party

### Q3. Adapter expansion

Section 4.4 listed adapter sets for: TTS (4), STT (3), image gen (4),
SMS, push, PII redaction. That's ~15 small adapters to write.

- **Ship one per category in v1.1** (cheapest path; e.g. just OpenAI
  Whisper for STT, ElevenLabs for TTS, Flux for image gen)
- **Ship a complete adapter set per category** (more work, sets the
  pattern that we're complete)
- **Defer multimodal adapters entirely** — wait for someone to ask

---

## 9. What I'd write next (after you decide)

Given Q1/Q2/Q3 answers:

1. Update `STRATEGY.md` to insert the new features as **Tier 0
   completeness** (before current Tier 1) — because Realtime + Embeddings
   + Tool adapters block the customer-facing demos.
2. Per-feature spec docs as needed (Realtime needs one; Tool adapters
   need one; the rest are small).
3. Update `STACK.md`'s service row (band ④ / ⑤ / ⑥) to include the new
   first-party services.
4. Update `PRODUCT.md` "What's REAL in v1" table for the new built-ins.

Total grooming → spec → ship: 8–10 weeks for everything; 4 weeks if we
do 4.1 only.
