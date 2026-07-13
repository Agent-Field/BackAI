// SPDX-License-Identifier: Apache-2.0

package steel

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
// 1. First verb on a session POSTs /v1/sessions with the steel-api-key
//    header and a JSON body, then attempts a CDP connect against the
//    websocketUrl from the response (apiKey appended).
// 2. When the runtime session dies (here: dial failure) the adapter
//    releases the Steel session: POST /v1/sessions/{id}/release with the
//    steel-api-key header.
// 3. connectURL prefers the response's websocketUrl and falls back to
//    wss://connect.steel.dev?apiKey=..&sessionId=.. when it is absent.
// 4. Unconfigured adapter (empty key): Configured() == false and verbs
//    return browser.ErrNotConfigured without any HTTP traffic.

// fakeCDP is a plain HTTP server standing in for the CDP gateway: it
// records the websocket upgrade attempt and refuses it, so the connect
// fails deterministically without a real browser.
type fakeCDP struct {
	mu   sync.Mutex
	hits []*http.Request
}

func (f *fakeCDP) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Clone(context.Background()))
		f.mu.Unlock()
		http.Error(w, "no browser here", http.StatusBadGateway)
	}
}

func (f *fakeCDP) requests() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.hits...)
}

func TestSessionCreateAndReleaseContract(t *testing.T) {
	cdp := &fakeCDP{}
	cdpSrv := httptest.NewServer(cdp.handler())
	defer cdpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(cdpSrv.URL, "http") + "?sessionId=steel-sess-1"

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
			apiKey:      r.Header.Get("steel-api-key"),
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		})
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":"steel-sess-1","status":"live","websocketUrl":%q}`, wsURL)
		case "/v1/sessions/steel-sess-1/release":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"success":true}`)
		default:
			t.Errorf("unexpected API call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	a := New("test-key", api.URL, true)
	_, err := a.Navigate(context.Background(), "runtime-sess", "https://example.com")
	if err == nil {
		t.Fatal("Navigate succeeded against a fake CDP endpoint; want connect failure")
	}
	a.Close() // waits for the async release triggered by the dial failure

	// REST contract: exactly create + release, in order.
	mu.Lock()
	got := append([]call(nil), calls...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("API calls = %d (%+v), want 2 (create, release)", len(got), got)
	}
	create, release := got[0], got[1]
	if create.method != http.MethodPost || create.path != "/v1/sessions" {
		t.Errorf("create = %s %s, want POST /v1/sessions", create.method, create.path)
	}
	if create.apiKey != "test-key" {
		t.Errorf("create steel-api-key = %q, want %q", create.apiKey, "test-key")
	}
	if !strings.HasPrefix(create.contentType, "application/json") {
		t.Errorf("create Content-Type = %q, want application/json", create.contentType)
	}
	if create.body == nil {
		t.Error("create body is not a JSON object")
	}
	if release.method != http.MethodPost || release.path != "/v1/sessions/steel-sess-1/release" {
		t.Errorf("release = %s %s, want POST /v1/sessions/steel-sess-1/release", release.method, release.path)
	}
	if release.apiKey != "test-key" {
		t.Errorf("release steel-api-key = %q, want %q", release.apiKey, "test-key")
	}

	// Connect attempt: the fake CDP got the upgrade request, with the
	// session id from the create response and the apiKey appended.
	hits := cdp.requests()
	if len(hits) == 0 {
		t.Fatal("no connect attempt reached the fake CDP endpoint")
	}
	q := hits[0].URL.Query()
	if q.Get("sessionId") != "steel-sess-1" {
		t.Errorf("connect sessionId = %q, want steel-sess-1", q.Get("sessionId"))
	}
	if q.Get("apiKey") != "test-key" {
		t.Errorf("connect apiKey = %q, want test-key (appended to websocketUrl)", q.Get("apiKey"))
	}
}

func TestConnectURLFallsBackToHostedGateway(t *testing.T) {
	t.Parallel()
	a := New("k123", "", true)
	got, err := a.connectURL(createSessionResponse{ID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://connect.steel.dev?apiKey=k123&sessionId=abc"
	if got != want {
		t.Errorf("connectURL = %q, want %q", got, want)
	}

	// websocketUrl present: reused, apiKey appended, sessionId kept.
	got, err = a.connectURL(createSessionResponse{ID: "abc", WebsocketURL: "wss://gw.example.com/cdp?sessionId=abc"})
	if err != nil {
		t.Fatal(err)
	}
	want = "wss://gw.example.com/cdp?apiKey=k123&sessionId=abc"
	if got != want {
		t.Errorf("connectURL = %q, want %q", got, want)
	}
}

func TestUnconfiguredAdapter(t *testing.T) {
	t.Parallel()
	a := New("", "", false)
	if a.Configured() {
		t.Error("Configured() = true with empty api key")
	}
	if a.ID() != "steel" {
		t.Errorf("ID() = %q, want steel", a.ID())
	}
	if _, err := a.Navigate(context.Background(), "", "https://example.com"); !errors.Is(err, browser.ErrNotConfigured) {
		t.Errorf("Navigate = %v, want ErrNotConfigured", err)
	}
	if _, err := a.Fill(context.Background(), "", "#x", "v"); !errors.Is(err, browser.ErrNotConfigured) {
		t.Errorf("Fill = %v, want ErrNotConfigured", err)
	}
}

// fakeDriver verifies the adapter delegates verbs (and their arguments)
// to the driver behind the small verbDriver interface.
type fakeDriver struct {
	lastVerb string
	lastArgs []string
}

func (f *fakeDriver) Navigate(_ context.Context, sid, url string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "navigate", []string{sid, url}
	return browser.Result{URL: url, Title: "t", StatusCode: 200}, nil
}

func (f *fakeDriver) ExtractText(_ context.Context, sid string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "extract_text", []string{sid}
	return browser.Result{Text: "body"}, nil
}

func (f *fakeDriver) Screenshot(_ context.Context, sid string) (browser.Result, error) {
	f.lastVerb, f.lastArgs = "screenshot", []string{sid}
	return browser.Result{ScreenshotBase64: "cGpn"}, nil
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
	a := New("key", "", false)
	a.driver = fd
	ctx := context.Background()

	if res, err := a.Navigate(ctx, "s1", "https://x.test"); err != nil || res.StatusCode != 200 {
		t.Fatalf("Navigate = %+v, %v", res, err)
	}
	if fd.lastVerb != "navigate" || fd.lastArgs[0] != "s1" || fd.lastArgs[1] != "https://x.test" {
		t.Errorf("navigate delegation got %q %v", fd.lastVerb, fd.lastArgs)
	}
	if _, err := a.Fill(ctx, "s1", "#in", "hello"); err != nil {
		t.Fatal(err)
	}
	if fd.lastVerb != "fill" || fd.lastArgs[2] != "hello" {
		t.Errorf("fill delegation got %q %v", fd.lastVerb, fd.lastArgs)
	}
}
