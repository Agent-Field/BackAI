// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// TestPickBrowserAdapter_CredsDriven verifies the browser adapter is built
// from resolved BrowserCreds (the Integrations-vault overlay path), not
// only from raw env vars.
func TestPickBrowserAdapter_CredsDriven(t *testing.T) {
	ad, err := pickBrowserAdapter("browser-use", BrowserCreds{
		BrowserUseURL: "http://sidecar:8000",
		AllowPrivate:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ad.Configured() {
		t.Fatal("browser-use with a creds-provided URL should be configured")
	}

	if _, err := pickBrowserAdapter("steel", BrowserCreds{}); err == nil ||
		!strings.Contains(err.Error(), "Integrations") {
		t.Fatalf("steel without key should error mentioning Integrations, got %v", err)
	}

	if _, err := pickBrowserAdapter("playwright", BrowserCreds{PlaywrightEndpoint: "wss://x"}); err != nil {
		t.Fatalf("playwright with creds endpoint should construct, got %v", err)
	}
}
