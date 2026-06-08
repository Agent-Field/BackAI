// SPDX-License-Identifier: Apache-2.0

// Package adapters defines the MultimodalAdapter abstraction used by the
// llmgateway to route audio/image calls to either the LiteLLM sidecar or
// a first-party HTTP adapter (ElevenLabs, Cartesia, Flux, fal.ai).
//
// The runtime keeps tenant resolution, cost ledger, budgets, and hooks
// on its side; an adapter is a leaf HTTP client that knows one
// upstream's quirks. Adapters never see tenant context — that lives in
// ctx values and the LLM gateway plumbing.
//
// Wiring lives in services/runtime/cmd/af-stack/main.go: each adapter
// constructor reads its provider key from env; if the key is unset the
// adapter is excluded from the registry and the gateway returns a clear
// 503 with "configure ELEVENLABS_API_KEY" (etc.) when a model routes to it.
package adapters

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by adapters for verbs they don't implement
// (e.g. ElevenLabs returns it for Transcribe and Image; fal returns it
// for Speech and Transcribe). The router uses this to fall through to
// the next candidate or to surface a "model not supported" error.
var ErrUnsupported = errors.New("multimodal: verb not supported by this adapter")

// ErrNoAdapter is returned by the registry when no adapter handles a
// model id. The HTTP layer renders it as 503 with a hint about which
// env var to set.
var ErrNoAdapter = errors.New("multimodal: no adapter registered for this model")

// SpeechRequest is the canonical TTS input passed to MultimodalAdapter.Speech.
//
// The body bytes are kept so a tunneling adapter (e.g. LiteLLM) can
// forward the exact upstream-compatible JSON without re-encoding;
// first-party adapters read the structured fields and build their own
// provider-specific body.
type SpeechRequest struct {
	// Model is the gateway-routable id (e.g. "openai/tts-1",
	// "elevenlabs/multilingual", "cartesia/sonic"). The first path
	// component picks the adapter; the rest is provider-specific.
	Model string
	// Input is the text to speak.
	Input string
	// Voice is the voice id / preset. Each provider has its own
	// catalogue (alloy/echo/... on OpenAI, "Rachel" or a UUID on
	// ElevenLabs). Empty is allowed for providers with a default.
	Voice string
	// ResponseFormat is the requested audio container (mp3/opus/etc.).
	// Empty -> the provider's default.
	ResponseFormat string
	// Speed is an optional playback rate multiplier (0.25..4.0). 0
	// means "use provider default".
	Speed float64
	// Body is the original OpenAI-compatible JSON body (for adapters
	// that tunnel it verbatim, like LiteLLM).
	Body []byte
}

// SpeechResponse is the canonical TTS output. Body is the audio bytes;
// ContentType is the response MIME (audio/mpeg, audio/wav, ...).
type SpeechResponse struct {
	Body        []byte
	ContentType string
	// CharCount is the input character count, used by cost tracking.
	// Populated by the gateway from the request; adapters don't have
	// to fill it.
	CharCount int
}

// TranscribeRequest is the canonical STT input passed to
// MultimodalAdapter.Transcribe and .Translate.
type TranscribeRequest struct {
	// Model is the gateway-routable id (e.g. "openai/whisper-1").
	Model string
	// ContentType is the multipart/form-data Content-Type header
	// (including the boundary) of the original request.
	ContentType string
	// Body is the multipart payload — file part plus any additional
	// form fields the caller supplied.
	Body []byte
	// Translate, when true, asks the adapter to translate to English
	// in the same call (mirrors OpenAI's /audio/translations endpoint).
	Translate bool
}

// TranscribeResponse mirrors the OpenAI JSON shape (or text, when the
// caller asked for response_format=text).
type TranscribeResponse struct {
	Body        []byte
	ContentType string
	// DurationSeconds, when known, drives per-minute cost tracking.
	// Adapters that don't have this leave it 0 and the gateway falls
	// back to a flat per-request rate or skips cost tracking entirely.
	DurationSeconds float64
}

