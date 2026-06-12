# Verification Checklist

This file defines the evidence required before the SupportDesk-first repo can
be considered public-ready.

Do not mark a milestone complete based only on intent or code review. Use
current command output, runtime behavior, rendered UI, or deploy evidence.

## Environment

Provider keys may be loaded from the user's shell profile:

```bash
source ~/.zshrc
```

When testing no-key demo mode, explicitly unset provider keys:

```bash
env -u OPENROUTER_API_KEY -u OPENAI_API_KEY -u ANTHROPIC_API_KEY <command>
```

## Static Checks

### Worktree Scope

```bash
git status --short
git diff --stat
```

Expected:

- Only intended milestone files are staged/committed.
- Existing unrelated Shipwright handler change is not included unless assigned.

### Runtime Tests

```bash
go test ./services/runtime/...
```

Expected:

- Runtime tests pass.

### Dashboard Type Check

```bash
pnpm --dir apps/dashboard exec tsc --noEmit --pretty false
```

Expected:

- Dashboard TypeScript passes.

### Customer App Type Check

```bash
pnpm --dir apps/customer-app exec tsc --noEmit --pretty false
```

Expected:

- Customer app TypeScript passes.

### Python SDK Tests

```bash
uv run --project packages/sdk-py pytest packages/sdk-py/tests -q
```

Expected:

- Python SDK tests pass.

### TypeScript SDK Tests

```bash
npm --prefix packages/sdk-ts test
```

Expected:

- TypeScript SDK tests pass.

## Local E2E: No-Key Demo Mode

Purpose:

Prove a fresh user can run SupportDesk without provider keys.

Command:

```bash
env -u OPENROUTER_API_KEY -u OPENAI_API_KEY -u ANTHROPIC_API_KEY docker compose up -d --build
./scripts/smoke-supportdesk-demo.sh
```

Expected:

- Customer app is reachable.
- User can sign up or test script can provision a user.
- First SupportDesk action succeeds.
- Runtime/admin has matching request/cost evidence.
- Evidence is marked as demo mode.
- No real provider call is claimed.

## Local E2E: Real LLM Mode

Purpose:

Prove the same flow works with a real provider key.

Command:

```bash
source ~/.zshrc
docker compose up -d --build
./scripts/smoke-supportdesk-real-llm.sh
```

Expected:

- First SupportDesk action uses real provider path.
- Cost/request evidence appears.
- Admin link opens matching event.

## Browser Walkthrough

Use Browser/Playwright after the app runs locally.

Steps:

1. Open customer app.
2. Sign up.
3. Complete first SupportDesk action.
4. Click "View in admin".
5. Confirm admin shows matching tenant, user, model/provider, cost, latency,
   request id, and trace/run data.

Expected:

- No dead links.
- No hidden required provider key in demo mode.
- No advanced feature detour.
- Text fits on desktop and mobile.

## Compose Validation

```bash
docker compose config --quiet
docker compose -f docker-compose.prod.yml config --quiet
```

Expected:

- Compose files parse.
- Production compose includes or clearly documents customer app.

## Deploy Target Validation

```bash
scripts/validate-deploy-targets.py
```

Expected:

- Helm, Fly, Railway, Render, and production compose config checks pass.

## Railway Live Check

Only run when Railway credentials and target project are configured.

```bash
railway up
railway status
```

Expected:

- Runtime is healthy.
- Customer app is reachable.
- Admin dashboard is reachable.
- No-key demo flow works.

## Public-Readiness Content Sweep

```bash
rg -n "TODO|coming soon|Phase [0-9]|SWE-AF|code-helper|Shipwright|AF Stack|AgentField" README.md docs examples apps/customer-app apps/dashboard development
```

Expected:

- Each hit is intentional.
- SupportDesk first-run docs do not lead with advanced features.
- AgentField references are architecture/substrate/deep-trace appropriate.

## Completion Evidence Template

Paste into `development/status.md` after each milestone:

```text
Milestone:
Commit:
Commands:
- <command>: <pass/fail>
Runtime evidence:
- <URL or screenshot path>
Notes:
- <external prereqs skipped or follow-up>
```
