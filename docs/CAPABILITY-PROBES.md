# Capability Probes

Capability probes are small runtime checks that turn operator configuration
into honest dashboard state.

Each probe implements:

```go
type Probe interface {
    ID() string
    Slot() string
    Schedule() time.Duration
    Run(ctx context.Context) (Result, error)
}
```

`Registry.RunAll` executes boot probes once. `Registry.StartScheduled`
reruns probes with a non-zero schedule. Results are available through
`Snapshot()` and `Get(id)`.

## Adapter Registry Integration

The probe registry can be connected to the adapter registry. When the
LiteLLM virtual-key probe completes, the `llm-chat` adapter row keeps the
Block 1 flat capability contract:

```json
{
  "virtual_keys_active": false,
  "key_management_mode": "stateless",
  "spend_tracking_exact": false
}
```

`GET /api/v1/admin/features` renders the same probe result in the feature
tree as `llm_gateway.virtual_keys_active`.

## Block 2 Probes

| Probe | Slot | Capability |
|---|---|---|
| `litellm-virtual-keys` | `llm-chat` | `llm_gateway.virtual_keys_active` |
| `litellm-spend-tracking` | `llm-chat` | `llm_gateway.spend_tracking_active` |
| `pg-stat-statements-loaded` | `data` | `db.stat_statements_loaded` |
| `pg-role-read-all-stats` | `data` | `db.role_has_read_all_stats` |

Probe failures do not crash the admin features endpoint. They appear as
details with `severity: unavailable`.

## Adding A Probe

1. Add a small type under `services/runtime/internal/probe`.
2. Return a stable `ProbeID`, slot, capability key, severity, and detail.
3. Register it in `cmd/af-stack`.
4. If it informs an adapter pill, add the view-specific capability mapping
   in the registry integration.
