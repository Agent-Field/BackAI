# SupportDesk-First BackAI DX Plan

## Purpose

This document defines the product and implementation plan for simplifying
BackAI/AF Stack around a polished first experience.

The goal is to make the repository viral as an AI app template while keeping
AgentField strategically present as part of the AI substrate. Users should
share, fork, and deploy the app template first. Increased AgentField usage is
an intended second-order effect because builders can inspect the architecture,
trace AI runs, and see AgentField inside the GitHub organization.

## Decision Summary

1. The default product experience is a polished SupportDesk AI app.
2. The headline is the AI app template. "AI backend" is the category
   explanation.
3. The customer app is the first screen. The admin dashboard is a separate
   operator surface revealed during the walkthrough.
4. No-key demo mode is required for local and Railway first-run virality.
5. Railway deploy should include customer app, admin dashboard, runtime, and
   Postgres.
6. Sandbox, S3 uploads, Stripe live billing, OAuth providers, MCP, coding
   harnesses, and Shipwright are advanced capabilities, not first-run
   requirements.
7. AgentField remains a core AI substrate component, not the main product
   brand.
8. The official OpenAI SDK is the fastest LLM DX. Suite SDKs provide
   platform-native helpers for agents, memory, jobs, costs, tenants, and
   admin operations.

## Product Positioning

BackAI is the open-source AI app template with the backend already wired.

The product should feel like this:

> Clone the repo and you have a real AI SaaS: customer app, auth, tenants,
> API keys, LLM gateway, cost tracking, billing stub, admin dashboard, and a
> working support workflow.

The category explanation is:

> BackAI is the AI backend inside the template.

This gives us both:

- Viral clarity: "AI app template" is immediately useful.
- Strategic category ownership: "AI backend" explains why the stack exists.

Avoid leading with AgentField, LiteLLM, MCP, sandboxes, or internal modules.
Those are substrate components and expansion paths.

## Strategic Role Of AgentField

AgentField should be visible but not loud.

In product UI:

- Use "AI runs", "traces", "agent runs", and "workflow" language.
- Show "Powered by AgentField" only in quiet places, such as trace detail or
  architecture footers.
- Link to AgentField for deep execution inspection where it helps.

In architecture docs:

- Explain AgentField as one of the core AI-specific substrate pieces.
- Place it next to LiteLLM and Postgres/pgvector, not above the whole product.

Recommended architecture language:

```text
AI substrate:
- AgentField: agent execution, run lifecycle, spans, workflow state.
- LiteLLM: provider routing and OpenAI-compatible model gateway.
- Postgres + pgvector: tenant data, cost ledger, memory, search, jobs.
```

Consequence:

- BackAI remains the shareable product.
- AgentField gets discovery from repo readers, architecture docs, trace links,
  and GitHub org adjacency.
- Users do not feel forced to adopt a separate agent framework before they
  understand the app template.

## Default First Experience

The first experience starts in the customer app.

```bash
git clone https://github.com/Agent-Field/backai supportdesk-ai
cd supportdesk-ai
cp .env.example .env
docker compose up
```

Expected URLs:

```text
Customer app: http://localhost:3000
Admin:        http://localhost:3001
API:          http://localhost:8080
```

If keeping existing internal ports temporarily, the docs can map them, but the
target viral DX should use familiar ports.

### Walkthrough Flow

1. User opens the customer app.
2. User signs up.
3. BackAI provisions:
   - user
   - tenant
   - owner membership
   - API key
   - billing customer in stub mode
4. User lands in a SupportDesk workspace with sample tickets and sample docs.
5. User asks a support question or clicks "Draft reply".
6. In demo mode, BackAI returns a deterministic response and writes realistic
   request/cost/run records.
7. With a real LLM key, the same action routes through the LLM gateway.
8. Customer app shows a walkthrough link: "View this request in admin".
9. User lands in admin with the matching request highlighted.
10. Admin shows tenant, user, model, cost, latency, request log, and trace.

The reveal is the product moment:

> I used the app as a customer, and the backend/admin already captured the
> operational evidence.

## No-Key Demo Mode

No-key demo mode is required.

Without it, the first-run experience depends on a provider key and many users
will drop before seeing the product.

Demo mode should:

- Return deterministic AI-like responses for SupportDesk actions.
- Write realistic cost events with a clear `demo_mode=true` marker.
- Write request/run records so the admin dashboard lights up.
- Avoid pretending calls were made to real providers.
- Make the "Add a real LLM key" path obvious.

