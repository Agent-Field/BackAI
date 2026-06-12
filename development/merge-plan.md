# Merge Plan

## Branch Strategy

Base branch:

```text
supportdesk-first-dx
```

Worker branches:

```text
supportdesk-shell
supportdesk-demo-provider
supportdesk-walkthrough
supportdesk-compose
supportdesk-railway
supportdesk-docs-examples
supportdesk-verification
```

Each worker branch should be rebased or merged onto `supportdesk-first-dx`
before final review.

## Merge Order

Recommended order:

1. M0 planning baseline.
2. M1 product shell.
3. M2 demo provider.
4. M3 walkthrough.
5. M4 compose first-run.
6. M5 Railway/deploy templates.
7. M6 docs/examples.
8. M7 verification fixes.

Why this order:

- Product shell clarifies names and routes before runtime wiring.
- Demo provider creates the evidence that walkthrough and admin need.
- Compose should integrate the app once core flow exists.
- Deploy templates should mirror the working local path.
- Docs should describe the tested shape, not an aspirational shape.

## Conflict Zones

| Area | Likely conflicting workers | Mitigation |
| --- | --- | --- |
| `README.md` | A, F | Worker A owns first rewrite; Worker F updates after implementation. |
| `apps/customer-app` | A, C, D | A changes shell, C changes walkthrough, D changes env/ports only. |
| `apps/dashboard` | A, C | A owns copy/nav, C owns deep-link evidence surfaces. |
| `services/runtime/internal/server` | B, C | B owns data/event contract, C consumes it. |
| `docker-compose.yml` | D, E | D owns dev compose, E owns production/deploy templates. |
| `deploy/*` | E only | Other workers should avoid deploy files. |
| `examples/README.md` | F only | Other workers add notes through F. |

## Commit Style

Use small commits by milestone:

```text
docs: add SupportDesk-first development plan
feat(customer): rebrand starter app to SupportDesk
feat(runtime): add no-key SupportDesk demo provider
feat(customer): link first action to admin evidence
chore(compose): make customer app part of first-run stack
chore(deploy): add customer app to Railway template
docs: add example capability manifests
test: add SupportDesk first-run smoke checks
```

## Review Gates

Before merging a worker branch:

1. `git diff --stat` is scoped to the worker brief.
2. No unrelated Shipwright handler changes are included.
3. Relevant tests or checks pass.
4. Any disabled external dependency degrades clearly.
5. `development/status.md` is updated with evidence.

## Push Strategy

Push each merged milestone so other workers can rebase:

```bash
git push origin supportdesk-first-dx
```

If a worker branch is pushed separately, include the worker name:

```bash
git push origin supportdesk-demo-provider
```

## Handling Existing Dirty Work

Current unrelated dirty file:

```text
examples/02-shipwright/handlers/handler.py
```

Do not stage it in SupportDesk-first commits unless the user explicitly asks
to include Shipwright work.

Use explicit path staging:

```bash
git add development SUPPORTDESK-FIRST-DX-PLAN.md
```

Avoid broad `git add .` while unrelated work remains.
