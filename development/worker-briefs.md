# Worker Briefs

Use these briefs to parallelize implementation. Each worker should branch from
`supportdesk-first-dx` and keep changes scoped to the assigned surfaces.

## Worker A: Product Shell And Copy

### Mission

Make the repo read like a SupportDesk-first BackAI app template.

### Inputs

- `development/supportdesk-first-dx-plan.md`
- `development/decisions.md`
- Current `README.md`
- `apps/customer-app`
- `apps/dashboard`

### Outputs

- README quickstart rewritten.
- Customer app shell rebranded to SupportDesk AI.
- First-run copy points to customer app before admin.
- AgentField references are subtle in app UI.

### Constraints

- Do not remove architecture docs.
- Do not erase AgentField; reposition it.
- Do not touch Shipwright handler changes unless explicitly assigned.

### Verification

```bash
rg -n "SWE-AF|Shipwright|code-helper|AgentField" README.md apps/customer-app apps/dashboard docs
pnpm --dir apps/customer-app exec tsc --noEmit --pretty false
pnpm --dir apps/dashboard exec tsc --noEmit --pretty false
```

## Worker B: Demo Provider And Evidence

### Mission

Build the no-key SupportDesk action path that writes admin-visible evidence.

### Inputs

- `services/runtime/internal/llmgateway`
- `services/runtime/internal/cost`
- `services/runtime/internal/server`
- customer app API routes
- admin cost/request surfaces

### Outputs

- Deterministic demo provider or SupportDesk demo route.
- Demo metadata in cost/request records.
- Real provider path preserved.
- Smoke script for no-key demo.

### Constraints

- Do not pretend demo calls are real provider calls.
- Do not require sandbox, S3, Stripe, or OAuth.
- Keep OpenAI-compatible endpoint working.

### Verification

```bash
env -u OPENROUTER_API_KEY -u OPENAI_API_KEY -u ANTHROPIC_API_KEY go test ./services/runtime/...
env -u OPENROUTER_API_KEY -u OPENAI_API_KEY -u ANTHROPIC_API_KEY ./scripts/smoke-supportdesk-demo.sh
```

## Worker C: Customer-To-Admin Walkthrough

### Mission

Make the first user journey self-guided and prove the admin evidence reveal.

### Inputs

- `apps/customer-app/src/app`
- `apps/dashboard/src/app`
- `apps/dashboard/src/lib/api.ts`
- runtime request/cost APIs

### Outputs

- Customer app walkthrough checklist.
- Admin dashboard walkthrough checklist.
- Deep link from SupportDesk action result to matching admin event.
- Admin page filter/highlight for exact request.

### Constraints

- Walkthrough only uses live first-run features.
- Avoid dead tabs and advanced module detours.
- Preserve auth redirects.

### Verification

Browser/local:

1. Sign up in customer app.
2. Run first SupportDesk action.
3. Click "View in admin".
4. Confirm exact evidence is visible.

Build:

```bash
pnpm --dir apps/customer-app exec tsc --noEmit --pretty false
pnpm --dir apps/dashboard exec tsc --noEmit --pretty false
```

## Worker D: Compose And Port DX

### Mission

Make local first-run match the public promise.

### Inputs

- `docker-compose.yml`
- `docker-compose.override.yml`
- `.env.example`
- `apps/customer-app/Dockerfile`
- `apps/dashboard/Dockerfile`

### Outputs

- Customer app included in default first-run.
- README and compose URLs agree.
- Demo mode defaults are present.
- Advanced services do not block first-run.

### Constraints

- Keep port overrides for conflicts.
- Do not require external provider keys.
- Do not require Docker sandbox for SupportDesk.

### Verification

```bash
docker compose config --quiet
docker compose up -d --build
curl -fsS http://localhost:8080/health
curl -fsS http://localhost:3000
curl -fsS http://localhost:3001
```

## Worker E: Railway And Deploy Templates

### Mission

Make one-click deploy match local first-run.

### Inputs

- `deploy/railway/railway.json`
- `deploy/railway/README.md`
- `deploy/render/render.yaml`
- `deploy/fly`
- `docker-compose.prod.yml`
- `deploy/caddy/Caddyfile`

### Outputs

- Railway template includes customer app.
- Sandbox disabled by default.
- S3/Stripe/OAuth optional.
- Public/private URLs wired.
- Production docs explain `app`, `admin`, and `api` services.

### Constraints

- Do not make first deploy require S3, E2B, Stripe, or OAuth.
- Keep serious production path honest about external services.

### Verification

```bash
scripts/validate-deploy-targets.py
docker compose -f docker-compose.prod.yml config --quiet
```

## Worker F: Docs And Example Capability Manifests

### Mission

Make examples and docs systematic instead of snowflake demos.

### Inputs

- `examples/README.md`
- `examples/*`
- `docs/*`
- `README.md`

### Outputs

- Capability manifest schema.
- SupportDesk manifest.
- Updated manifests for existing examples.
- Docs for local, Railway, production, attach mode, architecture, and
  advanced capabilities.

### Constraints

- Do not claim every example has identical external requirements.
- Keep SupportDesk as the default path.
- Keep AgentField as substrate in architecture docs.

### Verification

```bash
rg -n "TODO|coming soon|Phase [0-9]|SWE-AF|AF Stack" README.md docs examples
```

## Worker G: Verification And Release Readiness

### Mission

Prove public readiness and find integration breakage.

### Inputs

- All merged milestone branches.
- `development/verification.md`

### Outputs

- Completed verification log in `development/status.md`.
- Fixes for failures or clearly documented external prerequisites.
- Final public-readiness checklist.

### Constraints

- Do not accept narrow checks as proof of broad requirements.
- Verify the actual first-run path, not only unit tests.

### Verification

Run all commands in `development/verification.md`.
