# Feature Flags

BackAI ships durable runtime feature flags for the product you build
on the stack. Flags are stored in Postgres and scoped by tenant RLS, so
the dashboard, customer app, workload modules, and SDK clients all read
the same values.

This is general app configuration. It is not AgentField memory, run
state, trace state, or session state.

## Built-In Defaults

Fresh deploys expose a small default set even before any rows are
persisted:

- `experimental-cost-forecasts`
- `command-palette-recents`
- `verbose-run-logs`

Changing a flag writes an override row to `suite_feature_flags`.
Operators toggle flags from the dashboard **Build → Flags** page, which
shows whether each value is a built-in default or a persisted override.

## TypeScript

```ts
import { suite } from "@af-stack/sdk"

const flags = await suite.flags.list()

if (await suite.flags.isEnabled("command-palette-recents")) {
  // render or execute the flagged path
}

await suite.flags.set("verbose-run-logs", true)
```

## REST

List flags:

```bash
curl "$AF_STACK_URL/api/v1/config/flags" \
  -H "Authorization: Bearer $AF_STACK_API_KEY"
```

Set a flag:

```bash
curl -X PUT "$AF_STACK_URL/api/v1/config/flags/verbose-run-logs" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'
```
