---
title: Module — Harnesses
description: Probe-only inventory of installed CLI agent harnesses (Claude Code, Codex, Gemini CLI, OpenCode).
sidebar:
  order: 15
---

Probe-only inventory of installed CLI agent harnesses (Claude Code, Codex, Gemini CLI, OpenCode). The runtime reports whether each harness is installed and reachable but never installs them — that stays a CLI / docs concern.

## What it does

`harnesses.Prober` walks the supported `Provider` set and checks:

1. Is the binary on `PATH`?
2. Does `--version` produce sane output?
3. Are required env vars set? Missing → `NeedsAuth`.

Results are cached so list/get/probe don't fork a process every call.

Wire shapes (`HarnessProviderSchema`, `HarnessSchema`, `HarnessListSchema`) mirror `apps/dashboard/src/lib/api.ts`.

`main.go` constructs the service and warms the cache at boot. nil service ⇒ list returns `[]`, get returns `404`, probe returns `503`.

## Configuration

No dedicated module flag. The prober runs whenever the runtime starts; the service is wired unconditionally.

## REST endpoints

Registered in `services/runtime/internal/server/harnesses.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/harnesses` | List harness probe results. |
| `GET` | `/api/v1/harnesses/{provider}` | Get a single harness. |
| `POST` | `/api/v1/harnesses/{provider}/probe` | Re-run the probe (force refresh). |

## Database tables

None. Probe results are cached in process memory.

## Env vars

Required env per provider is provider-specific (e.g. `ANTHROPIC_API_KEY` for Claude Code, `OPENAI_API_KEY` for Codex). The prober reads each via `os.Getenv` in `prober.go`; missing values surface as `NeedsAuth`.

## Code map

- `interface.go` — `Provider` enum (`claude-code` / `codex` / `gemini-cli` / ...), wire types.
- `prober.go` — binary detection + version check + env var presence.
- `probes/` — per-provider probe specifications.

## Related

- Targets for [Skills](./skills/) attachment.
