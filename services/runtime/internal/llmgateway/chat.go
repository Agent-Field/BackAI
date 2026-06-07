// Package llmgateway implements an OpenAI-compatible LLM gateway shim.
//
// Wire shape closely matches the official OpenAI Python SDK request /
// response so callers can do:
//
//	client = openai.OpenAI(
//	    base_url="http://localhost:8080/api/v1/llm",
//	    api_key="af_<prefix>_<secret>",
//	)
//	client.chat.completions.create(model="...", messages=[...])
//
// and have it just work, unmodified.
//
// The invariant: every LLM call routes through AgentField. AgentField
// has a built-in LLM gateway (LiteLLM-backed); this shim wraps it.
// There is no Phase-7 escape hatch.
//
// Current state (MVP path): the Phase-7 implementation proxies to
// provider-native OpenAI-compatible endpoints (OpenRouter, OpenAI,
// Anthropic-compat) because the AgentField runtime in `docker compose`
// does not yet expose a built-in `__llm.chat` reasoner or
// `/api/v1/llm/chat/completions` shim. The public contract still says
// "routes through AF" — switching to the AF-native handler is purely
// an internal change (swap ProviderClient implementations) once AF
// lands the built-in handler. See gateway.go for the routing
// abstraction.
package llmgateway

import "encoding/json"

// ChatMessage is one entry in the conversation history.
//
// Matches OpenAI's chat.completions message shape. Content is `any` so
// it tolerates both the legacy string form and the new array-of-parts
// form (text + image_url) without forcing every caller through a
// custom type union.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall mirrors OpenAI's tool-call shape used in assistant messages.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction is the function part of a ToolCall.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a tool declaration on a chat request.
type Tool struct {
	Type     string         `json:"type"`
	Function map[string]any `json:"function,omitempty"`
}

// ResponseFormat carries OpenAI's `response_format` directive (e.g.
// `{"type": "json_object"}` or structured-output schema).
type ResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema map[string]any `json:"json_schema,omitempty"`
}

// ChatRequest is the inbound /chat/completions body shape.
//
// Fields named to match the OpenAI Python SDK exactly. Anything we
// don't yet propagate to the upstream is parsed but ignored; we error
// only on missing required fields (model + messages).
type ChatRequest struct {
	Model            string          `json:"model"`
	Messages         []ChatMessage   `json:"messages"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	N                *int            `json:"n,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	StreamOptions    map[string]any  `json:"stream_options,omitempty"`
	Stop             any             `json:"stop,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	MaxOutputTokens  *int            `json:"max_completion_tokens,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]any  `json:"logit_bias,omitempty"`
	User             string          `json:"user,omitempty"`
	Tools            []Tool          `json:"tools,omitempty"`
	ToolChoice       any             `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat `json:"response_format,omitempty"`
	Seed             *int            `json:"seed,omitempty"`
	// Extra holds any field we don't model explicitly so providers can
	// honour OpenAI-compat extensions (e.g. `repetition_penalty`).
	Extra map[string]json.RawMessage `json:"-"`
}

// ChatResponse is the non-streaming /chat/completions response.
type ChatResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []ChatChoice `json:"choices"`
	Usage             *Usage       `json:"usage,omitempty"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
}

// ChatChoice is one assistant turn returned in choices[].
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
	Logprobs     any         `json:"logprobs,omitempty"`
}

// Usage holds prompt + completion token counts (and total).
//
// Optional in the upstream response — when a provider omits it, the
// gateway records cost_usd=0 with an explicit "missing usage" log
// line so the operator notices.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatStreamChunk is one SSE event's `data: {...}` payload during a
// streaming chat response.
type ChatStreamChunk struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	Created           int64              `json:"created"`
	Model             string             `json:"model"`
	Choices           []ChatStreamChoice `json:"choices"`
	Usage             *Usage             `json:"usage,omitempty"`
	SystemFingerprint string             `json:"system_fingerprint,omitempty"`
}

// ChatStreamChoice is one streaming choice — `delta` carries the next
// fragment; `message` is unused for streams.
type ChatStreamChoice struct {
	Index        int         `json:"index"`
	Delta        ChatMessage `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}
