// SPDX-License-Identifier: Apache-2.0

// Package elevenlabs is the first-party multimodal adapter for the
// ElevenLabs TTS API.
//
// Wire shape: POST https://api.elevenlabs.io/v1/text-to-speech/{voice_id}
// with header xi-api-key=<API_KEY> and JSON body containing the text +
// model + voice settings.
//
// Models we recognise from the gateway model id:
//   - elevenlabs/multilingual           → eleven_multilingual_v2 (high-quality)
//   - elevenlabs/multilingual-v1        → eleven_multilingual_v1
//   - elevenlabs/turbo                  → eleven_turbo_v2_5 (low-latency)
//   - elevenlabs/turbo-v2               → eleven_turbo_v2
//   - elevenlabs/monolingual            → eleven_monolingual_v1 (en-only)
//   - elevenlabs/flash                  → eleven_flash_v2_5 (fastest)
//   - elevenlabs/<custom-model-id>      → passes through unchanged
//
// Voices: the request's Voice field is used verbatim. Either an
// ElevenLabs voice id (UUID) or a built-in alias ("Rachel", "Adam",
// "Bella", "Antoni") works.
package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

// EnvKey is the env var the adapter looks up at construction.
const EnvKey = "ELEVENLABS_API_KEY"

// DefaultBaseURL is the public ElevenLabs API root. Override via
// AF_STACK_ELEVENLABS_URL for staging/self-hosted gateways.
const DefaultBaseURL = "https://api.elevenlabs.io"

// Default voice aliases ElevenLabs ships with. Used when the caller
// passes a name rather than a UUID.
var voiceAliases = map[string]string{
	// English (US) — built-in voices, IDs from ElevenLabs catalogue.
	"rachel":   "21m00Tcm4TlvDq8ikWAM",
	"adam":     "pNInz6obpgDQGcFmaJgB",
	"bella":    "EXAVITQu4vr4xnSDxMaL",
	"antoni":   "ErXwobaYiN019PkySvjV",
	"domi":     "AZnzlk1XvdvUeBnXmlld",
	"elli":     "MF3mGyEYCl7XYWbV9V6O",
	"josh":     "TxGEqnHWrfWFTfGW9XjX",
	"arnold":   "VR6AewLTigWG4xSOukaG",
	"sam":      "yoZ06aMxZJJ28mfd3POQ",
	"thomas":   "GBv7mTt0atIp3Br8iCZE",
	"charlie":  "IKne3meq5aSn9XLyUdCD",
	"clyde":    "2EiwWnXFnvU5JabPnv8n",
	"emily":    "LcfcDJNUP1GQjkzn1xUU",
	"freya":    "jsCqWAovK2LkecY7zXl4",
	"glinda":   "z9fAnlkpzviPz146aGWa",
	"grace":    "oWAxZDx7w5VEj9dCyTzz",
	"jeremy":   "bVMeCyTHy58xNoL34h3p",
	"matilda":  "XrExE9yKIg1WjnnlVkGX",
	"michael":  "flq6f7yk4E4fJM5XTYuZ",
	"nicole":   "piTKgcLEGmPE4e6mEKli",
	"patrick":  "ODq5zmih8GrVes37Dizd",
	"serena":   "pMsXgVXv3BLzUgSXRplE",
	"daniel":   "onwK4e9ZLuTAKqWW03F9",
	"alice":    "Xb7hH8MSUJpSbSDYk0k2",
	"chris":    "iP95p4xoKVk53GoZ742B",
}

// Adapter is the ElevenLabs TTS adapter.
type Adapter struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Config configures the adapter at construction.
type Config struct {
	APIKey  string
	BaseURL string
	Timeout time.Duration
}

