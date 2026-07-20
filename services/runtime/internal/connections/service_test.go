// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixedNow() func() time.Time {
	t := time.Unix(1_700_000_000, 0)
	return func() time.Time { return t }
}

// testRegistry builds a one-provider registry pointing at the given base API
// and token endpoints, so provider calls hit local httptest servers.
func testRegistry(baseURL, tokenURL string) *Registry {
	return &Registry{byName: map[string]Descriptor{
		"acme": {
			Name:              "acme",
			Kind:              KindOAuth,
			BaseURL:           baseURL,
			AuthHeaderName:    "Authorization",
			AuthValuePrefix:   "Bearer ",
			AuthorizeEndpoint: "https://acme.test/authorize",
			TokenEndpoint:     tokenURL,
			ScopeSeparator:    " ",
		},
	}}
}

// Contract: Handle injects the connection's credential as the provider auth
// header (app code never supplies it), AND the raw credential never appears
// in the runtime logs.
func TestHandleInjectsAuthAndStripsFromLogs(t *testing.T) {
	const secretToken = "sk_live_SUPERSECRET_do_not_log_me"

	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// Also assert the app can't override auth: it tried to send its own.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	store := newFakeStore()
	store.seed("t1", Connection{
		ID: "c1", Provider: "acme", Kind: KindAPIKey, Status: StatusActive,
	}, credential{APIKey: secretToken}, "")

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	svc, err := New(Config{
		Store:      store,
		Registry:   testRegistry(api.URL, ""),
		HTTPClient: api.Client(),
		Log:        logger,
		Now:        fixedNow(),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	res, err := svc.Handle(context.Background(), "t1", "c1", HandleRequest{
		Method: "GET",
		Path:   "/user",
		// App attempts to smuggle its own Authorization — must be ignored.
		Headers: map[string]string{"Authorization": "Bearer attacker", "Accept": "application/json"},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d", res.Status)
	}
	if gotAuth != "Bearer "+secretToken {
		t.Fatalf("provider auth header = %q, want injected credential", gotAuth)
	}

	logs := logBuf.String()
	if strings.Contains(logs, secretToken) {
		t.Fatalf("credential leaked into logs:\n%s", logs)
	}
	if !strings.Contains(logs, "[REDACTED]") {
		t.Fatalf("expected a redaction marker in logs:\n%s", logs)
	}
}

// Contract: when an OAuth connection's access token is expired, N concurrent
// Handle calls trigger EXACTLY ONE refresh at the provider (single-flight),
// and every call proceeds with the refreshed token.
func TestHandleRefreshSingleFlight(t *testing.T) {
	var refreshCalls int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		// Hold the flight open so concurrent callers coalesce onto it.
		time.Sleep(40 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-access","refresh_token":"refreshed-refresh","expires_in":3600,"scope":"repo"}`))
	}))
	defer tokenSrv.Close()

	var mu sync.Mutex
	seenAuth := map[string]int{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenAuth[r.Header.Get("Authorization")]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	now := time.Unix(1_700_000_000, 0)
	store := newFakeStore()
	expired := now.Add(-time.Hour)
	store.seed("t1", Connection{
		ID: "c1", Provider: "acme", Kind: KindOAuth, Status: StatusActive,
		TokenExpiry: &expired, GrantedScopes: []string{"repo"},
	}, credential{AccessToken: "stale-access", RefreshToken: "stale-refresh"}, "")

	svc, err := New(Config{
		Store:      store,
		Registry:   testRegistry(api.URL, tokenSrv.URL),
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		ClientCreds: ClientCredsFunc(func(_ context.Context, _ string) (ClientCreds, bool) {
			return ClientCreds{ClientID: "cid", ClientSecret: "csec"}, true
		}),
		Log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	const n = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_, e := svc.Handle(context.Background(), "t1", "c1", HandleRequest{Method: "GET", Path: "/repos"})
			errs[idx] = e
		}(i)
	}
	close(start)
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("handle[%d] failed: %v", i, e)
		}
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("expected exactly 1 refresh call, got %d", got)
	}
	if saves := store.countSaves(); saves != 1 {
		t.Fatalf("expected exactly 1 SaveTokens, got %d", saves)
	}
	mu.Lock()
	defer mu.Unlock()
	if seenAuth["Bearer refreshed-access"] != n {
		t.Fatalf("expected all %d provider calls to use the refreshed token, saw %v", n, seenAuth)
	}
	// A refreshed audit event must have been emitted (exactly once).
	refreshed := 0
	for _, ev := range store.eventTypes() {
		if ev == "refreshed" {
			refreshed++
		}
	}
	if refreshed != 1 {
		t.Fatalf("expected 1 refreshed event, got %d (%v)", refreshed, store.eventTypes())
	}
}