Recommended UI copy:

```text
Demo mode
This response used the built-in demo provider. Add OPENROUTER_API_KEY to call
real models and keep the same cost and trace flow.
```

Consequence:

- Better virality and lower setup friction.
- Slight risk of fake-feeling output.
- Mitigation is explicit demo labeling and a one-step real-key upgrade path.

## Customer App Definition

The customer app is not merely an example. It is the replaceable starter app.

For the SupportDesk-first template:

- `apps/customer-app` is the end-user SaaS product.
- `apps/dashboard` is the operator/admin console.
- `services/runtime` is the backend API.
- `apps/backend/agents` contains AI workers and agent code.

The customer app should be polished enough to share, but structured so users
understand it is theirs to edit.

Docs should say:

```text
This is your product surface. Replace the pages, copy, workflow, and brand
with your own app.
```

Operationally optional:

- Existing products can ignore or remove the customer app and call the runtime
  API from Rails, Django, Phoenix, mobile, or another frontend.

Emotionally default:

- The repo should boot with the customer app because that is the viral product
  artifact.

## Admin Dashboard Role

The admin dashboard is separate from the customer app.

It should answer:

- Who are my tenants?
- What did each tenant spend?
- Which model/provider was used?
- Which feature or agent generated the cost?
- Which requests failed?
- What API keys exist?
- What billing state is this customer in?
- What happened in this run or trace?

First-run admin should avoid clutter. The walkthrough should only touch live
features:

- Home
- Cost
- Requests/runs
- Tenants
- API keys
- Billing stub
- Logs/metrics if useful

Hide or de-emphasize:

- MCP
- sandboxes
- coding harnesses
- Shipwright
- Firecracker/e2b
- Svix internals
- workload module theory

Those remain available in advanced docs and advanced nav states.

## OpenAI SDK And Suite SDK

Fastest path:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/api/v1/llm",
    api_key="af_...",
)

client.chat.completions.create(
    model="gpt-4.1-mini",
    messages=[{"role": "user", "content": "Draft a support reply"}],
)
```

Platform-native path:

```python
from af_stack import suite

await suite.llm.chat(
    model="gpt-4.1-mini",
    messages=[{"role": "user", "content": "Draft a support reply"}],
    labels={"feature": "support_reply"},
)
```

Do not maintain a full custom OpenAI SDK clone inside Suite.

Suite should provide thin LLM helpers plus BackAI-specific primitives:

- `suite.agents.call`
- `suite.memory.put/search`
- `suite.jobs.enqueue`
- `suite.webhooks.send`
- `suite.cost.events`
- `suite.admin.tenants`
- `suite.admin.keys`

Consequence:

- Official OpenAI SDK compatibility gives instant adoption.
- Suite SDK can focus on platform value without chasing every OpenAI client
  edge case.

## Deployment Philosophy

Every example should use the same deployment model. Different examples can
enable different capabilities.

Correct promise:

> Every example uses the same BackAI deployment model. Examples that require
> external capabilities declare them explicitly and degrade to demo mode when
> those keys are missing.

Incorrect promise:

> Every possible AI app has the same deployment complexity.

Deployment gets hard when an example requires external production services:

- S3/R2/Tigris for file uploads.
- E2B/gVisor/Firecracker for sandboxes.
- GitHub OAuth or `GH_TOKEN` for coding agents.
- Stripe keys and webhooks for live billing.
- OAuth provider apps and callback URLs for social login.
- Public webhook domains and signing secrets.
- DNS and TLS for production domains.

SupportDesk is the right first example because it proves the core backend loop
without requiring those heavy dependencies.

## Railway First Deploy

Railway is the primary viral hosted deploy path.

The Railway template should provision:

- Postgres with pgvector.
- Runtime.
- Customer app.
- Admin dashboard.
- AgentField substrate if the first-run trace/run experience depends on it.

Default Railway mode:

- demo LLM provider enabled.
- no sandbox.
- no S3 required.
- no Stripe required.
- no OAuth provider required.

Optional Railway fields:

- `OPENROUTER_API_KEY`
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `STRIPE_SECRET_KEY`
- `AF_STACK_S3_*`
- `E2B_API_KEY`
- OAuth provider keys

Consequence:

- More services than a runtime-only deploy.
- Much stronger product reveal because the deployed app matches the local
  SupportDesk-first promise.

The current deploy configs should be updated so Railway and Render include the
customer app as a first-class service. Production compose should also include
the customer app or clearly document how to enable it.

## Serious Production Paths

Production docs should distinguish early hosted deploys from serious
self-hosting.

Recommended hierarchy:

1. Railway: fastest public demo and early SaaS deploy.
2. Docker Compose + Caddy: small team or VPS production.
3. Helm: Kubernetes, enterprise, or serious platform teams.
4. Fly/Render: teams already standardized on those platforms.

Production requirements:

- domain names for customer app, admin, and API
- managed Postgres
- external S3/R2/Tigris if uploads or sandbox artifacts are enabled
- provider key for real LLM calls
- generated auth secret
- generated KMS key
- Stripe keys if live billing is enabled
- sandbox provider key only if sandbox capability is enabled

Recommended domain model:

```text
app.example.com    -> customer app
admin.example.com  -> admin dashboard
api.example.com    -> runtime
```

One-domain fallback is possible behind Caddy path routing, but subdomains are
clearer for the product mental model.

## Capability Manifest

Examples should declare what they need.

Example shape:

```yaml
example: supportdesk
name: SupportDesk AI
requires:
  - auth
  - tenants
  - llm_gateway
  - cost
  - admin
