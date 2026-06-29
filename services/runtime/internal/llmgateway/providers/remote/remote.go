// SPDX-License-Identifier: Apache-2.0

// Package remote implements llmgateway.Provider by talking the
// llm-chat-v1 HTTP protocol to a sidecar (Helicone, Portkey, direct
// OpenAI, vLLM, anything OpenAI-compatible). Selected by setting
// AF_STACK_LLM_GATEWAY_ADAPTER=remote in the runtime.
//
// One Adapter per AF_STACK_LLM_GATEWAY_ADAPTER_URL. Goroutine-safe;
// the underlying remote.Client owns the connection pool.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
)

// Adapter is an llmgateway.Provider backed by a remote sidecar that
// speaks the OpenAI-compatible llm-chat-v1 protocol.
type Adapter struct {
	client *remote.Client
	name   string
	caps   llmgateway.ProviderCapabilities
}

// Compile-time check: Adapter satisfies the gateway's Provider interface.
var _ llmgateway.Provider = (*Adapter)(nil)

// Config is the env-driven configuration.
type Config struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	MaxRetries int
}

// New constructs an Adapter and verifies the sidecar speaks the
// protocol via a sync GET /v1/capabilities at boot.
func New(ctx context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("llmgateway/remote: BaseURL required")
	}
	if cfg.Timeout <= 0 {
		// Chat completions can run long with reasoning models; allow
		// 120s by default. Streaming requests have no per-attempt
		// timeout (see remote.Client behaviour).
		cfg.Timeout = 120 * time.Second
	}
	c, err := remote.NewClient(remote.Config{
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("llmgateway/remote: client: %w", err)
	}
	a := &Adapter{client: c}
	if err := a.refreshCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("llmgateway/remote: capability probe: %w", err)
	}
	return a, nil
}

func (a *Adapter) refreshCapabilities(ctx context.Context) error {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	if resp.Slot != "llm-chat" {
		return fmt.Errorf("llmgateway/remote: adapter reports slot=%q; expected llm-chat", resp.Slot)
	}
	a.name = resp.Name
	if len(resp.Capabilities) > 0 {
		if err := json.Unmarshal(resp.Capabilities, &a.caps); err != nil {
			return fmt.Errorf("llmgateway/remote: decode capabilities: %w", err)
		}
	}
	return nil
}

// Name returns the active adapter's identifier from
// /v1/capabilities.name.
func (a *Adapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

// Capabilities returns the typed capability view (cached).
func (a *Adapter) ProviderCaps() llmgateway.ProviderCapabilities {
	if a == nil {
		return llmgateway.ProviderCapabilities{}
	}
	return a.caps
}

// Probe satisfies registry.Probe via /healthz.
func (a *Adapter) Probe(ctx context.Context) (string, error) {
	h, err := a.client.Health(ctx)
	if err != nil {
		return "unhealthy", err
	}
	return h.Status, nil
}

// Chat performs a non-streaming chat completion.
func (a *Adapter) Chat(ctx context.Context, req llmgateway.ChatRequest) (llmgateway.ChatResponse, error) {
	req.Stream = false
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodPost,
		Path:     "/v1/chat/completions",
		Body:     req,
		TenantID: req.User,
	})
	if err != nil {
		return llmgateway.ChatResponse{}, mapChatError(err)
	}
	var out llmgateway.ChatResponse
	if err := resp.DecodeJSON(&out); err != nil {
		return llmgateway.ChatResponse{}, fmt.Errorf("llmgateway/remote: decode chat: %w", err)
	}
	return out, nil
}

// ChatStream performs a streaming chat completion. Two channels are
// returned: chunks (deltas) and err (exactly one terminal value).
// Both close when the stream ends.
//
// The implementation reads the SSE body manually because OpenAI's
// streaming format uses a `data: [DONE]` sentinel that the generic
// SSE reader in remote.Client treats as a payload (and would try to
// JSON-decode). We special-case it here.
func (a *Adapter) ChatStream(ctx context.Context, req llmgateway.ChatRequest) (<-chan llmgateway.ChatStreamChunk, <-chan error) {
	req.Stream = true
	chunks := make(chan llmgateway.ChatStreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		resp, err := a.client.Do(ctx, remote.Request{
			Method:   http.MethodPost,
			Path:     "/v1/chat/completions",
			Body:     req,
			TenantID: req.User,
			Stream:   true,
		})
		if err != nil {
			errs <- mapChatError(err)
			return
		}
		defer resp.Body.Close()

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for sc.Scan() {
			line := sc.Text()
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				errs <- nil
				return
			}
			var chunk llmgateway.ChatStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				// Malformed SSE chunk — surface and stop.
				errs <- fmt.Errorf("llmgateway/remote: decode chunk: %w", err)
				return
			}
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case chunks <- chunk:
			}
		}
		if err := sc.Err(); err != nil {
			errs <- fmt.Errorf("llmgateway/remote: stream read: %w", err)
			return
		}
		// Stream closed without [DONE] sentinel.
		errs <- errors.New("llmgateway/remote: stream ended without [DONE]")
	}()

	return chunks, errs
}