// ImageRequest is the canonical image-generation input.
type ImageRequest struct {
	// Model is the gateway-routable id (e.g. "openai/dall-e-3",
	// "flux/schnell", "fal/fast-sdxl").
	Model string
	// Prompt is the image generation prompt.
	Prompt string
	// N is the number of images to generate (1 if zero).
	N int
	// Size is e.g. "1024x1024" — provider-specific.
	Size string
	// Quality is e.g. "standard" / "hd" (DALL-E-3) or provider-specific.
	Quality string
	// Style is e.g. "vivid" / "natural" (DALL-E-3) or provider-specific.
	Style string
	// ResponseFormat is "url" or "b64_json".
	ResponseFormat string
	// Body is the original OpenAI-compatible JSON body (tunneling
	// adapters forward verbatim).
	Body []byte
	// Edit / Variations parameters (multipart). Empty for plain
	// generations.
	IsEdit       bool
	IsVariations bool
	// MultipartContentType is set when the request arrived as
	// multipart/form-data (edits/variations). Adapters that don't
	// support these verbs return ErrUnsupported.
	MultipartContentType string
}

// ImageResponse mirrors the OpenAI /images/generations response shape.
//
// The gateway returns this verbatim to clients as JSON.
type ImageResponse struct {
	Created int64       `json:"created"`
	Data    []ImageItem `json:"data"`
}

// ImageItem is one image in data[]. Either URL or B64JSON is populated
// depending on the request's response_format.
type ImageItem struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// MultimodalAdapter is the abstraction every provider implements. The
// gateway picks one by inspecting the model prefix and dispatching the
// matching verb. Verbs the adapter doesn't implement return ErrUnsupported.
type MultimodalAdapter interface {
	// Name returns the adapter id ("litellm", "elevenlabs", "cartesia",
	// "flux", "fal"). Used as the provider field of cost events.
	Name() string
	// HandlesModel returns true when this adapter is the canonical
	// route for the given model id. The router calls this on every
	// registered adapter in registration order and picks the first hit.
	HandlesModel(model string) bool
	// SupportsTTS / STT / Image declare verbs the adapter implements.
	// Used by the gateway to fail fast with a clear error when a
	// request lands on an adapter that doesn't handle the verb.
	SupportsTTS() bool
	SupportsSTT() bool
	SupportsImage() bool
	// Speech generates audio from text. Return ErrUnsupported when
	// SupportsTTS() is false.
	Speech(ctx context.Context, req SpeechRequest) (SpeechResponse, error)
	// Transcribe converts audio to text. Set req.Translate to drive
	// /audio/translations (translate-to-English). Return ErrUnsupported
	// when SupportsSTT() is false.
	Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error)
	// Image generates / edits / varies images. The request's IsEdit /
	// IsVariations fields select the verb. Return ErrUnsupported when
	// SupportsImage() is false or when the specific verb isn't
	// implemented (e.g. variations).
	Image(ctx context.Context, req ImageRequest) (ImageResponse, error)
}

// Registry is the ordered list of adapters the gateway consults.
// PickAdapter walks the slice and returns the first one whose
// HandlesModel returns true for the given model id.
type Registry struct {
	adapters []MultimodalAdapter
}

// NewRegistry constructs a registry from the given adapter list. The
// order matters: first hit wins. Nil entries are silently skipped so
// callers can append unconditionally based on env presence.
func NewRegistry(adapters ...MultimodalAdapter) *Registry {
	r := &Registry{}
	for _, a := range adapters {
		if a != nil {
			r.adapters = append(r.adapters, a)
		}
	}
	return r
}

// Pick returns the adapter that handles the given model id, or nil.
func (r *Registry) Pick(model string) MultimodalAdapter {
	if r == nil {
		return nil
	}
	for _, a := range r.adapters {
		if a.HandlesModel(model) {
			return a
		}
	}
	return nil
}

// Names returns the registered adapter ids (for log lines + the
// /api/v1/llm/models response).
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a.Name())
	}
	return out
}

// HasPrefix is a small helper for adapter HandlesModel impls — they all
// share the same "match on first path component" pattern.
func HasPrefix(model, prefix string) bool {
	if len(model) <= len(prefix) {
		return false
	}
	if model[len(prefix)] != '/' {
		return false
	}
	return model[:len(prefix)] == prefix
}
