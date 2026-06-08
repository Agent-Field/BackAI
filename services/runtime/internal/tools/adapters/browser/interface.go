// SPDX-License-Identifier: Apache-2.0

// Package browser defines the BrowserAdapter contract — the per-backend
// interface that the runtime's `browser` tool routes verb invocations
// to. Three adapters live under sub-packages:
//
//   - browseruse — primary (HTTP sidecar)
//   - steel     — paid SaaS (stub until STEEL_API_KEY is set)
//   - playwright — wraps a remote Playwright driver (stub)
//
// The Adapter is intentionally minimal: five verbs that any browser
// automation backend can implement. Each Adapter MUST be safe for
// concurrent use and MUST NOT block indefinitely; the caller's context
// carries the per-call deadline.
package browser

import (
	"context"
	"errors"
)

// ErrNotConfigured is the shared sentinel that stub adapters return.
// The factory in tools/factory.go unwraps it to surface a clear error
// at startup rather than silently fallback to another adapter.
var ErrNotConfigured = errors.New("browser: adapter not configured")

// Result is the canonical return shape from browser verbs. Different
// verbs populate different fields:
//   - navigate: Title, URL, StatusCode
//   - extract_text: Text
//   - screenshot: ScreenshotBase64 (PNG)
//   - click / fill: nothing (caller checks err)
//
// Unused fields are omitted from JSON (omitempty).
type Result struct {
	URL              string `json:"url,omitempty"`
	Title            string `json:"title,omitempty"`
	StatusCode       int    `json:"status_code,omitempty"`
	Text             string `json:"text,omitempty"`
	ScreenshotBase64 string `json:"screenshot_base64,omitempty"`
}

// Adapter is the per-backend contract. The runtime's `browser` tool
// constructs one Adapter at boot (via factory.go) and routes verb
// invocations to it.
//
// Implementations may treat sessionID as opaque — it's a caller-
// provided string used by stateful backends (Steel, Playwright) to
// reuse a browser context across calls; stateless backends ignore it.
type Adapter interface {
	// ID returns the adapter identifier ("browser-use" / "steel" /
	// "playwright"). Used by the dashboard + audit log.
	ID() string

	// Configured reports whether the backing service is ready
	// (URL / API key present). Stub adapters return false.
	Configured() bool

	// Navigate loads a URL in a fresh (or reused) browser context.
	Navigate(ctx context.Context, sessionID, url string) (Result, error)

	// ExtractText returns the visible text of the current page.
	ExtractText(ctx context.Context, sessionID string) (Result, error)

	// Screenshot returns a base64-encoded PNG of the current page.
	Screenshot(ctx context.Context, sessionID string) (Result, error)

	// Click simulates a click on the element matching the CSS selector.
	Click(ctx context.Context, sessionID, selector string) (Result, error)

	// Fill sets the value of an input matching the CSS selector.
	Fill(ctx context.Context, sessionID, selector, value string) (Result, error)
}
