# Capability Matrix — What you can build today, what you still need

Honest assessment from multiple lenses. Goal: an indie or an
enterprise can fork this and ship 99% of AI app shapes without
leaving the stack. Everything missing must be **vendored OSS** or a
small adapter — never hand-rolled where popular OSS exists.

## Philosophy compliance check

Every entry below must satisfy:
1. **Vendor good OSS** — never hand-roll where an option exists
2. **Forkable template** — code-first config, not UI-only
3. **AI-native + general backend in one place** — both must work
4. **Operator + customer share infra** — same DB, same auth, same brand
5. **Production from day one** — multi-replica safe, audited, observable
6. **Multi-tenant from day one** — RLS, per-tenant scope by default

If a gap fixes by adding ONE sidecar + ONE adapter file, ship it.
If a gap needs a whole new architecture, defer.

---

## Lens 1 — App archetype: **Chatbot / conversational AI**

ChatGPT/Claude clones, customer support bots, Replika-style companions.

| Need | Have | Missing | OSS to vendor |
|---|---|---|---|
| Auth + tenants + sign-up | ✅ | — | better-auth (have) |
| Streaming LLM responses | ✅ | — | LiteLLM (have) |
| 100+ models routed | ✅ | — | LiteLLM (have) |
| Cost per message | ✅ | — | our cost ledger |
| **Conversation threads** | ❌ | First-class table | Add `suite_threads` + `suite_messages`. No good OSS to vendor; this is small. |
| **Auto-summarize old turns** | ❌ | When thread > N tokens | Wrap in our memory module |
| **Message persistence + retrieval** | ⚠️ | Memory has it but no thread-shaped query | Add `messages` view on top of memory |
| **Voice input** | ❌ | STT endpoint | LiteLLM supports `/audio/transcriptions` — just enable |
| **Voice output** | ❌ | TTS endpoint | LiteLLM supports it |
| **Image input** (vision) | ✅ | — | LiteLLM (have) |
| **Image output (avatars)** | ⚠️ | gen endpoint exists, no async UI | Add a job queue wrapper |
| **Function calling / tools** | ✅ | — | OpenAI compat (have) |
| **Per-user rate limit** (free tier abuse) | ❌ | Currently per-tenant only | Extend ratelimit module |
| **Per-user budget** | ❌ | Same | Extend cost module |
| **Realtime voice** | ❌ | OpenAI Realtime API | LiteLLM has it; needs WebSocket relay |
| **Chat UI primitives** | ⚠️ | Basic in customer-app | Could vendor [assistant-ui](https://github.com/Yonom/assistant-ui) |
| **Markdown + code rendering** | ✅ | react-markdown in code-helper | (have) |

**Verdict:** Can build a chatbot today. Missing the thread schema + per-user limits to do it well at scale.

---

## Lens 2 — App archetype: **Agent / autonomous tools** (Devin, SWE agents, browser agents)

| Need | Have | Missing | OSS to vendor |
|---|---|---|---|
| Multi-step agent runtime | ✅ | — | AgentField (have) |
| Sandbox for tool execution | ✅ | — | docker/gVisor/Firecracker/e2b (have) |
| MCP server hosting | ✅ | — | MCP in agent container (just refactored) |
| Harnesses (Claude Code etc) | ✅ | — | Now in agent container |
| Long-running jobs | ✅ | — | River (have) |
| **Sub-task DAG visualization** | ❌ | We render runs flat | [LangFuse](https://github.com/langfuse/langfuse) — embed as sidecar |
| **Per-step tool-call trace** | ❌ | No "what tool ran at step 3" view | LangFuse OR own table |
| **Real-time progress streaming** | ⚠️ | SSE works for LLM, not generic tasks | [Centrifugo](https://github.com/centrifugal/centrifugo) sidecar |
| **Approval gates / human-in-loop** | ❌ | No "pause for human" primitive | Add `suite_approvals` table + dashboard tab |
| **Re-run from any step** | ❌ | Standard agent-debug need | Schema + dashboard work |
| **Browser automation** | ❌ | No Playwright sandbox | [Browserless](https://www.browserless.io/) sidecar OR Playwright in agent container |
| **Code execution sandbox** | ✅ | — | Sandbox adapter (have) |
| **File system tool** | ⚠️ | Storage adapter exists, no FS-like API | Trivial wrapper |
| **Web search tool** | ❌ | — | [SearXNG](https://github.com/searxng/searxng) sidecar OR adapter for Tavily/Brave |

**Verdict:** Strong foundation. Missing LangFuse-style observability + approval gates. Both ~2 days to add.

---

## Lens 3 — App archetype: **RAG / knowledge apps**

Search-your-docs, customer support KB, internal AI search.

| Need | Have | Missing | OSS to vendor |
|---|---|---|---|
| Vector store | ✅ | — | pgvector (have) |
| Vector search API | ✅ | — | memory module (have) |
| Scoped namespaces | ✅ | — | tenant/user/agent/run scope (have) |
| **Document parsing (PDF, DOCX, HTML)** | ❌ | — | [Unstructured](https://github.com/Unstructured-IO/unstructured) OR [Marker](https://github.com/VikParuchuri/marker) sidecar |
| **Web scraping** | ❌ | — | [Firecrawl](https://github.com/mendableai/firecrawl) sidecar (now OSS) |
| **Chunking strategies** | ❌ | Currently caller's responsibility | Wrap LangChain text splitters |
| **Auto-embed on insert** | ❌ | Manual via memory.put | Add a `suite_collections` abstraction + trigger |
| **Re-ranking** | ❌ | Top-k vector only | [BGE-rerank](https://huggingface.co/BAAI/bge-reranker-large) via LiteLLM /rerank |
| **Hybrid search (vector + FTS)** | ❌ | — | pg_trgm + RRF fusion (PG built-in) |
| **Citation tracking** | ⚠️ | Source field exists, no UI surfacing | Schema + UI |
| **Document versioning** | ❌ | — | Adapter pattern; small |
| **Multi-tenant collections** | ✅ | — | (have via scope) |
| **Auto-update on source change** | ❌ | — | Cron + webhook from source system |

**Verdict:** The vector store is solid but the **document ingestion pipeline doesn't exist**. This is the single biggest gap for RAG apps. Firecrawl + Unstructured + a chunker = a real RAG stack in 3 days.

---

## Lens 4 — App archetype: **Content generation** (writing, code, marketing)

| Need | Have | Missing | OSS to vendor |
|---|---|---|---|
| LLM gateway w/ provider choice | ✅ | — | LiteLLM (have) |
| Cost tracking | ✅ | — | (have) |
| Output storage | ✅ | — | storage adapter (have) |
| **Prompt versioning** | ❌ | — | [Langfuse Prompt Management](https://langfuse.com/docs/prompts/) OR build our own (small) |
| **Prompt A/B testing** | ❌ | — | Same |
| **Output eval** (LLM-as-judge) | ❌ | — | [Promptfoo](https://github.com/promptfoo/promptfoo) sidecar |
| **Generation history per user** | ⚠️ | We have cost events but no "browse my generations" view | Schema + UI |
| **Edit / regenerate flow** | ❌ | — | Customer-app pattern |
| **Templates / presets** | ❌ | — | Workload module |
| **Brand voice / system prompt mgmt** | ❌ | — | Same as prompt versioning |
| **Streaming UI** | ✅ | — | (have) |

**Verdict:** Backend is fine. Missing prompt management — a critical gap that every content app reinvents.

---

## Lens 5 — App archetype: **Image / video / multimodal**

| Need | Have | Missing | OSS to vendor |
|---|---|---|---|
| Image gen endpoint | ⚠️ | Exists but sync | Wrap in jobs queue |
| Multimodal LLM input | ✅ | — | LiteLLM (have) |
| Storage with signed URLs | ✅ | — | (have) |
| **Async gen with job status** | ❌ | — | Wire jobs module to image endpoint |
| **Image transforms** (resize, crop) | ❌ | — | [imgproxy](https://imgproxy.net/) sidecar (OSS) |
| **CDN integration** | ❌ | Docs only | Reference deploy with CloudFront |
| **Video gen** | ❌ | — | LiteLLM doesn't proxy; need direct Runway/Pika adapter |
| **Audio processing (ffmpeg)** | ❌ | — | ffmpeg sidecar (alpine-ffmpeg image) |
| **Watermarking** | ❌ | — | imgproxy can do it |
| **Direct browser upload (presigned PUT)** | ❌ | — | Trivial endpoint; standard pattern |

**Verdict:** Image generation works but lacks async + transforms. Audio/video need a workload module.

---

## Lens 6 — App archetype: **Voice AI**

| Need | Have | Missing | OSS to vendor |
|---|---|---|---|
| **STT (transcription)** | ❌ | — | LiteLLM supports `/audio/transcriptions` (whisper, deepgram) — just enable |
| **TTS** | ❌ | — | LiteLLM supports `/audio/speech` (openai, elevenlabs) — just enable |
| **Realtime voice (OpenAI Realtime)** | ❌ | — | LiteLLM has it; WebSocket relay needed |
| **Speaker diarization** | ❌ | — | pyannote.audio in an agent container |
| **Voice activity detection** | ❌ | — | webrtcvad sidecar OR client-side |
| **Audio storage** | ✅ | — | (have via storage) |
| **Streaming audio playback** | ❌ | — | Standard browser API; docs only |

**Verdict:** Three endpoints away from working voice support — all via LiteLLM that's already wired.

---

## Lens 7 — Stakeholder: **Indie hacker / solo builder**

What they care about: zero-cost development, fast time-to-first-call,
not having to manage infrastructure on day 1.

| Need | Status |
|---|---|
| `docker compose up` works | ✅ |
| Free providers via OpenRouter | ✅ |
| Customer-app shell to fork | ✅ |
| Operator dashboard for ops | ✅ |
| Auth + signup flow | ✅ |
| First call in 60 seconds | ✅ |
| **Tutorial videos** | ❌ — recording brief exists, video doesn't |
| **Starter templates** for common patterns | ⚠️ 3 examples, want 15+ |
| **Deploy in one click** to Fly/Railway | ✅ |
| **Cost forecasting** before bill arrives | ⚠️ basic |
| **No-credit-card local dev** | ✅ (OpenRouter has free models) |

**Verdict:** Strong. Could use more example templates + videos.

---

## Lens 8 — Stakeholder: **B2B SaaS startup with paying customers**

What they care about: multi-tenancy, per-customer cost control,
billing, audit.

| Need | Status |
|---|---|
| Multi-tenant RLS | ✅ |
| Per-tenant cost ledger | ✅ |
| Per-tenant budgets | ✅ |
| **Per-USER budgets within a tenant** | ❌ — needs `(tenant_id, user_id)` budget key |
| **Per-API-key rate limits** | ❌ — currently per-tenant only |
| Stripe billing (live) | ✅ when key set |
| Usage meters | ✅ |
| **Self-serve plan upgrade** | ⚠️ Stripe Portal but no in-app upsell |
| **Free tier with hard cap** | ⚠️ Can set budget but no "after N calls, stop" |
| Audit log | ✅ |
| **API key scopes enforced** | ⚠️ Stored, not consistently enforced in handlers |
| **Service accounts** | ❌ — org-level integration keys, common B2B pattern |
| Email transactional | ✅ via Resend |
| **In-app notifications** | ❌ |
| **Customer support widget hook** | ❌ — Intercom/Crisp embed point |

**Verdict:** Solid plumbing, missing the per-user fairness + service-account patterns.

---

## Lens 9 — Stakeholder: **Enterprise / regulated industry**

What they care about: SSO, compliance, data residency, BYOK,
audit-everything.

| Need | Status |
|---|---|
| Audit log | ✅ |
| Secrets encrypted at rest | ✅ |
| **SSO / SAML** | ❌ — vendor [WorkOS](https://workos.com/) OR [Authentik](https://goauthentik.io/) (OSS) |
| **OIDC** | ⚠️ better-auth has it, not wired |
| **2FA / WebAuthn** | ⚠️ better-auth has it, not wired |
| **RBAC** (beyond owner/member) | ❌ — vendor [Casbin](https://casbin.org/) or [Oso](https://www.osohq.com/) |
| **BYOK** (bring own KMS) | ❌ — adapter pattern |
| **Data encryption at column level** | ❌ — needs middleware |
| **GDPR data export** | ❌ — `POST /admin/tenants/{id}/export` |
| **GDPR data delete (right to erasure)** | ❌ — cascade-delete with tombstones |
| **Data residency** | ⚠️ Helm supports; docs are thin |
| **SOC2 control documentation** | ❌ — paperwork, controls are mostly in place |
| **HIPAA BAA-ready posture** | ❌ — similar |
| **Penetration test procedure** | ✅ docs only |
| **Incident response runbook** | ✅ launch-day runbook covers |
| **Backup + DR drill** | ✅ docs |
| **Multi-region** | ⚠️ active-passive docs needed |

**Verdict:** The technical foundations are there (audit, encryption, RLS), the **compliance paperwork is missing**. Adding SSO + RBAC unblocks ~80% of enterprise procurement.

---

## Lens 10 — Stakeholder: **AI/ML team building internal tools**

What they care about: prompt iteration speed, eval pipelines, model
comparison, data access.

| Need | Status |
|---|---|
| LLM cost analytics | ✅ |
| Model A/B routing | ⚠️ LiteLLM supports per-call model spec, no router-level A/B |
| **Prompt versioning** | ❌ — biggest miss |
| **Eval suite** | ❌ — vendor [Promptfoo](https://github.com/promptfoo/promptfoo) |
| **Trace UI** (LangFuse-style) | ❌ — vendor [LangFuse](https://github.com/langfuse/langfuse) as sidecar |
| **Dataset management** | ❌ — store eval datasets in MinIO + adapter |
| **Embedding pipeline** | ⚠️ exists but manual |
| **Vector visualization** | ❌ — could add a UMAP plot tab |
| **Jupyter notebook access** | ❌ — sandbox could spawn one |
| **Pinned model versions** | ⚠️ LiteLLM config can pin |

**Verdict:** Backend is fine, but data-science DX is thin. **LangFuse + Promptfoo as sidecars** would be high-leverage adds.

---

## Top-of-list fixes (within philosophy)

If we can ship 6 things, the platform covers 99% of AI apps:

1. **Conversation threads** — `suite_threads` + `suite_messages` table, `suite.threads.create/append/get/list` SDK. No OSS to vendor; small (~150 LoC). Unlocks chatbot apps.

2. **Per-user budgets + per-API-key rate limits** — extend existing modules. Unblocks free/paid tier patterns.

3. **Document ingestion pipeline** — Firecrawl (web) + Unstructured (PDF/DOCX) as sidecars. Wire `suite.rag.ingest(source)`. Unlocks RAG apps.

4. **LangFuse sidecar** — single docker container, gives prompt mgmt + eval + trace UI all at once. Wire our cost events to push there. Unlocks content-gen + data team needs.

5. **SSO + RBAC** — Authentik (OSS) for SSO/SAML, Casbin for RBAC. Unlocks enterprise procurement.

6. **Centrifugo sidecar** — WebSocket realtime. Unlocks streaming agent progress, in-app notifications, live presence. Pusher-compat so SDKs are off the shelf.

That's the next 2 weeks of work, mostly sidecar + adapter writing.
Each item satisfies the philosophy: vendor good OSS, code-first
config, shared infra, production-ready, MT-aware.

---

## What stays "you write it"

Some things are genuinely yours to write because no OSS substitutes:

- Your domain logic (agents, prompts, business rules)
- Your customer-app UI (the Next.js you fork)
- Your marketing site
- Your specific workload modules (notes / contracts / whatever)
- Your prompts (we'd manage versions; you write the strings)

The platform's job is everything around that — auth, MT, cost, sandboxes, jobs, webhooks, notifications, observability, deploy. That stack is **complete enough today** that you can start a customer-app and ship.

The 6 gaps above are the difference between "you can build it" and "you don't have to think about it."
