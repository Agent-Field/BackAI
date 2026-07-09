# BackAI Configuration

`backai.config.yaml` is the operator-owned feature contract for a fork.
It does not generate Compose files in Block 2; operators still edit env
and Compose directly. Validate it with:

```bash
backai config validate
```

Start from `backai.config.yaml.example`.

## Deployment mode: SaaS vs Personal

BackAI ships with auth (multi-tenant login) and billing (Stripe + budget
paywall). Not every fork wants those. `AF_STACK_MODE` is a single master
switch that turns both families on or off:

| Mode | Auth | Billing | Use it when |
|---|---|---|---|
| `saas` (default) | governed by `AF_STACK_MODULE_MULTI_TENANCY` | governed by `AF_STACK_MODULE_BILLING` | You're running BackAI as a product with real, isolated customers. |
| `personal` | **forced off** | **forced off** | You're running BackAI just for yourself — no login on either frontend, no paywall, no Stripe, no budget `402`s. |

In personal mode:

- The runtime skips all auth: every request runs under the built-in
  default tenant (`00000000-0000-0000-0000-000000000000`). The operator
  RBAC guard on the dashboard is bypassed too — every operator page
  (including Secrets, Crons, Flags, Cache, Notifications, and OAuth) works
  unauthenticated.
- The budget gate is disabled, so LLM calls are never blocked by a
  `402` — but spend is **still metered**, so `Cost` still shows usage.
- No Stripe client is constructed; the billing surface is hidden in both
  the dashboard and the customer app.
- Both frontends skip their login/sign-in redirect and boot straight
  into the product as a single implicit user.

### Flipping it

One value, then restart. Either edit `.env`:

```bash
AF_STACK_MODE=personal   # or: saas
docker compose up -d      # restart to apply
```

…or use the CLI, which upserts the same var in `.env`:

```bash
af-stack mode            # print the current mode
af-stack mode personal   # single-user app: auth + billing off
af-stack mode saas       # back to multi-tenant SaaS
af-stack dev             # restart to apply
```

The switch is fully reversible; flip it as often as you like.

> **Gotcha.** The frontends enforce the mode in middleware baked into
> their images. If flipping the mode still redirects you to a login page,
> your frontend images predate the mode middleware — rebuild them from
> source (`docker compose build`) and restart.

> **Data caveat.** Data written while in personal mode is owned by the
> default tenant. When you switch back to `saas` with multi-tenancy on,
> that data stays under the default tenant and won't appear under a new
> per-customer tenant. This matters only if you build real data in
> personal mode and later expect it to belong to a specific customer.

`AF_STACK_MODE=personal` overrides the individual module flags below —
setting `AF_STACK_MODULE_MULTI_TENANCY=true` while in personal mode has no
effect. Switch to `saas` to let the module flags take over again.

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

- `logs.adapter`: `ring | loki | remote`
- `traces.adapter`: `empty | tempo | remote`
- `metrics.adapter`: `none | prometheus | remote`
- `errors.adapter`: `logfilter | glitchtip | remote`

Adapter selection env vars use `_ADAPTER`, not `_BACKEND`:

- `AF_STACK_LOGS_ADAPTER`
- `AF_STACK_TRACES_ADAPTER`
- `AF_STACK_METRICS_ADAPTER`
- `AF_STACK_ERRORS_ADAPTER`

Backend-specific URLs keep their backend names, for example
`AF_STACK_LOGS_LOKI_URL`.

## Validation

Layer 1 is structural: YAML shape, unknown fields, preset names, custom
completeness, and adapter enums. Legacy `errors.backend` is still accepted
as a read alias for existing config files, but new configs should use
`errors.adapter`.

Layer 2 is dependency validation: `metrics.container_metrics` requires
`metrics.enabled`, spend tracking requires virtual keys, and configured
providers require their env vars.

Postgres grants and loaded extensions are runtime capabilities, not static
CLI checks. They are surfaced by `GET /api/v1/admin/features` through
capability probes.
