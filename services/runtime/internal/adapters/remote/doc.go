// SPDX-License-Identifier: Apache-2.0

// Package remote provides the shared HTTP transport used by every
// remote-adapter shim (sandbox, storage, notifications, secrets,
// billing, multimodal). Per-slot shims live next to their slot's Go
// interface (e.g. services/runtime/internal/sandbox/adapters/remote)
// and use this package's Client to talk to a sidecar that implements
// the corresponding per-slot HTTP protocol from
// docs/adapters/protocols/.
//
// # Design goals
//
//   - **No buffering of large bodies.** Upload streams from io.Reader;
//     download returns the response body to the caller (caller closes).
//     SSE events are emitted as they arrive.
//
//   - **Connection pooling.** A single shared *http.Transport per Client
//     amortizes TLS / TCP setup across many requests.
//
//   - **Context-cancellation propagation.** Every Do() respects the
//     caller's ctx — cancellation tears down the underlying TCP conn.
//
//   - **Idempotency.** Write methods auto-generate
//     X-BackAI-Idempotency-Key from the caller-provided key (or a uuidv7
//     if none) so safe retries don't double-act on the sidecar.
//
//   - **Retry only on transient.** 5xx, 502/503/504, and 429 (with
//     Retry-After) trigger up to 3 retries with jittered exponential
//     backoff. 4xx (except 429) and 2xx do not retry.
//
//   - **Capability cache.** Per-adapter capabilities are cached for a
//     configurable TTL (default 5min) to avoid hammering /v1/capabilities
//     on every call.
//
// # Headers we always send
//
//	Authorization                 (when token set)
//	X-BackAI-Request-Id           (per-request uuidv7)
//	X-BackAI-Idempotency-Key      (write methods only)
//	X-BackAI-Tenant-Id            (when caller set TenantID on the request)
//	X-BackAI-Runtime-Version      (compile-time constant)
//	Content-Type                  (request body type)
//	X-BackAI-Content-Type         (mirrors Content-Type for envoy-style middleware that strips it)
//
// # Error model
//
// Non-2xx responses are decoded as RFC 7807 Problem objects. Callers get a
// typed *Problem error and can switch on Problem.Code (a slot-specific
// machine-readable code) without parsing strings.
package remote
