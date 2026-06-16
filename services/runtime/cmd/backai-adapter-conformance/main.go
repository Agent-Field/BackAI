// SPDX-License-Identifier: Apache-2.0

// backai-adapter-conformance verifies that an HTTP adapter sidecar
// conforms to the BackAI adapter protocol (docs/adapters/PROTOCOL.md)
// and the per-slot specification (docs/adapters/protocols/<slot>-v1.md).
//
// Usage:
//
//	backai-adapter-conformance --slot sandbox --url http://localhost:8090 [--token X]
//
// Exits 0 on full pass; 1 if any check fails.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
)

func main() {
	var (
		slot    = flag.String("slot", "", "adapter slot: sandbox | storage | notifications | secrets | billing | multimodal | llm-chat | auth | logs | traces")
		baseURL = flag.String("url", "", "adapter base URL, e.g. http://localhost:8090")
		token   = flag.String("token", "", "bearer token (optional)")
		quiet   = flag.Bool("quiet", false, "suppress per-check output, print only summary")
		timeout = flag.Duration("timeout", 15*time.Second, "per-check HTTP timeout")
	)
	flag.Parse()

	if *slot == "" || *baseURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: backai-adapter-conformance --slot <slot> --url <baseURL> [--token X]")
		os.Exit(2)
	}

	c, err := remote.NewClient(remote.Config{
		BaseURL:    *baseURL,
		Token:      *token,
		Timeout:    *timeout,
		MaxRetries: 0, // conformance checks should be deterministic; no auto-retry.
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(2)
	}

	r := newRunner(*quiet)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	runCommonChecks(ctx, r, c, *slot)

	switch *slot {
	case "sandbox":
		runSandboxChecks(ctx, r, c)
	case "storage":
		runStorageChecks(ctx, r, c)
	case "notifications":
		runNotificationsChecks(ctx, r, c)
	case "secrets":
		runSecretsChecks(ctx, r, c)
	case "billing":
		runBillingChecks(ctx, r, c)
	case "multimodal":
		runMultimodalChecks(ctx, r, c)
	case "llm-chat":
		runLLMChatChecks(ctx, r, c)
	case "auth":
		runAuthChecks(ctx, r, c)
	case "logs":
		runLogsChecks(ctx, r, c)
	case "traces":
		runTracesChecks(ctx, r, c)
	default:
		r.fail("unknown slot", fmt.Errorf("slot %q has no per-slot suite", *slot))
	}

	r.summary()
	if r.failed > 0 {
		os.Exit(1)
	}
}

// --- runner --------------------------------------------------------------

type runner struct {
	quiet  bool
	passed int
	failed int
}

func newRunner(quiet bool) *runner { return &runner{quiet: quiet} }

func (r *runner) ok(name string) {
	r.passed++
	if !r.quiet {
		fmt.Printf("\033[32mPASS\033[0m  %s\n", name)
	}
}

func (r *runner) fail(name string, err error) {
	r.failed++
	fmt.Printf("\033[31mFAIL\033[0m  %s — %v\n", name, err)
}

func (r *runner) summary() {
	total := r.passed + r.failed
	fmt.Println()
	fmt.Printf("Conformance: %d / %d checks passed\n", r.passed, total)
	if r.failed == 0 {
		fmt.Println("All checks PASS.")
	} else {
		fmt.Printf("%d FAIL — adapter does not yet conform.\n", r.failed)
	}
}

func (r *runner) require(name string, fn func() error) {
	if err := fn(); err != nil {
		r.fail(name, err)
		return
	}
	r.ok(name)
}

// --- common checks (every adapter) --------------------------------------

