# Contributing

Thanks for poking around AF Stack. Early build. Stuff change. PRs welcome.

## Quickstart

```bash
git clone https://github.com/Agent-Field/backai
cd backai
cp .env.example .env
make dev          # boots docker compose + runtime
make test         # go + ts + py
```

Read `README.md` for orientation, `PLAN.md` for architecture,
`ROADMAP.md` for what's next.

## Code style

Run formatters before pushing. Configs already in repo are the source of truth.

- **Go**: `gofmt`, `goimports`, `golangci-lint run` (config: `.golangci.yml`)
- **TypeScript**: `prettier --write`, `eslint` (configs: `.prettierrc`, per-app `eslint.config.*`)
- **Python**: `ruff format`, `ruff check` (config: `pyproject.toml`)

`lefthook install` hooks them into `git commit`. Skip once: `LEFTHOOK=0 git commit`.

## Tests

- New feature → tests in the same PR. No tests, no merge.
- Coverage is not a hard gate. PRs that drop overall coverage by **> 2 points**
  get extra scrutiny — bring a reason.
- Integration tests use `docker compose up`. Don't break the 60-second quickstart.

## Merging to `main`

`main` is protected for the public release. Do not push to it directly.

- Open a PR targeting `main`.
- `CI Success` and `Security Success` must be green.
- One approving review; resolve review threads before merge.
- Prefer squash merge. Re-sign the squash commit (`git commit -s`) if
  GitHub does not add the DCO trailer automatically.

A repo admin applies or updates the GitHub ruleset with
`scripts/apply-branch-protection.sh`. Details:
[`docs/branch-protection.md`](docs/branch-protection.md).

## Commits

- **DCO sign-off required.** Every commit must carry `Signed-off-by: Your Name <you@example.com>`.
  Easiest: `git commit -s`. No CLA, no paperwork.
- **Conventional Commits encouraged** (`feat:`, `fix:`, `docs:`, `chore:`,
  `refactor:`, `test:`). Makes changelogs nice. Not enforced.
- Keep PRs small. Reference the issue you're closing.

## How to add a workload module

See `docs/workload-modules.md`. Short version: implement the module
interface, register it, write a test, add a docs page.

## How to add a dashboard plugin

See `docs/dashboard-plugins.md`. Drop a plugin under `apps/dashboard/plugins/`,
declare its nav entries, run `pnpm generate:plugins`.

## How to add an adapter (LLM provider, storage, queue, etc.)

The LLM provider adapter is the canonical reference. Read
`services/runtime/internal/llmgateway/providers/` and copy that shape.

1. Implement the provider interface in a new file under `providers/`.
2. Wire it into the factory switch in
   `services/runtime/cmd/af-stack/main.go` (or the module's own factory).
3. Write a unit test against a mocked upstream.
4. Add a docs page under `docs/` and link it from `docs-site/`.

Same shape applies for storage, queue, vector backends, etc. Find the
existing adapter type, copy its file structure, fill in the interface.

## Where to ask

- **Design questions** → GitHub Discussions
- **Bugs** → GitHub Issues
- **Chat** → Discord (placeholder: https://discord.gg/agentfield)

## Labels (for triagers)

- `phase:0` … `phase:16` — roadmap phase
- `area:runtime` / `area:dashboard` / `area:sdk-py` / `area:sdk-ts` /
  `area:sdk-go` / `area:docs` / `area:ci` / `area:modules`
- `type:feat` / `type:fix` / `type:chore` / `type:docs` / `type:test` / `type:refactor`
- `priority:p0` (blocking) / `priority:p1` (next) / `priority:p2` (later)
- `good first issue` for newcomer-friendly tasks

## License

By contributing, you agree your work is licensed under Apache 2.0
(see `LICENSE`). Third-party dependency licenses are inventoried in
`THIRD-PARTY-LICENSES.md`.
