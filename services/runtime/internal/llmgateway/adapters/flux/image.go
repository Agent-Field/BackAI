// SPDX-License-Identifier: Apache-2.0

// Package flux is the first-party multimodal adapter for Black Forest
// Labs' Flux family. Flux is not hosted directly by BFL — it ships via
// fal.ai or Replicate, and this adapter delegates to whichever one the
// operator has configured.
//
// Routing:
//   - If FAL_API_KEY is set, use fal.ai (lowest latency, simple sync HTTP)
//   - Else if REPLICATE_API_KEY is set, use Replicate
//   - Else fail with a 503 hinting at the missing env var
//
// Gateway model ids:
//   - flux/schnell  → fal-ai/flux/schnell        (or black-forest-labs/flux-schnell on Replicate)
//   - flux/dev      → fal-ai/flux/dev            (or black-forest-labs/flux-dev)
//   - flux/pro      → fal-ai/flux-pro            (or black-forest-labs/flux-1.1-pro)
//   - flux/pro-ultra → fal-ai/flux-pro/v1.1-ultra
package flux

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

// FalKey / ReplicateKey are the env vars used at construction.
const (
	FalKey       = "FAL_API_KEY"
	ReplicateKey = "REPLICATE_API_KEY"
)

// Adapter is the Flux adapter. It picks a backend at construction time
// based on which env keys are set; runtime switching is not supported
// (operators reboot the runtime to change backends).
type Adapter struct {
	backend backendImpl
}

// Config configures the adapter explicitly. Construction order is
// FalAPIKey > ReplicateAPIKey. Tests can pass a custom backend instead.
type Config struct {
	FalAPIKey       string
	ReplicateAPIKey string
	HTTPTimeout     time.Duration
	// Backend, when set, overrides the env-driven backend selection.
	// Used by tests to inject a fake HTTP server.
	Backend BackendKind
	// BaseURLOverride, when set, replaces the default base URL for
	// whichever backend is selected. Used by tests.
	BaseURLOverride string
}

// BackendKind names the upstream provider.
type BackendKind string

const (
	BackendFal       BackendKind = "fal"
	BackendReplicate BackendKind = "replicate"
)

// New constructs the adapter. Returns nil when neither key is set.
func New(cfg Config) *Adapter {
	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	pickBackend := cfg.Backend
	if pickBackend == "" {
		switch {
		case cfg.FalAPIKey != "":
			pickBackend = BackendFal
		case cfg.ReplicateAPIKey != "":
			pickBackend = BackendReplicate
		default:
			return nil
		}
	}

	switch pickBackend {
	case BackendFal:
		if cfg.FalAPIKey == "" {
			return nil
		}
		base := cfg.BaseURLOverride
		if base == "" {
			base = "https://fal.run"
		}
		return &Adapter{backend: &falBackend{
			apiKey:  cfg.FalAPIKey,
			baseURL: strings.TrimRight(base, "/"),
			client:  client,
		}}
	case BackendReplicate:
		if cfg.ReplicateAPIKey == "" {
			return nil
		}
		base := cfg.BaseURLOverride
		if base == "" {
			base = "https://api.replicate.com"
		}
		return &Adapter{backend: &replicateBackend{
			apiKey:  cfg.ReplicateAPIKey,
			baseURL: strings.TrimRight(base, "/"),
			client:  client,
		}}
	default:
		return nil
	}
}

// NewFromEnv reads FAL_API_KEY / REPLICATE_API_KEY from env.
func NewFromEnv() *Adapter {
	return New(Config{
		FalAPIKey:       os.Getenv(FalKey),
		ReplicateAPIKey: os.Getenv(ReplicateKey),
	})
}

// Name returns the adapter id.
func (a *Adapter) Name() string {
	if a == nil || a.backend == nil {
		return "flux"
	}
	return "flux:" + a.backend.kind()
}

