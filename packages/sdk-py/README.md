# af-stack (Python SDK)

The Suite SDK for AF Stack — Python.

```python
from af_stack import suite, ctx

# Call an agent from app code
result = await suite.agents.call("notable-ai.summarize", {"text": "..."})

# Read tenant context (set by middleware)
tenant_id = ctx.tenant_id
```

## Two SDKs

- **AgentField SDK** (`agentfield.Agent`) — *defines* agents
- **Suite SDK** (this package) — *calls* agents and uses suite infrastructure

See the main repo: https://github.com/Agent-Field/backai

## Status

`v0.0.1` — package scaffold only. The full SDK lands in Phase 2 and
Phase 5+ of the build roadmap.

## License

Apache 2.0
