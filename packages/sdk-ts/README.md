# @af-stack/sdk

The Suite SDK for AF Stack — TypeScript.

```ts
import { suite, ctx } from "@af-stack/sdk"

// Call an agent from app code
const result = await suite.agents.call("notable-ai.summarize", { text: "..." })

// Read tenant context (set by middleware)
const tenantId = ctx.tenantId
```

## Two SDKs

- **AgentField SDK** — *defines* agents
- **Suite SDK** (this package) — *calls* agents and uses suite infrastructure

See the main repo: https://github.com/Agent-Field/backai

## Status

`v0.0.1` — package scaffold only. The full SDK lands in Phase 2 and
Phase 5+ of the build roadmap.

## License

Apache 2.0
