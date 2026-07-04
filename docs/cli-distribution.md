# CLI Distribution

## Install (available now)

The `af-stack` CLI is a single static Go binary. Three ways to get it —
all pull straight from this GitHub repo's Releases:

**1. Install script** (Linux / macOS) — downloads the latest release binary,
verifies its checksum, and puts it on your PATH:

```bash
curl -fsSL https://raw.githubusercontent.com/Agent-Field/backai/main/scripts/install.sh | bash
```

Pin a version or install dir with env: `AF_STACK_VERSION=v0.6.0`,
`AF_STACK_INSTALL_DIR="$HOME/.local/bin"`. Source:
[`scripts/install.sh`](../scripts/install.sh).

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

```bash
curl -fsSL https://backai.dev/install.sh | bash
```

One line. Same as AF. Sets up `af-stack` on PATH.

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

```bash
# Fork bootstrap
af-stack init --name "DocuChat" --color "#0A66C2" --logo ./logo.png

# Dev loop
af-stack dev --detach

# Scaffolds
af-stack agent new <name>
af-stack module new <id>
af-stack plugin new <id>

# Tools
af-stack mcp add/list/remove <url>
af-stack adapter list

# Identity + multi-tenancy (shipped: operator/keys/tenants/sessions)
af-stack operator create --email <email>    # allow a dashboard operator
af-stack operator key [--owner]             # mint an operator API key (needs DATABASE_URL)
af-stack keys list/issue/rotate/revoke/spend
af-stack tenants list
af-stack sessions list/revoke
af-stack user create/list/disable           # planned
af-stack secrets set/get/list/delete/rotate # planned

# Observability (shipped — all take AF_STACK_API_KEY = operator key)
af-stack logs --level error --limit 100
af-stack errors list/resolve/mute/reopen
af-stack audit --tenant <id>
af-stack runs --agent <id> --status failed
af-stack agents list
af-stack reasoners
af-stack activity --tenant <id>

# Database
af-stack db migrate / rollback / status     # planned

# Deploy
af-stack deploy --target=fly|railway|render|helm

# Misc
af-stack import-module <github-url>
af-stack self-update
```

Every CLI command maps to a documented REST endpoint or admin SDK call.
Operators can script via CLI; programmers can script via SDK.

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
- No silent self-update; `af-stack self-update` is explicit

## What we explicitly don't do

| Thing                                     | Why not                                                        |
| ----------------------------------------- | -------------------------------------------------------------- |
| npm wrapper for CLI                       | Fragments the install story; adds Node dep                     |
| bunx                                      | Niche audience                                                 |
| pipx                                      | Wrong tool for cross-language CLI                              |
| Auto-update daemon                        | User-explicit only                                             |
| Multiple binaries (CLI + server separate) | Single binary, modal commands (`af-stack serve` is the server) |

## Reference

- AgentField install pattern: `curl -fsSL https://agentfield.ai/install.sh | bash`
- goreleaser: https://goreleaser.com
- Caddy distribution: https://github.com/caddyserver/caddy/releases
- Stripe CLI: https://github.com/stripe/stripe-cli
- Supabase CLI: https://github.com/supabase/cli