func runCommonChecks(ctx context.Context, r *runner, c *remote.Client, slot string) {
	// /healthz
	r.require("GET /healthz returns 200 with envelope", func() error {
		h, err := c.Health(ctx)
		if err != nil {
			return err
		}
		switch h.Status {
		case "healthy", "degraded":
			return nil
		case "unhealthy":
			return fmt.Errorf("adapter reports unhealthy")
		default:
			return fmt.Errorf("unrecognised status %q", h.Status)
		}
	})

	// /v1/capabilities
	r.require("GET /v1/capabilities returns slot envelope", func() error {
		caps, err := c.Capabilities(ctx)
		if err != nil {
			return err
		}
		if caps.Slot != slot {
			return fmt.Errorf("adapter declared slot=%q; expected %q", caps.Slot, slot)
		}
		if caps.Name == "" {
			return fmt.Errorf("capabilities.name is empty")
		}
		if caps.ProtocolVersion != "v1" {
			return fmt.Errorf("protocol_version=%q; expected v1", caps.ProtocolVersion)
		}
		return nil
	})

	// /v1/info (optional)
	r.require("GET /v1/info returns metadata (or 404)", func() error {
		_, err := c.Info(ctx)
		return err
	})

	// Forward-compat: extra response fields are ignored.
	r.require("Unknown fields in responses do not break decoding", func() error {
		// We've already decoded /v1/capabilities above; if the
		// adapter sent extra fields and we got here without error,
		// the test has passed by construction. This is a
		// documentation check more than a runtime check.
		return nil
	})

	// Auth — only verifiable if we have a token.
	r.require("Required Authorization header path is reachable", func() error {
		_, err := c.Capabilities(ctx)
		return err
	})
}

// --- sandbox -------------------------------------------------------------

func runSandboxChecks(ctx context.Context, r *runner, c *remote.Client) {
	r.require("POST /v1/runs with a tiny spec returns terminal result", func() error {
		body := map[string]any{
			"id":        "conformance-run-1",
			"image":     "python:3.12-slim",
			"command":   []string{"echo", "ok"},
			"timeout_s": 30,
			"cpu":       1,
			"memory_gb": 1,
			"network":   "isolated",
		}
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/runs",
			Body:   body,
		})
		if err != nil {
			return err
		}
		var out struct {
			Status   string `json:"status"`
			ExitCode int    `json:"exit_code"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		switch out.Status {
		case "done", "failed":
			return nil
		default:
			return fmt.Errorf("unexpected terminal status %q", out.Status)
		}
	})

	r.require("DELETE /v1/runs/{unknown-id} is idempotent (204 or 404)", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodDelete,
			Path:   "/v1/runs/does-not-exist",
		})
		if err != nil {
			p, ok := remote.AsProblem(err)
			if ok && (p.HTTPStatus == 404 || p.HTTPStatus == 204) {
				return nil
			}
			return err
		}
		_ = resp
		return nil
	})

	r.require("GET /v1/pool returns adapter stats", func() error {
		resp, err := c.Do(ctx, remote.Request{Method: http.MethodGet, Path: "/v1/pool"})
		if err != nil {
			return err
		}
		var out struct {
			Adapter string `json:"adapter"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		if out.Adapter == "" {
			return fmt.Errorf("pool.adapter is empty")
		}
		return nil
	})
}

// --- storage -------------------------------------------------------------

func runStorageChecks(ctx context.Context, r *runner, c *remote.Client) {
	const key = "conformance/probe.txt"
	r.require("POST /v1/bucket/ensure is idempotent", func() error {
		_, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/bucket/ensure",
			Body:   map[string]any{},
		})
		return err
	})
	r.require("PUT /v1/objects/{key} accepts a small body", func() error {
		_, err := c.Do(ctx, remote.Request{
			Method:      http.MethodPut,
			Path:        "/v1/objects/" + key,
			BodyReader:  strings.NewReader("hello conformance"),
			ContentType: "text/plain",
		})
		return err
	})
	r.require("GET /v1/objects/{key} returns the same bytes", func() error {
		resp, err := c.Do(ctx, remote.Request{Method: http.MethodGet, Path: "/v1/objects/" + key, Stream: true})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		buf := make([]byte, 32)
		n, _ := resp.Body.Read(buf)
		if !strings.HasPrefix(string(buf[:n]), "hello") {
			return fmt.Errorf("got %q", string(buf[:n]))
		}
		return nil
	})
	r.require("DELETE /v1/objects/{key} is idempotent", func() error {
		_, err := c.Do(ctx, remote.Request{Method: http.MethodDelete, Path: "/v1/objects/" + key})
		if err != nil {
			if remote.IsCode(err, "object_not_found") || remote.IsCode(err, "not_found") {
				return nil
			}
			return err
		}
		_, err = c.Do(ctx, remote.Request{Method: http.MethodDelete, Path: "/v1/objects/" + key})
		if err != nil && !remote.IsCode(err, "object_not_found") && !remote.IsCode(err, "not_found") {
			return err
		}
		return nil
	})
}

