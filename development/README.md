# SupportDesk-First Public Repo Development

This folder is the coordination surface for the SupportDesk-first BackAI
rewrite. It turns the product direction in `SUPPORTDESK-FIRST-DX-PLAN.md` into
parallelizable milestones, worker briefs, merge gates, and verification
commands.

## Goal

Make this repository public-ready as a viral AI app template:

```text
Clone or one-click deploy BackAI.
Open the polished SupportDesk AI customer app.
Use it without an LLM key in demo mode.
Click through to the admin dashboard.
See the exact request, tenant, cost, model, and run evidence.
Add a provider key when ready.
Customize the app and deploy the same repo to production.
```

AgentField remains a core AI substrate component, not the main product brand.
It should be discoverable in architecture docs, traces, and GitHub adjacency,
while the shareable product is the BackAI app template.

## Operating Rules

- Keep the default first experience simple.
- Hide disabled or advanced modules from the first-run walkthrough.
- Do not make SupportDesk depend on sandbox, S3, Stripe live mode, OAuth
  providers, MCP, coding harnesses, or Shipwright.
- Keep the OpenAI SDK as the fastest LLM path.
- Use Suite SDKs for platform-native APIs, not as a full OpenAI SDK clone.
- Every example must use the same deploy model and declare extra capabilities.
- Every milestone needs a verification command or runtime evidence before it
  can be marked complete.

## Development Files

| File | Purpose |
| --- | --- |
| `status.md` | Milestone board, current state, merge log, and completion evidence. |
| `milestones.md` | Detailed milestone scope, acceptance criteria, and gates. |
| `worker-briefs.md` | Parallel worker packets with inputs, outputs, and constraints. |
| `merge-plan.md` | Branch naming, sequencing, review strategy, and conflict zones. |
| `verification.md` | End-to-end local, Railway, SDK, docs, and public-readiness checks. |
| `decisions.md` | Product decisions, consequences, and open questions. |

## Milestone Summary

| Milestone | Name | Primary outcome |
| --- | --- | --- |
| M0 | Planning baseline | Shared plan, worker split, verification gates. |
| M1 | Product shell | BackAI + SupportDesk positioning and customer-first entry. |
| M2 | No-key demo loop | SupportDesk action works without provider keys and records evidence. |
| M3 | Customer-to-admin walkthrough | First user can jump from app action to exact admin evidence. |
| M4 | Compose first-run | `docker compose up` boots customer app, admin, runtime, substrate. |
| M5 | Railway deploy | One-click deploy includes customer app, admin, runtime, Postgres. |
| M6 | Docs and examples | Public docs explain quickstart, deploy, attach mode, and examples. |
| M7 | Verification sweep | Local and production-like checks prove public readiness. |

## Definition Of Public Ready

The repo is public-ready when all of these are true:

1. Fresh clone can run the SupportDesk customer app without an LLM key.
2. The admin dashboard shows evidence for the first SupportDesk action.
3. A real OpenRouter/OpenAI/Anthropic key can switch the same flow to real
   models without changing app code.
4. Railway deploy produces a usable customer app and admin dashboard.
5. The README and docs explain the product in the right order:
   app template first, AI backend second, substrate architecture later.
6. Disabled advanced features do not clutter the first-run experience.
7. Examples declare their capabilities instead of implying every AI app has
   identical external requirements.
8. The repo can be tested end-to-end with documented commands.

## Current Priority

Start with one coherent vertical slice:

```text
SupportDesk customer app -> no-key demo answer -> cost/request record ->
admin deep link -> README quickstart
```

Do not begin by refactoring every module. The first 10 minutes of the product
experience are the highest-leverage surface.
