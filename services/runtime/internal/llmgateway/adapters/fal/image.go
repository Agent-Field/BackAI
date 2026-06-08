// SPDX-License-Identifier: Apache-2.0

// Package fal is the first-party multimodal adapter for fal.ai's
// hosted image-generation models.
//
// Wire shape: POST https://fal.run/<model> with
//
//	Authorization: Key <FAL_API_KEY>
//
// and a JSON body of provider-specific parameters. fal.ai responds with
// JSON containing an `images[]` array of `{url, content_type, width, height}`
// objects (or `image: {url}` for some models).
//
// Gateway model ids we recognise:
//   - fal/<model-slug>   → routes to fal.run/<model-slug>
//
// Example: `fal/fast-sdxl` → POST https://fal.run/fal-ai/fast-sdxl.
// We auto-prefix `fal-ai/` when the slug doesn't include a publisher.
package fal

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
const EnvKey = "FAL_API_KEY"

// DefaultBaseURL is the public fal.ai run URL.
const DefaultBaseURL = "https://fal.run"

// Adapter is the fal.ai image-generation adapter.
type Adapter struct {
	apiKey  string
	baseURL string
	prefix  string
	client  *http.Client
}

// Config configures the adapter.
type Config struct {
	APIKey  string
	BaseURL string
	// ModelPrefix is the gateway-side model prefix this adapter handles.
	// Defaults to "fal". A second instance with prefix "flux" + base
	// URL fal.run can serve Flux models too.
	ModelPrefix string
	Timeout     time.Duration
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
	prefix := cfg.ModelPrefix
	if prefix == "" {
		prefix = "fal"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &Adapter{
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(base, "/"),
		prefix:  prefix,
		client:  &http.Client{Timeout: timeout},
	}
}

// NewFromEnv constructs from env. Returns nil when FAL_API_KEY unset.
func NewFromEnv() *Adapter {
	return New(Config{
		APIKey:  os.Getenv(EnvKey),
		BaseURL: os.Getenv("AF_STACK_FAL_URL"),
	})
}

// Name returns the adapter id.
func (a *Adapter) Name() string {
	if a == nil {
		return "fal"
	}
	return a.prefix
}

// HandlesModel returns true for "fal/*" (or whatever ModelPrefix is set).
func (a *Adapter) HandlesModel(model string) bool {
	if a == nil {
		return false
	}
	return adapters.HasPrefix(model, a.prefix)
}

// SupportsTTS is false.
func (a *Adapter) SupportsTTS() bool { return false }

// SupportsSTT is false.
func (a *Adapter) SupportsSTT() bool { return false }

// SupportsImage is true.
func (a *Adapter) SupportsImage() bool { return true }

// Speech is unsupported.
func (a *Adapter) Speech(_ context.Context, _ adapters.SpeechRequest) (adapters.SpeechResponse, error) {
	return adapters.SpeechResponse{}, adapters.ErrUnsupported
}

// Transcribe is unsupported.
func (a *Adapter) Transcribe(_ context.Context, _ adapters.TranscribeRequest) (adapters.TranscribeResponse, error) {
	return adapters.TranscribeResponse{}, adapters.ErrUnsupported
}

// Image generates images via fal.ai /<model>.
func (a *Adapter) Image(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, error) {
	if a == nil {
		return adapters.ImageResponse{}, adapters.ErrUnsupported
	}
	if req.IsEdit || req.IsVariations {
		// fal.ai exposes per-model edit / inpainting endpoints; we
		// don't have a universal mapping, so return Unsupported and
		// let the gateway surface a 501.
		return adapters.ImageResponse{}, adapters.ErrUnsupported
	}
	if req.Prompt == "" {
		return adapters.ImageResponse{}, fmt.Errorf("fal: prompt is required")
	}
	slug := resolveModelSlug(req.Model, a.prefix)
	if slug == "" {
		return adapters.ImageResponse{}, fmt.Errorf("fal: model is required (e.g. fal/fast-sdxl)")
	}

	body := buildFalBody(req)
	payload, _ := json.Marshal(body)

	endpoint := a.baseURL + "/" + slug
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("fal: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Key "+a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("fal: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 400 {
		return adapters.ImageResponse{}, fmt.Errorf("fal: upstream %d: %s", resp.StatusCode, snippet(bodyBytes))
	}
	return decodeFalImageResponse(bodyBytes)
}

// resolveModelSlug strips the adapter prefix and auto-prepends "fal-ai/"
// when the user passes a bare model name. Returns the path component
// fal.run expects after the host.
//
// Examples (prefix=fal):
//   - "fal/fast-sdxl"            → "fal-ai/fast-sdxl"
//   - "fal/fal-ai/fast-sdxl"     → "fal-ai/fast-sdxl"
//   - "fal/black-forest-labs/flux-pro" → "black-forest-labs/flux-pro"
func resolveModelSlug(model, prefix string) string {
	suffix := strings.TrimPrefix(model, prefix+"/")
	if suffix == "" {
		return ""
	}
	if !strings.Contains(suffix, "/") {
		return "fal-ai/" + suffix
	}
	return suffix
}

// buildFalBody serializes the universal fields fal.ai models accept.
// Provider-specific extras (seed, num_inference_steps, guidance_scale)
// can be passed via the original request body when used as a tunnel.
func buildFalBody(req adapters.ImageRequest) map[string]any {
	out := map[string]any{
		"prompt": req.Prompt,
	}
	if req.N > 0 {
		out["num_images"] = req.N
	}
	if req.Size != "" {
		w, h := parseSize(req.Size)
		if w > 0 && h > 0 {
			out["image_size"] = map[string]any{
				"width":  w,
				"height": h,
			}
		}
	}
	// Tunnel any caller-supplied JSON body through so model-specific
	// fields (seed, scheduler, guidance_scale, etc.) reach upstream.
	if len(req.Body) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(req.Body, &extra); err == nil {
			for k, v := range extra {
				switch k {
				case "model", "prompt", "n", "size", "response_format":
					// Already mapped or not relevant.
					continue
				default:
					out[k] = v
				}
			}
		}
	}
	return out
}

// parseSize converts "1024x1024" → (1024, 1024). Returns (0,0) on parse
// failure so the adapter falls back to the model's default image size.
func parseSize(s string) (int, int) {
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	w, h := 0, 0
	fmt.Sscanf(parts[0], "%d", &w)
	fmt.Sscanf(parts[1], "%d", &h)
	return w, h
}

// decodeFalImageResponse converts a fal.ai response into our adapter
// shape. fal.ai uses several response shapes across models; we cover
// the two most common:
//
//   - {"images":[{"url":...,"content_type":...}, ...]}
//   - {"image":{"url":...}}
//
// And the asynchronous queue response (request_id) is not handled here —
// callers should use fal.run/<model> (synchronous) rather than
// queue.fal.run.
func decodeFalImageResponse(body []byte) (adapters.ImageResponse, error) {
	var raw struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Image struct {
			URL string `json:"url"`
		} `json:"image"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("fal: decode response: %w", err)
	}
	if raw.RequestID != "" && len(raw.Images) == 0 && raw.Image.URL == "" {
		return adapters.ImageResponse{}, fmt.Errorf("fal: received async queue response (request_id=%s); switch to synchronous endpoint or poll the queue", raw.RequestID)
	}
	out := adapters.ImageResponse{}
	if raw.Image.URL != "" {
		out.Data = append(out.Data, adapters.ImageItem{URL: raw.Image.URL})
	}
	for _, im := range raw.Images {
		if im.URL == "" {
			continue
		}
		out.Data = append(out.Data, adapters.ImageItem{URL: im.URL})
	}
	if len(out.Data) == 0 {
		return adapters.ImageResponse{}, fmt.Errorf("fal: response contained no images")
	}
	return out, nil
}

func snippet(body []byte) string {
	const max = 256
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "…"
}