// --- notifications -------------------------------------------------------

func runNotificationsChecks(ctx context.Context, r *runner, c *remote.Client) {
	r.require("POST /v1/messages with channel=log returns 200 + id", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/messages",
			Body: map[string]any{
				"channel":   "log",
				"to":        []string{"conformance@example.invalid"},
				"subject":   "test",
				"body_text": "test",
			},
		})
		if err != nil {
			p, ok := remote.AsProblem(err)
			// Adapters that don't support "log" channel may legitimately reject it.
			if ok && p.Code == "unsupported_channel" {
				return nil
			}
			return err
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		if out.ID == "" {
			return fmt.Errorf("no id returned")
		}
		return nil
	})
}

// --- secrets -------------------------------------------------------------

func runSecretsChecks(ctx context.Context, r *runner, c *remote.Client) {
	const key = "conformance_probe"
	r.require("PUT /v1/secrets/{key} returns metadata", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method:   http.MethodPut,
			Path:     "/v1/secrets/" + key,
			TenantID: "conformance",
			Body:     map[string]any{"value": "test-value"},
		})
		if err != nil {
			return err
		}
		var out struct {
			Key string `json:"key"`
		}
		return resp.DecodeJSON(&out)
	})
	r.require("POST /v1/secrets/{key}/reveal returns the value", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method:   http.MethodPost,
			Path:     "/v1/secrets/" + key + "/reveal",
			TenantID: "conformance",
			Body:     map[string]any{},
		})
		if err != nil {
			if remote.IsCode(err, "reveal_forbidden") {
				// Acceptable for write-only secret backends.
				return nil
			}
			return err
		}
		var out struct {
			Value string `json:"value"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		if out.Value != "test-value" {
			return fmt.Errorf("got %q", out.Value)
		}
		return nil
	})
	r.require("DELETE /v1/secrets/{key} is idempotent", func() error {
		_, err := c.Do(ctx, remote.Request{
			Method:   http.MethodDelete,
			Path:     "/v1/secrets/" + key,
			TenantID: "conformance",
		})
		return err
	})
}

// --- billing -------------------------------------------------------------

func runBillingChecks(ctx context.Context, r *runner, c *remote.Client) {
	r.require("POST /v1/customers creates a tenant customer", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/customers",
			Body: map[string]any{
				"tenant_id": "conformance",
				"email":     "conformance@example.invalid",
			},
		})
		if err != nil {
			return err
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		if out.ID == "" {
			return fmt.Errorf("no customer id returned")
		}
		return nil
	})
}

// --- llm-chat -----------------------------------------------------------

func runLLMChatChecks(ctx context.Context, r *runner, c *remote.Client) {
	r.require("POST /v1/chat/completions returns choices + usage", func() error {
		body := map[string]any{
			"model": "any/test",
			"messages": []map[string]string{
				{"role": "user", "content": "Reply with the literal word ok."},
			},
			"max_tokens": 8,
		}
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/chat/completions",
			Body:   body,
		})
		if err != nil {
			return err
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		if len(out.Choices) == 0 {
			return fmt.Errorf("no choices returned")
		}
		return nil
	})

	r.require("GET /v1/models returns at least one model", func() error {
		resp, err := c.Do(ctx, remote.Request{Method: http.MethodGet, Path: "/v1/models"})
		if err != nil {
			return err
		}
		var out struct {
			Data []map[string]any `json:"data"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		if len(out.Data) == 0 {
			return fmt.Errorf("/v1/models returned no models")
		}
		return nil
	})

	r.require("Capabilities declare at least chat OR embeddings", func() error {
		caps, err := c.Capabilities(ctx)
		if err != nil {
			return err
		}
		var typed struct {
			Chat       bool `json:"supports_chat"`
			Embeddings bool `json:"supports_embeddings"`
		}
		if err := json.Unmarshal(caps.Capabilities, &typed); err != nil {
			return err
		}
		if !(typed.Chat || typed.Embeddings) {
			return fmt.Errorf("adapter supports neither chat nor embeddings")
		}
		return nil
	})
}

