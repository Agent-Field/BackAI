// SPDX-License-Identifier: Apache-2.0

// Package playwright is the BrowserAdapter for a remote CDP/Playwright
// browser endpoint driven directly over the Chrome DevTools Protocol.
// PLAYWRIGHT_ENDPOINT is a websocket URL used as-is — this is exactly
// how Browserless works:
//
//	wss://chrome.browserless.io?token=KEY   (Browserless cloud)
//	ws://browserless:3000                   (self-hosted; needs
//	                                         AF_STACK_BROWSER_ALLOW_PRIVATE)
//
// Any server that speaks raw CDP over websocket (a Chrome launched with
// --remote-debugging-port behind a ws proxy, a Playwright server in CDP
// mode, etc.) works the same way. All five verbs are delegated to the
// shared cdpdriver engine; per-session state (sessionID -> live page)
// lives there.
package playwright

import (
	"context"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser/cdpdriver"
)

// Adapter drives a remote CDP endpoint. Construct via New().
type Adapter struct {
	endpoint string
	driver   *cdpdriver.Driver
}

// New returns a playwright adapter for the given CDP websocket URL. An
// empty endpoint produces an unconfigured adapter — Configured()
// returns false and every verb returns browser.ErrNotConfigured.
//
// allowPrivate re-permits loopback / RFC-1918 endpoints (a self-hosted
// Browserless on the compose network). Off by default; the factory
// gates it behind AF_STACK_BROWSER_ALLOW_PRIVATE.
func New(endpoint string, allowPrivate bool) *Adapter {
	endpoint = strings.TrimSpace(endpoint)
	a := &Adapter{endpoint: endpoint}
	a.driver = cdpdriver.New(cdpdriver.Options{
		AllowPrivate: allowPrivate,
		Endpoint: func(context.Context, string) (string, func(), error) {
			// One fixed endpoint for every sessionID; distinct sessions
			// still get distinct browser contexts (the endpoint's server
			// multiplexes connections). No provider session to release.
			return a.endpoint, nil, nil
		},
	})
	return a
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "playwright" }

// Configured reports whether PLAYWRIGHT_ENDPOINT is set.
func (a *Adapter) Configured() bool { return a != nil && a.endpoint != "" }

// Close tears down all live browser sessions.
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

// Compile-time interface assertion.
var _ browser.Adapter = (*Adapter)(nil)
