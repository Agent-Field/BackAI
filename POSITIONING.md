# AF Stack — Positioning, Consumption Model, and IA Audit

Three parts in one strategy doc:

1. **Positioning** — what AF Stack is (and what it isn't)
2. **Canonical DX** — how users consume the stack (fork vs API)
3. **Audit** — what to add, lose, or abstract to deliver that DX

---

## Part 1 — Positioning

### The one-line position

> **AF Stack is the open-source backend distribution for the AI era —
> Supabase-shape architecture, Cal.com-shape forkability, AI-native
> runtime, all Apache 2.0. The repo IS the product. There is no hosted
> version to compete with the fork.**

### Why this exact shape doesn't exist for non-AI backends

Three reinforcing reasons:

**1. Commercial conflict.** Every OSS BaaS (Supabase, Appwrite, Nhost)
makes its money on a hosted product. A _perfectly forkable_ open BaaS
sabotages that revenue, so they don't ship one. Supabase's own self-host
docs say self-host lacks branching, advanced metrics, managed
backups/PITR, the platform API. They keep self-host one step harder
than cloud — on purpose. **Nobody with the resources to build a great
forkable BaaS has the _incentive_ to do so.**

**2. No acute pain for non-AI.** Supabase hosted is cheap. Firebase
works. Vercel + a managed Postgres covers most apps. "Just use Postgres

- Auth.js" is fine. There's no fire that makes a non-AI team _need_ a
  forkable backend distribution.

**3. Middle-ground difficulty.** Between "BaaS hosted" and "DIY from
scratch," the middle ground (package operations, deploy targets,
multi-tenancy, dashboards) is a lot of work for unclear payoff. The
closest things (Cal.com, Plane, Outline) are _complete apps_, not
platforms. A forkable _platform_ nobody's built well.

### Why AI breaks the equilibrium

Three forces that make this gap _acute_:

1. **LLM cost / lock-in pain is real.** Spend can be 10× infra cost.
   Per-tenant cost attribution is mandatory. Hosted BaaS doesn't
   provide it. Helicone/Portkey help but don't have a backend.
2. **Vendor fragmentation is multi-dimensional.** OpenRouter / OpenAI /
   Anthropic / Google / Mistral / DeepSeek / Groq. Lock-in stacks:
   provider + costs + tracing + prompts + harnesses + MCP. The pain is
   acute enough that "I want to own the stack" wins.
3. **State that doesn't fit existing primitives.** Agent runs, memory
   scopes, tool-call traces, sandbox executions, MCP servers. None of
   these are Postgres-row-shaped. Existing BaaS doesn't have them.

**Net**: AI broke the equilibrium that made non-AI feel "solved." We're
in a 2018-Supabase-for-cloud-DBs window, but for the post-LLM era.

### Why Supabase-shape (not PocketBase-shape)

PocketBase is gorgeous, but wrong for our target:

| Dimension   | PocketBase    | What we need                                    |
| ----------- | ------------- | ----------------------------------------------- |
| Database    | SQLite        | Postgres + pgvector + queue + FTS               |
| Tenancy     | Single-tenant | Multi-tenant SaaS (PG RLS)                      |
| Compute     | None          | Sandboxes (docker / gVisor / Firecracker / e2b) |
| Concurrency | Single binary | Multi-replica + shared queue                    |
| Ceiling     | Side projects | Startup → enterprise                            |

PocketBase is for solo-dev side projects. AF Stack targets production AI
SaaS. **Wrong fit.**

But we steal PocketBase's _DX virtues_:

- One `docker compose up`, everything works
- Admin UI built-in (operator console)
- Self-host is the **first** path, not the second

**Net**: Supabase architecture, PocketBase DX, Cal.com forkability,
AgentField AI runtime.

### What we do differently from Supabase

Four explicit differentiators (the strategic moat):

1. **Operator-first, not cloud-first.** No hosted product to compete
   with the fork. The repo IS the product. Every commit ships to the
   user.
2. **All Apache 2.0.** No BSL, no "Community Edition" with crippled
   features. Every line is yours.
3. **Deploy targets ship in the repo.** `deploy/helm/`,
   `docker-compose.prod.yml`, Fly, Railway, Render — same code paths
   in dev and prod.
4. **AI primitives are first-class.** Agent runtime + LLM gateway +
   sandboxes + MCP + cost ledger have the same DX as the rest. Not
   partnerships, not bolt-ons.