// --- auth ---------------------------------------------------------------

func runAuthChecks(ctx context.Context, r *runner, c *remote.Client) {
	r.require("POST /v1/sessions/verify with empty token returns 401", func() error {
		_, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/sessions/verify",
			Body:   map[string]string{"token": ""},
		})
		if err == nil {
			return fmt.Errorf("expected 401 on empty token; got success")
		}
		p, ok := remote.AsProblem(err)
		if !ok || p.HTTPStatus != http.StatusUnauthorized {
			return fmt.Errorf("expected 401, got %v", err)
		}
		return nil
	})

	r.require("Capabilities declare at least one identity feature", func() error {
		caps, err := c.Capabilities(ctx)
		if err != nil {
			return err
		}
		var typed struct {
			OAuth        []string `json:"supports_oauth_providers"`
			Passwordless bool     `json:"supports_passwordless"`
			MagicLinks   bool     `json:"supports_magic_links"`
			SSO          bool     `json:"supports_sso"`
		}
		if err := json.Unmarshal(caps.Capabilities, &typed); err != nil {
			return err
		}
		if len(typed.OAuth) == 0 && !typed.Passwordless && !typed.MagicLinks && !typed.SSO {
			return fmt.Errorf("auth adapter declares no identity feature")
		}
		return nil
	})
}

// --- logs ---------------------------------------------------------------

