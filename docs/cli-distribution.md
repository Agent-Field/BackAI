# CLI Distribution Strategy

## Decision

**Single Go binary, installed via curl script. Match AgentField's pattern.**

```bash
curl -fsSL https://backai.dev/install.sh | bash
```

Same install shape as AgentField (`curl -fsSL https://agentfield.ai/install.sh | bash`).
Devs who use one already know the other.

**No npm wrapper. No bunx. No pipx.** Single distribution path keeps the
install story consistent across the AF ecosystem.

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

## Distribution channels (v1)

Same Go binary distributed multiple ways, pick whichever:

| Channel | Install | Build |
|---|---|---|
| **Install script** (primary) | `curl -fsSL https://backai.dev/install.sh \| bash` | Hosted script + GH releases |
| Homebrew (macOS) | `brew install agent-field/tap/af-stack` | goreleaser auto-updates tap |
| Scoop (Windows) | `scoop install af-stack` | goreleaser auto-updates bucket |
| Direct binary | GitHub Releases | goreleaser cross-compile |
| Docker image | `docker run afstack/cli:latest` | goreleaser |

## Why this is strategic

| Reason | Detail |
|---|---|
| Matches AgentField | Same `curl install.sh` pattern; consistent ecosystem identity |
| Cross-language | Python and JS devs both get one install path |
| No runtime dep | Go binary runs standalone; no node/python required at runtime |
| Fast | Native binary; no Node startup tax for every command |
| Code sharing | Suite runtime is Go; CLI shares internal packages |
| Industry standard for this shape | Caddy, Gitea, Stripe CLI, flyctl, gh, Supabase all use this |
| Doesn't fragment | One install command, one tool, one mental model |

## What the CLI does

```bash
# Scaffold (alternative to git clone)
af-stack init <project>

# Dev loop
af-stack dev / up / down / logs / status

# Modules
af-stack module enable/disable/list <name>

# Tools
af-stack mcp add/list/remove <url>
af-stack harness install <claude-code|codex|gemini|opencode>
af-stack sandbox install <gvisor>
af-stack skill install <pkg>

# Identity + multi-tenancy
af-stack tenant create/list/update
af-stack user create/list/disable
af-stack secrets set/get/list/delete/rotate
af-stack keys issue/rotate/revoke

# Database
af-stack db migrate / rollback / status

# Deploy
af-stack deploy --target=fly|railway|render|helm|nomad

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

| Thing | Why not |
|---|---|
| npm wrapper for CLI | Fragments the install story; adds Node dep |
| bunx | Niche audience |
| pipx | Wrong tool for cross-language CLI |
| Auto-update daemon | User-explicit only |
| Multiple binaries (CLI + server separate) | Single binary, modal commands (`af-stack serve` is the server) |

## Reference

- AgentField install pattern: `curl -fsSL https://agentfield.ai/install.sh | bash`
- goreleaser: https://goreleaser.com
- Caddy distribution: https://github.com/caddyserver/caddy/releases
- Stripe CLI: https://github.com/stripe/stripe-cli
- Supabase CLI: https://github.com/supabase/cli
