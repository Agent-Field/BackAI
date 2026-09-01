// SPDX-License-Identifier: Apache-2.0

package conncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/output"
)

func testClient(t *testing.T, h http.HandlerFunc) (*client.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	return &client.Client{BaseURL: srv.URL, HTTP: srv.Client()}, srv.Close
}

// Contract: `connection add --kind api_key --credential-stdin` sends the
// secret in the api_key body field (never as an argv), with provider/kind.
func TestConnectionAdd_APIKey_WireBody(t *testing.T) {
	var got map[string]any
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/connections" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c-1", "provider": "github", "kind": "api_key", "health": "ok",
		})
	})
	defer done()

	stdin := strings.NewReader("ghp_secret_token\n")
	var out bytes.Buffer
	err := runAdd(context.Background(), c,
		[]string{"--provider", "github", "--kind", "api_key", "--name", "ci", "--credential-stdin"},
		stdin, &out, io.Discard)
	if err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	if got["provider"] != "github" || got["kind"] != "api_key" || got["name"] != "ci" {
		t.Errorf("wire body wrong: %#v", got)
	}
	if got["api_key"] != "ghp_secret_token" {
		t.Errorf("credential not sent as api_key (or not trimmed): %#v", got["api_key"])
	}
	if !strings.Contains(out.String(), "c-1") {
		t.Errorf("expected the new connection id in output, got:\n%s", out.String())
	}
}

// Contract: api_key with an empty credential is a validation error and never
// hits the network.
func TestConnectionAdd_APIKey_EmptyCredential(t *testing.T) {
	called := false
	c, done := testClient(t, func(_ http.ResponseWriter, _ *http.Request) { called = true })
	defer done()
	err := runAdd(context.Background(), c,
		[]string{"--provider", "github", "--kind", "api_key", "--credential-stdin"},
		strings.NewReader(""), io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitValidation {
		t.Fatalf("empty-credential exit = %d, want %d (err=%v)", code, output.ExitValidation, err)
	}
	if called {
		t.Error("must not call the runtime with an empty credential")
	}
}

// Contract: `connection add --kind oauth` prints the authorize URL returned
// by the runtime (no credential involved).
func TestConnectionAdd_OAuth_PrintsURL(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "google", "kind": "oauth",
			"authorization_url": "https://accounts.google.com/o/oauth2/auth?x=1",
			"state":             "signed-state", "redirect_uri": "http://localhost:8080/connections/callback/google",
		})
	})
	defer done()
	var out bytes.Buffer
	err := runAdd(context.Background(), c,
		[]string{"--provider", "google", "--kind", "oauth", "--name", "gcal"},
		strings.NewReader(""), &out, io.Discard)
	if err != nil {
		t.Fatalf("runAdd oauth: %v", err)
	}
	if !strings.Contains(out.String(), "https://accounts.google.com/o/oauth2/auth?x=1") {
		t.Errorf("authorize URL not printed, got:\n%s", out.String())
	}
}

// Contract: add validates --provider and --kind before any network call.
func TestConnectionAdd_Validation(t *testing.T) {
	c := &client.Client{BaseURL: "http://127.0.0.1:0", HTTP: http.DefaultClient}
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no provider", []string{"--kind", "api_key"}, output.ExitUsage},
		{"no kind", []string{"--provider", "github"}, output.ExitUsage},
		{"bad kind", []string{"--provider", "github", "--kind", "sshkey"}, output.ExitUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runAdd(context.Background(), c, tc.args, strings.NewReader(""), io.Discard, io.Discard)
			if code := output.ExitCode(err); code != tc.want {
				t.Fatalf("exit = %d, want %d (err=%v)", code, tc.want, err)
			}
		})
	}
}

// Contract: `connection list --json` emits {connections:[...]} with metadata
// only (there is no credential field on the wire to leak).
func TestConnectionList_JSON(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/connections" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"connections": []map[string]any{
			{"id": "c-1", "provider": "github", "kind": "api_key", "name": "ci", "status": "active", "health": "ok"},
		}})
	})
	defer done()
	var out bytes.Buffer
	if err := runList(context.Background(), c, []string{"--json"}, &out, io.Discard); err != nil {
		t.Fatalf("runList: %v", err)
	}
	var got struct {
		Connections []connection `json:"connections"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json not valid JSON: %v (%s)", err, out.String())
	}
	if len(got.Connections) != 1 || got.Connections[0].ID != "c-1" {
		t.Fatalf("connections = %#v", got.Connections)
	}
	// The list surface must never carry a raw credential/value field.
	for _, forbidden := range []string{`"credential"`, `"value"`, `"api_key":`, `"secret":`} {
		if strings.Contains(out.String(), forbidden) {
			t.Errorf("list output must not contain %s, got:\n%s", forbidden, out.String())
		}
	}
}

// Contract: `remove <id>` without --yes and a declining stdin aborts and
// never calls DELETE.
func TestConnectionRemove_AbortsWithoutYes(t *testing.T) {
	called := false
	c, done := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	defer done()
	var out bytes.Buffer
	err := runRemove(context.Background(), c, []string{"c-1"}, strings.NewReader("n\n"), &out, io.Discard)
	if err != nil {
		t.Fatalf("declining should not be an error: %v", err)
	}
	if called {
		t.Error("DELETE must not be called when the user declines")
	}
	if !strings.Contains(out.String(), "aborted") {
		t.Errorf("expected 'aborted', got:\n%s", out.String())
	}
}

// Contract: `remove <id> --yes` calls DELETE /connections/{id}.
func TestConnectionRemove_YesCallsDelete(t *testing.T) {
	var path, method string
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_ = json.NewEncoder(w).Encode(map[string]bool{"revoked": true})
	})
	defer done()
	var out bytes.Buffer
	if err := runRemove(context.Background(), c, []string{"c-1", "--yes"}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatalf("runRemove --yes: %v", err)
	}
	if method != http.MethodDelete || path != "/api/v1/connections/c-1" {
		t.Fatalf("expected DELETE /api/v1/connections/c-1, got %s %s", method, path)
	}
	if !strings.Contains(out.String(), "removed connection c-1") {
		t.Errorf("expected success line, got:\n%s", out.String())
	}
}

// Contract: a runtime 404 on remove surfaces as the not-found exit code.
func TestConnectionRemove_NotFoundExit(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "CONNECTION_NOT_FOUND", "message": "not found"}})
	})
	defer done()
	err := runRemove(context.Background(), c, []string{"c-x", "--yes"}, strings.NewReader(""), io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitNotFound {
		t.Fatalf("404 exit = %d, want %d (err=%v)", code, output.ExitNotFound, err)
	}
}

// Contract: the dispatcher rejects an unknown subcommand (exit 2).
func TestConnectionRun_UnknownSubcommand(t *testing.T) {
	c := &client.Client{BaseURL: "http://127.0.0.1:0", HTTP: http.DefaultClient}
	err := Run(context.Background(), c, []string{"frobnicate"}, strings.NewReader(""), io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitUsage {
		t.Fatalf("unknown-subcommand exit = %d, want %d", code, output.ExitUsage)
	}
}
