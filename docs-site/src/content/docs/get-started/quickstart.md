---
title: Quickstart — 60 seconds to first call
description: Boot AF Stack with docker compose and make your first LLM call.
sidebar:
  order: 2
---

You need: Docker, an OpenRouter key (or OpenAI / Anthropic), and three
minutes. By the end you'll have an OpenAI-compatible LLM gateway running
locally with the full operator dashboard.

## 1. Clone and configure

```bash
git clone https://github.com/Agent-Field/backai af-stack
cd af-stack
cp .env.example .env
```

Open `.env` and set one provider key:

```bash
OPENROUTER_API_KEY=sk-or-v1-...
# Optional: a 64-char hex KMS key. dev-secret-change-me works for local.
AF_STACK_KMS_KEY=$(openssl rand -hex 32)
```

## 2. Boot the stack

```bash
docker compose up -d
```

Five services come up: Postgres (with pgvector), MinIO, the AgentField
control plane, the AF Stack runtime, and the operator dashboard. Wait
about 30 seconds for migrations to apply, then verify:

```bash
curl -s http://localhost:38080/health | jq
# → { "status": "alive", "uptime_s": 42 }
```

## 3. Make your first LLM call

The runtime exposes an OpenAI-compatible gateway at
`/api/v1/llm`. Point any OpenAI SDK at it:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:38080/api/v1/llm",
    api_key="not-needed-mt-off",  # multi-tenancy is off by default
)

resp = client.chat.completions.create(
    model="qwen/qwen-2.5-72b-instruct",
    messages=[{"role": "user", "content": "Why is the sky blue?"}],
    max_tokens=80,
)
print(resp.choices[0].message.content)
```

Or curl, if you prefer:

```bash
curl -s http://localhost:38080/api/v1/llm/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen/qwen-2.5-72b-instruct",
    "messages": [{"role":"user","content":"Why is the sky blue?"}],
    "max_tokens": 80
  }' | jq -r '.choices[0].message.content'
```

## 4. See the call in the dashboard

Open [http://localhost:33000](http://localhost:33000) and sign in with
the seeded operator account:

- **Email:** `operator@example.com`
- **Password:** `af-stack-demo-pwd`

Click **Operate → Cost**. Your call is there: model, tokens, USD cost,
timestamp. Click **Operate → Logs** to see the request lifecycle in
real-time.

## Verify it worked

You're done when:

- [ ] `curl http://localhost:38080/health` returns `{"status":"alive"}`.
- [ ] The OpenAI SDK or curl call returned a completion.
- [ ] The Cost tab shows a non-zero `period_total_usd`.

## What's next

- **Build something:** [Example 01 — Notable](https://github.com/Agent-Field/backai/tree/main/examples/01-notable)
  shows the full multi-tenant SaaS shape.
- **Customize:** [Guides → Customize the Dashboard](/guides/customize-dashboard/)
  or [Guides → Swap defaults](/guides/swap-defaults/).
- **Deploy:** [Deploy → Overview](/deploy/) covers Helm, Fly, Railway,
  Render, and production docker compose.
