// SPDX-License-Identifier: Apache-2.0

// Package steel is the BrowserAdapter for the Steel.dev hosted browser
// service (and self-hosted steel-browser, via baseURL).
//
// Lifecycle per runtime sessionID:
//
//  1. First verb: POST {baseURL}/v1/sessions (header `steel-api-key`)
//     creates a Steel session — response carries `id` and
//     `websocketUrl`.
//  2. The shared cdpdriver engine attaches to the session's CDP
//     websocket (canonically wss://connect.steel.dev?apiKey=K&sessionId=ID)
//     and serves all five verbs on it.
//  3. When the runtime session expires (idle TTL) or the adapter is
//     closed, POST {baseURL}/v1/sessions/{id}/release ends the Steel
//     session — Steel bills per session-minute, so this is not optional.
//
// REST calls go through internal/safehttp so the SSRF guards apply to
// the API base URL as well as (via cdpdriver's check) the CDP endpoint.
package steel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser/cdpdriver"
)

// DefaultBaseURL is the hosted Steel API.
const DefaultBaseURL = "https://api.steel.dev"

// defaultConnectURL is the hosted CDP gateway, used when the session
// response omits websocketUrl.
const defaultConnectURL = "wss://connect.steel.dev"

const releaseTimeout = 10 * time.Second

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

// Adapter drives Steel cloud browsers. Construct via New().
type Adapter struct {
	apiKey  string
	baseURL string
	client  *http.Client
	driver  verbDriver
}

// createSessionResponse is the subset of Steel's session object the
// adapter needs (POST /v1/sessions, 2xx).
type createSessionResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"` // "live" | "released" | "failed"
	WebsocketURL string `json:"websocketUrl"`
}

// New returns a Steel adapter. An empty apiKey produces an unconfigured
// adapter — Configured() returns false and every verb returns
// browser.ErrNotConfigured. An empty baseURL defaults to the hosted
// API (DefaultBaseURL); point it at a self-hosted steel-browser to
// avoid the SaaS.
//
// allowPrivate re-permits loopback / RFC-1918 addresses for both the
// REST base URL and the session's CDP endpoint (self-hosted Steel on
// the compose network). Off by default; the factory gates it behind
// AF_STACK_BROWSER_ALLOW_PRIVATE.
func New(apiKey, baseURL string, allowPrivate bool) *Adapter {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	opts := safehttp.Options{Timeout: 30 * time.Second}
	if allowPrivate {
		opts.AllowCIDRs = safehttp.LoopbackAndPrivateCIDRs()
	}
	a := &Adapter{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: baseURL,
		client:  safehttp.New(opts),
	}
	a.driver = cdpdriver.New(cdpdriver.Options{
		AllowPrivate: allowPrivate,
		Endpoint:     a.createSession,
	})
	return a
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "steel" }

// Configured reports whether STEEL_API_KEY is set.
func (a *Adapter) Configured() bool { return a != nil && a.apiKey != "" }

// Close releases every live Steel session and shuts the driver down.
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

// createSession is the cdpdriver Endpoint callback: it creates a Steel
// session over REST and returns its CDP connect URL plus a cleanup that
// releases that exact Steel session.
func (a *Adapter) createSession(ctx context.Context, _ string) (string, func(), error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/v1/sessions", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", nil, fmt.Errorf("steel: create session request: %w", err)
	}
	a.setHeaders(req)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("steel: create session: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil, fmt.Errorf("steel: create session read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("steel: create session status %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	var sess createSessionResponse
	if err := json.Unmarshal(body, &sess); err != nil {
		return "", nil, fmt.Errorf("steel: create session decode: %w", err)
	}
	if sess.ID == "" {
		return "", nil, fmt.Errorf("steel: create session response has no id: %s", truncate(string(body), 256))
	}

	wsURL, err := a.connectURL(sess)
	if err != nil {
		// The Steel session exists but we can't build a connect URL for
		// it — release it now rather than leaking billed minutes.
		a.releaseSession(sess.ID)
		return "", nil, err
	}
	steelID := sess.ID
	return wsURL, func() { a.releaseSession(steelID) }, nil
}

// connectURL derives the CDP websocket URL for a created session. The
// docs' canonical form is
//
//	wss://connect.steel.dev?apiKey=<key>&sessionId=<id>
//
// and the session response also carries a websocketUrl (without the
// apiKey). Preferring websocketUrl keeps self-hosted steel-browser
// working (its gateway is not connect.steel.dev); the apiKey query
// param is appended when missing.
func (a *Adapter) connectURL(sess createSessionResponse) (string, error) {
	raw := strings.TrimSpace(sess.WebsocketURL)
	if raw == "" {
		raw = defaultConnectURL + "?sessionId=" + url.QueryEscape(sess.ID)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("steel: bad websocketUrl %q: %w", raw, err)
	}
	q := u.Query()
	if q.Get("apiKey") == "" {
		q.Set("apiKey", a.apiKey)
	}
	if q.Get("sessionId") == "" {
		q.Set("sessionId", sess.ID)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// releaseSession ends a Steel session (POST /v1/sessions/{id}/release).
// Best-effort: invoked from cleanup paths that have no caller context,
// so it uses its own timeout; failures are swallowed (Steel's own
// session timeout is the backstop).
func (a *Adapter) releaseSession(steelID string) {
	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/v1/sessions/"+url.PathEscape(steelID)+"/release", bytes.NewReader([]byte("{}")))
	if err != nil {
		return
	}
	a.setHeaders(req)
	resp, err := a.client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
}

func (a *Adapter) setHeaders(req *http.Request) {
	req.Header.Set("steel-api-key", a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Compile-time interface assertion.
var _ browser.Adapter = (*Adapter)(nil)