optional:
  - stripe
  - s3_uploads
  - webhooks
demo_mode:
  llm: deterministic
  billing: stub
  seed_data: true
```

Other examples can extend the model:

```yaml
example: shipwright
requires:
  - auth
  - tenants
  - agentfield
  - jobs
optional:
  - sandbox
  - github
  - harnesses
demo_mode:
  patch_generation: deterministic
```

This lets all examples share the same deploy primitive while being honest
about external dependencies.

## SupportDesk Product Scope

First-run SupportDesk should include:

- sign up and sign in
- tenant auto-provisioning
- API key auto-provisioning
- sample tickets
- sample knowledge snippets
- draft reply action
- ask knowledge base action
- cost logging
- request/run logging
- admin deep link to exact request
- billing stub
- real-provider upgrade path

Avoid for first-run:

- file upload
- external S3
- live Stripe checkout
- social OAuth setup
- sandbox
- MCP
- coding agents
- complex workflow builder

Optional later:

- upload docs
- inbound webhook from Zendesk/Intercom/GitHub Issues
- outbound webhook when reply is drafted
- Stripe live mode
- multi-agent escalation flow
- AgentField deep trace links

## Documentation IA

Top-level docs should start with the template, not architecture.

Recommended docs flow:

1. Quickstart: run SupportDesk locally.
2. First walkthrough: customer app to admin reveal.
3. Add a real model key.
4. Customize the customer app.
5. Deploy on Railway.
6. Production deployment.
7. Use from an existing app.
8. Architecture.
9. Advanced capabilities.

Architecture docs should include AgentField prominently but proportionally as
one core AI substrate component.

## Dashboard IA Simplification

Default admin should be sparse on first run.

Recommended first-run groups:

```text
Home
Operate
  Requests
  Cost
  Runs
Customers
  Tenants
  Users
  API Keys
  Billing
Build
  App
  Agents
  Integrations
System
  Logs
  Metrics
  Settings
