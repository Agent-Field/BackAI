# CLI Distribution

## Install (available now)

The `af-stack` CLI is a single static Go binary. Three ways to get it —
all pull straight from this GitHub repo's Releases:

**1. Install script** (Linux / macOS) — downloads the latest release binary,
verifies its checksum, and puts it on your PATH:

```bash
curl -fsSL https://raw.githubusercontent.com/Agent-Field/backai/main/scripts/install.sh | bash
```

Pin a version or install dir with env: `AF_STACK_VERSION=v0.12.4` (bare
`0.12.4` works too), `AF_STACK_INSTALL_DIR="$HOME/.local/bin"`. The
script resolves the latest tag from the `releases/latest` redirect (no
GitHub API rate limit), verifies the archive against the release's
`checksums.txt` and refuses to install if that file cannot be fetched
(`AF_STACK_SKIP_CHECKSUM=1` overrides), and can pull both files from a
mirror instead of GitHub with `AF_STACK_DOWNLOAD_BASE=https://…`. When the
install dir is not on your PATH it prints the `export PATH=…` line to run.
Source: [`scripts/install.sh`](../scripts/install.sh).

**2. `go install`** (any platform with Go ≥ 1.25):

```bash
go install github.com/Agent-Field/backai/services/cli/cmd/af-stack@latest
```

On older Go toolchains, set `GOTOOLCHAIN=auto` so Go fetches the pinned
version. Note: `go install` does not stamp the release version into
`af-stack version` (only the release binaries do).

