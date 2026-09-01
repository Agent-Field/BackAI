# Releasing

Releases are **automatic on merge to `main`**. You never hand-cut a version.

## How it works

1. You merge a PR into `main`.
2. CI (`.github/workflows/ci.yml`) runs the full gate suite (lint + test across
   Go / Python / TypeScript, DCO, compose + deploy validation, docs). The
   security workflow (`.github/workflows/security.yml`) runs in parallel
   (`pnpm`/`npm`/`pip` audit, gosec, trivy, CodeQL on the public repo).
   Branch protection requires both **CI Success** and **Security Success**
   before merge — see [`docs/branch-protection.md`](branch-protection.md).
3. When CI succeeds, `.github/workflows/release.yml` fires and:
   - **Computes the next version** from the merged commit messages
     (Conventional Commits — see below). If nothing release-worthy changed, it
     stops here (no release).
   - **Builds & pushes** the container images to
     `ghcr.io/agent-field/af-stack-{runtime,dashboard,customer-app}:<version>`.
   - **Smoke-boots the runtime image** against a real Postgres + MinIO and waits
     for `/ready`. If the image can't boot or migrate, the release is aborted
     before anything is published — this is the regression gate.
   - **Tags** `vX.Y.Z`, cuts a **GitHub Release** with cross-compiled `af-stack`
     CLI binaries + a changelog (GoReleaser), and moves the `:latest` image tag.

Nothing is published unless the smoke gate passes, so `:latest` and every
GitHub Release point only at an image that provably boots.

## What decides the version (Conventional Commits)

The bump is derived from commit subjects since the last stable tag:

| Commit                                   | Bump   | Example         |
| ---------------------------------------- | ------ | --------------- |
| `feat: …`                                | minor  | 0.3.1 → 0.4.0   |
| `fix: …`                                 | patch  | 0.3.1 → 0.3.2   |
| `feat!: …` / `fix!: …` / `BREAKING CHANGE:` in body | major  | 0.3.1 → 1.0.0   |
| `docs:` / `chore:` / `ci:` / `test:` / `refactor:` | none   | no release      |

A merge that contains only non-releasing commits (docs, chore, …) produces no
release — that's intended.

## Manual release

Use the **Run workflow** button on the Release workflow
(`workflow_dispatch`). Set `dry_run: true` to compute and print the next version
without publishing.

## Consuming a release

- **Container images:** pin `AF_STACK_VERSION=<version>` for
  `docker-compose.prod.yml`, or set the Helm `image.*.tag`.
- **CLI:** download the binary for your platform from the GitHub Release, or
  build from source (`go build ./services/cli/cmd/af-stack`).
- **SDKs:** in the fork-first model the TypeScript (`@af-stack/sdk`) and Python
  (`af_stack`) SDKs live in the repo as workspace packages — you get them (and
  their types) by forking, no registry install needed. Publishing to
  npm/PyPI is only relevant for consuming the SDK from a separate external app
  and is intentionally out of scope for now.

## Requirements

The pipeline runs entirely on the built-in `GITHUB_TOKEN` (ghcr + releases) — no
extra secrets are required. Images publish under the repository's own org
(`ghcr.io/agent-field/…`). Multi-arch (arm64) images and Homebrew/Scoop taps are
deferred follow-ups (see `docs/cli-distribution.md`).
