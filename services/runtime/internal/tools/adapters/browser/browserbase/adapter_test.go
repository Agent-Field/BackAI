// SPDX-License-Identifier: Apache-2.0

package browserbase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
)

// --- Validation contract ---
//
// 1. First verb on a session POSTs /v1/sessions with the X-BB-API-Key
//    header and JSON body {"projectId": ...}, then attempts a CDP
//    connect against the response's connectUrl.
// 2. Unconfigured adapter (missing key or projectID): Configured() ==
//    false and verbs return browser.ErrNotConfigured without HTTP
//    traffic.
// 3. Verbs delegate arguments to the driver unchanged.

func TestSessionCreateContract(t *testing.T) {
	// Plain HTTP server standing in for the CDP gateway: records the
	// websocket upgrade attempt and refuses it.
	var cdpMu sync.Mutex
	var cdpHits []string
	cdpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdpMu.Lock()
		cdpHits = append(cdpHits, r.URL.String())
		cdpMu.Unlock()
		http.Error(w, "no browser here", http.StatusBadGateway)
	}))
	defer cdpSrv.Close()
	connectURL := "ws" + strings.TrimPrefix(cdpSrv.URL, "http") + "/?signingKey=sk-1"

	type call struct {
		method, path, apiKey, contentType string
		body                              map[string]any
	}
	var mu sync.Mutex
	var calls []call
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls = append(calls, call{
			method:      r.Method,
			path:        r.URL.Path,
			apiKey:      r.Header.Get("X-BB-API-Key"),
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		})
		mu.Unlock()
		if r.URL.Path != "/v1/sessions" {
			t.Errorf("unexpected API call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":"bb-sess-1","status":"RUNNING","connectUrl":%q,"projectId":"proj-1"}`, connectURL)
	}))
	defer api.Close()

	a := New("bb-key", "proj-1", true)
	a.baseURL = api.URL // hosted URL swapped for the fake in tests

	_, err := a.Navigate(context.Background(), "runtime-sess", "https://example.com")
	if err == nil {
		t.Fatal("Navigate succeeded against a fake CDP endpoint; want connect failure")
	}
	a.Close()

	mu.Lock()
	got := append([]call(nil), calls...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("API calls = %d (%+v), want 1 (create)", len(got), got)
	}
	create := got[0]
	if create.method != http.MethodPost || create.path != "/v1/sessions" {
		t.Errorf("create = %s %s, want POST /v1/sessions", create.method, create.path)
	}
	if create.apiKey != "bb-key" {
		t.Errorf("create X-BB-API-Key = %q, want %q", create.apiKey, "bb-key")
	}
	if !strings.HasPrefix(create.contentType, "application/json") {
		t.Errorf("create Content-Type = %q, want application/json", create.contentType)
	}
	if create.body["projectId"] != "proj-1" {
		t.Errorf(`create body projectId = %v, want "proj-1"`, create.body["projectId"])
	}

	// Connect attempt: the fake CDP got the upgrade request at the exact
	// connectUrl from the response (auth query string preserved).
	cdpMu.Lock()
	hits := append([]string(nil), cdpHits...)
	cdpMu.Unlock()
	if len(hits) == 0 {
		t.Fatal("no connect attempt reached the fake CDP endpoint")
	}
	if want := "/?signingKey=sk-1"; hits[0] != want {
		t.Errorf("connect URL = %q, want %q", hits[0], want)
	}
}

func TestUnconfiguredAdapter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, key, project string
	}{
		{"no key", "", "proj-1"},
		{"neither", "", ""},
	}
	// Key without project IS configured — Browserbase infers the project
	// from a single-project API key.
	if a := New("bb-key", "", false); !a.Configured() {
		t.Error("key without project: Configured() = false, want true")
	}
	for _, tc := range cases {
		a := New(tc.key, tc.project, false)
		if a.Configured() {
			t.Errorf("%s: Configured() = true", tc.name)
		}
		if _, err := a.Screenshot(context.Background(), ""); !errors.Is(err, browser.ErrNotConfigured) {
			t.Errorf("%s: Screenshot = %v, want ErrNotConfigured", tc.name, err)
		}
	}
	a := New("bb-key", "proj-1", false)
	if !a.Configured() {
		t.Error("Configured() = false with key + project set")
	}
	if a.ID() != "browserbase" {
		t.Errorf("ID() = %q, want browserbase", a.ID())
	}
}

// fakeDriver verifies delegation through the verbDriver seam.
type fakeDriver struct {
	lastVerb string
	lastArgs []string
}

func (f *fakeDriver) Navigate(_ context.Context, sid, url string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "navigate", []string{sid, url}
	return browser.Result{URL: url}, nil
}

func (f *fakeDriver) ExtractText(_ context.Context, sid string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "extract_text", []string{sid}
	return browser.Result{Text: "body"}, nil
}

func (f *fakeDriver) Screenshot(_ context.Context, sid string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "screenshot", []string{sid}
	return browser.Result{}, nil
}

func (f *fakeDriver) Click(_ context.Context, sid, sel string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "click", []string{sid, sel}
	return browser.Result{}, nil
}

func (f *fakeDriver) Fill(_ context.Context, sid, sel, val string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "fill", []string{sid, sel, val}
	return browser.Result{}, nil
}

func (f *fakeDriver) Close() {}

func TestVerbsDelegateToDriver(t *testing.T) {
	t.Parallel()
	fd := &fakeDriver{}
	a := New("bb-key", "proj-1", false)
	a.driver = fd
	ctx := context.Background()

	if _, err := a.Click(ctx, "s9", "#submit"); err != nil {
		t.Fatal(err)
	}
	if fd.lastVerb != "click" || fd.lastArgs[0] != "s9" || fd.lastArgs[1] != "#submit" {
		t.Errorf("click delegation got %q %v", fd.lastVerb, fd.lastArgs)
	}
	if _, err := a.ExtractText(ctx, "s9"); err != nil {
		t.Fatal(err)
	}
	if fd.lastVerb != "extract_text" {
		t.Errorf("extract_text delegation got %q", fd.lastVerb)
	}
}
