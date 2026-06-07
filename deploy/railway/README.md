# Railway deploy

What this gets you:

- Runtime + dashboard + Postgres (with pgvector) on Railway's managed
  infra, wired together over the private network.
- External S3 (Tigris/R2/AWS) — Railway's built-in storage is small;
  point at a real S3 for production.
- One-click deploy via the template manifest in `railway.json`.

## Walkthrough (CLI)

```bash
# 1. Install + auth.
npm i -g @railway/cli
railway login

# 2. From a clone of the repo:
git clone https://github.com/<you>/af-stack && cd af-stack

# 3. Create a project and push the template.
railway init --template ./deploy/railway/railway.json

# 4. Set the secrets that DON'T have generators (S3 + LLM key).
railway variables \
  --service runtime \
  --set AF_STACK_S3_ENDPOINT="https://fly.storage.tigris.dev" \
  --set AF_STACK_S3_BUCKET="af-stack-prod" \
  --set AF_STACK_S3_ACCESS_KEY="..." \
  --set AF_STACK_S3_SECRET_KEY="..." \
  --set OPENROUTER_API_KEY="..." \
  --set E2B_API_KEY="..."

# 5. Deploy.
railway up
```

## Walkthrough (web)

1. Click "Deploy on Railway" (button in the repo README).
2. Pick a project name + region.
3. Fill in the S3 + LLM + E2B fields in the form Railway renders from
   `railway.json`.
4. Hit Deploy. Railway provisions Postgres, builds both images, and
   wires `RUNTIME_URL` + `DATABASE_URL` between services.

## Validation

```bash
railway status
railway open                                                 # dashboard
curl https://<your-project>.up.railway.app/health            # runtime
```

## Common pitfalls

- The bundled Postgres uses pgvector — AgentField vector memory works out
  of the box. If you swap in your own Postgres, ensure the `vector`
  extension is enabled.
- `RUNTIME_URL` defaults to Railway's private domain — works only from
  inside Railway. Browser-side calls use `NEXT_PUBLIC_RUNTIME_URL`
  (public domain).
- `AF_STACK_AUTH_SECRET` is generated ONCE for the runtime and reused by
  the dashboard via `${{ runtime.AF_STACK_AUTH_SECRET }}`. Don't override
  one without the other or sessions break.
- Sandbox: defaults to `e2b`. Switching to `docker` will not work on
  Railway — there's no docker socket inside the container. Use `e2b` or
  `gvisor` (when 9.2 lands).
- Default `numReplicas = 1`. Scale via Railway's autoscaler or bump
  manually; the runtime is stateless once Postgres + S3 are external.