// New constructs the adapter. Returns nil when cfg.APIKey is empty so
// callers can append unconditionally based on env presence.
func New(cfg Config) *Adapter {
	if cfg.APIKey == "" {
		return nil
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &Adapter{
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(base, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

// NewFromEnv constructs the adapter from environment variables. Returns
// nil when ELEVENLABS_API_KEY is unset.
func NewFromEnv() *Adapter {
	return New(Config{
		APIKey:  os.Getenv(EnvKey),
		BaseURL: os.Getenv("AF_STACK_ELEVENLABS_URL"),
	})
}

// Name returns the adapter id.
func (a *Adapter) Name() string { return "elevenlabs" }

// HandlesModel returns true for any "elevenlabs/*" model id.
func (a *Adapter) HandlesModel(model string) bool {
	return adapters.HasPrefix(model, "elevenlabs")
}

// SupportsTTS is true.
func (a *Adapter) SupportsTTS() bool { return true }

// SupportsSTT is false — ElevenLabs ships STT separately; not yet
// wired into this adapter.
func (a *Adapter) SupportsSTT() bool { return false }

// SupportsImage is false.
func (a *Adapter) SupportsImage() bool { return false }

// Transcribe is unsupported.
func (a *Adapter) Transcribe(_ context.Context, _ adapters.TranscribeRequest) (adapters.TranscribeResponse, error) {
	return adapters.TranscribeResponse{}, adapters.ErrUnsupported
}

// Image is unsupported.
func (a *Adapter) Image(_ context.Context, _ adapters.ImageRequest) (adapters.ImageResponse, error) {
	return adapters.ImageResponse{}, adapters.ErrUnsupported
}

// Speech generates audio via ElevenLabs.
func (a *Adapter) Speech(ctx context.Context, req adapters.SpeechRequest) (adapters.SpeechResponse, error) {
	if a == nil {
		return adapters.SpeechResponse{}, adapters.ErrUnsupported
	}
	if req.Input == "" {
		return adapters.SpeechResponse{}, fmt.Errorf("elevenlabs: input is required")
	}
	voice := resolveVoice(req.Voice)
	if voice == "" {
		return adapters.SpeechResponse{}, fmt.Errorf("elevenlabs: voice is required (pass voice=<id-or-name>)")
	}
	upstream := resolveModelID(req.Model)
	respFormat := req.ResponseFormat
	if respFormat == "" {
		respFormat = "mp3_44100_128"
	}
	body := map[string]any{
		"text":     req.Input,
		"model_id": upstream,
	}
	// Speed/voice settings are optional. ElevenLabs accepts a nested
	// "voice_settings" object — we forward only when the caller set it.
	if req.Speed > 0 {
		body["voice_settings"] = map[string]any{"speed": req.Speed}
	}
	payload, _ := json.Marshal(body)

	q := url.Values{}
	q.Set("output_format", respFormat)
	endpoint := fmt.Sprintf("%s/v1/text-to-speech/%s?%s", a.baseURL, voice, q.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return adapters.SpeechResponse{}, fmt.Errorf("elevenlabs: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "audio/mpeg")
	httpReq.Header.Set("xi-api-key", a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return adapters.SpeechResponse{}, fmt.Errorf("elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		return adapters.SpeechResponse{}, fmt.Errorf("elevenlabs: upstream %d: %s", resp.StatusCode, snippet(bodyBytes))
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}
	return adapters.SpeechResponse{
		Body:        bodyBytes,
		ContentType: ct,
		CharCount:   len(req.Input),
	}, nil
}

// resolveModelID maps gateway-side aliases to ElevenLabs' model ids.
func resolveModelID(model string) string {
	// Strip the "elevenlabs/" prefix to get the local id.
	suffix := strings.TrimPrefix(model, "elevenlabs/")
	switch strings.ToLower(suffix) {
	case "", "multilingual", "multilingual-v2":
		return "eleven_multilingual_v2"
	case "multilingual-v1":
		return "eleven_multilingual_v1"
	case "turbo", "turbo-v2-5":
		return "eleven_turbo_v2_5"
	case "turbo-v2":
		return "eleven_turbo_v2"
	case "monolingual", "monolingual-v1":
		return "eleven_monolingual_v1"
	case "flash", "flash-v2-5":
		return "eleven_flash_v2_5"
	default:
		return suffix
	}
}

// resolveVoice maps a friendly alias to an ElevenLabs voice id. UUIDs
// (24-char alphanumeric) pass through unchanged.
func resolveVoice(voice string) string {
	if voice == "" {
		return ""
	}
	if id, ok := voiceAliases[strings.ToLower(voice)]; ok {
		return id
	}
	return voice
}

// snippet returns the first ~256 bytes of body as a UTF-8 string,
// trimmed for inclusion in error messages.
func snippet(body []byte) string {
	const max = 256
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "…"
}
