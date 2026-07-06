# SDK Reference

Two SDKs. Know which one you're in.

## `app.*` vs `suite.*`

| | `app.*` (AgentField) | `suite.*` (Suite) |
| --- | --- | --- |
| Scope | **Only inside an agent** (`apps/backend/agents/<name>/main.py`) | **Everywhere else** — customer app, workload modules, dashboard plugins, other agents |
| Purpose | *Define* reasoners | *Use* the platform |
| Key verbs | `app.reasoner`, `app.ai`, `app.call`, `app.run` | `suite.llm.chat`, `suite.agents.call`, `suite.jobs.enqueue`, … |
| Import | `from agentfield import Agent` | `from af_stack import suite` · `import { suite } from "@af-stack/sdk"` |

The rest of this page is the **`suite.*`** surface.

## Language parity

State it honestly:

| Language | Package | Status |
| --- | --- | --- |
| **Python** | `af-stack` | **Full reference** — the canonical, complete surface |
| **TypeScript** | `@af-stack/sdk` | **Parity** with Python (minus the asymmetries below) |
| **Go** | `packages/sdk-go` | **Planned** — an empty stub today (`Version` const only, no methods) |

**Cross-language asymmetry:**

- `suite.crons` — **Python only** (no `crons` in TS).
- `suite.activity`, `suite.flags` — **TypeScript only** (no `activity` /
  `flags` in Python).
- Skills placement differs: `suite.admin.skills` in Python vs top-level
  `suite.skills` in TypeScript.

Don't assume a TS method exists just because Python has it (or vice
versa) for those namespaces.

## `suite.*` namespaces

| Namespace | Methods | Notes |
| --- | --- | --- |
| `agents` | `call`, `call_async`, `stream`, `status`, `cancel`, `approve`, `deny`, `pending_approvals` | Call agent reasoners |
| `llm` | `chat`, `embed`, `models`, `cache_stats` | The gateway |
| `cost` | `events` | Cost ledger |
| `memory` | `get`, `put`, `delete`, `list`, `search` | Per-tenant KV + semantic |
| `storage` | `upload`, `download`, `signed_url`, `delete`, `list` | Object storage |
| `jobs` | `enqueue`, `get`, `retry`, `list` | Go handlers only — see [jobs.md](jobs.md) |
| `crons` | `list`, `create`, `get`, `set_active`, `delete` | **Python only** |
| `billing` | `customers`, `customer`, `meters`, `plans`, `entitlements`, `portal_link`, `checkout`, `meter`, `has_budget` | Stripe |
| `sandbox` | `run`, `list`, `get`, `stop`, `pool` | Isolated code execution |
| `webhooks` | `send`, `list`, `get`, `retry`, `subscribe`, `subscriptions`, `unsubscribe`, `emit` | See [webhooks.md](webhooks.md) |
| `notifications` | `send`, `email`, `list`, `get`, `stats` | |
| `secrets` | `get`, `reveal`, `put`, `delete`, `list`, `rotate` | |
| `auth` | `whoami` | |
| `oauth` | `authorize_url`, `connected`, `token`, `disconnect` | |
| `realtime` | `subscribe` | Live streams |
| `runs` | `subscribe` | Run traces |
| `search` | `search`, `upsert`, `delete` | |
| `tools` | `list_mcp_servers`, `add_mcp_server`, `remove_mcp_server`, `call_mcp`, `list_native`, `invoke_native`, … | MCP + native tools |
| `harnesses` | `list`, `get`, `probe` | |
| `approvals` | `request`, `get`, `list`, `decide` | |
| `shipwright` | `create`, `get`, `list`, `complete` | |
| `audio` | `speech`, `transcribe`, `translate` | |
| `images` | `generate`, `edit`, `variations` | |
| `admin.tenants` | `list`, `get`, `create`, `update`, `delete` | Operator scope |
| `admin.users` | `list` | |
| `admin.memberships` | `list`, `add`, `remove` | |
| `admin.keys` | `list`, `issue`, `revoke` | |
| `admin.audit` | `list` | |
| `admin.budgets` | `list`, `get`, `set` | |
| `admin.skills` | `list`, `install`, `uninstall`, `attach` | Python; top-level `suite.skills` in TS |

## One-liners

```python
from af_stack import suite

await suite.llm.chat(model="gpt-4o-mini", messages=[{"role": "user", "content": "Hi"}])
await suite.agents.call("my-agent.summarize", {"text": "…"})
await suite.jobs.enqueue("send-digest", {"tenant": "acme"})
await suite.sandbox.run(language="python", code="print(2 + 2)")
await suite.memory.put("last-seen", {"at": "2026-07-06"})
```

TypeScript is the same shape:

```ts
import { suite } from "@af-stack/sdk"
await suite.llm.chat({ model: "gpt-4o-mini", messages: [{ role: "user", content: "Hi" }] })
```
