# Completeness Audit — Can 99% of AI apps be built on this alone?

Goal: an indie developer or an enterprise team should fork AF Stack
and build their entire backend on it. They only write the front-end (+
custom agents). What are we missing?

This document inventories every recurring problem AI app builders hit
in 2026, maps to what we have / what's missing, and lists OSS we can
vendor.

## Inventory: what people building AI apps actually need

### A. LLM-native primitives

| Need | Have? | Notes |
|---|---|---|
| OpenAI-compatible gateway | ✅ | Customers use OpenAI SDK pointed at our `/api/v1/llm` |
| Multi-provider routing | 🔄 In flight | LiteLLM swap underway — 100+ providers |
| Cost ledger per call | ✅ | `suite_cost_events` row per call |
| Per-tenant budget enforcement | ✅ | Pre-call hook gates over-budget calls with 402 |
| **Per-USER budget (within a tenant)** | ❌ | Add: budgets keyed on `(tenant_id, user_id)`. Critical for B2C apps with free/paid users. |
| **Per-API-key rate limits** | ❌ | We have per-tenant rate limits, not per-key. Add: key-scoped buckets. |
| Streaming SSE | ✅ | Pass-through via OpenAI compat |
| Function / tool calling | ✅ | Pass-through |
| Vision / multimodal input | ✅ | Pass-through |
| Embeddings endpoint | ✅ | `/api/v1/llm/embeddings` |
| Image generation | ✅ | `/api/v1/llm/images/generations` |
| **Speech-to-text** | ❌ | Common need (voice notes, transcription). LiteLLM supports it — gateway already covers it once LiteLLM lands. |
| **Text-to-speech** | ❌ | Same as above. |
| Exact-match LLM cache | ✅ | `suite_llm_cache` |
| **Semantic cache** | ❌ | Use [GPTCache](https://github.com/zilliztech/GPTCache) — significant cost savings (15-40% hit rate typical) |
| **Prompt management / versioning** | ❌ | Use [PromptLayer](https://promptlayer.com/) or build our own. Critical for prod — you want to know which prompt version produced which output for any bug. |
| **Prompt A/B testing** | ❌ | Same. Need: route a fraction of traffic to a new prompt, compare outputs. |
| **Eval framework** | ❌ | Use [Promptfoo](https://github.com/promptfoo/promptfoo) or [Inspect AI](https://github.com/UKGovernmentBEIS/inspect_ai). Critical for "did my prompt change make things worse?" detection. |
| **Content moderation / guardrails** | ❌ | Use [Llama Guard](https://huggingface.co/meta-llama/Llama-Guard-3-8B) (via LiteLLM) or [NVIDIA NeMo Guardrails](https://github.com/NVIDIA/NeMo-Guardrails). Without this you ship a vulnerable app. |
| **PII redaction before LLM call** | ❌ | Use [Microsoft Presidio](https://github.com/microsoft/presidio). Required for any healthcare / fintech / EU customer. |
| **Conversation threads (first-class)** | ❌ | We have memory but no `suite_threads` table with messages. Most chatbot apps reinvent this. |
| **Long-running task UI / job status** | ⚠️ Partial | Sandbox runs have it; general async LLM jobs don't. |
| **Token counting before send** | ✅ | Gateway computes it |
| **Token budget per message** | ⚠️ Partial | Can set `max_tokens`, no soft warning before |
| Provider failover | 🔄 LiteLLM | LiteLLM handles |
| Load balancing across providers | 🔄 LiteLLM | LiteLLM handles |
| Cost forecasting | ✅ | Basic — extrapolate from current run-rate |

### B. RAG / Knowledge primitives

| Need | Have? | Notes |
|---|---|---|
| Vector store | ✅ | pgvector via `suite_memory` |
| Vector search API | ✅ | `/api/v1/memory/search` |
| Scope by tenant / user / agent / run | ✅ | First-class |
| **Document parsing (PDF, DOCX, HTML)** | ❌ | Use [Unstructured](https://github.com/Unstructured-IO/unstructured), [Marker](https://github.com/VikParuchuri/marker), or [Apache Tika](https://tika.apache.org/) |
| **Web scraping for RAG** | ❌ | Use [Firecrawl](https://github.com/mendableai/firecrawl) (now OSS), [Crawl4AI](https://github.com/unclecode/crawl4ai), or [trafilatura](https://github.com/adbar/trafilatura) |
| **Chunking strategies** | ❌ | LangChain or LlamaIndex have standard chunkers. We should expose `app.rag.chunk()` |
| **Embedding pipeline (auto-embed on insert)** | ⚠️ Partial | Memory module supports embeddings but no "embed everything in table X automatically" trigger |
| **Re-ranking after vector search** | ❌ | Use [Cohere Rerank](https://cohere.com/rerank) or [BGE rerank](https://huggingface.co/BAAI/bge-reranker-large) via LiteLLM |
| **Hybrid search (vector + keyword)** | ❌ | pgvector + pg_trgm + RRF fusion. Standard pattern. |
| **Citation / source tracking** | ❌ | Memory records the source field but no first-class "from this query result, which sources contributed" |
| **Document versioning** | ❌ | When the source doc updates, re-embed. Standard pattern; we don't ship it. |

### C. Agent & workflow primitives

| Need | Have? | Notes |
|---|---|---|
| AF Agents (multi-reasoner) | ✅ | AgentField hosts them |
| Tool/function definitions | ✅ | Via OpenAI compat |
| **MCP integration** | 🔄 In flight | Refactor to agent-container hosting |
| Harnesses (Claude Code etc.) | 🔄 In flight | Same refactor |
| Skills (AF skillkit) | ✅ | Install/attach |
| Sandboxes for tool execution | ✅ | 4 adapters |
| **Long-term agent memory** | ✅ | `suite_memory` scoped per agent |
| **Conversation memory / summarization** | ❌ | When a chat thread gets long, auto-summarize older messages |
| **Agent observability (LangFuse-style)** | ⚠️ Partial | We have runs + cost + logs, no per-step trace visualization |
| **Workflow orchestration (multi-step DAG)** | ⚠️ Partial | Jobs queue handles linear flows; no DAG-aware UI |
| **Human-in-the-loop approval** | ❌ | "Pause this agent, wait for a human to click Approve" — common need. Add a `suite_approvals` table + dashboard tab |
| **Tool call tracing** | ❌ | When the LLM invokes tool X with args Y, log the I/O so devs can debug |
| **Re-run from any step** | ❌ | Standard agent-debug pattern |

### D. Real-time + delivery primitives

| Need | Have? | Notes |
|---|---|---|
| Webhooks outbound | 🔄 Svix swap | Reliable delivery + retry + dedup |
| Webhooks inbound | ✅ | HMAC + dedup + forward |
| Background jobs | ✅ | River |
| Cron schedules | ✅ | robfig/cron + scheduler |
| Notifications (email) | ✅ | Outbox + Resend adapter |
| **In-app notifications** | ❌ | Standard pattern — Add `suite_inapp_notifications` + WebSocket push |
| **WebSockets / Realtime** | ❌ | Use [Centrifugo](https://github.com/centrifugal/centrifugo) — Pusher-compatible self-hosted. Critical for chat-style apps. |
| **SMS** | ❌ | Notifications adapter could grow a Twilio adapter |
| **Mobile push notifications** | ❌ | Need a Firebase / Expo adapter or [Knock](https://knock.app/) |
| **Server-Sent Events (SSE) primitive** | ⚠️ Partial | Used for LLM streaming, no general SSE channel |

### E. Auth + tenancy + RBAC

| Need | Have? | Notes |
|---|---|---|
| Email/password | ✅ | better-auth |
| Magic links | ✅ | better-auth |
| Google OAuth | ✅ | Code path; needs keys |
| GitHub OAuth | ⚠️ | Env vars documented, 5-line code-edit to enable |
| **SSO / SAML** | ❌ | Use [WorkOS](https://workos.com/) or [Authentik](https://goauthentik.io/) (OSS). Required for enterprise. |
| **OIDC** | ⚠️ | better-auth has OIDC support, not wired |
| **2FA / TOTP** | ❌ | better-auth has it, not wired |
| **WebAuthn / passkeys** | ❌ | better-auth has it, not wired |
| Multi-tenancy via RLS | ✅ | PG GUC pattern |
| Member roles (owner/admin/member) | ⚠️ Partial | `suite_memberships.role` is a free-form text; no RBAC enforcement yet |
| **RBAC** | ❌ | Use [Casbin](https://casbin.org/) or [Oso](https://www.osohq.com/) (now OSS for self-host). Per-resource permissions, role inheritance |
| **API key scopes** | ⚠️ Partial | Stored but not enforced consistently across handlers |
| API keys | ✅ | bcrypt-hashed, rotatable |
| **Service accounts** | ❌ | Like an API key but for an org-level integration. Common B2B pattern. |
| **Audit log** | ✅ | Newly wired |

### F. Billing + monetization

| Need | Have? | Notes |
|---|---|---|
| Stripe integration | ✅ | When key set |
| Subscription plans | ⚠️ Partial | Schema supports it, no plan-management UI |
| Usage metering | ✅ | `suite_usage_meters` |
| **Usage-based pricing** | ⚠️ Partial | Meters exist; price calculation hard-coded in client code |
| **Free tier with hard caps** | ❌ | Need: per-plan limits in `suite_plans` table that the budget gate reads |
| **Coupons / trials** | ❌ | Stripe supports it, we don't expose it |
| **Lago** as alternative | ❌ | Open-source billing — alternative to Stripe for orgs that hate Stripe fees |
| **Self-serve billing portal** | ✅ | Stripe Portal when live mode |
| **Invoice download** | ❌ | Stripe handles it; we don't surface it in our UI |
| **Multi-currency** | ❌ | Stripe supports; we don't pass through |

### G. Storage + files

| Need | Have? | Notes |
|---|---|---|
| Object storage (S3 + MinIO) | ✅ | Adapter |
| Signed URLs | ✅ | Per-object |
| **Direct browser upload (presigned PUT)** | ❌ | Common need — generate a one-shot URL the browser uses to PUT |
| **Multipart upload** | ❌ | For files > 100MB |
| **Image transforms (resize/crop/format)** | ❌ | Use [imgproxy](https://imgproxy.net/) (OSS) — standard pattern |
| **CDN integration** | ❌ | Document the CloudFront / Cloudflare R2 pattern |
| **Virus scanning on upload** | ❌ | Use [ClamAV](https://www.clamav.net/) sidecar |
| **Content type sniffing** | ❌ | Use [`mimetype`](https://github.com/gabriel-vasile/mimetype) lib |

### H. Search

| Need | Have? | Notes |
|---|---|---|
| Vector search | ✅ | pgvector |
| **Full-text search** | ❌ | PG's `tsvector` is free, just need an API surface. Standard pattern. |
| **Hybrid search (vector + FTS + rerank)** | ❌ | See RAG section |
| **Faceted search** | ❌ | Use [Meilisearch](https://github.com/meilisearch/meilisearch) (OSS) — drop-in for product/document search |

### I. Analytics + product

| Need | Have? | Notes |
|---|---|---|
| **Product analytics events** | ❌ | Use [PostHog](https://github.com/PostHog/posthog) (OSS) — events, funnels, retention |
| **A/B testing framework** | ❌ | PostHog also does this |
| **Feature flags** | ❌ | PostHog does this OR [Unleash](https://github.com/Unleash/unleash) (OSS) |
| Cost analytics | ✅ | Cost tab |
| Per-user analytics | ❌ | Same as above |

### J. Reliability + ops

| Need | Have? | Notes |
|---|---|---|
| Health + readiness probes | ✅ | `/health` + `/ready` with drain semantics |
| Structured logs | ✅ | slog JSON + ring buffer |
| Prometheus metrics | ✅ | `/metrics` |
| OTel tracing | ✅ | Wired |
| Graceful shutdown | ✅ | Ordered drain |
| **Error tracking (Sentry-style)** | ❌ | Use [Glitchtip](https://glitchtip.com/) (OSS) or [Sentry self-hosted](https://github.com/getsentry/self-hosted) |
| **Uptime monitoring** | ❌ | Use [Uptime Kuma](https://github.com/louislam/uptime-kuma) (OSS) |
| **Status page** | ❌ | Use [Statping](https://github.com/statping/statping) (OSS) |
| Backup + restore | ✅ | Docs + scripts |
| Multi-replica safety | ✅ | `FOR UPDATE SKIP LOCKED` |
| **Multi-region** | ❌ | Docs the pattern (read replicas + active-passive failover) |
| **DR procedure** | ✅ | docs/backup-restore.md |

### K. Developer experience

| Need | Have? | Notes |
|---|---|---|
| Python SDK | ✅ | `af_stack.*` |
| TypeScript SDK | ✅ | `@af-stack/sdk` |
| **Go SDK** | ❌ | Common request from go-shops |
| **Rust SDK** | ❌ | Smaller demand |
| **Swift / Kotlin SDKs** | ❌ | For mobile; we recommend OpenAI SDK for now |
| CLI | ✅ | `af-stack` |
| **Local dev hot reload** | ⚠️ | Compose watch supported; not documented |
| **VS Code extension** | ❌ | Common for SaaS templates |
| OpenAPI 3.1 spec | ✅ | Auto-generated + Scalar browser |
| **Postman collection** | ❌ | Generate from OpenAPI |
| **Type generation for clients** | ⚠️ Partial | Scalar can generate; not packaged |

### L. Compliance + enterprise

| Need | Have? | Notes |
|---|---|---|
| Audit log | ✅ | Real |
| Encryption at rest (secrets) | ✅ | AES-256-GCM |
| **Encryption at rest (data tables)** | ❌ | Need: PG TDE config / app-level enc on selected columns |
| **BYOK (Bring Your Own Key)** | ❌ | Enterprise need — KMS instead of our envelope |
| **GDPR data export / delete** | ❌ | Common — need `POST /api/v1/admin/tenants/{id}/export` + `delete` endpoints |
| **Data residency** | ❌ | Multi-region docs |
| **SOC2 / HIPAA / ISO controls docs** | ❌ | The controls are mostly in place (audit log, MT, encryption); the *paperwork* isn't |
| **Penetration test report** | ❌ | Procedure doc; the test itself is per-deploy |
| **Privacy policy boilerplate** | ❌ | Standard template |

### M. Misc commonly-asked-for

| Need | Have? | Notes |
|---|---|---|
| **Email sending volume** | ⚠️ Partial | Resend covers it; deliverability is Resend's job |
| **Onboarding flows** | ❌ | Tour, checklist, sample-data seeder — basic UX patterns |
| **In-app help / docs widget** | ❌ | Embed our docs site |
| **Internationalization (i18n)** | ❌ | Use [next-intl](https://github.com/amannn/next-intl) for the dashboard |
| **Time zones** | ⚠️ Partial | Backend stores UTC, no per-user TZ |
| **API change feed** | ❌ | RSS for "what changed in the API" |
| **Discord / Slack bot template** | ❌ | Common starter — agent + auth + Slack OAuth |
| **AI app templates / scaffolds** | ⚠️ Partial | 3 examples, not 30 |

## Top 20 gaps for 99% coverage

If I had to pick the top 20 things to add for "99% of AI apps" coverage:

1. **LiteLLM** for 100+ providers — in flight
2. **Svix** for reliable webhooks — in flight
3. **Conversation threads + summarization** — `suite_threads` table + auto-summarize after N tokens
4. **Per-user budgets** — `(tenant_id, user_id)` keyed in `suite_budgets`
5. **Per-API-key rate limits** — scoped buckets, not just per-tenant
6. **GPTCache** for semantic LLM cache — 15-40% cost reduction
7. **Document parsing** for RAG — Unstructured + Marker
8. **Web scraping** for RAG — Firecrawl
9. **Prompt management** — `suite_prompts` table with versions + A/B
10. **Eval framework** — Promptfoo integration
11. **Content moderation** — Llama Guard via LiteLLM + Presidio for PII
12. **Hybrid search** — pgvector + tsvector + RRF
13. **WebSockets** — Centrifugo sidecar
14. **In-app notifications** — `suite_inapp_notifications` + WebSocket push
15. **PostHog** — analytics + feature flags + A/B
16. **Sentry / Glitchtip** — error tracking
17. **RBAC** — Casbin/Oso
18. **SSO/SAML** — WorkOS or Authentik
19. **imgproxy** — image transforms
20. **Human-in-the-loop** — `suite_approvals` + dashboard

## What we ship next

**Tier 1 (must-have for v1.1):**
- Top 5 gaps: Conversation threads, per-user budgets, per-API-key rate limits, GPTCache, document parsing
- These are 1-2 days each.

**Tier 2 (next batch):**
- Prompt management, Eval, Moderation, Hybrid search, WebSockets
- These are 2-5 days each.

**Tier 3 (enterprise wave):**
- RBAC, SSO/SAML, GDPR export, BYOK
- These are 1 week each.

## Where this matters

The premise is "an indie can build their AI SaaS on this alone".
Right now we cover the **plumbing** beautifully (auth, MT, billing,
cost, jobs, webhooks, sandboxes, MCP, harnesses) but miss several
**AI-app-specific** primitives (conversation threads, prompt mgmt,
eval, moderation) that every real AI product reinvents.

The fastest way to close this is **vendor the OSS that already
exists** — that's the same lesson from the OSS-AUDIT. The 20 items
above are mostly "spin up sidecar X + add adapter Y", not "build from
scratch".
