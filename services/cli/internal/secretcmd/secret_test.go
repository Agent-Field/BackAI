// SPDX-License-Identifier: Apache-2.0

package secretcmd

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

// Contract: `secrets set <key> --value-stdin` PUTs {value:...} to
// /vault/secrets/<key>, sending the value in the body (never argv), and
// prints the reference back.
func TestSecretsSet_WireBody(t *testing.T) {
	var got map[string]any
	var gotPath, gotMethod string
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": "stripe", "reference": "secret:stripe", "updated_at": "2026-07-20T00:00:00Z",
		})
	})
	defer done()

	var out bytes.Buffer
	err := runSet(context.Background(), c, []string{"stripe", "--value-stdin"},
		strings.NewReader("sk_test_123\n"), &out, io.Discard)
	if err != nil {
		t.Fatalf("runSet: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/v1/vault/secrets/stripe" {
		t.Fatalf("expected PUT /api/v1/vault/secrets/stripe, got %s %s", gotMethod, gotPath)
	}
	if got["value"] != "sk_test_123" {
		t.Errorf("value not sent (or not trimmed): %#v", got["value"])
	}
	if !strings.Contains(out.String(), "secret:stripe") {
		t.Errorf("expected the reference in output, got:\n%s", out.String())
	}
}

// Contract: an empty value is a validation error and never hits the network.
func TestSecretsSet_EmptyValue(t *testing.T) {
	called := false
	c, done := testClient(t, func(_ http.ResponseWriter, _ *http.Request) { called = true })
	defer done()
	err := runSet(context.Background(), c, []string{"stripe", "--value-stdin"},
		strings.NewReader(""), io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitValidation {
		t.Fatalf("empty-value exit = %d, want %d (err=%v)", code, output.ExitValidation, err)
	}
	if called {
		t.Error("must not PUT an empty secret value")
	}
}

// Contract: `secrets set` without a key is a usage error (exit 2).
func TestSecretsSet_RequiresKey(t *testing.T) {
	c := &client.Client{BaseURL: "http://127.0.0.1:0", HTTP: http.DefaultClient}
	err := runSet(context.Background(), c, []string{"--value-stdin"}, strings.NewReader("x"), io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitUsage {
		t.Fatalf("no-key exit = %d, want %d (err=%v)", code, output.ExitUsage, err)
	}
}

// Contract: `secrets list --json` emits {secrets:[...]} of metadata with NO
// plaintext value field.
func TestSecretsList_JSON_NoValues(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/vault/secrets" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{
			{"key": "stripe", "reference": "secret:stripe", "updated_at": "2026-07-20T00:00:00Z", "description": nil},
		}})
	})
	defer done()
	var out bytes.Buffer
	if err := runList(context.Background(), c, []string{"--json"}, &out, io.Discard); err != nil {
		t.Fatalf("runList: %v", err)
	}
	var got struct {
		Secrets []secretMetadata `json:"secrets"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json not valid JSON: %v (%s)", err, out.String())
	}
	if len(got.Secrets) != 1 || got.Secrets[0].Key != "stripe" {
		t.Fatalf("secrets = %#v", got.Secrets)
	}
	if strings.Contains(out.String(), `"value"`) {
		t.Error("secret list must never carry a plaintext value field")
	}
}

// Contract: a runtime 503 (vault not configured) maps to the remote exit.
func TestSecretsList_VaultUnconfigured(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "SECRETS_NOT_CONFIGURED", "message": "no vault"}})
	})
	defer done()
	err := runList(context.Background(), c, nil, io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitRemote {
		t.Fatalf("503 exit = %d, want %d (err=%v)", code, output.ExitRemote, err)
	}
}

// Contract: the dispatcher rejects an unknown subcommand (exit 2).
func TestSecretsRun_UnknownSubcommand(t *testing.T) {
	c := &client.Client{BaseURL: "http://127.0.0.1:0", HTTP: http.DefaultClient}
	err := Run(context.Background(), c, []string{"reveal"}, strings.NewReader(""), io.Discard, io.Discard)
	if code := output.ExitCode(err); code != output.ExitUsage {
		t.Fatalf("unknown-subcommand exit = %d, want %d", code, output.ExitUsage)
	}
}
