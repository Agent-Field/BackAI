# Render deploy

What this gets you:

- Runtime + dashboard as Render web services, both built from this repo's
  Dockerfiles.
- Managed Postgres 16 with pgvector preinstalled.
- Three env-var groups (LLM, S3, sandbox) so secrets are set ONCE.

## Deploy button

Drop this in your fork's README so others can one-click deploy:

```
[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/<you>/af-stack)
```

The button URL pattern is `https://render.com/deploy?repo=<repo-url>`.
Render reads `render.yaml` from the repo root — we keep ours at
`deploy/render/render.yaml` and symlink or set
`renderYAMLPath: deploy/render/render.yaml` in the dashboard if you keep
it nested.

## Manual walkthrough

1. Fork the repo.
2. Go to render.com -> New -> Blueprint.
3. Point at your fork. Select `deploy/render/render.yaml` as the
   blueprint file.
4. Render parses the file, lists what it'll create:
   - 1 Postgres database (af-stack-postgres)
   - 2 web services (runtime + dashboard)
   - 3 env-var groups (LLM, S3, sandbox)
5. Fill in the `sync: false` placeholders:
   - LLM: at least one of `OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`,
     `OPENAI_API_KEY`.
   - S3: endpoint, bucket, access key, secret key.
   - Sandbox: `E2B_API_KEY`.
6. Hit "Apply Blueprint". Render builds the images, runs migrations on
   first boot, and starts both services.

## Validation

```bash
# Runtime
curl https://af-stack-runtime.onrender.com/health
curl https://af-stack-runtime.onrender.com/ready

# Dashboard
open https://af-stack-dashboard.onrender.com/
```

## Common pitfalls

- `fromService.property: host` returns the public Render hostname for the
  dashboard's `NEXT_PUBLIC_RUNTIME_URL` (so browsers can reach it). The
  server-side `RUNTIME_URL` uses `hostport`, which is the internal
  `host:port` pair — faster, no public egress charges.
- `generateValue: true` for `AF_STACK_KMS_KEY` + `AF_STACK_AUTH_SECRET`
  runs ONCE on first apply. Re-generating them rotates the keys and
  invalidates all sessions + vault entries — don't redeploy with new
  values lightly.
- Render Postgres `standard` plan = $19/mo as of 2026. Use `starter` for
  dev environments; bump to `pro` if you need >256 connections.
- Sandbox: default is `e2b` because Render containers can't run Docker.
  Switch to gvisor when 9.2 lands.
- `region: oregon` is hardcoded — change to whichever Render region is
  closest to your users + your Postgres.
