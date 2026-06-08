// SPDX-License-Identifier: Apache-2.0

package llmgateway

import "context"

// authCtxKey is unexported so callers can only set / read via
// WithLiteLLMKey / litellmKeyFromContext. Defense against the public
// context-key footgun.
type authCtxKey int

const litellmKeyCtxKey authCtxKey = 0

// WithLiteLLMKey returns a copy of ctx that carries the given LiteLLM
// virtual-key secret. The LiteLLM provider's HTTP client reads it via
// litellmKeyFromContext on every request — when set, it overrides the
// master key in the Authorization header so the call is attributed to
// (and budget-checked against) that virtual key.
//
// The runtime calls this in the LLM gateway handler chain after
// resolving the per-tenant LiteLLM key from the secrets vault. Empty
// key means "no override; use master key".
//
// Why context rather than gateway-method args: the Provider interface
// shouldn't carry auth semantics in every signature (Chat / ChatStream /
// Embeddings / Images / AudioSpeech / AudioTranscription would all need
// duplicated args). Context plumbing is the standard Go answer here.
func WithLiteLLMKey(ctx context.Context, key string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, litellmKeyCtxKey, key)
}

// litellmKeyFromContext returns the per-request LiteLLM key stored on
// ctx, or "" when none is set. The empty-string return signals "fall
// back to the master key" — preserving pre-#22 behaviour for the no-
// tenancy / no-vault build modes.
func litellmKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(litellmKeyCtxKey).(string); ok {
		return v
	}
	return ""
}
