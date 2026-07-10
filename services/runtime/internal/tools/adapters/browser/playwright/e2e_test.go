// SPDX-License-Identifier: Apache-2.0

package playwright

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2EAllVerbs drives all five browser verbs through the playwright
// adapter (and therefore the shared cdpdriver engine) against a real
// CDP endpoint. Guarded by CDP_E2E_WS; run it with a local Browserless:
//
//	docker run -d --rm --name cdp-e2e --dns 8.8.8.8 -p 3123:3000 ghcr.io/browserless/chromium
//	CDP_E2E_WS=ws://localhost:3123 go test ./services/runtime/internal/tools/adapters/browser/... -run E2E -v
//
// --- Validation contract ---
//
//  1. navigate returns the final URL, page title, and HTTP 200 for a
//     public page.
//  2. extract_text on the SAME session_id sees that page (session state
//     persists across verbs) and returns its body text.
//  3. screenshot returns valid base64 whose bytes carry the PNG magic.
//  4. fill sets an input's value; click fires a button; the combination
//     is observable in a follow-up extract_text.
func TestE2EAllVerbs(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("CDP_E2E_WS"))
	if endpoint == "" {
		t.Skip("CDP_E2E_WS not set; skipping live CDP e2e")
	}

	a := New(endpoint, true) // local containers live on private addresses
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const sid = "e2e-session"

	// 1. navigate
	nav, err := a.Navigate(ctx, sid, "https://example.com/")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if !strings.Contains(nav.URL, "example.com") {
		t.Errorf("navigate URL = %q, want example.com", nav.URL)
	}
	if !strings.Contains(nav.Title, "Example Domain") {
		t.Errorf("navigate Title = %q, want to contain Example Domain", nav.Title)
	}
	if nav.StatusCode != 200 {
		t.Errorf("navigate StatusCode = %d, want 200", nav.StatusCode)
	}

	// 2. extract_text on the same session sees the same page.
	text, err := a.ExtractText(ctx, sid)
	if err != nil {
		t.Fatalf("extract_text: %v", err)
	}
	if !strings.Contains(text.Text, "Example Domain") {
		t.Errorf("extract_text = %q, want to contain Example Domain", text.Text)
	}

	// 3. screenshot is a real PNG.
	shot, err := a.Screenshot(ctx, sid)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	png, err := base64.StdEncoding.DecodeString(shot.ScreenshotBase64)
	if err != nil {
		t.Fatalf("screenshot base64: %v", err)
	}
	magic := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if len(png) < len(magic) || string(png[:len(magic)]) != string(magic) {
		t.Errorf("screenshot bytes lack PNG magic (got % x)", png[:min(8, len(png))])
	}

	// 4. fill + click on a data: URL page, observed via extract_text.
	page := "data:text/html,<html><body>" +
		"<input id='name'>" +
		"<button id='go' onclick=\"document.getElementById('out').textContent=" +
		"'clicked:'+document.getElementById('name').value\">Go</button>" +
		"<div id='out'></div>" +
		"</body></html>"
	if _, err := a.Navigate(ctx, sid, page); err != nil {
		t.Fatalf("navigate data URL: %v", err)
	}
	if _, err := a.Fill(ctx, sid, "#name", "backai"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if _, err := a.Click(ctx, sid, "#go"); err != nil {
		t.Fatalf("click: %v", err)
	}
	out, err := a.ExtractText(ctx, sid)
	if err != nil {
		t.Fatalf("extract_text after click: %v", err)
	}
	if !strings.Contains(out.Text, "clicked:backai") {
		t.Errorf("post-click text = %q, want to contain clicked:backai", out.Text)
	}
}
