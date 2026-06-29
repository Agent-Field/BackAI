// SPDX-License-Identifier: Apache-2.0

// Package remote implements the MultimodalAdapter interface by talking the
// HTTP protocol defined in docs/adapters/protocols/multimodal-v1.md to a
// remote sidecar adapter.
//
// One Adapter per remote endpoint. Goroutine-safe; the underlying
// remote.Client owns the connection pool.
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

// Adapter is a MultimodalAdapter backed by a remote sidecar speaking the
// multimodal-v1 protocol.
type Adapter struct {
	client *remote.Client
	caps   multimodalCaps
	name   string // active adapter name from /v1/capabilities

	// CachedAt is the time we last refreshed capabilities.
	CachedAt time.Time
}

// Compile-time interface check.
var _ adapters.MultimodalAdapter = (*Adapter)(nil)

// Config is the env-driven configuration.
type Config struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	MaxRetries int
}

// New returns an Adapter for the given config. Performs a synchronous
// GET /v1/capabilities to verify the sidecar speaks the protocol; if
// that call fails, returns the error so the runtime can refuse to start.
func New(ctx context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("remote: BaseURL required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 2
	}

	c, err := remote.NewClient(remote.Config{
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("remote: client: %w", err)
	}

	a := &Adapter{client: c}
	if err := a.refreshCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("remote: capabilities probe: %w", err)
	}
	return a, nil
}

// refreshCapabilities pulls /v1/capabilities and decodes the multimodal-
// specific shape into our typed multimodalCaps.
func (a *Adapter) refreshCapabilities(ctx context.Context) error {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	if resp.Slot != "multimodal" {
		return fmt.Errorf("remote: adapter reports slot=%q; expected multimodal", resp.Slot)
	}
	a.name = resp.Name
	var caps multimodalCaps
	if len(resp.Capabilities) > 0 {
		if err := json.Unmarshal(resp.Capabilities, &caps); err != nil {
			return fmt.Errorf("remote: decode capabilities: %w", err)
		}
	}
	a.caps = caps
	a.CachedAt = time.Now()
	return nil
}

// Name returns the active adapter's name (from /v1/capabilities).
func (a *Adapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

// HandlesModel returns true when this adapter is the canonical route for
// the given model id, based on capabilities.model_prefixes.
func (a *Adapter) HandlesModel(model string) bool {
	if a == nil {
		return false
	}
	for _, prefix := range a.caps.ModelPrefixes {
		if adapters.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

// SupportsTTS returns true when the adapter supports text-to-speech.
func (a *Adapter) SupportsTTS() bool {
	if a == nil {
		return false
	}
	return a.caps.SupportsTTS
}

// SupportsSTT returns true when the adapter supports speech-to-text.
func (a *Adapter) SupportsSTT() bool {
	if a == nil {
		return false
	}
	return a.caps.SupportsSTT
}

// SupportsImage returns true when the adapter supports image generation.
func (a *Adapter) SupportsImage() bool {
	if a == nil {
		return false
	}
	return a.caps.SupportsImageGeneration || a.caps.SupportsImageEdit || a.caps.SupportsImageVariation
}

// Speech generates audio from text by posting to /v1/audio/speech.
func (a *Adapter) Speech(ctx context.Context, req adapters.SpeechRequest) (adapters.SpeechResponse, error) {
	if a == nil {
		return adapters.SpeechResponse{}, adapters.ErrUnsupported
	}
	if !a.SupportsTTS() {
		return adapters.SpeechResponse{}, adapters.ErrUnsupported
	}

	// Build request body matching the protocol shape.
	body := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}
	if req.Voice != "" {
		body["voice"] = req.Voice
	}
	if req.ResponseFormat != "" {
		body["response_format"] = req.ResponseFormat
	}
	if req.Speed > 0 {
		body["speed"] = req.Speed
	}

	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodPost,
		Path:   "/v1/audio/speech",
		Body:   body,
	})
	if err != nil {
		return adapters.SpeechResponse{}, err
	}
	defer resp.Body.Close()

	// Response is raw audio bytes.
	audioBytes, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MiB cap
	if err != nil {
		return adapters.SpeechResponse{}, fmt.Errorf("remote: read audio body: %w", err)
	}

	// Extract char count from header.
	charCount := 0
	if s := resp.Header.Get("X-Backai-Char-Count"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			charCount = n
		}
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}

	return adapters.SpeechResponse{
		Body:        audioBytes,
		ContentType: ct,
		CharCount:   charCount,
	}, nil
}

