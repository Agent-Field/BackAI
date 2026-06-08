// SPDX-License-Identifier: Apache-2.0

// multimodal.go — the Gateway-side facade for audio + image calls that
// dispatches through the adapters.Registry rather than the chat-only
// ProviderClient.
//
// The pre-multimodal Gateway routes /audio/* and /images/* directly to
// LiteLLM via the ProviderClient interface. Once first-party adapters
// (ElevenLabs, Cartesia, Flux, fal.ai) enter the picture, that single
// pipe isn't enough: we need to pick an adapter based on the model id.
//
// The Gateway keeps a *Multimodal in addition to its ProviderClient.
// When a multimodal request arrives, the handler calls Gateway.Speech /
// Transcribe / Image; those methods consult the registry, fall back to
// the legacy LiteLLM provider for backward compatibility (e.g.
// "openai/tts-1" still works even when no explicit "openai/tts-1"
// adapter is registered), and return a single unified shape.
//
// Backward-compat path: when no Multimodal is wired, Speech / Transcribe /
// Image fall through to the existing ProviderClient.AudioSpeech /
// AudioTranscription / Images methods — so a runtime that doesn't
// construct a Multimodal still serves /audio/* and /images/* exactly
// as it did before.

package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

// Multimodal wraps an adapters.Registry plus a default-fallback
// MultimodalAdapter (typically the LiteLLM-backed one). The fallback
// handles any model not claimed by an explicit first-party adapter,
// keeping the OpenAI catalogue available out-of-the-box.
type Multimodal struct {
	registry *adapters.Registry
	fallback adapters.MultimodalAdapter
}

// NewMultimodal constructs the facade. The fallback may be nil — in
// that case unmatched models return adapters.ErrNoAdapter.
func NewMultimodal(registry *adapters.Registry, fallback adapters.MultimodalAdapter) *Multimodal {
	return &Multimodal{
		registry: registry,
		fallback: fallback,
	}
}

// Adapters returns the registered first-party adapter ids + the
// fallback (when set). Used by /api/v1/llm/models to list which
// providers are live in this runtime.
func (m *Multimodal) Adapters() []string {
	if m == nil {
		return nil
	}
	out := m.registry.Names()
	if m.fallback != nil {
		out = append(out, m.fallback.Name())
	}
	return out
}

// pick returns the adapter that should handle the given model id. The
// registry wins over the fallback; a missing fallback returns nil.
func (m *Multimodal) pick(model string) adapters.MultimodalAdapter {
	if m == nil {
		return nil
	}
	if a := m.registry.Pick(model); a != nil {
		return a
	}
	return m.fallback
}

// WithMultimodal wires the multimodal facade into the gateway. May be
// called from the runtime's boot code after the adapter list is
// assembled. Calling it again replaces the previous wiring.
func (g *Gateway) WithMultimodal(m *Multimodal) *Gateway {
	if g == nil {
		return g
	}
	g.multimodal = m
	return g
}

// Multimodal returns the multimodal facade, or nil when none is wired.
// Handlers use this to decide whether to consult the registry path.
func (g *Gateway) MultimodalRegistry() *Multimodal {
	if g == nil {
		return nil
	}
	return g.multimodal
}

// Speech dispatches a SpeechRequest through the multimodal registry.
// When no multimodal is wired, falls through to the ProviderClient's
// AudioSpeech method (legacy behaviour).
func (g *Gateway) Speech(ctx context.Context, req adapters.SpeechRequest) (adapters.SpeechResponse, string, error) {
	if g == nil {
		return adapters.SpeechResponse{}, "", ErrNoProvider
	}
	if req.Model == "" {
		return adapters.SpeechResponse{}, "", &APIError{Code: ErrCodeInvalidRequest, Message: "model is required"}
	}
	if g.multimodal != nil {
		a := g.multimodal.pick(req.Model)
		if a == nil {
			return adapters.SpeechResponse{}, "", noAdapterError(req.Model, "tts")
		}
		if !a.SupportsTTS() {
			return adapters.SpeechResponse{}, a.Name(), unsupportedVerb(a.Name(), req.Model, "tts")
		}
		resp, err := a.Speech(ctx, req)
		if errors.Is(err, adapters.ErrUnsupported) {
			return adapters.SpeechResponse{}, a.Name(), unsupportedVerb(a.Name(), req.Model, "tts")
		}
		if resp.CharCount == 0 {
			resp.CharCount = len(req.Input)
		}
		return resp, a.Name(), err
	}
	// Legacy fallthrough.
	if g.provider == nil {
		return adapters.SpeechResponse{}, "", ErrNoProvider
	}
	body := req.Body
	if len(body) == 0 {
		body = legacySpeechBody(req)
	}
	out, err := g.provider.AudioSpeech(ctx, AudioSpeechRequest{
		Model: req.Model,
		Input: req.Input,
		Body:  body,
	})
	if err != nil {
		return adapters.SpeechResponse{}, g.provider.Name(), err
	}
	return adapters.SpeechResponse{
		Body:        out.Body,
		ContentType: out.ContentType,
		CharCount:   len(req.Input),
	}, g.provider.Name(), nil
}

