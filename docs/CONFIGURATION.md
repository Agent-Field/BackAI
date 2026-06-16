# BackAI Configuration

`backai.config.yaml` is the operator-owned feature contract for a fork.
It does not generate Compose files in Block 2; operators still edit env
and Compose directly. Validate it with:

```bash
backai config validate
```

Start from `backai.config.yaml.example`.

## Presets

| Preset | Meaning |
|---|---|
| `lean` | Block 1 admin features on; no new observability services; LiteLLM virtual keys off. |
| `full-observability` | Lean plus future logs/traces/metrics/errors slots set to their first real adapters. |
| `production` | Full observability plus intended LiteLLM virtual keys and exact spend tracking. |
| `custom` | No baseline. Every feature must be explicitly listed. |

When `preset != custom`, the `features:` block overrides individual
fields. When `preset = custom`, partial configs fail Layer 1 validation.

## Feature Flags

Block 1 features:

- `db_health`
- `provider_health_polling`
- `notifications_mute`
- `brand_override`
- `search_index_stats`
- `cron_manual_trigger`
- `cache_flush`
- `api_key_rotate`

LiteLLM gateway features:

- `llm_gateway.virtual_keys`: operator intent. Runtime probes LiteLLM and
  activates mirroring only when `/key/info` confirms virtual keys.
- `llm_gateway.spend_tracking`: requires virtual keys.

Future slots declared in Block 2:

- `logs.backend`: `ring | loki | remote`
- `traces.backend`: `empty | tempo | remote`
- `metrics.backend`: `none | prometheus | remote`
- `errors.backend`: `logfilter | glitchtip | remote`

Adapter selection env vars use `_ADAPTER`, not `_BACKEND`:

- `AF_STACK_LOGS_ADAPTER`
- `AF_STACK_TRACES_ADAPTER`
- `AF_STACK_METRICS_ADAPTER`
- `AF_STACK_ERRORS_ADAPTER`

Backend-specific URLs keep their backend names, for example
`AF_STACK_LOGS_LOKI_URL`.

## Validation

Layer 1 is structural: YAML shape, unknown fields, preset names, custom
completeness, and backend enums.

Layer 2 is dependency validation: `metrics.container_metrics` requires
`metrics.enabled`, spend tracking requires virtual keys, and configured
providers require their env vars.

Postgres grants and loaded extensions are runtime capabilities, not static
CLI checks. They are surfaced by `GET /api/v1/admin/features` through
capability probes.
