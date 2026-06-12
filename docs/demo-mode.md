# Demo Mode And Real Provider Mode

BackAI is designed to boot without an LLM provider key.

The default runtime setting is:

```bash
AF_STACK_DEMO_MODE=auto
```

In `auto` mode:

- If no provider key is present, SupportDesk AI uses the deterministic demo
  provider.
- If `OPENROUTER_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or another
  supported provider key is present, the same `/api/v1/llm/*` endpoint routes
  through LiteLLM.

The customer app does not change between modes. It still sends the request
through the BackAI gateway, includes a request id, resolves tenant identity,
and writes a cost event for the admin dashboard.

## No-Key Mode

Use this for first-run demos, template evaluation, screenshots, and docs:

```bash
AF_STACK_DEMO_MODE=true docker compose up
```

The demo provider returns a SupportDesk-shaped answer and writes normal cost
ledger rows with provider `demo`. The response includes the fingerprint
`demo-supportdesk`, so tests can prove the no-key path is active.

No-key mode is not a fake UI path. It exercises the runtime gateway, auth,
tenant resolution, cost ledger, request-id deep link, customer app, and admin
dashboard.

## Real Provider Mode

Set a provider key and leave `AF_STACK_DEMO_MODE=auto`, or force real-provider
mode with:

```bash
AF_STACK_DEMO_MODE=false
OPENROUTER_API_KEY=... docker compose up
```

The runtime detects provider-key presence, routes through the LiteLLM sidecar,
and records cost events with provider `litellm`.

For most forks, OpenRouter is the simplest first key because the bundled
LiteLLM config maps BackAI's default model,
`qwen/qwen-2.5-72b-instruct`, to OpenRouter.

## Production Guidance

- Keep `AF_STACK_DEMO_MODE=auto` for public templates and trial deploys.
- Set at least one provider key before putting real users on the app.
- Keep all app-level LLM calls behind `/api/v1/llm/*`; direct provider calls
  bypass tenant, policy, cost, and admin evidence.
- Do not require sandbox, S3, Stripe, or OAuth for the SupportDesk first run.
  Add those when the forked product actually needs them.