**3. Direct download** from the
[Releases page](https://github.com/Agent-Field/backai/releases) — each
release attaches `af-stack_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows)
plus `checksums.txt` for `{linux,darwin,windows} × {amd64,arm64}`. Extract
and move `af-stack` onto your PATH.

## Strategy

**Single Go binary. No npm wrapper, no bunx, no pipx** — one distribution
path keeps the install story consistent across the AF ecosystem, matching
AgentField's `curl … | bash` shape.

## Goal

Lowest friction "0 → deploy", modular and powerful after that.

The quickstart in README doesn't even need the CLI. The CLI is for daily
power features after first contact.

### First contact (no install required)

```bash
git clone https://github.com/Agent-Field/backai my-app
cd my-app
cp .env.example .env
docker compose up
```

Four commands. Works on any machine with git + docker. Browser opens
dashboard, the dev is "in."

### Install CLI for power features

One line — the install script from
[Install (available now)](#install-available-now) above. It puts `af-stack`
on your PATH.

## Distribution channels

Same Go binary, multiple channels. Status reflects what is wired today.

| Channel                      | Install                                                                              | Status                                   |
| ---------------------------- | ----------------------------------------------------------------------------------- | ---------------------------------------- |
| **Install script** (primary) | `curl -fsSL https://raw.githubusercontent.com/Agent-Field/backai/main/scripts/install.sh \| bash` | **Available** ([`scripts/install.sh`](../scripts/install.sh)) |
| `go install`                 | `go install github.com/Agent-Field/backai/services/cli/cmd/af-stack@latest`         | **Available**                            |
| Direct binary                | [GitHub Releases](https://github.com/Agent-Field/backai/releases)                   | **Available** (goreleaser cross-compile) |
| Homebrew (macOS)             | `brew install agent-field/tap/af-stack`                                              | Planned — needs the `agent-field/homebrew-tap` repo |
| Scoop (Windows)              | `scoop install af-stack`                                                             | Planned — needs the Scoop bucket repo    |

> A vanity `https://backai.dev/install.sh` redirect to the raw GitHub script
> can be added later; the raw URL above works today without any extra hosting.
> No CLI Docker image is published — the runtime/dashboard/customer-app
> images live at `ghcr.io/agent-field/af-stack-*`, not the CLI.

## Why this is strategic

| Reason                           | Detail                                                        |
| -------------------------------- | ------------------------------------------------------------- |
| Matches AgentField               | Same `curl install.sh` pattern; consistent ecosystem identity |
| Cross-language                   | Python and JS devs both get one install path                  |
| No runtime dep                   | Go binary runs standalone; no node/python required at runtime |
| Fast                             | Native binary; no Node startup tax for every command          |
| Code sharing                     | Suite runtime is Go; CLI shares internal packages             |
| Industry standard for this shape | Caddy, Gitea, Stripe CLI, flyctl, gh, Supabase all use this   |
| Doesn't fragment                 | One install command, one tool, one mental model               |

## What the CLI does

### Shipped today

Every command below exists in the current binary (see
[`services/cli/cmd/af-stack/main.go`](../services/cli/cmd/af-stack/main.go)).

```bash
# Fork bootstrap + dev loop (run inside a clone of this repo)
af-stack init --name "DocuChat" --color "#0A66C2"
# optional: --logo ./your-logo.svg sets the light+dark mark in brand.yaml
af-stack dev --detach
af-stack mode personal|saas                  # auth+billing off ⇄ multi-tenant SaaS
af-stack upgrade [--check]                   # pull latest upstream into this fork

# Scaffolds (run inside a clone of this repo)
af-stack agent new <name>
af-stack module new <id>
af-stack plugin new <id>
af-stack adapter new <slot> [name] [--dir <parent>]   # remote-adapter sidecar (no checkout needed)

# Validate a scaffold — offline, no runtime and no key
# (exit 0 valid, 5 failed validation, 4 directory missing, 2 bad args)
af-stack module validate <dir> [--json]      # e.g. workload-modules/notes
af-stack agent validate <dir> [--json]       # e.g. apps/backend/agents/supportdesk

# Tools
af-stack mcp list/add/remove/call <name>
af-stack adapter list

# Tenant secrets vault (AF_STACK_API_KEY = tenant key)
af-stack secrets set <key> [--value-stdin] [--description]
af-stack secrets list [--json]               # metadata + secret:<key> refs only

# Migrations (goose; needs DATABASE_URL or AF_STACK_DATABASE_URL, and a checkout or --dir)
af-stack db diff|push|generate|reset         # `status` aliases `diff`, `push` aliases goose up

# Identity + multi-tenancy (operator/keys/tenants/sessions)
# Both operator commands talk to Postgres directly, so both need
# DATABASE_URL (or AF_STACK_DATABASE_URL) — not a running runtime.
af-stack operator create --email <email>     # allow a dashboard operator
af-stack operator key [--owner]              # mint an operator API key
af-stack keys list/issue/rotate/revoke/spend
af-stack tenants list
af-stack sessions list/revoke

# Billing
af-stack billing ...                         # set up Stripe plans + pricing (agent-first)

# Observability (all take AF_STACK_API_KEY = operator key)
af-stack logs --level error --limit 100
af-stack errors list/resolve/mute/reopen
af-stack audit --tenant <id>
af-stack runs --agent <id> --status failed
af-stack agents list
af-stack reasoners
af-stack activity --tenant <id>

# Deploy (run inside a clone of this repo)
af-stack deploy helm|fly|railway|render
```

Every *shipped* CLI command maps to a documented REST endpoint or admin
SDK call. Operators can script via CLI; programmers can script via SDK. Flags,
REST endpoints and exit codes live in
[`docs/cli-admin.md`](cli-admin.md) — see [Secrets](cli-admin.md#secrets) and
[Diagnostics & migrations](cli-admin.md#diagnostics--migrations) for the two
groups above.

> **Fork upgrade gotcha.** The compiled `bin/af-stack` committed in an
> older fork predates newer subcommands. Rebuild the CLI before running
> `af-stack upgrade` — `make build-cli` (which runs
> `go build -o bin/af-stack ./services/cli/cmd/af-stack`) — then rebuild
> the images afterwards (`docker compose build`) so the runtime and
> frontends match the upgraded source.

### Planned / not yet shipped

These are **not runnable against a current binary** — they are on the
roadmap. Don't assume they exist.

```bash
af-stack user create/list/disable            # planned
af-stack secrets get/delete/rotate           # planned — set/list ship today; the runtime exposes metadata GET, DELETE and /rotate over REST
af-stack db down <n>                         # planned — `af-stack db reset` rolls all the way back today
af-stack import-module <github-url>          # planned
af-stack self-update                         # planned — use `af-stack upgrade` to pull upstream into a fork
```

## Build system

**goreleaser** (industry standard for Go CLI distribution). Single config
produces:

- Binaries for `{linux,macos,windows} x {amd64,arm64}`
- Homebrew formula auto-PR'd to tap repo
- Scoop manifest auto-PR'd to bucket repo
- Docker images
- GitHub Release with checksums + changelog
- Linux distro packages (v2: deb/rpm via nfpm)

Trigger on git tag push.

## Versioning

- CLI and suite runtime share a version (same monorepo, same release)
- CLI v1.x talks to suite runtime v1.x (semver)
- CLI checks server version on first connect, warns on mismatch
- No silent self-update. Binary self-update (`af-stack self-update`) is
  planned; today `af-stack upgrade` pulls the latest upstream stack into
  your fork.

## What we explicitly don't do

| Thing                                     | Why not                                                        |
| ----------------------------------------- | -------------------------------------------------------------- |
| npm wrapper for CLI                       | Fragments the install story; adds Node dep                     |
| bunx                                      | Niche audience                                                 |
| pipx                                      | Wrong tool for cross-language CLI                              |
| Auto-update daemon                        | User-explicit only                                             |
| A CLI Docker image                        | The CLI is a single static Go binary from Releases; the runtime, dashboard and customer-app ship as their own images (`ghcr.io/agent-field/af-stack-*`) |

## Reference

- AgentField install pattern: `curl -fsSL https://agentfield.ai/install.sh | bash`
- goreleaser: https://goreleaser.com
- Caddy distribution: https://github.com/caddyserver/caddy/releases
- Stripe CLI: https://github.com/stripe/stripe-cli
- Supabase CLI: https://github.com/supabase/cli
