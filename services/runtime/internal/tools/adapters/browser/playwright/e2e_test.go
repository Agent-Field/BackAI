// SPDX-License-Identifier: Apache-2.0

package playwright

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
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
//     local fixture.
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

	// Keep the fixture local so the contract does not depend on a public site.
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><head><title>Browser contract</title></head><body>Browser contract</body></html>"))
	}))
	defer fixture.Close()

	// 1. navigate
	nav, err := a.Navigate(ctx, sid, fixture.URL+"/")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if nav.URL != fixture.URL+"/" {
		t.Errorf("navigate URL = %q, want %q", nav.URL, fixture.URL+"/")
	}
	if !strings.Contains(nav.Title, "Browser contract") {
		t.Errorf("navigate Title = %q, want to contain Browser contract", nav.Title)
	}
	if nav.StatusCode != 200 {
		t.Errorf("navigate StatusCode = %d, want 200", nav.StatusCode)
	}

	// 2. extract_text on the same session sees the same page.
	text, err := a.ExtractText(ctx, sid)
	if err != nil {
		t.Fatalf("extract_text: %v", err)
	}
	if !strings.Contains(text.Text, "Browser contract") {
		t.Errorf("extract_text = %q, want to contain Browser contract", text.Text)
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
		"<input id='name' class='name'><input id='other' class='name' value='untouched'>" +
		"<button class='go' onclick=\"document.getElementById('out').textContent=" +
		"'clicked:'+document.getElementById('name').value+'|'+document.getElementById('other').value\">Go</button>" +
		"<button class='go' onclick=\"document.getElementById('out').textContent='wrong button'\">Other</button>" +
		"<div id='out'></div>" +
		"</body></html>"
	if _, err := a.Navigate(ctx, sid, page); err != nil {
		t.Fatalf("navigate data URL: %v", err)
	}
	// Both selectors match twice: these verbs must still act on the first
	// CSS match after the typed-selector API migration.
	const value = `backai "quoted" \ <tag>`
	if _, err := a.Fill(ctx, sid, ".name", value); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if _, err := a.Click(ctx, sid, ".go"); err != nil {
		t.Fatalf("click: %v", err)
	}
	out, err := a.ExtractText(ctx, sid)
	if err != nil {
		t.Fatalf("extract_text after click: %v", err)
	}
	if !strings.Contains(out.Text, "clicked:"+value+"|untouched") {
		t.Errorf("post-click text = %q, want to contain %q", out.Text, "clicked:"+value+"|untouched")
	}
}
