# Railway deploy

What this gets you:

- Customer app, admin dashboard, runtime, LiteLLM, AgentField control
  plane, and Postgres (with pgvector) on Railway's managed infra, wired
  together over the private network.
- No-key SupportDesk demo mode by default. Add a provider key later and
  the same customer app switches to real model calls through LiteLLM.
- External S3 (Tigris/R2/AWS) for production storage. The SupportDesk
  first run does not require S3, but production workloads should set it.
- One-click deploy via the template manifest in `railway.json`.

## Walkthrough (CLI)

```bash
# 1. Install + auth.
npm i -g @railway/cli
railway login

# 2. From a clone of the repo:
git clone https://github.com/<you>/backai && cd backai

# 3. Create a project and push the template.
railway init --template ./deploy/railway/railway.json

# 4. Optional: set secrets for production mode.
railway variables \
  --service runtime \
  --set AF_STACK_S3_ENDPOINT="https://fly.storage.tigris.dev" \
  --set AF_STACK_S3_BUCKET="af-stack-prod" \
  --set AF_STACK_S3_ACCESS_KEY="..." \
  --set AF_STACK_S3_SECRET_KEY="..." \
  --set E2B_API_KEY="..."

railway variables \
  --service litellm \
  --set OPENROUTER_API_KEY="..."

# 5. Deploy.
railway up
```

## Walkthrough (web)

1. Click "Deploy on Railway" (button in the repo README).
2. Pick a project name + region.
3. Leave provider keys blank for the no-key SupportDesk demo, or set
   `OPENROUTER_API_KEY` on `litellm` for real model calls.
4. Hit Deploy. Railway provisions Postgres, builds customer/admin/runtime
   images, starts LiteLLM and AgentField, and wires private `RUNTIME_URL`
   - `DATABASE_URL` between services.

## Validation

```bash
railway status
railway open --service customer                              # customer app
railway open --service dashboard                             # admin dashboard
curl https://<your-project>.up.railway.app/health            # runtime
```

First-run path:

1. Open the customer service.
2. Sign up.
3. Use Support Chat on a realistic customer request.
4. Open Requests to see the customer-facing history.
5. Open the admin service separately to inspect the exact cost event and run evidence.

For more detail on no-key mode and real-provider mode, see
[`../../docs/demo-mode.md`](../../docs/demo-mode.md).

## Common pitfalls

- The bundled Postgres uses pgvector — AgentField vector memory works out
  of the box. If you swap in your own Postgres, ensure the `vector`
  extension is enabled.
- `RUNTIME_URL` defaults to Railway's private domain — works only from
  inside Railway. Browser-side calls use `NEXT_PUBLIC_RUNTIME_URL`
  (public domain).
- `AF_STACK_DEMO_MODE=auto` is intentional. With no provider key, runtime
  uses the deterministic SupportDesk demo provider. When you set
  `OPENROUTER_API_KEY` or another provider key on `litellm`, runtime
  detects it and routes through LiteLLM.
- `AF_STACK_AUTH_SECRET` is generated ONCE for the runtime and reused by
  the dashboard and customer app via `${{ runtime.AF_STACK_AUTH_SECRET }}`.
  Don't override one without the others or sessions break.
- Sandbox: defaults to `e2b`. Switching to `docker` will not work on
  Railway — there's no docker socket inside the container. The SupportDesk
  first run does not need sandbox credentials.
- Default `numReplicas = 1`. Scale via Railway's autoscaler or bump
  manually; the runtime is stateless once Postgres + S3 are external.
