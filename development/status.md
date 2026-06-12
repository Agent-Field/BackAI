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
| M0 | Planning baseline | In progress | Codex | `development/*` committed and pushed. |
| M1 | Product shell | Not started | TBD | README, customer app shell, dashboard copy updated. |
| M2 | No-key demo loop | Not started | TBD | Fresh clone supports first SupportDesk action without provider key. |
| M3 | Customer-to-admin walkthrough | Not started | TBD | Customer action deep-links to matching admin evidence. |
| M4 | Compose first-run | Not started | TBD | `docker compose up` boots full first experience. |
| M5 | Railway deploy | Not started | TBD | Railway template includes customer app and can deploy in demo mode. |
| M6 | Docs and examples | Not started | TBD | Docs flow and capability manifests landed. |
| M7 | Verification sweep | Not started | TBD | Local E2E, SDK checks, docs/build/deploy checks pass. |

## Merge Log

| Date | Commit | Scope | Verification |
| --- | --- | --- | --- |
| 2026-06-12 | Pending | Planning baseline | Docs review only. |

## Current Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Customer app still feels like a SWE-AF demo. | Users misunderstand the repo as a coding-agent product. | Rebrand and restructure around SupportDesk first. |
| No-key demo mode could feel fake. | Loss of trust. | Label demo mode explicitly and write real platform records. |
| Railway template omits customer app. | Hosted deploy does not match local promise. | Add customer app service and demo-mode defaults. |
| Dashboard exposes too many advanced modules. | First-run feels heavy. | Hide disabled modules and focus walkthrough on live evidence. |
| AgentField becomes too loud or invisible. | Brand confusion or lost second-order adoption. | Subtle UI presence, stronger architecture docs. |

## Next Concrete Work

1. Commit and push M0.
2. Start M1 with customer app rebrand and README rewrite.
3. Start M2 design in parallel: runtime demo provider and evidence-writing
   contract.
4. Keep the first vertical slice small enough to verify locally.
