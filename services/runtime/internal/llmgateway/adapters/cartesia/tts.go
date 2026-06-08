// SPDX-License-Identifier: Apache-2.0

// Package cartesia is the first-party multimodal adapter for the
// Cartesia Sonic TTS API.
//
// Wire shape: POST https://api.cartesia.ai/tts/bytes with headers
//
//	X-API-Key: <CARTESIA_API_KEY>
//	Cartesia-Version: 2024-06-10
//
// and a JSON body containing model_id, transcript, voice, and
// output_format. Response is raw audio bytes (default WAV PCM).
//
// Gateway model ids we recognise:
//   - cartesia/sonic           → sonic-english
//   - cartesia/sonic-english   → sonic-english
//   - cartesia/sonic-multilingual → sonic-multilingual
//   - cartesia/<custom>        → passes through unchanged
package cartesia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

// EnvKey is the env var the adapter looks up at construction.
const EnvKey = "CARTESIA_API_KEY"

// DefaultBaseURL is the public Cartesia API root.
const DefaultBaseURL = "https://api.cartesia.ai"

// DefaultAPIVersion is the Cartesia-Version header value. Refresh as
// upstream rolls breaking changes — newer versions are backward compat
// for at least 6 months per Cartesia docs.
const DefaultAPIVersion = "2024-06-10"

// Adapter is the Cartesia adapter.
type Adapter struct {
	apiKey     string
	baseURL    string
	apiVersion string
	client     *http.Client
}

// Config configures the adapter.
type Config struct {
	APIKey     string
	BaseURL    string
	APIVersion string
	Timeout    time.Duration
}

// New constructs the adapter. Returns nil when cfg.APIKey is empty.
func New(cfg Config) *Adapter {
	if cfg.APIKey == "" {
		return nil
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	ver := cfg.APIVersion
	if ver == "" {
		ver = DefaultAPIVersion
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &Adapter{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimRight(base, "/"),
		apiVersion: ver,
		client:     &http.Client{Timeout: timeout},
	}
}

// NewFromEnv constructs from env. Returns nil when CARTESIA_API_KEY unset.
func NewFromEnv() *Adapter {
	return New(Config{
		APIKey:     os.Getenv(EnvKey),
		BaseURL:    os.Getenv("AF_STACK_CARTESIA_URL"),
		APIVersion: os.Getenv("AF_STACK_CARTESIA_VERSION"),
	})
}

// Name returns the adapter id.
func (a *Adapter) Name() string { return "cartesia" }

// HandlesModel returns true for any "cartesia/*" model id.
func (a *Adapter) HandlesModel(model string) bool {
	return adapters.HasPrefix(model, "cartesia")
}

// SupportsTTS is true.
func (a *Adapter) SupportsTTS() bool { return true }

// SupportsSTT is false.
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

// Speech generates audio via Cartesia /tts/bytes.
func (a *Adapter) Speech(ctx context.Context, req adapters.SpeechRequest) (adapters.SpeechResponse, error) {
	if a == nil {
		return adapters.SpeechResponse{}, adapters.ErrUnsupported
	}
	if req.Input == "" {
		return adapters.SpeechResponse{}, fmt.Errorf("cartesia: input is required")
	}
	if req.Voice == "" {
		return adapters.SpeechResponse{}, fmt.Errorf("cartesia: voice is required (Cartesia voice id)")
	}
	modelID := resolveModelID(req.Model)
	outFmt := mapResponseFormat(req.ResponseFormat)
	body := map[string]any{
		"model_id":   modelID,
		"transcript": req.Input,
		"voice": map[string]any{
			"mode": "id",
			"id":   req.Voice,
		},
		"output_format": outFmt,
		"language":      "en",
	}
	if req.Speed > 0 {
		body["voice"].(map[string]any)["__experimental_controls"] = map[string]any{
			"speed": req.Speed,
		}
	}
	payload, _ := json.Marshal(body)

	endpoint := a.baseURL + "/tts/bytes"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return adapters.SpeechResponse{}, fmt.Errorf("cartesia: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("X-API-Key", a.apiKey)
	httpReq.Header.Set("Cartesia-Version", a.apiVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return adapters.SpeechResponse{}, fmt.Errorf("cartesia: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		return adapters.SpeechResponse{}, fmt.Errorf("cartesia: upstream %d: %s", resp.StatusCode, snippet(bodyBytes))
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/wav"
	}
	return adapters.SpeechResponse{
		Body:        bodyBytes,
		ContentType: ct,
		CharCount:   len(req.Input),
	}, nil
}

// resolveModelID maps short aliases to Cartesia model ids.
func resolveModelID(model string) string {
	suffix := strings.TrimPrefix(model, "cartesia/")
	switch strings.ToLower(suffix) {
	case "", "sonic", "sonic-english":
		return "sonic-english"
	case "sonic-multilingual":
		return "sonic-multilingual"
	default:
		return suffix
	}
}

// mapResponseFormat converts the OpenAI-style response_format strings
// to Cartesia's output_format object. WAV PCM 16-bit at 44.1kHz is the
// default — closest to what OpenAI returns for tts-1.
func mapResponseFormat(format string) map[string]any {
	switch strings.ToLower(format) {
	case "mp3":
		return map[string]any{
			"container":   "mp3",
			"bit_rate":    128000,
			"sample_rate": 44100,
		}
	case "wav", "":
		return map[string]any{
			"container":   "wav",
			"encoding":    "pcm_s16le",
			"sample_rate": 44100,
		}
	case "pcm":
		return map[string]any{
			"container":   "raw",
			"encoding":    "pcm_s16le",
			"sample_rate": 44100,
		}
	default:
		return map[string]any{
			"container":   "wav",
			"encoding":    "pcm_s16le",
			"sample_rate": 44100,
		}
	}
}

func snippet(body []byte) string {
	const max = 256
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "…"
}
