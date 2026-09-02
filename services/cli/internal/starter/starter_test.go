// SPDX-License-Identifier: Apache-2.0

package starter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Contract: the support files the bundled stack mounts are the repo's own
// (the compose mounts them at the same paths the checkout stack uses), so
// they cannot drift from what the runtime and LiteLLM are tested against.
func TestBundledSupportFilesMatchRepo(t *testing.T) {
	repo := filepath.Join("..", "..", "..", "..")
	for asset, canonical := range map[string]string{
		"postgres-init.sh":    filepath.Join(repo, "scripts", "postgres-init.sh"),
		"litellm-config.yaml": filepath.Join(repo, "apps", "backend", "litellm-config.yaml"),
	} {
		want, err := os.ReadFile(canonical)
		if err != nil {
			t.Fatalf("read %s: %v", canonical, err)
		}
		got := BackendFiles("1.2.3")[BackendDir+"/"+asset]
		if got != string(want) {
			t.Errorf("assets/%s drifted from %s — copy it over", asset, canonical)
		}
	}
}

// Contract: a released CLI pins the release images to its own version; a
// development build pins :latest.
func TestImageTagFollowsCLIVersion(t *testing.T) {
	cases := map[string]string{
		"0.12.8": "0.12.8", "v0.12.8": "0.12.8", "1.0.0-rc.1": "1.0.0-rc.1",
		"0.0.1": "latest", "": "latest", "dev": "latest", "abc123": "latest",
	}
	for in, want := range cases {
		if got := ImageTag(in); got != want {
			t.Errorf("ImageTag(%q) = %q, want %q", in, got, want)
		}
	}
	compose := BackendFiles("0.12.8")[ComposeFile]
	for _, img := range []string{
		"ghcr.io/agent-field/af-stack-runtime:${AF_STACK_VERSION:-0.12.8}",
		"ghcr.io/agent-field/af-stack-dashboard:${AF_STACK_VERSION:-0.12.8}",
		"ghcr.io/agent-field/af-stack-supportdesk-agent:${AF_STACK_VERSION:-0.12.8}",
	} {
		if !strings.Contains(compose, img) {
			t.Errorf("compose does not pin %s", img)
		}
	}
	if strings.Contains(compose, tagPlaceholder) {
		t.Error("compose still contains the tag placeholder")
	}
}

// Contract: the compose file is valid YAML, publishes every port in Ports
// through its .env key, and mounts the support files EnsureBackend writes.
func TestComposeStackIsCoherent(t *testing.T) {
	compose := BackendFiles("0.12.8")[ComposeFile]
	var doc struct {
		Services map[string]struct {
			Image   string   `yaml:"image"`
			Ports   []string `yaml:"ports"`
			Volumes []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(compose), &doc); err != nil {
		t.Fatalf("compose is not valid YAML: %v", err)
	}
	for _, p := range Ports {
		svc, ok := doc.Services[p.Service]
		if !ok {
			t.Fatalf("Ports names service %q which the compose file lacks", p.Service)
		}
		want := "${" + p.Env + ":-" + strconv.Itoa(p.Default) + "}:" + strconv.Itoa(p.Target)
		found := false
		for _, binding := range svc.Ports {
			if binding == want {
				found = true
			}
		}
		if !found {
			t.Errorf("service %s does not publish %s (have %v)", p.Service, want, svc.Ports)
		}
	}
	for svc, s := range doc.Services {
		if s.Image == "" {
			t.Errorf("service %s has no image: the bundled stack must never need a source build", svc)
		}
	}
	for _, mount := range []string{"./backend/postgres-init.sh:", "./backend/litellm-config.yaml:"} {
		if !strings.Contains(compose, mount) {
			t.Errorf("compose does not mount %s", mount)
		}
	}
}

// Contract: EnsureBackend fills in what is missing and leaves edited files
// alone, so an app that customised its compose file survives `dev`.
func TestEnsureBackendIsIdempotentAndNonDestructive(t *testing.T) {
	root := t.TempDir()
	written, err := EnsureBackend(root, "0.12.8")
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 3 || !HasBackend(root) {
		t.Fatalf("first EnsureBackend wrote %v", written)
	}
	custom := "services: {}\n# customised\n"
	if err := os.WriteFile(filepath.Join(root, ComposeFile), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, BackendDir, "postgres-init.sh")); err != nil {
		t.Fatal(err)
	}
	written, err = EnsureBackend(root, "0.12.8")
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != BackendDir+"/postgres-init.sh" {
		t.Fatalf("second EnsureBackend wrote %v, want only the removed file", written)
	}
	got, _ := os.ReadFile(filepath.Join(root, ComposeFile))
	if string(got) != custom {
		t.Fatal("EnsureBackend overwrote a customised compose file")
	}
	info, _ := os.Stat(filepath.Join(root, BackendDir, "postgres-init.sh"))
	if info.Mode()&0o111 == 0 {
		t.Fatal("postgres-init.sh must be executable for the postgres entrypoint")
	}
}

