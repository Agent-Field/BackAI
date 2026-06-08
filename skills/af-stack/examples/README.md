# Example Apps — the shape catalog

When the user describes a new app, classify it into one of these shapes
first. The shape determines which edit surfaces and primitives are
used. Then look at the closest existing example walkthrough for the
patterns.

## App shapes (6 categories)

| Shape | What the AI does | Key primitives | Reference example |
|---|---|---|---|
| **Vertical copilot** | Domain expert sitting alongside a user (medical / legal / finance / sales) | RAG · agents · structured output · vertical integrations | (TBD) Halcyon · Harvest |
| **Autonomous worker** | Agent that does work and reports results | Agents · tools · sandboxes · long-horizon state · scheduled work | [`forge.md`](forge.md) reactive case; (TBD) Mercer for parallel |
| **Multimodal generative** | Produces media (text + image + audio + video) | Multimodal gateway · storage · jobs · real-time collab | (TBD) Atelier · Compose |
| **Voice / real-time** | Real-time conversation / phone / live transcription | WebRTC · STT/TTS · low-latency agents · telephony | (TBD) Sundial |
| **Data / analytics** | Talks to data, generates reports, runs queries | SQL tool · sandboxes · embeddings · realtime | (TBD) Atlas |
| **Operational / back-office** | Replaces a human function (finance / ops / legal / HR) | Ingestion · structured extraction · approvals · audit trail | (TBD) Pinion · Specter |

## 12 example apps (catalog)

These are realistic 2026 AI startup ideas. Each is buildable on AF
Stack. Sorted roughly by complexity.

| Name | Pitch | Shape | Comparable products |
|---|---|---|---|
| **Forge** | GitHub PR review co-pilot | Reactive single-shot agent | CodeRabbit · Greptile |
| **DocuChat** | ChatGPT for your team's documents | Vertical copilot (RAG) | Glean · NotebookLM |
| **Halcyon** | AI medical scribe | Vertical copilot + multimodal | Abridge · Nabla |
| **Mercer** | Autonomous outbound SDR | Autonomous worker | Artisan · 11x |
| **Pinion** | AI bookkeeper | Operational | Pilot · Ember |
| **Sundial** | AI receptionist for SMB | Voice / real-time | Vapi · Bland |
| **Atelier** | AI presentation/design studio | Multimodal generative | Gamma · Tome |
| **Compass** | Personal AI life-admin | Autonomous worker (personal) | Martin · Lindy |
| **Harvest** | AI equity research analyst | Vertical copilot (RAG) | Hebbia · AlphaSense |
| **Nexus** | AI app builder (meta) | Autonomous worker (codegen) | Lovable · Bolt |
| **Atlas** | Natural-language BI | Data / analytics | Hex · Julius |
| **Beacon** | AI customer success / churn | Operational + reactive | Catalyst |

## How to use this catalog

**When the user says "build me X":**

1. **Find the closest shape** in the table above.
2. **Look at the reference example walkthrough** if one exists. If not,
   use the patterns from `forge.md` (reactive) as a starting point.
3. **Adapt** — swap the agent reasoner, swap the workload-module routes,
   swap the customer-app pages. The platform-level wiring stays the
   same.
4. **Map primitives** using the table in `SKILL.md`. If any are 🚧
   (roadmap), warn the user and propose a workaround.

## Why one platform handles all of these

Every interesting AI startup needs **6+ of the 8 bands** in `STACK.md`.
The differences between the 12 apps above aren't in the platform layer
— they're in the user's edit surfaces:

| Differs across apps | Same across apps |
|---|---|
| Agent reasoners (the prompts + logic) | Multi-tenancy + RLS |
| Workload module routes (the domain) | Auth + sign-up |
| Customer-app pages (the UX) | LLM gateway + cost ledger |
| Dashboard plugin (the metrics) | Sandboxes / jobs / crons |
| Branding | Webhooks / notifications / billing |
| Adapter choices | Storage / secrets / audit |

That's the wedge. Pulling these together yourself = 6–12 months.
Building on AF Stack = 1–4 weeks per app.

## Pattern library — common compositions

Beyond the 12 named apps, here are the *primitive compositions* that
recur:

### Composition A — Reactive event handler
**Trigger**: webhook in (Stripe / GitHub / Slack / etc.)
**Flow**: webhook → job → agent → result → DB write + callback
**Examples**: Forge, fraud-detection bot, support triage

### Composition B — Scheduled agent run
**Trigger**: cron
**Flow**: cron fires → job → agent (possibly parallel) → results → notification
**Examples**: Daily briefing (Mercury), nightly reconciliation (Pinion), weekly digest

