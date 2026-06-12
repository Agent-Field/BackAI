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
| M6 | Docs and examples | Complete | Codex | Public docs now cover demo mode, attach-existing-app mode, repo ownership, example capabilities, Railway first run, and cleaned BackAI naming. |
| M7 | Verification sweep | Complete | Codex | Runtime Go suite, app typechecks/builds, compose config, deploy validation, Railway JSON, manifest parsing, and public text scan pass. |
| M8 | Root repo hygiene | Complete | Codex | Root now keeps operational/public files only; planning docs moved to `development/`, durable docs/assets moved to `docs/`, `brand.yaml` is canonical, and the tracked root binary is removed/ignored. |
| M9 | Local first-experience launch | Complete | Codex | Demo mode and real-provider mode both launched locally; customer signup, onboarding key, SupportDesk request, admin cost ledger, and request-id deep link verified. |
| M10 | Local port conflict DX | Complete | Codex | `scripts/preflight.mjs` detects occupied local ports and duplicate BackAI host-port assignments, prints override env vars, and does not stop unrelated services; compose defaults now bind runtime on `8080`. |
| M11 | Public-ready completion audit | Complete | Codex | `development/public-ready-audit.md` maps original requirements to current evidence, remaining proof gaps, local URLs, and local credentials. |
| M12 | Guided AgentField first run | Complete | Codex | SupportDesk now registers as an AgentField agent with a 10-reasoner graph; customer/admin driver.js tours are live; customer SupportDesk action shows billing branch, nested policy/evidence reasoners, execution link, cost/tokens, and admin evidence link; admin Agents links agent/reasoner badges to AgentField discovery. |

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
| 2026-06-12 | `741d66e` | M4 OpenRouter E2E | Runtime restarted with `AF_STACK_DEMO_MODE=false` and OpenRouter key from zsh; logs showed `llm gateway: litellm sidecar`; customer LLM proxy returned real provider text; cost event for `codex-openrouter-1781281222` recorded provider `litellm` and nonzero cost. |
| 2026-06-12 | `ea210cd` | M5 Railway template | `python3 -m json.tool deploy/railway/railway.json`; `python3 scripts/validate-deploy-targets.py`. |
| 2026-06-12 | `3976fd1` | M6 demo mode docs | `rg -n "github.com/<you>/af-stack|AF_STACK_DEMO_MODE|demo-supportdesk|Railway|no-key|BackAI|SupportDesk" README.md docs/demo-mode.md deploy/railway/README.md deploy/README.md docs/deploy.md`; `python3 scripts/validate-deploy-targets.py`. |
| 2026-06-12 | `a1de6b5` | M6 repo and examples DX | `ruby -e 'require "yaml"; ...' examples/*/capabilities.yaml`; `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`; `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`; public text scan has no `AF Stack`, `SWE-AF`, or `coming soon` hits outside code comments/archive. |
| 2026-06-12 | `6ab9cce` | M7 final verification | `ruby -e 'require "yaml"; ...' examples/*/capabilities.yaml`; `rg -n "coming soon|SWE-AF|AF Stack" ...`; `docker compose config --quiet`; `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`; `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`; `python3 -m json.tool deploy/railway/railway.json`; `python3 scripts/validate-deploy-targets.py`; `GOCACHE=/tmp/backai-go-build go test ./services/runtime/...`; `pnpm --dir apps/customer-app build`; `pnpm --dir apps/dashboard build`. |
| 2026-06-12 | `b8d9ff5` | M8 root repo hygiene | Root listing audited; root binary removed and ignored; `BRAND.yaml` renamed to `brand.yaml`; planning files moved under `development/`; public docs and screenshots moved under `docs/`; stale root doc and old repo URL references scanned and corrected. |
| 2026-06-12 | `677da9d` | Admin deep-link query fix | During local first-experience testing, unauthenticated admin cost links preserved `request_id` at `/login` but dropped it from `next`; middleware now redirects to `/login?next=/operate/cost?request_id=...`. Verified with `curl -I`. |
| 2026-06-12 | `a49003f` | M9 local first-experience launch | Demo mode: `AF_STACK_DEMO_MODE=true` with blank provider keys, customer signup `demo-1781283223@example.com`, request `codex-demo-1781283223`, provider `demo`, cost event `0.000113`. Real mode: OpenRouter key from zsh, runtime logs `llm gateway: litellm sidecar`, request `codex-real-1781283477`, provider `litellm`, cost event `0.000048`. |
| 2026-06-12 | `9b496d0` | M10 local port conflict DX | `node scripts/preflight.mjs` passes when the current BackAI compose stack owns ports; intentional conflict `AF_STACK_PORT=33000 node scripts/preflight.mjs` fails with a clear override message; `docker compose config --quiet` passes. |
| 2026-06-12 | `38dee6b` | Hide advanced Shipwright from first run | `AF_STACK_SHOW_SHIPWRIGHT=false` by default; dashboard/customer typechecks pass; Shipwright compose overlay config passes; rebuilt local apps; runtime `/ready` and customer `/sign-up` return 200; admin render includes `showShipwright:false`. |
| 2026-06-12 | Current change | M11 public-ready audit and conflict DX cleanup | Markdown link checker over 83 repo-facing files passes; public stale-reference scan reviewed; examples capabilities parse; preflight current-stack, occupied-port, and duplicate host-port checks behave correctly; app typechecks and compose config pass. |
| 2026-06-12 | Current change | First-time wow path | Fresh browser customer flow: `Use demo details` -> API key reveal -> workspace first-run panel -> Support Desk draft -> `$0.000134` / `268 tok` result -> exact admin cost deep link. Admin login verified locally after resetting `operator@backai.local` to `backai-admin-pwd`. |
| 2026-06-12 | Current change | M12 guided AgentField first run | Runtime execution `exec_20260612_183959_l8bkscyw` succeeded through `supportdesk.reply_plan` with `graph_depth: 3`, `billing_policy_review`, `refund_guardrail`, and `billing_evidence_check`. AgentField discovery returns `supportdesk` with 10 reasoners; runtime `/api/v1/agents` projects the same reasoners for admin. Browser verification passed: customer tour `This is the customer product`, Support Desk tour `One action, two backend proofs`, admin tour `Start from the customer action`, Agents tour `AgentField discovery, not mock metadata`, nested graph visible, AgentField discovery link for `billing_evidence_check` opened, console errors `0`. |

