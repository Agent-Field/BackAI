# LLM Chat Gateway Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md).
>
> **Slot:** `llm-chat` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/llmgateway/Provider`

## Purpose

The LLM chat gateway adapter handles the platform's chat and embedding
calls: the path every `/api/v1/llm/*` request from the runtime
eventually goes through. Built-in adapter today: LiteLLM (sidecar).
Remote adapters can be anything OpenAI-compatible: Helicone, Portkey,
direct OpenAI, Together AI, Groq via a proxy, on-prem LLM servers.

This protocol intentionally mirrors OpenAI's `/v1/chat/completions`,
`/v1/embeddings`, and `/v1/models` shapes — adapters that already
proxy OpenAI need almost no translation.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/chat/completions` | Chat completion (sync or streaming) |
| `POST` | `/v1/embeddings` | Embed text |
| `GET` | `/v1/models` | List models this adapter serves |
| `GET` | `/v1/capabilities` | Capability declaration |
| `GET` | `/healthz` | Liveness |
| `GET` | `/v1/info` | Optional metadata |

## 1. `POST /v1/chat/completions`

OpenAI-shaped chat completion request.

**Request body**:

```json
{
  "model": "openai/gpt-4o",
  "messages": [
    {"role": "system", "content": "You are helpful."},
    {"role": "user", "content": "What's 2+2?"}
  ],
  "max_tokens": 64,
  "temperature": 0.7,
  "top_p": 1.0,
  "stream": false,
  "tools": [],
  "tool_choice": "auto",
  "response_format": {"type": "text"},
  "user": "tenant-acme"
}
```

The full OpenAI body is forwarded verbatim. Adapters that don't
support a particular field MAY ignore it or return
`unsupported_parameter`. The `user` field, when set by the runtime,
contains the tenant id for upstream attribution / spend tracking.

**Response (200 OK, non-streaming)**:

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1700000000,
  "model": "openai/gpt-4o-2024-08-06",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "4"},
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 14,
    "completion_tokens": 1,
    "total_tokens": 15
  }
}
```

`usage` is **required** when known so the runtime can record cost.
When upstream doesn't report usage, the adapter MAY estimate based on
its own tokeniser and add `"usage_estimated": true` to the response
(extension field, runtime ignores when not present).

**Response (200 OK, streaming when `stream=true`)**: `Content-Type:
text/event-stream`. Each event is one OpenAI chunk:

```
data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"4"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-abc","usage":{"prompt_tokens":14,"completion_tokens":1,"total_tokens":15}}

data: [DONE]
```

The final usage chunk is optional but recommended; the runtime sums
deltas when absent. Clients may opt in explicitly by passing
`stream_options: {"include_usage": true}` per OpenAI's spec — adapters
MUST honour the opt-in. `data: [DONE]` is the canonical stream
terminator (matches OpenAI). Adapters MUST emit it.

### Reasoning / thinking-token models

For models that expose reasoning tokens (OpenAI o1 / o3, Kimi-K2,
DeepSeek-R1, Claude with extended thinking), adapters MAY include
`reasoning_content` alongside `content` in `delta` (streaming) or
`message` (non-streaming):

```json
{"role":"assistant","content":"The answer is 4.","reasoning_content":"Let me think: 2+2 is..."}
```

The runtime preserves both fields end-to-end. Callers that want to
filter the reasoning trace from their UI can drop the field; cost
events count both.

### Cost-tracking response headers (recommended)

Adapters that know their per-call cost SHOULD set:

```
X-Backai-Response-Cost-Usd: 0.000420
X-Backai-Model-Used: openai/gpt-4o-2024-08-06
```

The runtime records these directly into the cost ledger. When absent,
the runtime estimates from `usage` and its own pricing catalog.

## 2. `POST /v1/embeddings`

**Request body**:

```json
{
  "model": "openai/text-embedding-3-small",
  "input": ["text to embed", "another text"],
  "encoding_format": "float",
  "dimensions": 1536,
  "user": "tenant-acme"
}
```

`input` MAY be a string OR an array of strings (matches OpenAI).

**Response (200 OK)**:

```json
{
  "object": "list",
  "data": [
    {"object": "embedding", "index": 0, "embedding": [0.001, 0.002, ...]},
    {"object": "embedding", "index": 1, "embedding": [0.003, 0.004, ...]}
  ],
  "model": "openai/text-embedding-3-small",
  "usage": {"prompt_tokens": 7, "total_tokens": 7}
}
```

`encoding_format=base64` is also supported (matches OpenAI). When the
adapter doesn't support base64, returns `unsupported_parameter`.

## 3. `GET /v1/models`

List models this adapter serves.

**Query params**:

| Param | Default | Meaning |
|---|---|---|
| `verb` | empty | Filter by capability: `chat`, `embedding`. Empty returns all. |

**Response (200 OK)**:

```json
{
  "object": "list",
  "data": [
    {
      "id": "openai/gpt-4o",
      "object": "model",
      "created": 1700000000,
      "owned_by": "openai",
      "verbs": ["chat"],
      "context_window": 128000,
      "max_output_tokens": 16384,
      "supports_streaming": true,
      "supports_tools": true,
      "supports_vision": true,
      "supports_json_mode": true,
      "input_cost_per_million_tokens": 2.50,
      "output_cost_per_million_tokens": 10.00,
      "deprecation_date": null
    },
    {
      "id": "openai/text-embedding-3-small",
      "object": "model",
      "verbs": ["embedding"],
      "embedding_dimensions": 1536,
      "input_cost_per_million_tokens": 0.02
    }
  ]
}
```

Extension fields (verbs, context_window, capability flags, costs) are
BackAI-specific and feed the dashboard's model catalog. Adapters MAY
omit any field they don't know.

## 4. `GET /v1/capabilities`

```json
{
  "name": "litellm",
  "version": "1.40.0",
  "slot": "llm-chat",
  "protocol_version": "v1",
  "vendor": "BerriAI",
  "homepage": "https://litellm.ai",
  "capabilities": {
    "supports_chat": true,
    "supports_embeddings": true,
    "supports_streaming": true,
    "supports_tools": true,
    "supports_vision": true,
    "supports_json_mode": true,
    "supports_logprobs": true,
    "supports_fallback_chains": true,
    "supports_per_tenant_attribution": true,
    "model_prefixes": ["openai/", "anthropic/", "google/", "openrouter/"],
    "max_completion_tokens": 8192,
    "max_context_window": 200000,
    "rate_limit_per_minute": 10000,
    "default_model": "openai/gpt-4o-mini",
    "fallback_chain_default": ["openai/gpt-4o", "anthropic/claude-3.5-sonnet"],
    "tokeniser_available": true
  }
}
```

| Key | Type | Meaning |
|---|---|---|
| `supports_chat` / `_embeddings` | bool | Verb support; runtime rejects unsupported verbs before dispatch. |
| `supports_streaming` | bool | Whether `stream=true` is honoured. If `false`, runtime falls back to non-streaming. |
| `supports_tools` | bool | Whether `tools` and `tool_choice` are honoured. |
| `supports_vision` | bool | Whether image inputs (`image_url` content parts) are accepted. |
| `supports_json_mode` | bool | Whether `response_format={"type":"json_object"}` works. |
| `supports_logprobs` | bool | Whether `logprobs` parameter is honoured. |
| `supports_fallback_chains` | bool | Adapter has its own routing/fallback; runtime can leave it to the adapter. |
| `supports_per_tenant_attribution` | bool | Adapter honours the `user` field for upstream attribution. |
| `model_prefixes` | string[] | Model id prefixes the adapter handles. Runtime uses this for routing when multiple adapters coexist. |
| `max_completion_tokens` | int | Adapter's upper bound on `max_tokens`. |
| `max_context_window` | int | Largest context window across the adapter's models. |
| `rate_limit_per_minute` | int | Adapter's known upstream RPM ceiling. Runtime self-throttles. |
| `default_model` | string | Used when callers don't specify. |
| `fallback_chain_default` | string[] | Ordered list of fallback model ids the adapter applies on primary failure. |
| `tokeniser_available` | bool | Whether the adapter can pre-count tokens (informational). |

## 5. Error codes

| Code | HTTP | Meaning |
|---|---|---|
| `model_not_found` | 404 | Model id not handled by this adapter. |
| `unsupported_parameter` | 422 | A request field isn't supported (e.g., `logprobs` on an adapter that doesn't do them). |
| `context_length_exceeded` | 413 | Prompt+max_tokens exceeds the model's window. |
| `content_filter` | 400 | Upstream's safety filter rejected the request. `detail` carries the category. |
| `rate_limited` | 429 | Upstream throttled. Include `Retry-After` or `retry_after_seconds`. |
| `quota_exceeded` | 429 | Adapter-level quota / budget exhausted. |
| `provider_error` | 502 | Upstream returned an error; `detail` echoes it. |
| `provider_unavailable` | 503 | Upstream unreachable. |
| `unauthorized` | 401 | Bearer token rejected by adapter, OR upstream rejected the provider key. |
| `invalid_request` | 400 | Malformed request body. |
| `internal_error` | 500 | Catch-all. |

### Vision content parts

Adapters declaring `supports_vision: true` MUST accept the OpenAI
array-of-content-parts shape for user messages:

```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "What's in this image?"},
    {"type": "image_url", "image_url": {"url": "https://example.com/cat.png"}}
  ]
}
```

`image_url.url` may be an HTTPS URL or a `data:` URL with base64
payload (`data:image/png;base64,iVBORw...`). Adapters MAY cap image
size; reject oversized inputs with `413 input_too_large`.

### Tool / function calling

Adapters declaring `supports_tools: true` MUST forward the `tools` and
`tool_choice` request fields verbatim and return `tool_calls` in the
response:

```json
{
  "id": "chatcmpl-tools",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": null,
      "tool_calls": [{
        "id": "call_abc",
        "type": "function",
        "function": {
          "name": "get_weather",
          "arguments": "{\"location\":\"Paris\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}
```

In streaming mode, `tool_calls` arrive incrementally as deltas — each
chunk carries an array of partial `tool_calls` whose `function.arguments`
are concatenated by the consumer (matches OpenAI's spec).

### Connector-specific extension headers

Adapters wrapping observability / routing proxies (Helicone, Portkey,
OpenLLMetry) often consume their own headers in addition to OpenAI's
body. The runtime forwards any `Helicone-*`, `x-portkey-*`,
`traceparent`, `langfuse-*` headers received from the client
**verbatim** to the adapter. The protocol itself stays vendor-neutral;
operators configure these via the customer-app or in workload-module
code. The adapter is free to use or ignore them.

## 6. Compatibility with OpenAI-shaped error envelopes

The universal contract requires RFC 7807 error responses. Many
OpenAI-compatible upstreams instead return:

```json
{"error":{"message":"...","type":"invalid_request_error","code":"...","param":"..."}}
```

Adapters that wrap such upstreams MUST translate to RFC 7807 before
returning to the runtime. The conformance harness rejects raw
OpenAI-style error envelopes — wrap them:

```json
{
  "type": "https://docs.backai.dev/errors/llm-chat/invalid-request",
  "title": "Invalid request",
  "status": 400,
  "code": "invalid_request",
  "detail": "model is required",
  "request_id": "01HZ..."
}
```

The original upstream `error.code` can be carried in an extension
field (`upstream_code`) if useful for debugging.

## 7. Behavior notes

- **Pass-through bodies.** Adapters that wrap an OpenAI-compatible
  upstream MAY forward the body verbatim — the protocol shape matches.
- **Token attribution.** Set the OpenAI `user` field (request body) to
  the tenant id. Adapters that support per-tenant attribution
  (Helicone, Portkey, LiteLLM virtual keys) MUST honour this for
  upstream spend tracking.
- **Cost reporting.** Always include `usage` when known; set
  `X-Backai-Response-Cost-Usd` when the adapter can compute cost
  directly. The runtime prefers the response cost header over its
  own catalog estimate.
- **Streaming termination.** The `data: [DONE]` sentinel is mandatory.
  Without it, the runtime treats the stream as failed.
- **Multi-modal content (vision).** Adapters declaring
  `supports_vision: true` MUST accept the array-of-content-parts shape
  for user messages (text + image_url parts) per OpenAI's spec.
- **Tool calls.** Adapters declaring `supports_tools: true` MUST
  preserve the `tools` request field and return `tool_calls` in
  responses per OpenAI's spec.

## 8. Mapping back to the Go interface

| Go method | HTTP call |
|---|---|
| `ChatCompletions(ctx, req)` | `POST /v1/chat/completions` (stream=false) |
| `ChatCompletionsStream(ctx, req)` | `POST /v1/chat/completions` (stream=true) → SSE channel |
| `Embeddings(ctx, req)` | `POST /v1/embeddings` |
| `ListModels(ctx, verb)` | `GET /v1/models?verb=...` |
| `Capabilities()` | cached `GET /v1/capabilities` |

## 9. Conformance checklist

- [ ] `POST /v1/chat/completions` with a small prompt returns a JSON response with `choices[0].message.content` and `usage`
- [ ] Streaming `POST /v1/chat/completions` emits at least one SSE chunk and terminates with `data: [DONE]`
- [ ] `POST /v1/embeddings` returns `data[*].embedding` as a float array of the declared `embedding_dimensions`
- [ ] `GET /v1/models?verb=chat` returns only chat-capable models
- [ ] `GET /v1/models?verb=embedding` returns only embedding-capable models
- [ ] Unsupported model returns `404 + model_not_found`
- [ ] Adapter declaring `supports_streaming=false` honours `stream=true` by silently returning the non-streaming shape (or rejects with `unsupported_parameter`)
- [ ] Cost header `X-Backai-Response-Cost-Usd` is set when capability declares cost tracking
- [ ] Idempotent re-`POST` with same `X-BackAI-Idempotency-Key` returns identical response (typically by adapter-side dedup or LLM determinism with `temperature=0`)
- [ ] Bearer auth enforced on all endpoints
