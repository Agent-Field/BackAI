package mcp

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
)

// newTestClient points the CLI client at the given test server.
func newTestClient(srv *httptest.Server) *client.Client {
	return &client.Client{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}
}

func TestRun_List_EmptyOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/servers" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ServerList{Servers: nil})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), newTestClient(srv), []string{"list"}, &stdout, &stderr); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No MCP servers configured") {
		t.Fatalf("expected empty hint, got: %s", stdout.String())
	}
}

func TestRun_List_TableOutput(t *testing.T) {
	connected := "2026-06-06T12:00:00Z"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ServerList{
			Servers: []Server{
				{
					Name:            "github",
					Transport:       "stdio",
					Command:         []string{"uvx", "mcp-server-github"},
					IsEnabled:       true,
					Status:          "ready",
					ToolsCount:      12,
					InstalledAt:     "2026-06-06T10:00:00Z",
					LastConnectedAt: &connected,
				},
			},
		})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	if err := Run(context.Background(), newTestClient(srv), []string{"list"}, &stdout, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"NAME", "github", "stdio", "ready", "12", connected} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}

func TestRun_Add_Stdio(t *testing.T) {
	var capturedPath string
	var capturedBody CreateServerInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(Server{
			Name:        capturedBody.Name,
			Transport:   capturedBody.Transport,
			Command:     capturedBody.Command,
			Env:         capturedBody.Env,
			Status:      "connecting",
			InstalledAt: "2026-06-06T10:00:00Z",
		})
	}))
	defer srv.Close()

	args := []string{
		"add", "github",
		"--transport", "stdio",
		"--command", "uvx mcp-server-github",
		"--env", "GITHUB_TOKEN=secret:github_token",
		"--description", "GitHub MCP",
	}
	var stdout bytes.Buffer
	if err := Run(context.Background(), newTestClient(srv), args, &stdout, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if capturedPath != "/api/v1/mcp/servers" {
		t.Fatalf("unexpected path: %s", capturedPath)
	}
	if capturedBody.Name != "github" || capturedBody.Transport != "stdio" {
		t.Fatalf("unexpected body: %+v", capturedBody)
	}
	if len(capturedBody.Command) != 2 || capturedBody.Command[0] != "uvx" {
		t.Fatalf("unexpected command split: %+v", capturedBody.Command)
	}
	if capturedBody.Env["GITHUB_TOKEN"] != "secret:github_token" {
		t.Fatalf("unexpected env: %+v", capturedBody.Env)
	}
	if capturedBody.Description != "GitHub MCP" {
		t.Fatalf("unexpected description: %q", capturedBody.Description)
	}
	if !strings.Contains(stdout.String(), `Added MCP server "github"`) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestRun_Add_SSE_RequiresURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no http call expected when validation fails")
	}))
	defer srv.Close()

	err := Run(context.Background(), newTestClient(srv),
		[]string{"add", "acme", "--transport", "sse"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--url") {
		t.Fatalf("expected --url error, got: %v", err)
	}
}

func TestRun_Add_Stdio_RequiresCommand(t *testing.T) {
	err := Run(context.Background(), &client.Client{},
		[]string{"add", "github", "--transport", "stdio"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--command") {
		t.Fatalf("expected --command error, got: %v", err)
	}
}

func TestRun_Remove(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	if err := Run(context.Background(), newTestClient(srv),
		[]string{"remove", "github"}, &stdout, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if capturedPath != "/api/v1/mcp/servers/github" {
		t.Fatalf("unexpected path: %s", capturedPath)
	}
	if !strings.Contains(stdout.String(), `Removed MCP server "github"`) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestRun_Call_WithJSONArgs(t *testing.T) {
	var capturedBody CallInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(CallResult{
			Content: []map[string]interface{}{
				{"type": "text", "text": "ok"},
			},
			IsError:    false,
			DurationMS: 42,
		})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	if err := Run(context.Background(), newTestClient(srv),
		[]string{"call", "github", "search_repos", "--json", `{"q":"agentfield"}`},
		&stdout, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if capturedBody.Server != "github" || capturedBody.Tool != "search_repos" {
		t.Fatalf("unexpected body: %+v", capturedBody)
	}
	if capturedBody.Arguments["q"] != "agentfield" {
		t.Fatalf("unexpected args: %+v", capturedBody.Arguments)
	}
	out := stdout.String()
	if !strings.Contains(out, `"duration_ms": 42`) || !strings.Contains(out, `"is_error": false`) {
		t.Fatalf("unexpected stdout: %s", out)
	}
}

func TestRun_Call_NoArgs(t *testing.T) {
	var capturedBody CallInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(CallResult{
			Content:    []map[string]interface{}{},
			IsError:    false,
			DurationMS: 5,
		})
	}))
	defer srv.Close()

	if err := Run(context.Background(), newTestClient(srv),
		[]string{"call", "github", "list_repos"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if capturedBody.Arguments != nil {
		t.Fatalf("expected nil arguments; got %+v", capturedBody.Arguments)
	}
}

func TestRun_Call_ErrorFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(CallResult{
			Content:    []map[string]interface{}{{"type": "text", "text": "rate limited"}},
			IsError:    true,
			DurationMS: 100,
		})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), newTestClient(srv),
		[]string{"call", "github", "search_repos", "--json", `{}`}, &stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "is_error=true") {
		t.Fatalf("expected error, got %v", err)
	}
	if !strings.Contains(stdout.String(), "[error]") {
		t.Fatalf("expected [error] tag in stdout, got: %s", stdout.String())
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	err := Run(context.Background(), &client.Client{}, []string{"foo"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("expected unknown-subcommand error, got: %v", err)
	}
}

func TestRun_APIError_PropagatesAsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "MCP_DUPLICATE_NAME",
				"message": "server name already exists",
			},
		})
	}))
	defer srv.Close()

	err := Run(context.Background(), newTestClient(srv),
		[]string{"add", "github", "--transport", "stdio", "--command", "x"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected APIError")
	}
	if !strings.Contains(err.Error(), "MCP_DUPLICATE_NAME") {
		t.Fatalf("expected code in error string, got: %v", err)
	}
}
