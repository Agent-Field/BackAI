# Public-Ready Audit

Last updated: 2026-06-12

This file is the current completion audit for the SupportDesk-first public repo
work. It records what is proven by current files and local runtime evidence,
and what still needs stronger proof before calling the whole goal complete.

## Scope

Original objective:

- Create a thorough `development/` coordination folder.
- Split the public-ready work into milestones and worker-friendly packets.
- Reorganize the repo so public root files are clean and durable.
- Make SupportDesk AI the default customer app and first experience.
- Keep AgentField as a subtle core substrate, not the headline product.
- Support no-key demo mode and real-provider mode through the same flow.
- Include customer app, admin dashboard, walkthrough, deploy path, examples,
  and production path.
- Commit and push scoped work as it lands.
- Validate actual end-to-end behavior, including OpenRouter-backed mode.

## Evidence Matrix

| Requirement | Current evidence | Status |
| --- | --- | --- |
| Development coordination folder exists | `development/README.md`, `status.md`, `milestones.md`, `worker-briefs.md`, `merge-plan.md`, `verification.md`, `decisions.md` | Proven |
| Milestones are tracked with completion evidence | `development/status.md` records M0-M10 with commit IDs and verification notes | Proven |
| Root repo is clean for public readers | Root tracked files are operational/public only: README, license/security/contributing, env, compose, deploy/docs/examples/apps/packages/services | Proven |
| Planning docs moved away from root | Planning and strategy files live under `development/`; durable docs live under `docs/` | Proven |
| Customer app is first-run product | README quickstart opens `http://localhost:34000`; app title is SupportDesk AI; local browser opened `/sign-up` successfully | Proven |
| No-key demo mode works | Local demo mode previously verified with blank provider keys, SupportDesk request `codex-demo-1781283223`, provider `demo`, cost event recorded | Proven |
| Real provider mode works | Local real mode previously verified with OpenRouter key from zsh, request `codex-real-1781283477`, provider `litellm`, nonzero cost event | Proven |
| Customer-to-admin evidence handoff works | Request IDs are carried into cost events; admin deep-link query preservation fixed in `677da9d`; local admin login/setup verified | Proven |
| Local compose default matches docs | `docker compose config --quiet` passes; current stack exposes runtime `8080`, admin `33000`, customer `34000`; README matches | Proven |
| Port conflicts are handled without stopping other services | `scripts/preflight.mjs` passes for current BackAI stack, fails clearly for occupied ports, and fails before Docker starts when two BackAI services use the same host port | Proven |
| Advanced Shipwright surface is not default first-run nav | `AF_STACK_SHOW_SHIPWRIGHT=false` default; admin layout passes `showShipwright:false`; Shipwright nav hidden by default | Proven |
| Examples declare capabilities | `ruby -e 'require "yaml"; ...' examples/*/capabilities.yaml` passes for starter, Notable, Shipwright, LLM gateway only, Deep Research | Proven |
| Shipwright example follows current compose model | `docker compose -f docker-compose.yml -f examples/02-shipwright/docker-compose.yml config --quiet` passes; docs no longer reference removed override | Proven |
| Public docs have valid relative markdown links | Node link checker over README, docs, deploy, examples, customer app, dashboard, and development checked 83 repo-facing markdown files | Proven |
| Dashboard and customer app typecheck | `pnpm --dir apps/dashboard exec tsc --noEmit --pretty false`; `pnpm --dir apps/customer-app exec tsc --noEmit --pretty false` | Proven |
| Local rebuilt apps run | `docker compose up -d --build dashboard customer-app`; runtime `/ready`; customer `/sign-up`; compose `ps` healthy | Proven |
| Railway template exists and validates statically | Earlier `python3 scripts/validate-deploy-targets.py` and Railway JSON formatting passed | Proven statically |
| Live Railway deploy works | Not run in this session | Not proven |
| CI/CD green | User asked to stop CI/CD follow-up for now | Deferred |
| Full repo tests | Runtime/app focused suites were run earlier; no full final all-language test sweep after latest docs/nav cleanup | Partially proven |

## Current Local First Experience

Running locally in demo mode:

| Surface | URL |
| --- | --- |
| Customer app | `http://localhost:34000/sign-up` |
| Admin dashboard | `http://localhost:33000/login` |
| Runtime | `http://localhost:8080` |
| AgentField control plane | `http://localhost:8081` |
| LiteLLM | `http://localhost:4000` |

Local accounts after the database reset:

| Surface | Email | Password |
| --- | --- | --- |
| Customer app | `demo@backai.local` | `backai-demo-pwd` |
| Admin operator | `operator@backai.local` | `backai-admin-pwd` |

## Remaining Before Goal Completion

These are the items that should be checked before marking the full objective
complete:

1. Run one final full verification sweep after deciding whether to keep the
   unrelated local Shipwright handler change.
2. Re-run real-provider SupportDesk E2E once more after all nav/docs changes,
   using the OpenRouter key from zsh.
3. If launch requires hosted proof, perform a live Railway deploy check.
4. Resume CI/CD check only when explicitly allowed.
5. Decide whether archived screenshot logs that mention the old `af-stack`
   GitHub repo should be regenerated or left as historical artifacts.
