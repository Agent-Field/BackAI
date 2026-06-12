# Status Board

Last updated: 2026-06-12

## Branch

Working branch:

```text
supportdesk-first-dx
```

Known unrelated worktree change to preserve:

```text
examples/02-shipwright/handlers/handler.py
```

Do not include that file in SupportDesk-first commits unless the scope
explicitly changes.

## Milestones

| ID | Milestone | Status | Owner | Completion evidence |
| --- | --- | --- | --- | --- |
| M0 | Planning baseline | Complete | Codex | `development/*` committed and pushed in `f4208f7`. |
| M1 | Product shell | Complete | Codex | README and customer app shell updated in `7601ebb`; customer/dashboard typechecks pass. |
| M2 | No-key demo loop | Complete | Codex | Demo chat provider implemented in `7fe011b`; runtime Go suite and compose config pass. |
| M3 | Customer-to-admin walkthrough | Complete | Codex | Customer action carries `X-Request-ID`, cost ledger stores/filter by request id, admin cost page accepts `request_id`, and customer workspace shows first-run steps. |
| M4 | Compose first-run | Complete | Codex | Compose builds and boots customer app, admin, runtime, demo provider, and request-id cost ledger path. |
| M5 | Railway deploy | Complete | Codex | Railway template now includes customer, admin, runtime, LiteLLM, AgentField, Postgres, demo mode, and private service wiring. |
| M6 | Docs and examples | Not started | TBD | Docs flow and capability manifests landed. |
| M7 | Verification sweep | Not started | TBD | Local E2E, SDK checks, docs/build/deploy checks pass. |

## Merge Log

| Date | Commit | Scope | Verification |
| --- | --- | --- | --- |
| 2026-06-12 | `f4208f7` | Planning baseline | Docs review only. |
| 2026-06-12 | `7601ebb` | M1 product shell | `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`; `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`. |
| 2026-06-12 | `7fe011b` | M2 demo provider | `GOCACHE=/tmp/backai-go-build go test ./services/runtime/...`; `docker compose config --quiet`. |
| 2026-06-12 | `e666364` | M3 customer-to-admin walkthrough | `GOCACHE=/tmp/backai-go-build go test ./services/runtime/internal/cost ./services/runtime/internal/server ./services/runtime/cmd/af-stack`; `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`; `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`. |
| 2026-06-12 | `c71efc0` | M4 compose first-run ports | `docker compose config --quiet`; `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`; `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`. |
| 2026-06-12 | `cd6b50c` | M3 first-run panel | `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`. |
| 2026-06-12 | `b17f442` | M4 no-key E2E | `docker compose up -d --build postgres litellm agentfield runtime dashboard customer-app`; customer signup via `http://localhost:34000/api/auth/sign-up/email`; onboarding key minted; customer LLM proxy returned `demo-supportdesk`; `GET /api/v1/cost/events?tenant=...&request_id=...` returned exactly one event. |
| 2026-06-12 | Pending | M4 OpenRouter E2E | Runtime restarted with `AF_STACK_DEMO_MODE=false` and OpenRouter key from zsh; logs showed `llm gateway: litellm sidecar`; customer LLM proxy returned real provider text; cost event for `codex-openrouter-1781281222` recorded provider `litellm` and nonzero cost. |
| 2026-06-12 | Pending | M5 Railway template | `python3 -m json.tool deploy/railway/railway.json`; `python3 scripts/validate-deploy-targets.py`. |

## Current Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Customer app still feels like a SWE-AF demo. | Users misunderstand the repo as a coding-agent product. | Rebrand and restructure around SupportDesk first. |
| No-key demo mode could feel fake. | Loss of trust. | Label demo mode explicitly and write real platform records. |
| Railway template omits customer app. | Hosted deploy does not match local promise. | Add customer app service and demo-mode defaults. |
| Dashboard exposes too many advanced modules. | First-run feels heavy. | Hide disabled modules and focus walkthrough on live evidence. |
| AgentField becomes too loud or invisible. | Brand confusion or lost second-order adoption. | Subtle UI presence, stronger architecture docs. |

## Next Concrete Work

1. Commit and push the Railway template update.
2. Add public docs for no-key mode vs real-key mode.
3. Start M6 docs/examples cleanup.
4. Keep the first vertical slice small enough to verify locally.