// Contract: a busy default port moves to the next free one and lands in
// .env; a free port stays; a port bound by our own compose project stays
// even though it is busy; COMPOSE_PROJECT_NAME is derived from the dir.
func TestAllocatePortsMovesOnlyForeignConflicts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "My App!")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pin every port to one this test controls so the machine's own
	// services (a local Postgres on 5432, say) cannot skew the outcome, then
	// occupy two of them: one "foreign", one "ours".
	foreign := listen(t)
	ours := listen(t)
	var env strings.Builder
	for _, p := range Ports {
		switch p.Env {
		case "AF_STACK_PORT":
			fmt.Fprintf(&env, "%s=%d\n", p.Env, foreign)
		case "AGENTFIELD_PORT":
			fmt.Fprintf(&env, "%s=%d\n", p.Env, ours)
		default:
			fmt.Fprintf(&env, "%s=%d\n", p.Env, freePort(t))
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(env.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	owned := func(_ string, p Port, hostPort int) bool {
		return p.Env == "AGENTFIELD_PORT" && hostPort == ours
	}
	alloc, err := AllocatePorts(root, owned)
	if err != nil {
		t.Fatal(err)
	}
	if alloc.Resolved["AF_STACK_PORT"] == foreign {
		t.Fatalf("busy foreign port %d was kept", foreign)
	}
	if alloc.Resolved["AGENTFIELD_PORT"] != ours {
		t.Fatalf("port bound by our own compose project was moved: %d -> %d", ours, alloc.Resolved["AGENTFIELD_PORT"])
	}
	if len(alloc.Moved) != 1 || alloc.Moved[0].Port.Env != "AF_STACK_PORT" {
		t.Fatalf("moved = %+v, want exactly AF_STACK_PORT", alloc.Moved)
	}
	if alloc.Project != "my-app" {
		t.Fatalf("project = %q, want my-app", alloc.Project)
	}
	seen := map[int]bool{}
	for k, v := range alloc.Resolved {
		if seen[v] {
			t.Fatalf("two services resolved to the same host port %d (%s)", v, k)
		}
		seen[v] = true
	}
	got, _ := ReadEnv(root)
	if got["AF_STACK_PORT"] != strconv.Itoa(alloc.Resolved["AF_STACK_PORT"]) {
		t.Fatalf(".env AF_STACK_PORT = %q, want %d", got["AF_STACK_PORT"], alloc.Resolved["AF_STACK_PORT"])
	}
	if got["AGENTFIELD_PORT"] != strconv.Itoa(ours) || got["COMPOSE_PROJECT_NAME"] != "my-app" {
		t.Fatalf(".env after allocation = %v", got)
	}
}

// Contract: SetEnv replaces existing keys in place, appends new ones once,
// and seeds a missing .env from .env.example so documented keys survive.
func TestSetEnvSeedsFromExampleAndReplacesInPlace(t *testing.T) {
	root := t.TempDir()
	example := "# url\nAF_STACK_URL=http://localhost:8080\nAF_STACK_API_KEY=\n"
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetEnv(root, map[string]string{"AF_STACK_URL": "http://localhost:8082", "COMPOSE_PROJECT_NAME": "x"}, "# added"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".env"))
	got := string(data)
	want := "# url\nAF_STACK_URL=http://localhost:8082\nAF_STACK_API_KEY=\n\n# added\nCOMPOSE_PROJECT_NAME=x\n"
	if got != want {
		t.Fatalf(".env =\n%s\nwant\n%s", got, want)
	}
	// Second write: in place, no duplicate lines.
	if err := SetEnv(root, map[string]string{"AF_STACK_URL": "http://localhost:8083", "COMPOSE_PROJECT_NAME": "y"}, "# added"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(root, ".env"))
	if strings.Count(string(data), "AF_STACK_URL=") != 1 || strings.Count(string(data), "COMPOSE_PROJECT_NAME=") != 1 {
		t.Fatalf("duplicate keys after second SetEnv:\n%s", data)
	}
	if !strings.Contains(string(data), "COMPOSE_PROJECT_NAME=y") {
		t.Fatalf("value not replaced:\n%s", data)
	}
}

// Contract: WaitReady returns once /ready answers 200 and reports the last
// status when it never does; WaitAgent recognises the runtime's
// {"agents":[{"node_id":...}]} shape.
func TestWaitReadyAndWaitAgent(t *testing.T) {
	var readyAfter, listAfter int
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		if readyAfter++; readyAfter < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"booting"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("/api/v1/agents", func(w http.ResponseWriter, _ *http.Request) {
		if listAfter++; listAfter < 2 {
			_, _ = w.Write([]byte(`{"agents":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"agents":[{"node_id":"supportdesk","reasoners":["echo"]}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	if err := WaitReady(ctx, srv.URL, 10*time.Second, nil); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if !WaitAgent(ctx, srv.URL, "supportdesk", 10*time.Second, nil) {
		t.Fatal("WaitAgent did not see the registered agent")
	}

	never := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"db_unavailable"}`))
	}))
	defer never.Close()
	err := WaitReady(ctx, never.URL, 10*time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "db_unavailable") {
		t.Fatalf("WaitReady timeout error should carry the last status, got: %v", err)
	}
	if WaitAgent(ctx, never.URL, "supportdesk", 10*time.Millisecond, nil) {
		t.Fatal("WaitAgent should give up on a runtime that never lists the agent")
	}
}

// freePort returns a port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func listen(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l.Addr().(*net.TCPAddr).Port
}