```

Advanced features can be hidden until enabled:

- MCP
- sandboxes
- skills
- harnesses
- Shipwright
- webhooks
- storage
- crons
- DB studio

Consequence:

- First-run dashboard feels focused.
- Advanced users can still discover the platform depth.
- Disabled/stub features no longer make the product feel unfinished.

## Implementation Plan

### Phase 0: Branch And Guardrails

Create a feature branch:

```bash
git switch -c supportdesk-first-dx
```

Before code edits:

- Confirm current dirty files and preserve unrelated work.
- Keep the existing Shipwright modifications untouched unless explicitly in
  scope.
- Do not use `lsp_diagnostic`.

### Phase 1: Product Shell And Copy

Tasks:

- Rename customer app branding from SWE-AF to SupportDesk AI.
- Update landing page to SupportDesk-first.
- Update README headline to "AI app template" with "AI backend" as category
  explanation.
- Add "What you edit" guidance for customer app, agents, runtime, dashboard.
- Add quiet AgentField references in architecture, not homepage hero.

Acceptance:

- A new user understands what the repo is in under 30 seconds.
- AgentField is discoverable but not the main headline.

### Phase 2: No-Key Demo Provider

Tasks:

- Add deterministic demo provider for SupportDesk actions.
- Ensure demo calls write cost/request/run records.
- Mark demo records with metadata.
- Add UI labels explaining demo mode.
- Add "Add real LLM key" CTA.

Acceptance:

- Fresh clone works without `OPENROUTER_API_KEY`.
- Admin dashboard has real-looking evidence after first action.
- No UI claims a real provider was called in demo mode.

### Phase 3: Walkthrough

Tasks:

- Add customer app onboarding checklist.
- Add admin onboarding checklist.
- Link a customer action to the exact admin request/cost/run record.
- Add seed data for sample tickets and support knowledge.

Acceptance:

- User can complete the first walkthrough in less than five minutes.
- The walkthrough touches only live, configured features.

### Phase 4: Local Compose Simplification

Tasks:

- Make customer app part of default `docker compose up` first experience.
- Normalize local ports if feasible:
  - customer app: `3000`
  - admin: `3001`
  - API: `8080`
- Keep escape hatches for existing port conflicts.
- Ensure AgentField and LiteLLM details are hidden unless needed.

Acceptance:

- One command boots the full SupportDesk experience.
- README and compose agree on URLs.

### Phase 5: Railway Template

Tasks:

- Add customer app service to Railway template.
- Keep sandbox disabled by default.
- Keep S3 optional.
- Keep Stripe optional.
- Support demo mode without provider keys.
- Ensure public/private URLs are correct for customer app, admin, runtime.

Acceptance:

- Railway deploy produces a usable public SupportDesk customer app and admin.
- Required fields are minimal.
- Real LLM keys can be added after deploy.

### Phase 6: Production Docs And Deploy Targets

Tasks:

- Update production compose to include customer app or document enablement.
- Update Render/Fly docs to explain customer app deployment.
- Update Caddy examples for `app`, `admin`, and `api` subdomains.
- Add production checklist.

Acceptance:

- Local, Railway, and production stories agree on service roles.
- Serious production docs do not hide real external requirements.

### Phase 7: Example Capability Manifests

Tasks:

- Add capability manifest schema.
- Add SupportDesk manifest.
- Add or update manifests for Shipwright, Deep Research, LLM gateway only,
  and future DocuChat-style examples.
- Update examples README with "same deploy model, declared capabilities"
  language.

Acceptance:

- Examples are comparable.
- Missing external dependencies degrade clearly or are documented as required.

## Risks

### Risk: SupportDesk feels like the whole product

Mitigation:

- Say "replaceable starter app" repeatedly.
- Keep docs focused on edit surfaces.
- Add examples showing other app types.

### Risk: Demo mode feels fake

Mitigation:

- Label demo mode clearly.
- Write real platform records.
- Make real-provider upgrade one env var.

### Risk: Railway template becomes heavy

Mitigation:

- Disable sandbox and S3 by default.
- Avoid live Stripe and OAuth in the first deploy.
- Keep customer app, admin, runtime, and Postgres as the core.

### Risk: AgentField becomes invisible

Mitigation:

- Mention it in architecture.
- Link traces and advanced run details to AgentField.
- Keep GitHub org and docs adjacency strong.

### Risk: First-run dashboard exposes too much

Mitigation:

- Hide disabled modules.
- Keep advanced features out of the walkthrough.
- Move deep primitives to advanced docs.

## Non-Goals

For the first simplification branch, do not prioritize:

- full sandbox production hardening
- Shipwright production readiness
- live Stripe checkout
- OAuth provider setup
- S3 upload-heavy RAG
- marketplace plugin semantics
- maintaining a custom OpenAI SDK clone
- making every advanced example no-key and no-config

## Open Questions

1. Should the default repo name and visible product name be BackAI, AF Stack,
   or another final brand?
2. Should SupportDesk AI live directly in `apps/customer-app`, or should the
   repo support swapping starter apps by config?
3. Should demo mode be runtime-wide or SupportDesk-specific?
4. Should local ports be changed immediately, or should the first branch keep
   existing ports and only improve docs?
5. Should AgentField be required in the Railway template from day one, or can
   the first deploy degrade trace depth when AgentField is disabled?

## Recommended Next Action

Create `supportdesk-first-dx`, then implement the smallest coherent vertical
slice:

1. SupportDesk branding and customer app first screen.
2. No-key demo response path.
3. Cost/request record visible in admin.
4. Customer-to-admin walkthrough link.
5. README quickstart aligned to the new flow.

Do not start by refactoring every module. The product gets simpler when the
first 10 minutes are excellent.
