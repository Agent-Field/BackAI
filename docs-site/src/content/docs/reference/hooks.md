---
title: Hooks reference
description: Every hooks.Hook* constant the runtime fires. Payload shapes, short-circuit semantics, registration snippet.
sidebar:
  order: 18
---

Named extension points the runtime fires at well-defined moments. Handlers register against a `HookPoint`; `Engine.Fire` runs them in registration order, threading the payload through each. A handler returning a non-nil error short-circuits the chain.

Constants live in `services/runtime/internal/hooks/hooks.go`.

## Engine model

```go
type Handler func(ctx context.Context, payload any) (any, error)

type Engine struct {
    Register(point HookPoint, h Handler) error
    Fire(ctx context.Context, point HookPoint, payload any) (any, error)
    Count(point HookPoint) int
    Points() []HookPoint
}
```

Payloads are typed as `any` for flexibility. Each hook documents the concrete shape below. A typed adapter (`HookFunc[T]`) is provided in the package for safety.

| Hook constant | Wire string | Fires from | Wired in v1 |
|---|---|---|---|
| `HookGatewayPreAuth` | `gateway.pre_auth` | `server.tenantResolver` | yes |
| `HookGatewayPostAuth` | `gateway.post_auth` | `server.tenantResolver` | yes |
| `HookAFPreExecute` | `af.pre_execute` | AgentField bridge | scaffold |
| `HookAFPostExecute` | `af.post_execute` | AgentField bridge | scaffold |
| `HookLLMPreCall` | `llm.pre_call` | `server.handleLLM*` via `fireLLMPreCall` | yes (cost guard) |
| `HookLLMPostCall` | `llm.post_call` | `server.handleLLM*` via `fireLLMPostCallBest` | yes (cost ledger) |
| `HookSandboxPreRun` | `sandbox.pre_run` | `sandbox.Service.Run` | scaffold |
| `HookSandboxPostRun` | `sandbox.post_run` | `sandbox.Service.Run` | scaffold |
| `HookStoragePreUpload` | `storage.pre_upload` | `server.handleStorageUpload` | yes |
| `HookNotifPreSend` | `notifications.pre_send` | `notifications.Service.Send` | scaffold |
| `HookBillingPreCharge` | `billing.pre_charge` | `billing.Service` | scaffold |
| `HookTenantCreated` | `tenant.created` | `tenancy.Manager.CreateTenant` | yes |

Hooks marked "scaffold" are declared as constants but not yet fired by the runtime in v1 — they reserve the contract for downstream modules that will plug in later.

---

## HookGatewayPreAuth

**Fires** in `server.tenantResolver` (`server/tenant_resolver.go`), before any credential is inspected. Public paths (`isPublicPath`) bypass entirely.

**Payload** (`map[string]any`):

| Key | Type | Description |
|---|---|---|
| `method` | `string` | HTTP method. |
| `path` | `string` | Request path. |

**Short-circuit?** Yes. A non-nil error from any handler causes a `401 PRE_AUTH_DENIED` response. The error message is not exposed to the client.

**Use case** — IP allowlists, captcha gates, per-route deny rules before any tenancy lookup happens.

---

## HookGatewayPostAuth

**Fires** in `server.tenantResolver.firePostAuth`, after the principal has been resolved (API key, session, or default tenant).

**Payload** (`map[string]any`):

| Key | Type | Description |
|---|---|---|
| `method` | `string` | HTTP method. |
| `path` | `string` | Request path. |
| `tenant_id` | `string` | Resolved tenant id (may be `default`). |
| `api_key_id` | `string` | API key id, empty if session-resolved. |
| `user_id` | `string` | User id, empty if key-resolved. |
| `source` | `string` | `default`, `api_key`, or `session`. |

**Short-circuit?** No — errors log but don't abort. A post-auth hook is by definition advisory.

**Use case** — request tagging, custom audit, per-user feature flags.

---

## HookLLMPreCall

**Fires** in `server/llm.go::fireLLMPreCall`, before the upstream LLM call. Handlers are wired in `cmd/af-stack/main.go` (`hookEngine.Register(hooks.HookLLMPreCall, cost.PreCallHandler(costBudgets))`).

**Payload** (`server.LLMPreCallPayload`):

| Field (JSON) | Type | Description |
|---|---|---|
| `tenant_id` | `string` | Tenant id (omitempty). |
| `api_key_id` | `string` | API key id (omitempty). |
| `model` | `string` | Model id. |
| `provider` | `string` | Provider id (`openrouter`, `openai`, `anthropic`, ...). |
| `request_id` | `string` | X-Request-ID or auto. |
| `operation` | `string` | `chat`, `embeddings`, or `images`. |
| `stream` | `bool` | SSE or sync. |
| `estimated_cost_usd` | `float64` | Forecast based on prompt size + pricing. |
| `prompt_chars_hint` | `int` | Approximate input size. |

**Short-circuit?** Yes. The budget guard returns `cost.ErrBudgetExceeded`; the gateway maps to HTTP `402 BUDGET_EXCEEDED` and still fires `HookLLMPostCall` so the rejection is logged.

