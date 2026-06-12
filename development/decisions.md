# Product Decisions And Consequences

## D1: Lead With AI App Template

Decision:

BackAI should lead with "AI app template" and explain "AI backend" as the
category.

Consequence:

- More viral and immediately useful.
- Less abstract than category-only messaging.
- Requires the default app to be polished enough to carry the promise.

Rejected alternative:

- Lead with "AI backend" only.

Reason:

- Strategically strong but too abstract for first contact.

## D2: SupportDesk Is The Default Demo

Decision:

The default first experience is a polished SupportDesk AI app.

Consequence:

- Everyone understands the workflow.
- It naturally demonstrates auth, tenants, LLM gateway, cost tracking, admin,
  API keys, and billing stub.
- It avoids sandbox, S3, GitHub, and Stripe as first-run blockers.

Rejected alternative:

- Coding-agent/Shipwright first.

Reason:

- Impressive but biases users toward "agent automation infra" instead of
  "backend for AI apps".

## D3: Customer App First, Admin Reveal Second

Decision:

The first browser target is the customer app. The admin dashboard is a
separate surface reached during the walkthrough.

Consequence:

- Users feel they cloned a real SaaS product.
- The admin reveal proves the backend value.
- Production needs to deploy customer app, admin, and runtime.

Rejected alternative:

- Start in admin.

Reason:

- Feels like infrastructure software, not a viral app template.

## D4: No-Key Demo Mode

Decision:

SupportDesk must work without provider keys.

Consequence:

- Much better first-run and shareability.
- Requires explicit labeling to avoid fake-provider trust issues.
- Requires demo records to still exercise real platform surfaces.

Rejected alternative:

- Require OpenRouter/OpenAI key before first action.

Reason:

- Honest but too much dropoff before the product moment.

## D5: AgentField As Substrate

Decision:

AgentField is a core AI substrate component, not the main product brand.

Consequence:

- BackAI can be the shareable template.
- AgentField gains second-order adoption through architecture docs, trace
  links, and repo adjacency.
- Product UI must avoid over-branding AgentField.

Rejected alternative:

- Make AgentField the headline.

Reason:

- Dilutes the app-template strategy and competes with the "backend" message.

## D6: OpenAI SDK First, Suite SDK For Platform Power

Decision:

Use official OpenAI SDK compatibility as the fastest model-call DX. Suite SDKs
wrap platform-native primitives.

Consequence:

- Low client maintenance burden.
- Familiar adoption path.
- Suite can focus on BackAI-specific value.

Rejected alternative:

- Build a full OpenAI SDK clone under Suite.

Reason:

- High maintenance surface and little strategic value.

## D7: Railway Includes Customer App

Decision:

Railway one-click deploy must include customer app, admin, runtime, and
Postgres.

Consequence:

- Hosted first-run matches local first-run.
- More services in the template.
- Stronger viral demo and less product mismatch.

Rejected alternative:

- Railway deploys runtime and admin only.

Reason:

- Easier but misses the SupportDesk-first product promise.

## D8: Sandbox Is Not In First Deploy

Decision:

Sandbox is advanced and disabled by default for SupportDesk first-run and
Railway.

Consequence:

- No E2B key or Docker socket needed.
- Simpler deploy.
- Coding-agent examples must declare sandbox as an optional/required
  capability.

Rejected alternative:

- Include sandbox in first deploy.

Reason:

- Adds setup friction for a capability SupportDesk does not need.

## D9: Same Deploy Model, Capability-Specific Requirements

Decision:

Every example uses the same BackAI deploy model, but examples declare their
extra capabilities.

Consequence:

- Avoids snowflake demos.
- Keeps the platform promise honest.
- Lets heavy examples like Shipwright require sandbox/GitHub while SupportDesk
  remains no-key friendly.

Rejected alternative:

- Claim every example has identical deploy complexity.

Reason:

- False for real AI apps with uploads, sandboxes, webhooks, OAuth, or live
  billing.

## Open Questions

1. Final brand: BackAI, AF Stack, or another name?
2. Should SupportDesk live directly in `apps/customer-app`, or be selectable
   by example config?
3. Should demo mode be runtime-wide or SupportDesk-specific first?
4. Should local ports change immediately to `3000/3001/8080`?
5. Should AgentField be mandatory in Railway first deploy or gracefully
   degradable?