// Contract: using a revoked connection through the HANDLE returns ErrRevoked
// (the handler maps this to 409 CONNECTION_REVOKED).
func TestHandleRevokedConnection(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("provider must not be called for a revoked connection")
	}))
	defer api.Close()

	store := newFakeStore()
	store.seed("t1", Connection{
		ID: "c1", Provider: "acme", Kind: KindAPIKey, Status: StatusRevoked,
	}, credential{APIKey: "sk_dead"}, "")

	svc, err := New(Config{
		Store:      store,
		Registry:   testRegistry(api.URL, ""),
		HTTPClient: api.Client(),
		Log:        slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Now:        fixedNow(),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.Handle(context.Background(), "t1", "c1", HandleRequest{Method: "GET", Path: "/x"})
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
}

// Contract: the HANDLE refuses a path that carries a scheme/host, so app code
// can't retarget the injected credential at an arbitrary server (SSRF /
// credential-theft guard).
func TestHandleRejectsAbsolutePath(t *testing.T) {
	store := newFakeStore()
	store.seed("t1", Connection{ID: "c1", Provider: "acme", Kind: KindAPIKey, Status: StatusActive},
		credential{APIKey: "sk_live"}, "")

	svc, err := New(Config{
		Store:      store,
		Registry:   testRegistry("https://api.acme.test", ""),
		HTTPClient: &http.Client{},
		Log:        slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Now:        fixedNow(),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.Handle(context.Background(), "t1", "c1", HandleRequest{
		Method: "GET", Path: "https://evil.example.com/steal",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for absolute path, got %v", err)
	}
}

// Contract: CreateAPIKey stores the connection, emits a "created" event, and
// the returned Connection carries no credential material.
func TestCreateAPIKeyEmitsCreatedEvent(t *testing.T) {
	store := newFakeStore()
	svc, err := New(Config{
		Store:      store,
		Registry:   DefaultRegistry(),
		HTTPClient: &http.Client{},
		Log:        slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Now:        fixedNow(),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	conn, err := svc.CreateAPIKey(context.Background(), "t1", CreateAPIKeyParams{
		Provider: "stripe", Name: "prod", APIKey: "sk_live_123",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if conn.Kind != KindAPIKey || conn.Provider != "stripe" {
		t.Fatalf("unexpected connection: %+v", conn)
	}
	if conn.Health != HealthOK {
		t.Fatalf("expected health ok, got %q", conn.Health)
	}
	found := false
	for _, ev := range store.eventTypes() {
		if ev == "created" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a created event, got %v", store.eventTypes())
	}
}

// Contract: creating an api_key connection with no credential is rejected.
func TestCreateAPIKeyRequiresCredential(t *testing.T) {
	svc, err := New(Config{
		Store:      newFakeStore(),
		Registry:   DefaultRegistry(),
		HTTPClient: &http.Client{},
		Log:        slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Now:        fixedNow(),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.CreateAPIKey(context.Background(), "t1", CreateAPIKeyParams{Provider: "stripe", APIKey: "  "}); !errors.Is(err, ErrCredentialRequired) {
		t.Fatalf("expected ErrCredentialRequired, got %v", err)
	}
}