// Embeddings calls POST /v1/embeddings.
func (a *Adapter) Embeddings(ctx context.Context, req llmgateway.EmbeddingsRequest) (llmgateway.EmbeddingsResponse, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodPost,
		Path:     "/v1/embeddings",
		Body:     req,
		TenantID: req.User,
	})
	if err != nil {
		return llmgateway.EmbeddingsResponse{}, mapChatError(err)
	}
	var out llmgateway.EmbeddingsResponse
	if err := resp.DecodeJSON(&out); err != nil {
		return llmgateway.EmbeddingsResponse{}, fmt.Errorf("llmgateway/remote: decode embeddings: %w", err)
	}
	return out, nil
}

// ListModels calls GET /v1/models?verb=<verb>. Empty verb returns all.
// This satisfies the ProviderListMore interface — the dashboard's
// model catalog uses it.
func (a *Adapter) ListModels(ctx context.Context, verb string) (llmgateway.ProviderCatalog, error) {
	q := make(map[string][]string)
	if verb != "" {
		q["verb"] = []string{verb}
	}
	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodGet,
		Path:   "/v1/models",
		Query:  q,
	})
	if err != nil {
		return llmgateway.ProviderCatalog{}, err
	}
	var out llmgateway.ProviderCatalog
	if err := resp.DecodeJSON(&out); err != nil {
		return llmgateway.ProviderCatalog{}, fmt.Errorf("llmgateway/remote: decode models: %w", err)
	}
	return out, nil
}

// Capabilities returns the typed capability view. Part of
// ProviderListMore.
func (a *Adapter) Capabilities(ctx context.Context) (llmgateway.ProviderCapabilities, error) {
	if err := a.refreshCapabilities(ctx); err != nil {
		return llmgateway.ProviderCapabilities{}, err
	}
	return a.caps, nil
}

// mapChatError translates an RFC 7807 problem into either a
// pass-through or a typed gateway error. The runtime's higher-level
// chat handler uses these to set 4xx/5xx response status codes
// correctly for the client.
func mapChatError(err error) error {
	if err == nil {
		return nil
	}
	p, ok := remote.AsProblem(err)
	if !ok {
		return err
	}
	// Surface the underlying problem unchanged — the runtime's
	// existing error-to-HTTP machinery in internal/server/llm.go
	// inspects status codes already. We keep this minimal so we
	// don't double-translate.
	return p
}

// ResponseCostFromHeaders reads X-Backai-Response-Cost-Usd from a
// response. Useful for callers that want the adapter's
// authoritative cost figure (versus the runtime's estimate from
// pricing.Catalog). Exposed for the gateway's cost recorder.
func ResponseCostFromHeaders(h http.Header) (cost float64, model string, ok bool) {
	v := h.Get("X-Backai-Response-Cost-Usd")
	if v == "" {
		return 0, "", false
	}
	c, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, "", false
	}
	return c, h.Get("X-Backai-Model-Used"), true
}

// RawChatCompletions exposes a low-level escape hatch for callers
// that need to pass an OpenAI-shaped body straight through without
// the typed ChatRequest re-marshal. Returns the raw response bytes
// and Content-Type so the gateway can forward it verbatim to the
// client when the adapter declares verbatim-pass-through. Optional;
// the main interface methods cover the typed path.
func (a *Adapter) RawChatCompletions(ctx context.Context, body []byte, headers http.Header) ([]byte, string, http.Header, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method:       http.MethodPost,
		Path:         "/v1/chat/completions",
		BodyReader:   bytes.NewReader(body),
		ContentType:  "application/json",
		ExtraHeaders: headers,
	})
	if err != nil {
		return nil, "", nil, mapChatError(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", nil, fmt.Errorf("llmgateway/remote: read raw body: %w", err)
	}
	return buf, resp.Header.Get("Content-Type"), resp.Header.Clone(), nil
}