// HandlesModel returns true for any "flux/*" model id.
func (a *Adapter) HandlesModel(model string) bool {
	return adapters.HasPrefix(model, "flux")
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

// Image generates an image via the configured Flux backend.
func (a *Adapter) Image(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, error) {
	if a == nil || a.backend == nil {
		return adapters.ImageResponse{}, adapters.ErrUnsupported
	}
	if req.IsEdit || req.IsVariations {
		// Flux exposes inpainting on a different endpoint; not in scope
		// for v1.
		return adapters.ImageResponse{}, adapters.ErrUnsupported
	}
	if req.Prompt == "" {
		return adapters.ImageResponse{}, fmt.Errorf("flux: prompt is required")
	}
	return a.backend.generate(ctx, req)
}

// backendImpl is the small interface the two backends share.
type backendImpl interface {
	kind() string
	generate(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, error)
}

// ─── fal.ai backend ──────────────────────────────────────────────────────

type falBackend struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func (b *falBackend) kind() string { return "fal" }

func (b *falBackend) generate(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, error) {
	slug := falSlug(req.Model)
	endpoint := b.baseURL + "/" + slug
	payload := falPayload(req)
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("flux/fal: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Key "+b.apiKey)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("flux/fal: %w", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 400 {
		return adapters.ImageResponse{}, fmt.Errorf("flux/fal: upstream %d: %s", resp.StatusCode, snippet(respBytes))
	}
	return decodeFalResponse(respBytes)
}

// falSlug maps "flux/schnell" → "fal-ai/flux/schnell" (and variants).
func falSlug(model string) string {
	suffix := strings.TrimPrefix(model, "flux/")
	switch strings.ToLower(suffix) {
	case "schnell":
		return "fal-ai/flux/schnell"
	case "dev":
		return "fal-ai/flux/dev"
	case "pro":
		return "fal-ai/flux-pro"
	case "pro-ultra", "pro/ultra":
		return "fal-ai/flux-pro/v1.1-ultra"
	case "pro-1-1", "pro1.1", "pro-v1-1":
		return "fal-ai/flux-pro/v1.1"
	default:
		// Allow callers to specify a raw fal model slug after the prefix.
		if strings.HasPrefix(suffix, "fal-ai/") {
			return suffix
		}
		return "fal-ai/" + suffix
	}
}

func falPayload(req adapters.ImageRequest) map[string]any {
	out := map[string]any{
		"prompt": req.Prompt,
	}
	if req.N > 0 {
		out["num_images"] = req.N
	}
	if req.Size != "" {
		w, h := parseSize(req.Size)
		if w > 0 && h > 0 {
			out["image_size"] = map[string]any{"width": w, "height": h}
		}
	}
	if len(req.Body) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(req.Body, &extra); err == nil {
			for k, v := range extra {
				switch k {
				case "model", "prompt", "n", "size", "response_format":
					continue
				default:
					out[k] = v
				}
			}
		}
	}
	return out
}

func decodeFalResponse(body []byte) (adapters.ImageResponse, error) {
	var raw struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("flux/fal: decode response: %w", err)
	}
	out := adapters.ImageResponse{}
	for _, im := range raw.Images {
		if im.URL != "" {
			out.Data = append(out.Data, adapters.ImageItem{URL: im.URL})
		}
	}
	if len(out.Data) == 0 {
		return adapters.ImageResponse{}, fmt.Errorf("flux/fal: response contained no images")
	}
	return out, nil
}

// ─── Replicate backend ───────────────────────────────────────────────────

type replicateBackend struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func (b *replicateBackend) kind() string { return "replicate" }

// generate uses Replicate's `Prefer: wait` synchronous mode so callers
// don't have to poll for results.
func (b *replicateBackend) generate(ctx context.Context, req adapters.ImageRequest) (adapters.ImageResponse, error) {
	owner, name := replicateModelPath(req.Model)
	endpoint := fmt.Sprintf("%s/v1/models/%s/%s/predictions", b.baseURL, owner, name)
	input := falPayload(req)
	payload, _ := json.Marshal(map[string]any{"input": input})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("flux/replicate: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	httpReq.Header.Set("Prefer", "wait")

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("flux/replicate: %w", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 400 {
		return adapters.ImageResponse{}, fmt.Errorf("flux/replicate: upstream %d: %s", resp.StatusCode, snippet(respBytes))
	}
	return decodeReplicateResponse(respBytes)
}

// replicateModelPath maps "flux/schnell" → ("black-forest-labs", "flux-schnell").
func replicateModelPath(model string) (owner, name string) {
	suffix := strings.TrimPrefix(model, "flux/")
	switch strings.ToLower(suffix) {
	case "schnell":
		return "black-forest-labs", "flux-schnell"
	case "dev":
		return "black-forest-labs", "flux-dev"
	case "pro":
		return "black-forest-labs", "flux-1.1-pro"
	case "pro-ultra":
		return "black-forest-labs", "flux-1.1-pro-ultra"
	default:
		// Allow callers to pass a full "owner/name" after "flux/".
		if parts := strings.SplitN(suffix, "/", 2); len(parts) == 2 {
			return parts[0], parts[1]
		}
		return "black-forest-labs", suffix
	}
}

func decodeReplicateResponse(body []byte) (adapters.ImageResponse, error) {
	var raw struct {
		Output any    `json:"output"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return adapters.ImageResponse{}, fmt.Errorf("flux/replicate: decode response: %w", err)
	}
	out := adapters.ImageResponse{}
	switch v := raw.Output.(type) {
	case string:
		if v != "" {
			out.Data = append(out.Data, adapters.ImageItem{URL: v})
		}
	case []any:
		for _, item := range v {
			if u, ok := item.(string); ok && u != "" {
				out.Data = append(out.Data, adapters.ImageItem{URL: u})
			}
		}
	}
	if len(out.Data) == 0 {
		if raw.Status != "" && raw.Status != "succeeded" {
			return adapters.ImageResponse{}, fmt.Errorf("flux/replicate: prediction status=%q (Prefer: wait timed out)", raw.Status)
		}
		return adapters.ImageResponse{}, fmt.Errorf("flux/replicate: response contained no images")
	}
	return out, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────

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

func snippet(body []byte) string {
	const max = 256
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "…"
}
