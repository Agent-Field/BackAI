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
     `ghcr.io/agent-field/af-stack-{runtime,dashboard,customer-app,supportdesk-agent}:<version>`
     and tries to mark each GHCR package public (see
     [GHCR package visibility](#ghcr-package-visibility)).
   - **Smoke-boots the runtime image** against a real Postgres + MinIO and waits
     for `/ready`. Then it asserts every image is anonymously pullable and
     scaffolds an app with `af-stack init` + `npm start`. If any of those
     fail, the release is aborted before anything is published.
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

## GHCR package visibility

`af-stack init` writes a `docker-compose.yml` that pulls the four images
**without** a registry login. GHCR creates packages **private** on first
push, even when this repo is public. A private image means every
scaffolded app fails to boot.

The Release workflow tries `scripts/publish-ghcr-packages.sh` after each
push. The GitHub REST API often cannot change visibility for org-owned
container packages (PATCH returns 404). When that happens, an org owner
does this **once** (later releases reuse the same names and stay public):

1. Open [github.com/orgs/Agent-Field/packages](https://github.com/orgs/Agent-Field/packages).
2. For `af-stack-runtime`, `af-stack-dashboard`, `af-stack-customer-app`,
   and `af-stack-supportdesk-agent`: **Package settings → Change
   visibility → Public**.
3. Re-run **Actions → Release → Run workflow**.

Or, authenticated as an org owner / package admin:

```bash
scripts/publish-ghcr-packages.sh
```

`scripts/assert-ghcr-public.sh` is the smoke gate: it must list every
private package (it must not crash on the first 401) and fail the
release until they are public.