### The union nobody's built

| What we take                                                             | From                                         |
| ------------------------------------------------------------------------ | -------------------------------------------- |
| Backend service shape (Postgres / auth / storage / functions / realtime) | Supabase                                     |
| Self-host-first, single-deploy DX, built-in admin UI                     | PocketBase                                   |
| Forkable _complete product_, deploy-from-repo                            | Cal.com / Plane / Outline                    |
| Operator console + multi-tenancy + billing wiring                        | enterprise SaaS starters (BoxyHQ / Makerkit) |
| AI runtime (agents / sandboxes / MCP / cost ledger)                      | AgentField + LiteLLM (ours)                  |

Each analog has 1–2 pieces. None has all five. **That's why nobody's
built this — the union is new.**

### Strategic risk + mitigation

**Risk**: Forkable distribution only works if the deploy story is
bulletproof. If `docker compose up` fails on day 1, the prospect is
gone. We need to win on **DX first**, then features.

**Trap**: Becoming "another Linux distribution problem" — too many
knobs, every install different, no canonical good experience.

**Mitigation**:

- Production-grade defaults. Operator sets ~3 env vars.
- Deploy targets shipped + CI-tested.
- First-impression > feature-breadth. The first hour decides.

---

## Part 2 — Canonical DX (the consumption model)

Your question: "**Do users write their backend code IN our template, or
do they host us and consume APIs from a separate app?**"

The answer is unambiguous, and it's important.

### The canonical path: fork-and-edit

This is the Cal.com / Plane / Outline pattern. **Users clone our repo,
edit the parts that are theirs, and deploy the whole thing as one
unit.**

Why this is the canonical path, not optional:

1. **AgentField agents are Python processes in the repo.** They live
   under `apps/backend/agents/<name>/`. They ship with the deploy. You
   can't "host AF Stack and write agents elsewhere" — the agents are
   part of the deploy graph.
2. **The customer-facing app is forkable.** `apps/customer-app/` is
   meant to be branded, restyled, and customized into the user's
   product. It's a Next.js app shipped _with_ the backend.
3. **Workload modules and dashboard plugins are in-repo.** The Go
   handler + migrations live next to the runtime; the UI tab lives in
   the dashboard. Both ship together.
4. **One deploy unit = simpler operations.** No "two repos, two CI
   pipelines, two deploys" — `docker compose up`, `helm install`, or
   `flyctl deploy` ships everything.

**What "the user's code" means in the canonical path**:

```
apps/customer-app/              ← THEIR product (Next.js)
  - they brand it
  - they edit pages
  - they add their flows
apps/backend/agents/<name>/     ← THEIR agents (Python)
  - they write the reasoners
  - they declare MCP servers + harnesses
workload-modules/<id>/          ← THEIR modules (Go)
  - they add domain-specific HTTP routes + migrations
apps/dashboard/plugins/<id>/    ← THEIR dashboard tabs (TS)
  - operator views they care about
deploy/                         ← THEIR deploy config
  - branded values.yaml / fly.toml / etc.
```

Plus the suite runtime, AgentField, LiteLLM, Postgres, MinIO, Svix —
all in the same deploy. Their fork. Their product.

### The secondary path: API-only consumption

For three legitimate cases:

| Case                    | Why API-only fits                                   |
| ----------------------- | --------------------------------------------------- |
| Mobile apps             | They can't fork a Go runtime; they call REST/SDK    |
| Existing apps adding AI | Already have a backend; just want our AI primitives |
| Multi-frontend setups   | Web + mobile + desktop all hit the same backend     |

For these, the runtime is deployable as a pure API server. The customer-
app and dashboard can be hosted alongside or skipped entirely (set
`AF_STACK_MODULE_*` flags). The Python / TS / Go SDKs work from any
client.

But **API-only is the secondary path**, not the canonical one. We
optimize the fork-and-edit experience. API-only "just works" because we
have stable REST + SDKs.

### Why this is different from Supabase

Supabase's model: deploy Supabase as a backend, write a separate Next.js
app that calls its APIs. **Two repos. Two deploys.** Their hosted
offering optimizes that.

AF Stack's model: one repo, one deploy, your code lives inside. Closer
to Cal.com than Supabase in _consumption pattern_, even though our
_architecture_ is Supabase-shape.

### Concrete example: someone builds "DocuChat" (a doc-Q&A SaaS)

