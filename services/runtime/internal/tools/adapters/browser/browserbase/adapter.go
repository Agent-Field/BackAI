// SPDX-License-Identifier: Apache-2.0

// Package browserbase is the BrowserAdapter for the Browserbase hosted
// browser service (https://docs.browserbase.com).
//
// Lifecycle per runtime sessionID:
//
//  1. First verb: POST https://api.browserbase.com/v1/sessions (header
//     `X-BB-API-Key`, body {"projectId": ...}) creates a session — the
//     response's `connectUrl` is a ready-to-dial CDP websocket URL with
//     auth baked in.
//  2. The shared cdpdriver engine attaches to connectUrl and serves all
//     five verbs on it.
//  3. Browserbase ends a session when its CDP connection closes (the
//     default keepAlive=false), so idle-TTL teardown in cdpdriver —
//     which cancels the chromedp contexts — is the release; no explicit
//     release call is required.
//
// REST calls go through internal/safehttp so the SSRF guards apply.
package browserbase

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
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser/cdpdriver"
)

// DefaultBaseURL is the hosted Browserbase API.
const DefaultBaseURL = "https://api.browserbase.com"

// verbDriver is the slice of cdpdriver.Driver the adapter consumes.
// Tests may swap in a fake.
type verbDriver interface {
	Navigate(ctx context.Context, sessionID, url string) (browser.Result, error)
	ExtractText(ctx context.Context, sessionID string) (browser.Result, error)
	Screenshot(ctx context.Context, sessionID string) (browser.Result, error)
	Click(ctx context.Context, sessionID, selector string) (browser.Result, error)
	Fill(ctx context.Context, sessionID, selector, value string) (browser.Result, error)
	Close()
}

// Adapter drives Browserbase cloud browsers. Construct via New().
type Adapter struct {
	apiKey    string
	projectID string
	baseURL   string
	client    *http.Client
	driver    verbDriver
}

// createSessionResponse is the subset of Browserbase's session object
// the adapter needs (POST /v1/sessions, 201).
type createSessionResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"` // PENDING | RUNNING | ERROR | ...
	ConnectURL string `json:"connectUrl"`
}

// New returns a Browserbase adapter. Configured() requires both the
// API key and the project ID; when either is empty every verb returns
// browser.ErrNotConfigured.
//
// allowPrivate re-permits loopback / RFC-1918 CDP endpoints (only
// relevant when pointing the adapter at a mock in tests — hosted
// Browserbase always hands out public wss:// URLs). Off by default;
// the factory gates it behind AF_STACK_BROWSER_ALLOW_PRIVATE.
func New(apiKey, projectID string, allowPrivate bool) *Adapter {
	opts := safehttp.Options{Timeout: 30 * time.Second}
	if allowPrivate {
		opts.AllowCIDRs = safehttp.LoopbackAndPrivateCIDRs()
	}
	a := &Adapter{
		apiKey:    strings.TrimSpace(apiKey),
		projectID: strings.TrimSpace(projectID),
		baseURL:   DefaultBaseURL,
		client:    safehttp.New(opts),
	}
	a.driver = cdpdriver.New(cdpdriver.Options{
		AllowPrivate: allowPrivate,
		Endpoint:     a.createSession,
	})
	return a
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "browserbase" }

// Configured reports whether BROWSERBASE_API_KEY and
// BROWSERBASE_PROJECT_ID are both set.
func (a *Adapter) Configured() bool {
	return a != nil && a.apiKey != "" && a.projectID != ""
}

// Close tears down every live browser session.
func (a *Adapter) Close() {
	if a != nil && a.driver != nil {
		a.driver.Close()
	}
}

// Navigate loads a URL in the session's page.
func (a *Adapter) Navigate(ctx context.Context, sessionID, url string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	return a.driver.Navigate(ctx, sessionID, url)
}

// ExtractText returns the visible text of the current page.
func (a *Adapter) ExtractText(ctx context.Context, sessionID string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	return a.driver.ExtractText(ctx, sessionID)
}

// Screenshot returns a base64-encoded PNG of the current page.
func (a *Adapter) Screenshot(ctx context.Context, sessionID string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	return a.driver.Screenshot(ctx, sessionID)
}

// Click clicks the element matching the CSS selector.
func (a *Adapter) Click(ctx context.Context, sessionID, selector string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	return a.driver.Click(ctx, sessionID, selector)
}

// Fill sets the value of an input matching the CSS selector.
func (a *Adapter) Fill(ctx context.Context, sessionID, selector, value string) (browser.Result, error) {
	if !a.Configured() {
		return browser.Result{}, browser.ErrNotConfigured
	}
	return a.driver.Fill(ctx, sessionID, selector, value)
}

// createSession is the cdpdriver Endpoint callback: it creates a
// Browserbase session over REST and returns its connectUrl. No cleanup
// closure — Browserbase releases the session when the CDP connection
// drops.
func (a *Adapter) createSession(ctx context.Context, _ string) (string, func(), error) {
	payload, err := json.Marshal(map[string]string{"projectId": a.projectID})
	if err != nil {
		return "", nil, fmt.Errorf("browserbase: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/v1/sessions", bytes.NewReader(payload))
	if err != nil {
		return "", nil, fmt.Errorf("browserbase: create session request: %w", err)
	}
	req.Header.Set("X-BB-API-Key", a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("browserbase: create session: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil, fmt.Errorf("browserbase: create session read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("browserbase: create session status %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	var sess createSessionResponse
	if err := json.Unmarshal(body, &sess); err != nil {
		return "", nil, fmt.Errorf("browserbase: create session decode: %w", err)
	}
	if sess.ConnectURL == "" {
		return "", nil, fmt.Errorf("browserbase: create session response has no connectUrl: %s", truncate(string(body), 256))
	}
	return sess.ConnectURL, nil, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Compile-time interface assertion.
var _ browser.Adapter = (*Adapter)(nil)