// Transcribe dispatches an STT (or translation) request through the
// registry. Falls back to the legacy ProviderClient when no multimodal
// is wired.
func (g *Gateway) Transcribe(ctx context.Context, req adapters.TranscribeRequest) (adapters.TranscribeResponse, string, error) {
	if g == nil {
		return adapters.TranscribeResponse{}, "", ErrNoProvider
	}
	if req.Model == "" {
		return adapters.TranscribeResponse{}, "", &APIError{Code: ErrCodeInvalidRequest, Message: "model is required"}
	}
	if g.multimodal != nil {
		a := g.multimodal.pick(req.Model)
		if a == nil {
			return adapters.TranscribeResponse{}, "", noAdapterError(req.Model, "stt")
		}
		if !a.SupportsSTT() {
			return adapters.TranscribeResponse{}, a.Name(), unsupportedVerb(a.Name(), req.Model, "stt")
		}
		resp, err := a.Transcribe(ctx, req)
		if errors.Is(err, adapters.ErrUnsupported) {
			return adapters.TranscribeResponse{}, a.Name(), unsupportedVerb(a.Name(), req.Model, "stt")
		}
		return resp, a.Name(), err
	}
	if g.provider == nil {
		return adapters.TranscribeResponse{}, "", ErrNoProvider
	}
	out, err := g.provider.AudioTranscription(ctx, AudioTranscriptionRequest{
		Model:       req.Model,
		ContentType: req.ContentType,
		Body:        req.Body,
	})
	if err != nil {
		return adapters.TranscribeResponse{}, g.provider.Name(), err
	}
	return adapters.TranscribeResponse{
		Body:        out.Body,
		ContentType: out.ContentType,
	}, g.provider.Name(), nil
}

// Image dispatches an image generation/edit/variation request.
func (g *Gateway) Image(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, string, error) {
	if g == nil {
		return adapters.ImageResponse{}, "", ErrNoProvider
	}
	if g.multimodal != nil {
		a := g.multimodal.pick(req.Model)
		if a == nil {
			return adapters.ImageResponse{}, "", noAdapterError(req.Model, "image")
		}
		if !a.SupportsImage() {
			return adapters.ImageResponse{}, a.Name(), unsupportedVerb(a.Name(), req.Model, "image")
		}
		resp, err := a.Image(ctx, req)
		if errors.Is(err, adapters.ErrUnsupported) {
			return adapters.ImageResponse{}, a.Name(), unsupportedVerb(a.Name(), req.Model, "image")
		}
		return resp, a.Name(), err
	}
	if g.provider == nil {
		return adapters.ImageResponse{}, "", ErrNoProvider
	}
	if req.IsEdit || req.IsVariations {
		// Legacy ProviderClient interface doesn't expose edits/variations.
		// Operators that want these on the legacy path need to wire a
		// Multimodal facade.
		return adapters.ImageResponse{}, "", &APIError{
			Code:    ErrCodeModelNotSupported,
			Message: "image edits/variations require the multimodal facade (none wired)",
			Status:  503,
		}
	}
	out, err := g.provider.Images(ctx, ImagesRequest{
		Model:  req.Model,
		Prompt: req.Prompt,
		Size:   req.Size,
	})
	if err != nil {
		return adapters.ImageResponse{}, g.provider.Name(), err
	}
	resp := adapters.ImageResponse{
		Created: out.Created,
	}
	for _, it := range out.Data {
		resp.Data = append(resp.Data, adapters.ImageItem{
			URL:           it.URL,
			B64JSON:       it.B64JSON,
			RevisedPrompt: it.RevisedPrompt,
		})
	}
	return resp, g.provider.Name(), nil
}

// ─── helpers ─────────────────────────────────────────────────────────────

func noAdapterError(model, verb string) error {
	return &APIError{
		Code:    ErrCodeModelNotSupported,
		Message: fmt.Sprintf("no multimodal adapter registered for model %q (verb=%s); check the provider's env key is set", model, verb),
		Status:  503,
	}
}

func unsupportedVerb(adapterName, model, verb string) error {
	return &APIError{
		Code:    ErrCodeModelNotSupported,
		Message: fmt.Sprintf("adapter %q does not support %s for model %q", adapterName, verb, model),
		Status:  400,
	}
}

// legacySpeechBody serialises a SpeechRequest into a basic OpenAI body
// for the no-Multimodal fall-through path. Used only when the request
// arrives without a tunnel body.
func legacySpeechBody(req adapters.SpeechRequest) []byte {
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}
	if req.Voice != "" {
		payload["voice"] = req.Voice
	}
	if req.ResponseFormat != "" {
		payload["response_format"] = req.ResponseFormat
	}
	if req.Speed > 0 {
		payload["speed"] = req.Speed
	}
	out, _ := json.Marshal(payload)
	return out
}
