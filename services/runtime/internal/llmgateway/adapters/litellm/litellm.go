// SPDX-License-Identifier: Apache-2.0

// Package litellm wraps the existing LiteLLMProvider so it satisfies
// the multimodal adapters.MultimodalAdapter interface. LiteLLM already
// supports OpenAI's audio + image surface (whisper-1, tts-1, dall-e-3,
// gpt-image-1) via its OpenAI passthrough; this adapter is the seam
// between the unified MultimodalAdapter shape and LiteLLM's request DTOs.
//
// HandlesModel matches any model with a prefix that LiteLLM is
// configured for via litellm-config.yaml. The default set is
// "openai/tts-1", "openai/tts-1-hd", "openai/whisper-1", "openai/dall-e-3",
// "openai/gpt-image-1". Custom routes (e.g. "anthropic/...") are still
// handled by LiteLLM via the existing /chat/completions path — not via
// this adapter.
package litellm

import (
	"context"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

// litellmCaller is the small slice of LiteLLMProvider that this adapter
// needs. Defined as an interface so tests can stub it out without
// constructing a real provider.
type litellmCaller interface {
	AudioSpeechRaw(ctx context.Context, model string, body []byte) (audioBody []byte, contentType string, err error)
	AudioTranscriptionRaw(ctx context.Context, model, ct string, body []byte, translate bool) (respBody []byte, respCT string, err error)
	ImagesRaw(ctx context.Context, edit, variations bool, multipartCT string, body []byte) (respBody []byte, err error)
}

// Adapter is the multimodal adapter backed by a LiteLLM sidecar.
type Adapter struct {
	caller litellmCaller
	// prefixes is the set of model prefixes this adapter handles.
	// Constructor accepts a custom set so operators can teach LiteLLM
	// about additional vendor namespaces without recompiling.
	prefixes []string
}

// New constructs a LiteLLM-backed multimodal adapter. The caller arg is
// typically the runtime's existing *LiteLLMProvider — see
// llmgateway.NewLiteLLMMultimodalAdapter for the bridge.
//
// prefixes defaults to the OpenAI-multimodal set when empty.
func New(caller litellmCaller, prefixes ...string) *Adapter {
	if len(prefixes) == 0 {
		prefixes = []string{
			"openai/tts-1",
			"openai/tts-1-hd",
			"openai/whisper-1",
			"openai/dall-e-2",
			"openai/dall-e-3",
			"openai/gpt-image-1",
		}
	}
	return &Adapter{caller: caller, prefixes: prefixes}
}

// Name returns the adapter id.
func (a *Adapter) Name() string { return "litellm" }

// HandlesModel returns true when the given model is one of the
// configured prefixes (exact match) or has one as its first path
// component.
func (a *Adapter) HandlesModel(model string) bool {
	if a == nil {
		return false
	}
	for _, p := range a.prefixes {
		if model == p {
			return true
		}
		if adapters.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// SupportsTTS implements MultimodalAdapter.
func (a *Adapter) SupportsTTS() bool { return true }

// SupportsSTT implements MultimodalAdapter.
func (a *Adapter) SupportsSTT() bool { return true }

// SupportsImage implements MultimodalAdapter.
func (a *Adapter) SupportsImage() bool { return true }

// Speech delegates to LiteLLM /audio/speech.
func (a *Adapter) Speech(ctx context.Context, req adapters.SpeechRequest) (adapters.SpeechResponse, error) {
	if a == nil || a.caller == nil {
		return adapters.SpeechResponse{}, adapters.ErrUnsupported
	}
	body := req.Body
	if len(body) == 0 {
		// Caller didn't tunnel a body; synthesize one from the structured fields.
		body = buildSpeechBody(req)
	}
	audio, ct, err := a.caller.AudioSpeechRaw(ctx, req.Model, body)
	if err != nil {
		return adapters.SpeechResponse{}, err
	}
	return adapters.SpeechResponse{
		Body:        audio,
		ContentType: ct,
		CharCount:   len(req.Input),
	}, nil
}

// Transcribe delegates to LiteLLM /audio/transcriptions (or
// /audio/translations when req.Translate is true).
func (a *Adapter) Transcribe(ctx context.Context, req adapters.TranscribeRequest) (adapters.TranscribeResponse, error) {
	if a == nil || a.caller == nil {
		return adapters.TranscribeResponse{}, adapters.ErrUnsupported
	}
	body, ct, err := a.caller.AudioTranscriptionRaw(ctx, req.Model, req.ContentType, req.Body, req.Translate)
	if err != nil {
		return adapters.TranscribeResponse{}, err
	}
	return adapters.TranscribeResponse{
		Body:        body,
		ContentType: ct,
	}, nil
}

// Image delegates to LiteLLM /images/generations (or /images/edits /
// /images/variations when the request flags those verbs).
func (a *Adapter) Image(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, error) {
	if a == nil || a.caller == nil {
		return adapters.ImageResponse{}, adapters.ErrUnsupported
	}
	body := req.Body
	if !req.IsEdit && !req.IsVariations && len(body) == 0 {
		body = buildImageBody(req)
	}
	respBody, err := a.caller.ImagesRaw(ctx, req.IsEdit, req.IsVariations, req.MultipartContentType, body)
	if err != nil {
		return adapters.ImageResponse{}, err
	}
	return decodeImageResponse(respBody)
}