**Use case** — budget enforcement, PII guard, model allowlist per tenant.

---

## HookLLMPostCall

**Fires** in `server/llm.go::fireLLMPostCallBest`, after the upstream LLM call (or after a pre-call rejection). Best-effort: errors log but don't affect the response.

**Payload** (`server.LLMPostCallPayload`):

| Field (JSON) | Type | Description |
|---|---|---|
| `tenant_id` | `string` | Tenant id (omitempty). |
| `api_key_id` | `string` | API key id (omitempty). |
| `model` | `string` | Model id. |
| `provider` | `string` | Provider id. |
| `request_id` | `string` | Correlation id. |
| `operation` | `string` | `chat`, `embeddings`, `images`. |
| `stream` | `bool` | |
| `prompt_tokens` | `int` | |
| `completion_tokens` | `int` | |
| `total_tokens` | `int` | |
| `cost_usd` | `float64` | |
| `cost_known` | `bool` | False when pricing is missing. |
| `cached` | `bool` | True on LLM-cache hit. |
| `latency_ms` | `int` | Wall-clock latency. |
| `status_code` | `int` | Upstream HTTP status (or 0 on rejection). |
| `error_code` | `string` | Empty on success. |
| `occurred_at` | `string` | RFC3339Nano UTC. |

**Short-circuit?** No.

**Use case** — cost ledger (the default `cost.PostCallHandler`), Datadog metrics, downstream invoicing aggregator.

---

## HookStoragePreUpload

**Fires** in `server.handleStorageUpload` before the bytes are streamed to the adapter.

**Payload** (`map[string]any`):

| Key | Type | Description |
|---|---|---|
| `key` | `string` | Object key including tenant prefix. |
| `size_hint` | `int64` | `Content-Length` (multipart header). |
| `content_type` | `string` | Multipart-declared content type. |

**Short-circuit?** Yes. A non-nil error returns `403 UPLOAD_REJECTED` with the handler's message.

**Use case** — per-tenant quota, file-type allowlist, virus scan gate.

---

## HookTenantCreated

**Fires** in `tenancy.Manager.CreateTenant` after the row is committed. Uses a fresh background context with a 5s timeout so a request-deadline doesn't abort the chain.

**Payload** (`tenancy.Tenant`):

```go
type Tenant struct {
    ID        string
    Slug      string
    Name      string
    Plan      string
    Settings  map[string]interface{}
    Quota     map[string]interface{}
    CreatedAt time.Time
    DeletedAt *time.Time
}
```

**Short-circuit?** No — errors log but never roll back the tenant row. Hooks are observers, not invariants.

**Use case** — provision Stripe customer, seed default secrets, send welcome notification, create per-tenant MCP server.

---

## HookAFPreExecute / HookAFPostExecute

**Status:** scaffolded; not fired by the runtime in v1. The constants reserve the contract so an AgentField bridge module can fire them around `app.harness()` invocations without redefining the wire shape.

---

## HookSandboxPreRun / HookSandboxPostRun

**Status:** scaffolded; not fired by `sandbox.Service` in v1. Reserved for guards that need to gate or annotate sandbox runs (image allowlist, post-run artefact scan).

---

## HookNotifPreSend

**Status:** scaffolded; not fired by `notifications.Service.Send` in v1. Reserved for per-recipient suppression and compliance gates.

---

## HookBillingPreCharge

**Status:** scaffolded; not fired by `billing.Service` in v1. Reserved for hard-cap enforcement before a metered charge writes through.

---

## Register your own handler

Handlers are typed `func(ctx context.Context, payload any) (any, error)`. Register them on the engine the runtime constructs at boot.

```go
import (
    "context"
    "fmt"
    "log/slog"

    "github.com/Agent-Field/backai/services/runtime/internal/hooks"
)

// Reject uploads larger than 10 MiB.
func sizeGuard(ctx context.Context, payload any) (any, error) {
    p, _ := payload.(map[string]any)
    if size, ok := p["size_hint"].(int64); ok && size > 10<<20 {
        return payload, fmt.Errorf("file too large: %d bytes", size)
    }
    return payload, nil
}

func wire(engine *hooks.Engine, log *slog.Logger) {
    if err := engine.Register(hooks.HookStoragePreUpload, sizeGuard); err != nil {
        log.Error("hook register failed", "error", err)
    }
}
```

Handlers fire in registration order. Mutating the payload is allowed — return the modified value (or `payload` unchanged) and downstream handlers see your changes. Return `nil, fmt.Errorf(...)` to short-circuit a chain where the hook semantics allow it (see the table above).

For type safety, use the typed adapter pattern; reference handlers live in `services/runtime/internal/cost/hooks.go` (`PreCallHandler`, `PostCallHandler`) and assert the payload type before processing.

## Related

- [Modules reference](./modules/) — which module fires which hook.
- `services/runtime/internal/hooks/hooks.go` — source of truth for constants.
- `services/runtime/internal/cost/hooks.go` — reference handler implementations.