### Composition C — Interactive chat with memory
**Trigger**: customer-app form submit
**Flow**: page → workload module → agent w/ Session memory → streaming response
**Examples**: DocuChat, Halcyon (live note), Compass

### Composition D — Long-running parallel agent fleet
**Trigger**: customer uploads list
**Flow**: queue N jobs → N agents → write back per-item → aggregate
**Examples**: Mercer (1000 prospects), Atelier (slide-per-job)

### Composition E — RAG over private corpus
**Trigger**: customer uploads docs or syncs source
**Flow**: ingest job (parse + chunk + embed) → store in memory → at
query time, retrieve + LLM with context
**Examples**: DocuChat, Halcyon (patient history), Harvest

### Composition F — Tool-using agent
**Trigger**: customer query
**Flow**: customer page → workload module → agent with declared tools
(SQL / browser / fs / exec) → tool results → final answer
**Examples**: Atlas (SQL), Mercer (browser + email), Nexus (code exec)

### Composition G — Multi-modal generation pipeline
**Trigger**: customer prompt
**Flow**: customer page → workload module → agent invokes multimodal
(TTS / image / video) in parallel → assemble → store → notify
**Examples**: Atelier, Compose (podcast)

### Composition H — Real-time / voice
**Trigger**: WebRTC / phone call
**Flow**: streaming audio in → STT → agent (low-latency) → TTS →
streaming audio out
**Examples**: Sundial, Halcyon (live)

### Composition I — Approval-gated action
**Trigger**: any of the above triggers a sensitive action
**Flow**: agent proposes → `suite.approvals.request()` (Phase 3) → blocks
→ operator approves → execute
**Examples**: Pinion (high-value transfer), Specter (regulatory filing)

### Composition J — Streaming event processor
**Trigger**: continuous webhook / change stream
**Flow**: events arrive → workload module classifies → batches relevant
ones → triggers agents → results → notify
**Examples**: Beacon (churn signals)

Pick the composition(s) that match what the user describes; combine if
needed.

## Per-shape primitive checklist

When building a new app, check off the primitives the shape needs.

### Vertical copilot (RAG-shaped)
- [ ] Storage (doc upload)
- [ ] Embeddings (Phase 2; or LiteLLM /embeddings today)
- [ ] Memory (vector search)
- [ ] Agents (the synth reasoner)
- [ ] Customer App pages (upload, chat)
- [ ] Workload Module (ingest, search, ask routes)
- [ ] Optional: webhooks-out for "your doc is ready" notifications

### Autonomous worker
- [ ] Agents (multi-step + harness)
- [ ] Sandboxes (for tools execution)
- [ ] Jobs + crons (scheduled runs)
- [ ] Memory (long-horizon per-target state)
- [ ] OAuth-on-behalf (Phase 2; for acting in 3rd party APIs)
- [ ] Tool adapters (Phase 2; browser/search/etc.)

### Multimodal generative
- [ ] Multimodal API (Phase 2; TTS / STT / image)
- [ ] Storage (assets)
- [ ] Jobs (parallel generation)
- [ ] Customer App (preview / edit UI)
- [ ] Realtime (Phase 2; collab editing)

### Voice / real-time
- [ ] WebRTC integration (LiveKit / Twilio adapter)
- [ ] Multimodal (STT / TTS)
- [ ] Agents (low-latency reasoners)
- [ ] Telephony provider (Twilio / Vapi)

### Data / analytics
- [ ] SQL tool (Phase 2; or via MCP today)
- [ ] Sandboxes (query execution)
- [ ] Realtime (Phase 2; live charts)
- [ ] Embeddings (schema understanding)

### Operational / back-office
- [ ] Webhooks IN (event streams)
- [ ] Approvals (Phase 3; gates on actions)
- [ ] Audit log (full history)
- [ ] Memory (per-account history)
- [ ] Notifications + Billing

## Adding a new example

When the user ships a new app on AF Stack and wants to document the
pattern:

1. Build the app in their fork (`apps/customer-app/`, `apps/backend/agents/<name>/`,
   `examples/<name>/`, `apps/dashboard/plugins/<name>/`).
2. Write a walkthrough modeled after `forge.md`.
3. Drop the walkthrough into `skills/af-stack/examples/<name>.md`.
4. Add a row to the "12 example apps" table above.
5. Add a row to the relevant "Pattern library" entry if it's a new
   composition.

The skill stays current as the example library grows.
