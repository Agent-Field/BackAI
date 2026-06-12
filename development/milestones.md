# Milestones

## M0: Planning Baseline

### Outcome

The repo has a clear development coordination folder that can support parallel
workers and keep the product direction stable.

### Scope

- Add `development/` folder.
- Capture product decisions and consequences.
- Define milestones, worker briefs, merge plan, and verification gates.
- Preserve unrelated dirty work.

### Acceptance Criteria

- `development/README.md` explains the goal and public-ready definition.
- `development/status.md` tracks milestone state.
- `development/worker-briefs.md` can be handed to parallel workers.
- `development/verification.md` defines concrete checks.
- Planning files are committed and pushed.

### Out Of Scope

- Product UI implementation.
- Runtime demo provider.
- Railway deploy changes.

## M1: Product Shell

### Outcome

The repo reads and boots like a SupportDesk-first AI app template, not a
generic infrastructure project or a SWE-AF demo.

### Scope

- Rebrand customer app from SWE-AF/code-helper/Shipwright-first language to
  SupportDesk AI.
- Update the main README headline and quickstart.
- Keep "AI backend" as category explanation, not the primary headline.
- Make customer app the first destination in docs.
- Keep AgentField subtle in product copy.

### Acceptance Criteria

- README tells the user to open the customer app first.
- Customer app landing/auth/dashboard copy is SupportDesk-focused.
- Admin dashboard copy does not over-emphasize AgentField.
- Architecture docs still identify AgentField as core AI substrate.

### Verification

```bash
rg -n "SWE-AF|Shipwright|code-helper|AgentField" README.md apps/customer-app apps/dashboard docs development/supportdesk-first-dx-plan.md development
```

Review each hit and confirm it is intentional.

## M2: No-Key Demo Loop

### Outcome

A fresh clone can complete the first SupportDesk action without any provider
key, while still writing realistic platform evidence.

### Scope

- Add deterministic demo response path for SupportDesk actions.
- Write request/cost/run evidence with `demo_mode=true` metadata.
- Keep real provider path compatible with OpenAI SDK endpoint.
- Add UI labels that distinguish demo provider from real provider calls.

### Acceptance Criteria

- With no `OPENROUTER_API_KEY`, the SupportDesk action succeeds.
- Admin cost/request surface shows the demo action.
- Records are visibly marked as demo mode.
- With `OPENROUTER_API_KEY`, the same action can use real models.

### Verification

```bash
env -u OPENROUTER_API_KEY -u OPENAI_API_KEY -u ANTHROPIC_API_KEY docker compose up -d
./scripts/smoke-supportdesk-demo.sh
```

Real-key check:

```bash
source ~/.zshrc
docker compose up -d
./scripts/smoke-supportdesk-real-llm.sh
```

## M3: Customer-To-Admin Walkthrough

### Outcome

The first user can start in the customer app and click through to the exact
admin evidence for the action they just took.

### Scope

- Customer app onboarding checklist.
- Admin dashboard onboarding checklist.
- Stable request/run/cost identifier linking.
- Deep link from SupportDesk action result to admin detail page.
- Highlight or filter exact evidence on admin page.

### Acceptance Criteria

- User can follow the walkthrough without reading docs.
- The link opens admin on the exact event, not a generic dashboard.
- If admin auth is required, the post-auth redirect preserves the target.
- The flow works in demo mode and real-key mode.

### Verification

Use browser automation or manual local run:

1. Open customer app.
2. Sign up.
3. Run first SupportDesk action.
4. Click "View in admin".
5. Confirm matching tenant/user/request/cost appears.

## M4: Compose First-Run

### Outcome

`docker compose up` boots the full SupportDesk first experience.

### Scope

- Include customer app in default compose path.
- Normalize first-run ports if practical.
- Keep override support for port conflicts.
- Ensure demo mode works in compose.
- Keep advanced services optional where possible.

### Acceptance Criteria

- Fresh clone plus `.env` plus `docker compose up` exposes:
  - customer app
  - admin dashboard
  - runtime API
- README URL list matches compose behavior.
- Advanced services do not block first-run if unused.

### Verification

```bash
docker compose config --quiet
docker compose up -d --build
curl -fsS http://localhost:8080/health
curl -fsS http://localhost:34000
curl -fsS http://localhost:33000
```

## M5: Railway Deploy

### Outcome

Railway one-click deploy produces the same first experience as local compose.

### Scope

- Add customer app service to Railway template.
- Set demo mode defaults.
- Disable sandbox by default.
- Make S3, Stripe, OAuth, and sandbox provider optional.
- Wire public/private URLs among runtime, customer app, and admin.

### Acceptance Criteria

- Railway template validates.
- Required deploy fields are minimal.
- Deployed app can complete no-key SupportDesk demo.
- Real provider key can be added later.

### Verification

Static:

```bash
scripts/validate-deploy-targets.py
```

Live, when Railway credentials are configured:

```bash
railway up
curl -fsS https://<runtime-domain>/health
```

## M6: Docs And Examples

### Outcome

Public docs explain the product in the right order and examples declare
capabilities honestly.

### Scope

- Rewrite README quickstart around SupportDesk.
- Add docs for:
  - local quickstart
  - no-key demo mode
  - real provider key upgrade
  - Railway deploy
  - production deploy
  - attach mode for existing apps
  - architecture/substrate
- Add capability manifests for examples.
- Update examples README with the shared deploy model.

### Acceptance Criteria

- New user can run the first experience from README alone.
- Existing-app users can find attach-mode docs.
- Architecture docs explain AgentField without centering it as the product.
- Examples do not imply every app has identical infrastructure needs.

### Verification

```bash
rg -n "TODO|coming soon|Phase [0-9]|SWE-AF|AF Stack" README.md docs examples apps/customer-app
```

Each hit must be intentional or fixed.

## M7: Verification Sweep

### Outcome

The repo is safe to publish.

### Scope

- Local E2E demo mode.
- Local real-key LLM mode using shell-provided key.
- Runtime tests.
- Customer app and dashboard type/build checks.
- SDK tests.
- Deploy config validation.
- Public-readiness content sweep.

### Acceptance Criteria

- Verification commands in `development/verification.md` pass or have
  documented external prerequisites.
- No disabled advanced module blocks the first-run path.
- Public docs and UI agree.

### Verification

Run the full checklist in `development/verification.md` and paste evidence
into `development/status.md`.