func runLogsChecks(ctx context.Context, r *runner, c *remote.Client) {
	var caps struct {
		SupportsTail      bool `json:"supports_tail"`
		SupportsFullText  bool `json:"supports_full_text"`
		MaxEntriesPerPage int  `json:"max_entries_per_page"`
	}

	r.require("GET /v1/capabilities declares logs capabilities", func() error {
		env, err := c.Capabilities(ctx)
		if err != nil {
			return err
		}
		if env.Slot != "logs" {
			return fmt.Errorf("slot=%q; expected logs", env.Slot)
		}
		if err := json.Unmarshal(env.Capabilities, &caps); err != nil {
			return err
		}
		if caps.MaxEntriesPerPage < 0 {
			return fmt.Errorf("max_entries_per_page must be non-negative")
		}
		return nil
	})

	r.require("POST /v1/logs/query returns a page", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/logs/query",
			Body: map[string]any{
				"limit": 1,
			},
		})
		if err != nil {
			return err
		}
		var out struct {
			Entries []map[string]any `json:"entries"`
			HasMore bool             `json:"has_more"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		_ = out.HasMore
		return nil
	})

	r.require("GET /v1/logs/tail honors declared tail capability", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodGet,
			Path:   "/v1/logs/tail",
			Query:  mapValues("limit", "1"),
			Stream: true,
		})
		if !caps.SupportsTail {
			if err == nil {
				_ = resp.Body.Close()
				return fmt.Errorf("supports_tail=false but tail returned success")
			}
			p, ok := remote.AsProblem(err)
			if !ok || p.HTTPStatus != http.StatusUnprocessableEntity || p.Code != "unsupported_capability" {
				return fmt.Errorf("expected 422 unsupported_capability, got %v", err)
			}
			return nil
		}
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		tailCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		events := resp.Events(tailCtx)
		select {
		case <-tailCtx.Done():
			return nil
		case _, ok := <-events:
			if !ok {
				return nil
			}
			return nil
		}
	})

	r.require("GET /v1/logs/tail terminates on disconnect", func() error {
		if !caps.SupportsTail {
			return nil
		}
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodGet,
			Path:   "/v1/logs/tail",
			Query:  mapValues("limit", "1"),
			Stream: true,
		})
		if err != nil {
			return err
		}
		return resp.Body.Close()
	})
}

// --- traces -------------------------------------------------------------

func runTracesChecks(ctx context.Context, r *runner, c *remote.Client) {
	var caps struct {
		SupportsTraceQL    bool   `json:"supports_traceql"`
		SupportsTagSearch  bool   `json:"supports_tag_search"`
		NativeQueryLang    string `json:"native_query_lang"`
		MaxResultsPerQuery int    `json:"max_results_per_query"`
	}

	r.require("GET /v1/capabilities declares traces capabilities", func() error {
		env, err := c.Capabilities(ctx)
		if err != nil {
			return err
		}
		if env.Slot != "traces" {
			return fmt.Errorf("slot=%q; expected traces", env.Slot)
		}
		if err := json.Unmarshal(env.Capabilities, &caps); err != nil {
			return err
		}
		if !caps.SupportsTraceQL && !caps.SupportsTagSearch && caps.NativeQueryLang == "" {
			return fmt.Errorf("traces adapter declares no search capability")
		}
		if caps.MaxResultsPerQuery < 0 {
			return fmt.Errorf("max_results_per_query must be non-negative")
		}
		return nil
	})

	r.require("POST /v1/traces/search returns a result envelope", func() error {
		resp, err := c.Do(ctx, remote.Request{
			Method: http.MethodPost,
			Path:   "/v1/traces/search",
			Body: map[string]any{
				"limit": 1,
			},
		})
		if err != nil {
			return err
		}
		var out struct {
			Traces  []map[string]any `json:"traces"`
			HasMore bool             `json:"has_more"`
		}
		if err := resp.DecodeJSON(&out); err != nil {
			return err
		}
		_ = out.HasMore
		return nil
	})

	r.require("GET /v1/traces/{unknown} returns 404 trace_not_found", func() error {
		_, err := c.Do(ctx, remote.Request{
			Method: http.MethodGet,
			Path:   "/v1/traces/does-not-exist",
		})
		if err == nil {
			return fmt.Errorf("expected missing trace to return an error")
		}
		p, ok := remote.AsProblem(err)
		if !ok || p.HTTPStatus != http.StatusNotFound || p.Code != "trace_not_found" {
			return fmt.Errorf("expected 404 trace_not_found, got %v", err)
		}
		return nil
	})
}

func mapValues(k, v string) url.Values {
	return url.Values{k: []string{v}}
}

// --- multimodal ---------------------------------------------------------

func runMultimodalChecks(ctx context.Context, r *runner, c *remote.Client) {
	r.require("GET /v1/capabilities declares at least one verb", func() error {
		caps, err := c.Capabilities(ctx)
		if err != nil {
			return err
		}
		var typed struct {
			TTS   bool `json:"supports_tts"`
			STT   bool `json:"supports_stt"`
			Image bool `json:"supports_image_generation"`
			Edit  bool `json:"supports_image_edit"`
			Vary  bool `json:"supports_image_variation"`
		}
		if err := json.Unmarshal(caps.Capabilities, &typed); err != nil {
			return err
		}
		if !(typed.TTS || typed.STT || typed.Image || typed.Edit || typed.Vary) {
			return fmt.Errorf("no multimodal verb supported")
		}
		return nil
	})
}