The fork-and-edit DX, step by step:

```
1. Clone AF Stack:
   git clone github.com/Agent-Field/af-stack docuchat

2. Brand it:
   - Edit brand.yaml (name, codename, palette, logos, domains)
   - Replace logo + favicon

3. Write the agent:
   apps/backend/agents/docuchat/main.py
     - reasoners: ingest, search, answer
     - uses suite.storage + suite.llm.embed + suite.memory

4. Customize the customer app:
   apps/customer-app/src/app/(app)/upload/page.tsx
     - drag-and-drop upload UI
   apps/customer-app/src/app/(app)/chat/page.tsx
     - chat interface that calls suite.agents.call("docuchat.answer")

5. Deploy:
   helm install docuchat ./deploy/helm/af-stack -f values-prod.yaml

6. Their customers sign up at app.docuchat.com — the customer-app.
   Operator at admin.docuchat.com — the dashboard.
   API at api.docuchat.com — the runtime.
```

Three subdomains, one deploy, their code. **That's the canonical DX.**

---

## Part 2.5 — Vocabulary (Workload Module · Dashboard Plugin · Adapter)

These three terms recur in this doc and in `STRATEGY.md`. They're easy
to confuse. Definitions:

### Workload Module

A **backend extension** — Go HTTP routes + DB migrations + (optionally)
jobs and crons + a manifest. Lives in `workload-modules/<id>/`. Loaded
at runtime startup; routes are mounted under `/workload/<id>/...`. Has
its own DB tables (under its own schema).

**Think of it as**: a Django app or a Rails engine. A mini-app inside
the platform.

**Examples**:

- `workload-modules/knowledge/` → `POST /workload/knowledge/ingest`,
  `POST /workload/knowledge/search`; `knowledge_documents` table
- `workload-modules/git-workload/` → git checkout / diff / PR routes
  used by Shipwright
- A user's own `workload-modules/notes/` → `POST /workload/notes`;
  `notes` table

### Dashboard Plugin

A **UI extension for the operator console** — a sidebar tab + page.
Lives in `apps/dashboard/plugins/<id>/`. Pure TypeScript/React; calls
the runtime REST API. Auto-discovered at the next dashboard build.

**Think of it as**: an admin-UI plugin. Pure frontend, no backend.

**Examples**:

- `apps/dashboard/plugins/cost-explorer/` → adds a "Cost Explorer" tab
- A Knowledge module's matching `plugins/knowledge/` → renders an
  upload UI for the module's routes
- A user's `plugins/sales-dashboard/` → shows their domain metrics

### Adapter

A **swappable implementation behind a single Go interface** for an
_existing_ platform primitive. NOT a plugin — a config-level swap.
Operator picks one via env var. We ship multiple in-tree.

**Think of it as**: a strategy pattern. Same interface, different
backend.

**Examples**:

- `Sandbox` interface → `docker` / `gvisor` / `firecracker` / `e2b`
  adapters; chosen via `AF_STACK_SANDBOX_ADAPTER=...`
- `Storage` interface → `minio` / `s3` adapters; `AF_STACK_S3_ADAPTER`
- `Billing` interface → `stripe` / `lago` adapters (planned);
  `AF_STACK_BILLING_ADAPTER`
- `Notifications` interface → `log` / `resend` adapters

### How they compose

A single domain capability typically ships as a **bundle**:

```
workload-modules/knowledge/        ← Workload Module (routes + tables)
apps/dashboard/plugins/knowledge/  ← Dashboard Plugin (upload UI)
services/runtime/internal/embeddings/adapters/  ← Adapter (which embedder)
```

Three artifacts, one capability. None of them is "installed" from a
marketplace — they're code the user edits _in their fork_. Same edit
model as Cal.com / Plane / Outline.

### Quick contrast

|                      | Adds                                    | Lives in                                          | Loaded via      | Marketplace?           |
| -------------------- | --------------------------------------- | ------------------------------------------------- | --------------- | ---------------------- |
| **Workload Module**  | Backend routes + tables + jobs          | `workload-modules/<id>/`                          | Runtime startup | No — user code in fork |
| **Dashboard Plugin** | UI tab in operator console              | `apps/dashboard/plugins/<id>/`                    | Build-time scan | No — user code in fork |
| **Adapter**          | Swap of an existing primitive's backend | `services/runtime/internal/<area>/adapters/<id>/` | Env var         | No — we ship multiple  |