// Transcribe converts audio to text by posting to /v1/audio/transcriptions
// or /v1/audio/translations (depending on req.Translate).
func (a *Adapter) Transcribe(ctx context.Context, req adapters.TranscribeRequest) (adapters.TranscribeResponse, error) {
	if a == nil {
		return adapters.TranscribeResponse{}, adapters.ErrUnsupported
	}
	if !a.SupportsSTT() {
		return adapters.TranscribeResponse{}, adapters.ErrUnsupported
	}

	// Route to the correct endpoint based on Translate flag.
	path := "/v1/audio/transcriptions"
	if req.Translate {
		path = "/v1/audio/translations"
	}

	resp, err := a.client.Do(ctx, remote.Request{
		Method:      http.MethodPost,
		Path:        path,
		BodyReader:  io.NopCloser(strings.NewReader(string(req.Body))),
		ContentType: req.ContentType,
	})
	if err != nil {
		return adapters.TranscribeResponse{}, err
	}
	defer resp.Body.Close()

	// Response body is either JSON or plain text depending on response_format.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		return adapters.TranscribeResponse{}, fmt.Errorf("remote: read transcription body: %w", err)
	}

	// Extract duration from header.
	durationSeconds := 0.0
	if s := resp.Header.Get("X-Backai-Duration-Seconds"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			durationSeconds = f
		}
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}

	return adapters.TranscribeResponse{
		Body:            respBody,
		ContentType:     ct,
		DurationSeconds: durationSeconds,
	}, nil
}

// Image generates, edits, or varies images by posting to the appropriate
// endpoint (/v1/images/generations, /v1/images/edits, or /v1/images/variations).
func (a *Adapter) Image(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, error) {
	if a == nil {
		return adapters.ImageResponse{}, adapters.ErrUnsupported
	}
	if !a.SupportsImage() {
		return adapters.ImageResponse{}, adapters.ErrUnsupported
	}

	// Determine the endpoint and request structure.
	var doReq remote.Request

	if req.IsEdit {
		if !a.caps.SupportsImageEdit {
			return adapters.ImageResponse{}, adapters.ErrUnsupported
		}
		// Edits use multipart body.
		doReq = remote.Request{
			Method:      http.MethodPost,
			Path:        "/v1/images/edits",
			BodyReader:  io.NopCloser(strings.NewReader(string(req.Body))),
			ContentType: req.MultipartContentType,
		}
	} else if req.IsVariations {
		if !a.caps.SupportsImageVariation {
			return adapters.ImageResponse{}, adapters.ErrUnsupported
		}
		// Variations use multipart body.
		doReq = remote.Request{
			Method:      http.MethodPost,
			Path:        "/v1/images/variations",
			BodyReader:  io.NopCloser(strings.NewReader(string(req.Body))),
			ContentType: req.MultipartContentType,
		}
	} else {
		if !a.caps.SupportsImageGeneration {
			return adapters.ImageResponse{}, adapters.ErrUnsupported
		}
		// Generations use JSON body.
		body := map[string]any{
			"model":  req.Model,
			"prompt": req.Prompt,
		}
		if req.N > 0 {
			body["n"] = req.N
		}
		if req.Size != "" {
			body["size"] = req.Size
		}
		if req.Quality != "" {
			body["quality"] = req.Quality
		}
		if req.Style != "" {
			body["style"] = req.Style
		}
		if req.ResponseFormat != "" {
			body["response_format"] = req.ResponseFormat
		}
		doReq = remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/images/generations",
			Body:   body,
		}
	}

	resp, err := a.client.Do(ctx, doReq)
	if err != nil {
		return adapters.ImageResponse{}, err
	}
	defer resp.Body.Close()

	// Response is JSON matching the OpenAI shape.
	var result adapters.ImageResponse
	if err := resp.DecodeJSON(&result); err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("remote: decode image response: %w", err)
	}

	return result, nil
}

// --- wire shapes ---------------------------------------------------------

// multimodalCaps represents the capabilities object returned by the
// multimodal adapter at /v1/capabilities.
type multimodalCaps struct {
	SupportsTTS            bool     `json:"supports_tts"`
	SupportsSTT            bool     `json:"supports_stt"`
	SupportsImageGeneration bool   `json:"supports_image_generation"`
	SupportsImageEdit       bool     `json:"supports_image_edit"`
	SupportsImageVariation  bool     `json:"supports_image_variation"`
	ModelPrefixes           []string `json:"model_prefixes"`
	SupportsStreamingTTS    bool     `json:"supports_streaming_tts"`
	DefaultVoice            string   `json:"default_voice"`
	MaxInputChars           int      `json:"max_input_chars"`
	AudioFormats            []string `json:"audio_formats"`
}