## Current Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Advanced Shipwright code still exists as an example and route. | Users could mistake advanced coding-agent paths for the default product if they enable it too early. | Hidden from default first-run nav with `AF_STACK_SHOW_SHIPWRIGHT=false`; public docs frame it as an advanced example. |
| No-key demo mode could feel fake. | Loss of trust. | Label demo mode explicitly and write real platform records. |
| Railway live deploy has not been run after the final docs/nav cleanup. | Hosted launch proof is static, not live. | Run a live Railway deploy check before public announcement if hosted proof is required. |
| CI/CD follow-up is paused by user request. | PR merge readiness is not fully observed from GitHub checks. | Resume PR checks only when explicitly allowed. |
| Dashboard still contains advanced modules behind flags. | First-run can feel heavy if flags are changed without intent. | Keep disabled modules hidden by default and focus walkthrough on live evidence. |
| AgentField becomes too loud or invisible. | Brand confusion or lost second-order adoption. | Subtle UI presence, stronger architecture docs. |
| No-key demo response can leak prompt internals. | First-run feels fake or noisy. | Demo provider now strips AgentField plan context from the visible reply; the plan remains in the dedicated evidence panel. |

## Next Concrete Work

1. Re-run real-provider SupportDesk E2E once more after final audit cleanup.
2. Run a live Railway deploy check if launch requires hosted proof.
3. Monitor PR #118 checks only after CI/CD work resumes.
4. Decide whether archived screenshot logs with old `af-stack` issue URLs should remain historical or be regenerated.