---

## Part 3 — Audit: what to add, lose, abstract

Given the canonical DX above, what's currently aligned, what's not, and
what's missing. Organized by what action it triggers.

### 3.1 What to ADD

These are gaps blocking the canonical DX. Priority-ordered.

#### A1. A `make brand` flow (or `af-stack init`)

Today the user clones the repo and figures out theming, brand.yaml,
logo replacement, customer-app config by reading docs. Need a one-step
flow:

```bash
af-stack init --name "DocuChat" --color "#0A66C2" --logo ./logo.png
```

That edits brand.yaml, generates brand.css for both Next.js apps, drops
the logo in the right places, updates default agent names. **Effort:
1 week.** Massive first-impression win.

#### A2. A `starter` example that's the canonical fork basis

The current examples are _demo_ apps (Notable, Shipwright, etc.). What's
missing is a **bare-bones template** that says "this is what your fork
starts from." Should be:

- One agent (echo + summarize)
- A customer-app with sign-up → first action → "you're in"
- A dashboard plugin showing one custom metric
- A workload module with a single route + migration
- README walks through all four

Place at `examples/starter/`. Marked as "the recommended starting
point." Other examples (Notable, Shipwright, Deep Research) are _what
you can build_. **Effort: 1 week.**

#### A3. The 11 completeness features (from EXTENSIBILITY.md)

Realtime, Embeddings API, Multimodal, Search API, Tool adapters, etc.
Already documented. ~8–10 weeks of focused work for full completeness.
Without these, "you can build any AI app on the fork" isn't true.

#### A4. Deploy targets — make sure each one is CI-verified

We have Helm, Fly, Railway, Render, prod compose. Need to confirm
each one actually deploys an end-to-end working system in CI, not just
"the binary boots." **Effort: 1 week to audit + fix.**

#### A5. First-run onboarding in the dashboard

After `docker compose up`, the operator hits the dashboard. Today they
see empty tables. Need a "Getting started" panel:

- "Create your first tenant"
- "Issue an API key"
- "Set a budget"
- "Open the customer app"

Like the Supabase Studio "Quickstart" panel. **Effort: ½ week.**

#### A6. Auth bootstrap

Today better-auth creates users on first sign-up, mirrors to
`suite_users`, creates a default-tenant membership. Good.

What's missing: there's no "I'm the first user, make me the operator"
flow. A fresh deploy needs the first sign-up to be auto-promoted to the
admin role. Currently relies on default-tenant magic. **Effort: ½ week
to wire properly.**

### 3.2 What to LOSE / RECONSIDER

Things in the repo that don't fit the Cal.com-style forkable template,
or that create confusion.

#### L1. The feature-flags form (already on cleanup list)

Stale TODO; toggles are no-op. Hide behind env var per cleanup plan.

#### L2. Build → Harnesses (already on cleanup list)

Collapse into Build → Agents. Harnesses live in the agent container.

#### L3. Examples 02/04/05 stubs (already on cleanup list)

Remove `.gitkeep`-only directories.

#### L4. The dashboard plugin registry feel

The current `/plugins` tab makes dashboard plugins feel like a
marketplace. In the canonical DX, dashboard plugins are _user code in
their fork_. The tab should be **read-only display of installed
plugins**, not "browse / install / activate." Today the page just
shows registered plugins; the framing should make clear these come from
the operator's fork at build time. **Rename to "Dashboard Plugins"
section under Build, or remove the standalone tab.**

#### L5. Settings → Feature flags placeholder

Per A6 cleanup; gate behind env.

#### L6. The Stripe stub Portal button (already half-done)

Today it's hidden when stub. Keep hidden. The bigger ask is: stop
showing billing UI at all when `AF_STACK_BILLING_ADAPTER=none`. Should
auto-hide the Customers → Customer Billing tab + the customer-app
Billing page. **Effort: ½ day.**

#### L7. Old "Phase N" copy in dashboards

Various components have `// Phase 12.1` comments and a few stale
sub-titles ("Phase 13 ships..."). Sweep when touched, not as a separate
pass.

### 3.3 What to ABSTRACT (better)

Where the current shape works but the abstraction is leaky or confusing.

#### B1. Sidebar IA — Build has 14 items mixing concerns

The current Build group mixes:

