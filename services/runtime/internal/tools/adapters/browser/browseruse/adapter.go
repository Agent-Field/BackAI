// SPDX-License-Identifier: Apache-2.0

// Package browseruse is the primary BrowserAdapter implementation. It
// speaks HTTP to a long-running browser-use sidecar (the operator runs
// the sidecar themselves; we do NOT ship it in docker-compose because
// it has heavy native dependencies — chromium, etc.). The sidecar URL
// comes from the BROWSER_USE_URL env var.
//
// Endpoints expected on the sidecar:
//
//	POST /navigate      {url, session_id} -> {title, url, status_code}
//	POST /extract-text  {session_id}      -> {text}
//	POST /screenshot    {session_id}      -> {screenshot_base64}
//	POST /click         {selector, session_id}        -> {}
//	POST /fill          {selector, value, session_id} -> {}
//
// All requests go through internal/safehttp.Client so the SSRF guards
// apply (BROWSER_USE_URL must NOT resolve into a private CIDR unless
// the operator added it to the allowlist).
package browseruse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
)

// Adapter is the browser-use HTTP client. Construct via New().
type Adapter struct {
	baseURL string
	client  *http.Client
}

// New returns a browser-use adapter for the given sidecar URL. An empty
// URL produces an unconfigured adapter — Configured() returns false and
// every verb returns browser.ErrNotConfigured.
//
// allowPrivate re-permits loopback / RFC-1918 sidecar addresses (a
// docker-compose service name or localhost). Off by default so a hosted
// deployment can't point the browser tool at internal services; the
// factory gates it behind AF_STACK_BROWSER_ALLOW_PRIVATE.
func New(baseURL string, allowPrivate bool) *Adapter {
	opts := safehttp.Options{Timeout: 120 * time.Second}
	if allowPrivate {
		opts.AllowCIDRs = safehttp.LoopbackAndPrivateCIDRs()
	}
	return &Adapter{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  safehttp.New(opts),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "browser-use" }

// Configured reports whether the sidecar URL is set.
func (a *Adapter) Configured() bool { return a != nil && a.baseURL != "" }

// Navigate POSTs /navigate.
func (a *Adapter) Navigate(ctx context.Context, sessionID, url string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	body := map[string]any{"url": url, "session_id": sessionID}
	var out browser.Result
	if err := a.do(ctx, "/navigate", body, &out); err != nil {
		return browser.Result{}, err
	}
	return out, nil
}

// ExtractText POSTs /extract-text.
func (a *Adapter) ExtractText(ctx context.Context, sessionID string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	var out browser.Result
	if err := a.do(ctx, "/extract-text", map[string]any{"session_id": sessionID}, &out); err != nil {
		return browser.Result{}, err
	}
	return out, nil
}

// Screenshot POSTs /screenshot.
func (a *Adapter) Screenshot(ctx context.Context, sessionID string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	var out browser.Result
	if err := a.do(ctx, "/screenshot", map[string]any{"session_id": sessionID}, &out); err != nil {
		return browser.Result{}, err
	}
	return out, nil
}

// Click POSTs /click.
func (a *Adapter) Click(ctx context.Context, sessionID, selector string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	var out browser.Result
	if err := a.do(ctx, "/click", map[string]any{
		"selector":   selector,
		"session_id": sessionID,
	}, &out); err != nil {
		return browser.Result{}, err
	}
	return out, nil
}

// Fill POSTs /fill.
func (a *Adapter) Fill(ctx context.Context, sessionID, selector, value string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	var out browser.Result
	if err := a.do(ctx, "/fill", map[string]any{
		"selector":   selector,
		"value":      value,
		"session_id": sessionID,
	}, &out); err != nil {
		return browser.Result{}, err
	}
	return out, nil
}

// do is the shared HTTP plumbing. JSON in, JSON out, errors include
// the response status code.
func (a *Adapter) do(ctx context.Context, path string, body any, into *browser.Result) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("browseruse: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("browseruse: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("browseruse: do: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 8<<20) // 8 MB cap (screenshots)
	respBody, readErr := io.ReadAll(limited)
	if readErr != nil {
		return fmt.Errorf("browseruse: read: %w", readErr)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("browseruse: sidecar status %d: %s", resp.StatusCode, truncate(string(respBody), 256))
	}
	if len(respBody) == 0 || into == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, into); err != nil {
		// Tolerate non-JSON 2xx (sidecar returns {} for fire-and-forget verbs)
		return nil
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
