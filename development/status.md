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
| M3 | Customer-to-admin walkthrough | In progress | Codex | Customer action now carries `X-Request-ID`; cost ledger stores/filter by request id; admin cost page accepts `request_id`. |
| M4 | Compose first-run | Not started | TBD | `docker compose up` boots full first experience. |
| M5 | Railway deploy | Not started | TBD | Railway template includes customer app and can deploy in demo mode. |
| M6 | Docs and examples | Not started | TBD | Docs flow and capability manifests landed. |
| M7 | Verification sweep | Not started | TBD | Local E2E, SDK checks, docs/build/deploy checks pass. |

## Merge Log

| Date | Commit | Scope | Verification |
| --- | --- | --- | --- |
| 2026-06-12 | `f4208f7` | Planning baseline | Docs review only. |
| 2026-06-12 | `7601ebb` | M1 product shell | `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`; `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`. |
| 2026-06-12 | `7fe011b` | M2 demo provider | `GOCACHE=/tmp/backai-go-build go test ./services/runtime/...`; `docker compose config --quiet`. |
| 2026-06-12 | Pending | M3 customer-to-admin walkthrough | `GOCACHE=/tmp/backai-go-build go test ./services/runtime/internal/cost ./services/runtime/internal/server ./services/runtime/cmd/af-stack`; `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false`; `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`. |

## Current Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Customer app still feels like a SWE-AF demo. | Users misunderstand the repo as a coding-agent product. | Rebrand and restructure around SupportDesk first. |
| No-key demo mode could feel fake. | Loss of trust. | Label demo mode explicitly and write real platform records. |
| Railway template omits customer app. | Hosted deploy does not match local promise. | Add customer app service and demo-mode defaults. |
| Dashboard exposes too many advanced modules. | First-run feels heavy. | Hide disabled modules and focus walkthrough on live evidence. |
| AgentField becomes too loud or invisible. | Brand confusion or lost second-order adoption. | Subtle UI presence, stronger architecture docs. |

## Next Concrete Work

1. Commit and push M3 request-id walkthrough.
2. Add a guided walkthrough entry point that tells users when to jump from the
   customer app to admin.
3. Start M4 compose first-run: make `docker compose up` boot the complete
   SupportDesk demo path.
4. Keep the first vertical slice small enough to verify locally.