- "Your product config" (Agents, Modules, Skills, MCP, Integrations)
- "Infrastructure plumbing" (Database, Storage, Secrets)
- "Configuration state" (Auth, Billing — read-only display of env-driven choices)
- "Operational data with config" (Webhooks endpoints, Jobs, Sandboxes, Crons)

**Proposed IA** (the 4-group shape):

```
[Home]   Build   Operate   Customers   Infrastructure          ⌘K   [user]
```

- **Build (your product config — what your fork ships)**:
  Agents · Modules · Skills · MCP · Integrations (Tool adapters) ·
  Webhooks (endpoints) · Auth (read-only display) · Billing (read-only display)

- **Operate (what's running right now)**:
  Runs · Shipwright · Cost · Queues · Sandbox Activity · Webhook
  Deliveries · Logs · Metrics · Notifications · Crons

- **Customers (your end users — gated by multi-tenancy)**:
  Tenants · Users · API Keys · Customer Billing · Audit

- **Infrastructure (low-level plumbing)**:
  Database · Storage · Secrets

- **Settings** (operator account, separate)

That's 8 + 9 + 5 + 3 = 25 items split across 4 clear mental modes.
Current count is ~30 in a confused split. **Effort: ½ week for IA
refactor (page moves, sidebar update).**

#### B2. Adapter swap UI — make it discoverable

Today an operator swaps adapters via env vars. The dashboard shows the
_active_ adapter passively (good) but doesn't show _what other adapters
exist_. Add a single "Adapters" page (under Infrastructure or
Settings) that lists:

- Storage: minio (active) | s3 | r2 | gcs | azure-blob
- Sandbox: docker (active) | gvisor | firecracker | e2b
- Notifications: log (active) | resend | postmark | sendgrid | ses | mailgun
- Billing: stripe (active) | lago | none

Each row shows "active / available," with a "Configure" link to the
docs page for that adapter. **Read-only — config still happens in env.**
But the operator now _knows what's possible_ without spelunking through
code. **Effort: ½ week.**

#### B3. "What's mine to edit" map in the README + docs

Add to README a clear table of the four edit surfaces (per Part 2's
DocuChat example). Today the README emphasizes operator commands; it
should emphasize _what to edit in your fork_. **Effort: small,
doc-only.**

#### B4. Brand surface as a single config

Today brand state is spread across BRAND.yaml + apps/dashboard/brand.css

- apps/customer-app/brand.css + various logo files. Consolidate to:

```yaml
# brand.yaml at root
name: DocuChat
codename: docuchat
display_name: DocuChat
short_description: AI-powered document Q&A for legal teams.

palette:
  primary: "#0A66C2"
  accent: "#16A34A"
  dark_mode: true

logos:
  light: ./brand/logo-light.svg
  dark: ./brand/logo-dark.svg
  favicon: ./brand/favicon.ico

domains:
  dashboard: admin.docuchat.com
  customer_app: app.docuchat.com
  api: api.docuchat.com
```

`af-stack init` (A1 above) reads/writes this and writes the generated CSS /
logo copies / config. **One file changes everything.** Effort: 1 week
(includes A1).

#### B5. Customer-app conventions — what to edit, what not to

The customer-app has auth pages, sign-up flow, a sample code-helper.
Need clear labels in code comments / README about:

- "Edit this freely" (pages under (app)/, components/)
- "Don't edit" (auth/ — wired to better-auth; api/ — proxies to runtime)
- "Customize via brand.yaml" (theme, layout shell)

So a user opening the fork understands the contract within 10 minutes.
**Effort: doc + comments.**

#### B6. The CLI as the primary surface

`af-stack` exists today (`mcp list/add`, `harness list/install`).
Should grow to be the _first surface_ a user touches in their fork:

```bash
af-stack init           # set up brand, generate brand.css, etc.
af-stack dev            # docker compose up + tail logs + open browser
af-stack agent new <name>          # scaffold a new agent
af-stack module new <id>           # scaffold a workload module
af-stack plugin new <id>           # scaffold a dashboard plugin
af-stack adapter list              # show all adapters + active choice
af-stack deploy [target]           # deploy to helm/fly/railway/render
```

That's the canonical user surface. **Effort: 2 weeks for full CLI v2.**

### 3.4 What's already aligned (don't touch)

Listing these to be explicit that we don't churn things that work:

- `docker compose up` boots a complete system
- The 8-band stack architecture (STACK.md)
- The Customers section (tenants / users / api keys / audit)
- The Operate section (runs / cost / queues / sandbox activity)
- The cost ledger + budgets
- The PG RLS multi-tenancy
- The secrets vault
- The MCP + Skills + (collapsed) Harnesses surface
- The OpenAPI-compat LLM gateway
- The sandbox adapter portfolio
- The hooks engine
- Better-auth wiring

---

## Part 4 — Execution Checklist

Tick each item as it lands. Phase 1 is the DX work that delivers the
canonical "fork → brand → deploy your AI SaaS" promise. Phases 2–4 are
the feature completeness, Tier 1 product, and enterprise tracks.

This is the canonical task graph — pass it to an executing agent.

### Phase 0 — Cleanup pass (done by another agent ✅)

- [x] Archive 7 superseded docs (`PLAN.md`, `PRD.md`, `TECH-SPEC.md`,
      `ROADMAP.md`, `PRIMITIVES.md`, `COMPLETENESS-AUDIT.md`,
      `CAPABILITY-MATRIX.md`) to `docs/archive/`
- [x] Merge `PLAN-NEXT.md` + `PLAN-CLEAN.md` → `STRATEGY.md`
- [x] Remove `.gitkeep`-only stubs (`examples/04`, `examples/05`,
      `workload-modules/change-stream-listener`,
      `workload-modules/multimodal-storage`)
- [x] Collapse Build → Harnesses sidebar entry into Build → Agents
- [x] Hide stale feature-flags form behind `NEXT_PUBLIC_SHOW_FEATURE_FLAGS`
- [x] Create `STACK.md` (Supabase-shaped 8-band layered diagram)
- [x] Create `POSITIONING.md` (this doc)
- [x] Refresh `ARCHITECTURE.md` (point at `STACK.md` for diagram)
- [x] Refresh `OSS-AUDIT.md` (item statuses updated)

### Phase 1 — DX polish (~8 weeks)

This phase makes the canonical "fork → brand → deploy" path bulletproof.
Without it, the positioning in Part 1 is aspirational. With it, it's
real.

#### 1a. Sidebar IA refactor (~½ week)

Move from the current 3-group layout to a cleaner 4-group split.

- [x] Create `Infrastructure` sidebar group
- [x] Move `Database` from Build → Infrastructure
- [x] Move `Storage` from Build → Infrastructure
- [x] Move `Secrets` from Build → Infrastructure
- [x] Confirm Build group contains only product-config items
      (Agents · Modules · Skills · MCP · Integrations · Webhooks
      endpoints · Auth · Billing)
- [x] Confirm Operate group contains only live runtime data
      (Runs · Shipwright · Cost · Queues · Sandbox Activity · Webhook
      Deliveries · Logs · Metrics · Notifications · Crons)
- [x] Update `apps/dashboard/src/lib/nav.ts` with the new structure
- [x] Update `NAVBAR.md` to reflect the new IA

#### 1b. Adapter swap UI (~½ week)

- [x] Add `Infrastructure → Adapters` page in dashboard
- [x] List adapter choices per primitive (Storage / Sandbox /
      Notifications / Billing / future ones)
- [x] Mark which is active from env
- [x] Link each row to `docs/adapters/<id>.md` for config instructions
- [x] Read-only — no in-UI swap (config still lives in env vars)

#### 1c. Hide billing UI when adapter=none (~½ day)

- [x] Hide `Customers → Customer Billing` sidebar entry when
      `AF_STACK_BILLING_ADAPTER=none`
- [x] Hide customer-app `/billing` page in the same case
- [x] Add empty-state with "Enable billing" link to docs

#### 1d. Brand surface as single `brand.yaml` (~1 week)

- [x] Define `brand.yaml` schema at repo root (name, codename,
      display_name, palette, logos, domains)
- [x] Generate `apps/dashboard/src/app/brand.css` from `brand.yaml`
- [x] Generate `apps/customer-app/src/app/brand.css` from `brand.yaml`
- [x] Update layout shells to read brand name + logos
- [x] Migrate existing `BRAND.yaml` content to new `brand.yaml`;
      deprecate `BRAND.yaml`

#### 1e. `af-stack init` command (~1 week, depends on 1d)

- [x] Add `af-stack init` subcommand to the CLI
- [x] Accept flags: `--name`, `--color`, `--logo` (or interactive
      prompts)
- [x] Write `brand.yaml`, generate CSS, copy logo into
      `apps/dashboard/public/` + `apps/customer-app/public/`
- [x] Set default agent name to user's chosen project name

#### 1f. `examples/starter/` template (~1 week)

The canonical bare-bones fork basis. Other examples are _demos of what
you can build_; starter is _what you start from_.

- [x] Create `examples/starter/`
- [x] Ship one agent (`agents/starter/main.py` — echo + summarize)
- [x] Customer-app sign-up → first action flow wired
- [x] One dashboard plugin showing one custom metric
- [x] One workload module with one route + one migration
- [x] `README.md` walking through all four edit surfaces
- [x] Update `examples/README.md` — mark `starter/` as recommended
      starting point

#### 1g. Customer-app edit conventions (~½ week)

- [x] Add inline code comments labeling edit zones (free / do-not-touch
      / brand-only)
- [x] Add `apps/customer-app/EDITING.md` documenting the contract
- [x] Same for `apps/dashboard/EDITING.md` (for fork-and-edit users
      who want to customize the operator console)

#### 1h. CLI v2 (~2 weeks)

The CLI becomes the primary developer surface in the fork.

- [x] `af-stack init` (already part of 1e)
- [x] `af-stack dev` — `docker compose up` + tail logs + open browser
- [x] `af-stack agent new <name>` — scaffold a new agent under
      `apps/backend/agents/<name>/`
- [x] `af-stack module new <id>` — scaffold a workload module
- [x] `af-stack plugin new <id>` — scaffold a dashboard plugin
- [x] `af-stack adapter list` — show active + available adapters
- [x] `af-stack deploy <target>` — deploy via helm / fly / railway /
      render

#### 1i. First-run onboarding (~½ week)

- [x] Add "Getting Started" panel to dashboard Home when system is fresh
- [x] Steps: Create tenant → Issue API key → Set budget → Open customer
      app
- [x] Auto-dismiss panel once steps complete

#### 1j. Auth bootstrap (~½ week)

- [x] On fresh deploy, auto-promote first sign-up to operator role
- [x] Document the bootstrap flow in `docs/auth.md`
- [x] Add operator-creation CLI fallback: `af-stack operator create`

#### 1k. Deploy target CI verification (~1 week)

- [x] Helm chart: deploy to kind in CI, verify chart-owned health
      endpoints (`/health`, `/ready`)
- [x] Fly.io: config dry-run validation in CI (`flyctl config validate`
      when authenticated; static TOML validation otherwise)
- [x] Fly.io: staging app for end-to-end deploy + health probes
      (`scripts/test-fly-staging.sh`, credential-gated)
- [x] Railway: template validation in CI
- [x] Render: Blueprint validation in CI
- [x] `docker-compose.prod.yml`: production compose syntax + Caddy
      health-route validation in CI
- [x] `docker-compose.prod.yml`: credentialed production-like bring-up
      in CI, verify services healthy against external Postgres/S3
      (`scripts/test-prod-compose-smoke.sh`, credential-gated)

#### 1l. README rewrite (~½ week)

- [x] Lead with the canonical DX (Part 2 of this doc), not the feature
      list
- [x] Add "What's mine to edit" table (four edit surfaces)
- [x] Add "What's pre-wired vs configurable" reference
- [x] Link to `STACK.md` for architecture, `POSITIONING.md` for
      strategy

#### 1m. Dashboard plugin discoverability (~½ day)

- [x] Move `/plugins` tab into Build group as "Dashboard Plugins"
- [x] Reframe page as read-only list of plugins picked up at build time
- [x] Remove any "browse / install / activate" language

### Phase 2 — Completeness features (~8–10 weeks)

The 11 features from `EXTENSIBILITY.md` that take us from "Phase 16
launch-ready" to "any AI app builds on this without forking."

#### General-backend parity (~5 weeks)

- [x] **Realtime** — Postgres LISTEN/NOTIFY → WebSocket bridge; SDK
      `suite.realtime.subscribe(table, filter)` (~2 weeks)
- [x] **Search API** — REST shim over PG FTS + pgvector hybrid; SDK
      `suite.search(q, mode)` (~1 week)
- [x] **User activity log** — `suite_user_activity` table + SDK (~½ week)
- [x] **Feature flags wired** — `/api/v1/config/flags` endpoint;
      un-hide the form (~½ week)
- [x] **File transforms** — thumbnail / resize on storage GETs (~1 week)

#### AI-specific completeness (~5 weeks)

- [x] **Embeddings API** — `/api/v1/embeddings`, OpenAI-compat,
      LiteLLM-routed (~½ week)
- [x] **Multimodal API** — `/api/v1/audio/{speech,transcriptions}`,
      `/api/v1/images/generations` (~1 week)
- [x] **Realtime run subscriptions** — WebSocket stream of AgentField
      spans for live run viewers (~1 week)
- [x] **Agent tool adapters** — browser-use, SearXNG, fs, exec, HTTP,
      SQL; per-tenant enable (~2 weeks)
- [x] **PII redaction + moderation** — gateway pre/post hooks; regex
      default + Presidio adapter (~1 week)
- [x] **OAuth-on-behalf-of-user** — for agents acting as user in 3rd
      party APIs (~2 weeks; can defer to v1.2 if scope is tight)

### Phase 3 — STRATEGY Tier 1 (~5 weeks)

Already specified in `STRATEGY.md`. Re-listed here for one ordered
view.

- [x] **LiteLLM virtual keys** — per-user budgets + per-key rate limits
      via LiteLLM (~1 week)
- [x] **Billing adapter** — extract Stripe interface; add Lago adapter
      (~1 week)
- [x] **Shipwright** — autonomous AI agent factory (customer task →
      sandboxed agent with claude-code/codex/gemini → live progress
      → PR/patch) (~1 week)
- [x] **AgentField data in dashboard** — inline run summary + control
      actions in AF Stack; deep DAG / step inspector links out to
      AgentField so AF Stack does not duplicate AI-stateful execution
      state. Memory remains on the existing suite memory surface per
      `STRATEGY.md` / `docs/agentfield-integration.md` (~1 week)
- [x] **Approvals primitive** — `suite_approvals` table + REST API +
      Python/TypeScript SDK + dashboard tab (~1 week)

### Phase 4 — Enterprise (~4 weeks)

- [x] **SSO/SAML** — Authentik (self-host) or WorkOS (managed)
- [x] **RBAC** — Casbin or Oso layered over PG RLS
- [x] **BYOK secrets** — cloud KMS adapters (AWS / GCP / Azure)
- [x] **GDPR** — data export + erase endpoints

### Total scope

| Phase                    | Weeks                    |
| ------------------------ | ------------------------ |
| 0. Cleanup               | done ✅                  |
| 1. DX polish             | ~8                       |
| 2. Completeness features | ~8–10                    |
| 3. STRATEGY Tier 1       | ~5                       |
| 4. Enterprise            | ~4                       |
| **Total**                | **~25 weeks (6 months)** |

After all four phases, no one is closer to **the canonical
open-source backend for AI products** than us — because the players
who could build it (Supabase / Appwrite / Nhost) have commercial
reasons not to.

---

## How to use this checklist

1. **Pass this whole doc to an executing agent.** Parts 1–3 explain
   the strategic frame so the agent knows _why_ each item exists.
2. **The agent picks the next unticked item** in Phase order. Phase 1
   items must complete before Phase 2 starts (DX before features).
3. **Within a phase**, items are mostly independent — they can run in
   parallel if multiple agents are working.
4. **Tick the box** when an item lands. Update this doc as you go.
5. **If an item is descoped or deferred**, edit the checkbox to
   `[ ] (deferred to vN.M)` rather than removing it.
6. **If something new gets added**, append it to the appropriate phase
   with rationale linked to a section of this doc.

---

## Summary in one paragraph

> \*\*AF Stack is Supabase-shape architecture + Cal.com-shape forkability
>
> - AgentField AI runtime, all Apache 2.0, with no hosted version to
>   compete with the fork.\*\* Users clone the repo, brand it, write their
>   agents + customer app + workload modules in-tree, and deploy the whole
>   thing as one unit (Helm / Fly / Railway / Render / compose). API-only
>   consumption is supported for mobile and existing-app cases but is
>   secondary. To make this DX bulletproof, we need: a `make brand` flow,
>   a canonical `starter` example, a Build/Operate/Customers/Infrastructure
>   IA refactor, an Adapters page, deploy-target CI checks, and the 11
>   completeness features. Once that's done, no one is closer to "the open
>   backend for AI products" than us — because the players who could build
>   it (Supabase / Appwrite / Nhost) have commercial reasons not to.
