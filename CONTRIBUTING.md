# Contributing

Thanks for your interest in contributing to AF Stack (working name).

This project is in early build. The shape will change. Issues, PRs, and
ideas all welcome.

## Quick start for contributors

```bash
git clone https://github.com/Agent-Field/backai
cd backai
cp .env.example .env
make dev
```

Read `README.md` for orientation, `PLAN.md` for architecture,
`ROADMAP.md` for what's being built next.

## Development workflow

- Create an issue describing the change before opening a PR for non-trivial
  work
- Reference the issue in your PR description
- Keep PRs small and focused; large PRs are hard to review
- Run `make lint` and `make test` before pushing
- Sign your commits if you can (not required)

## Pre-commit hooks

We use [lefthook](https://github.com/evilmartians/lefthook) for fast,
parallel pre-commit checks across Go, Python, and TypeScript.

```bash
# install lefthook (one-time)
brew install lefthook       # macOS
# or: scoop install lefthook (Windows)
# or: see https://github.com/evilmartians/lefthook for other platforms

# enable hooks in this repo
lefthook install
```

Hooks run automatically on `git commit`. To skip once: `LEFTHOOK=0 git commit`.

## Code style

- **Go**: `gofmt`, `golangci-lint` rules in `.golangci.yml`
- **Python**: `ruff`, `mypy` strict where applied
- **TypeScript**: `eslint`, `prettier`, strict `tsconfig`
- **Commits**: Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`,
  `refactor:`, `test:`)

## Testing

- Each package has a unit test suite
- Integration tests run against `docker compose up`
- CI runs lint, unit tests, integration tests on every PR
- Don't break the 60-second quickstart

## Issue and PR labels

- `phase:0` … `phase:16` — which roadmap phase
- `area:runtime` / `area:dashboard` / `area:sdk-py` / `area:sdk-ts` /
  `area:sdk-go` / `area:docs` / `area:ci` / `area:modules`
- `type:feat` / `type:fix` / `type:chore` / `type:docs` / `type:test` /
  `type:refactor`
- `priority:p0` (blocking) / `priority:p1` (next) / `priority:p2` (later)
- `good first issue` for newcomer-friendly tasks

## License

By contributing, you agree your contributions are licensed under
Apache 2.0 (see `LICENSE`).
