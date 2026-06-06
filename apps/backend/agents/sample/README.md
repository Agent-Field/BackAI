# Sample agent

The smallest possible AgentField agent. Used by the 60-second quickstart.

Two reasoners:

- `echo` — returns input verbatim. Always available, no LLM key needed.
- `summarize` — uses an LLM to summarize text. Only registered if one of
  `OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, or `OPENAI_API_KEY` is set.

## Local

```bash
pip install -r requirements.txt
AGENTFIELD_SERVER=http://localhost:8081 python main.py
```

## Via compose

It runs automatically as part of `docker compose up`. Reach it via the
suite gateway:

```bash
# Echo (no key needed)
curl -X POST http://localhost:8080/api/v1/agents/sample.echo \
  -H "Content-Type: application/json" \
  -d '{"input":{"hello":"world"}}'

# Summarize (needs an LLM key in .env)
curl -X POST http://localhost:8080/api/v1/agents/sample.summarize \
  -H "Content-Type: application/json" \
  -d '{"input":{"text":"Long article goes here..."}}'
```

## Customize

Edit `main.py`. Hot reload watches the file in dev mode.
