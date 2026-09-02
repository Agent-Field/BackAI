// SPDX-License-Identifier: Apache-2.0

// Package starter is the bundled backend of an app scaffolded by
// `af-stack init <name>`: the docker-compose stack that boots BackAI from
// the published release images, plus the pieces `af-stack dev` needs to run
// it there without a checkout — conflict-free host ports, the app's .env,
// and a readiness wait on the runtime.
package starter

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed assets/docker-compose.yml
var composeTemplate string

//go:embed assets/postgres-init.sh
var postgresInit string

//go:embed assets/litellm-config.yaml
var litellmConfig string

// ComposeFile is the compose file `af-stack init` writes into the app.
const ComposeFile = "docker-compose.yml"

// BackendDir holds the support files the compose stack mounts.
const BackendDir = "backend"

const tagPlaceholder = "__AF_STACK_TAG__"

// ImageTag maps the CLI version to the release image tag the bundled stack
// pins: the bare semver for a released binary, "latest" for a development
// build (the "0.0.1" placeholder, "dev", or anything that is not a version).
func ImageTag(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" || v == "0.0.1" || !semverLike.MatchString(v) {
		return "latest"
	}
	return v
}

var semverLike = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// BackendFiles returns the relative-path -> contents map of the bundled
// backend for a CLI of the given version.
func BackendFiles(version string) map[string]string {
	return map[string]string{
		ComposeFile:                         strings.ReplaceAll(composeTemplate, tagPlaceholder, ImageTag(version)),
		BackendDir + "/postgres-init.sh":    postgresInit,
		BackendDir + "/litellm-config.yaml": litellmConfig,
	}
}

// EnsureBackend writes any bundled-backend file missing from root and
// returns the paths it wrote, sorted. Existing files are left alone so an
// app that edited its compose file keeps its edits.
func EnsureBackend(root, version string) ([]string, error) {
	var written []string
	for rel, contents := range BackendFiles(version) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return written, fmt.Errorf("create %s: %w", rel, err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		// #nosec G306 -- compose stack and mounted config, not secrets.
		if err := os.WriteFile(path, []byte(contents), mode); err != nil {
			return written, fmt.Errorf("write %s: %w", rel, err)
		}
		written = append(written, rel)
	}
	sort.Strings(written)
	return written, nil
}

// HasBackend reports whether root carries the bundled compose file.
func HasBackend(root string) bool {
	_, err := os.Stat(filepath.Join(root, ComposeFile))
	return err == nil
}

// Port is one host port the bundled stack publishes.
type Port struct {
	// Service and Target identify the compose service and in-container port,
	// which is how `docker compose port` recognises a binding as ours.
	Service string
	Target  int
	// Env is the .env key that overrides the host port; Default is the
	// documented default.
	Env     string
	Default int
	Label   string
}

// Ports lists every host port the bundled stack publishes, in the order the
// endpoint map prints them.
var Ports = []Port{
	{Service: "runtime", Target: 8080, Env: "AF_STACK_PORT", Default: 8080, Label: "API runtime"},
	{Service: "dashboard", Target: 3000, Env: "AF_STACK_DASHBOARD_PORT", Default: 33000, Label: "Operator dashboard"},
	{Service: "agentfield", Target: 8080, Env: "AGENTFIELD_PORT", Default: 8081, Label: "AgentField UI"},
	{Service: "runtime", Target: 9090, Env: "AF_STACK_METRICS_PORT", Default: 9090, Label: "Metrics"},
	{Service: "litellm", Target: 4000, Env: "LITELLM_PORT", Default: 4000, Label: "LiteLLM"},
	{Service: "minio", Target: 9000, Env: "MINIO_PORT", Default: 9000, Label: "MinIO API"},
	{Service: "minio", Target: 9001, Env: "MINIO_CONSOLE_PORT", Default: 9001, Label: "MinIO console"},
	{Service: "postgres", Target: 5432, Env: "POSTGRES_PORT", Default: 5432, Label: "Postgres"},
}

// OwnedPortFunc reports whether the compose project in root already binds
// host port p for the given service/target — i.e. the port is busy because
// OUR stack is running, which is not a conflict.
type OwnedPortFunc func(root string, p Port, hostPort int) bool

// ComposeOwnsPort is the OwnedPortFunc backed by `docker compose port`.
func ComposeOwnsPort(root string, p Port, hostPort int) bool {
	// #nosec G204 -- service and target come from the fixed Ports table above.
	cmd := exec.Command("docker", "compose", "port", p.Service, strconv.Itoa(p.Target))
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	suffix := ":" + strconv.Itoa(hostPort)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), suffix) {
			return true
		}
	}
	return false
}

// Allocation is the outcome of AllocatePorts.
type Allocation struct {
	// Resolved maps each .env key to the host port the stack will bind.
	Resolved map[string]int
	// Moved lists the ports that had to change, in Ports order.
	Moved []Move
	// Project is the compose project name.
	Project string
}

// Move records one reallocated port.
type Move struct {
	Port Port
	From int
	To   int
}

// AllocatePorts is the Go counterpart of the checkout's preflight --fix:
// for every published port it keeps the configured value when it is free
// or already bound by this compose project, and otherwise probes upward
// for the next free host port. Changes are written to <root>/.env (seeded
// from .env.example when .env does not exist) together with a stable
// COMPOSE_PROJECT_NAME derived from the directory name.
func AllocatePorts(root string, owned OwnedPortFunc) (*Allocation, error) {
	env, err := ReadEnv(root)
	if err != nil {
		return nil, err
	}
	alloc := &Allocation{Resolved: map[string]int{}}
	overrides := map[string]string{}
	claimed := map[int]bool{}

	for _, p := range Ports {
		want := p.Default
		if v := strings.TrimSpace(env[p.Env]); v != "" {
			n, convErr := strconv.Atoi(v)
			if convErr != nil || n <= 0 || n > 65535 {
				return nil, fmt.Errorf("%s=%q in .env is not a valid port", p.Env, v)
			}
			want = n
		}
		free := !claimed[want] && canBind(want)
		if free || (!claimed[want] && owned != nil && owned(root, p, want)) {
			claimed[want] = true
			alloc.Resolved[p.Env] = want
			continue
		}
		next, findErr := nextFreePort(want+1, claimed)
		if findErr != nil {
			return nil, findErr
		}
		claimed[next] = true
		alloc.Resolved[p.Env] = next
		alloc.Moved = append(alloc.Moved, Move{Port: p, From: want, To: next})
		overrides[p.Env] = strconv.Itoa(next)
	}

	alloc.Project = strings.TrimSpace(env["COMPOSE_PROJECT_NAME"])
	if alloc.Project == "" {
		alloc.Project = ProjectName(root)
		overrides["COMPOSE_PROJECT_NAME"] = alloc.Project
	}
	if len(overrides) > 0 {
		if err := SetEnv(root, overrides, "# Auto-allocated by `af-stack dev` to avoid port conflicts."); err != nil {
			return nil, err
		}
	}
	return alloc, nil
}

// ProjectName is the compose project name for an app directory: its base
// name lowercased and slugged, "backai" when nothing survives.
func ProjectName(root string) string {
	base := strings.ToLower(filepath.Base(root))
	var b strings.Builder
	lastDash := true
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "backai"
	}
	return slug
}

func canBind(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func nextFreePort(start int, claimed map[int]bool) (int, error) {
	for p := start; p <= 65535; p++ {
		if !claimed[p] && canBind(p) {
			return p, nil
		}
	}
	return 0, errors.New("no free host port found")
}

// ReadEnv parses <root>/.env into a map. A missing file is an empty map.
// Values from the process environment win, as they do for docker compose.
func ReadEnv(root string) (map[string]string, error) {
	values := map[string]string{}
	f, err := os.Open(filepath.Join(root, ".env"))
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for k := range values {
		if v, ok := os.LookupEnv(k); ok {
			values[k] = v
		}
	}
	return values, nil
}

// SetEnv writes key=value pairs into <root>/.env, replacing existing lines
// for those keys and appending the rest under comment. When .env does not
// exist it is seeded from .env.example so the documented keys survive.
func SetEnv(root string, values map[string]string, comment string) error {
	envPath := filepath.Join(root, ".env")
	var lines []string
	data, err := os.ReadFile(envPath)
	switch {
	case err == nil:
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	case os.IsNotExist(err):
		if ex, exErr := os.ReadFile(filepath.Join(root, ".env.example")); exErr == nil {
			lines = strings.Split(strings.TrimRight(string(ex), "\n"), "\n")
		}
	default:
		return err
	}
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	remaining := map[string]string{}
	for k, v := range values {
		remaining[k] = v
	}
	for i, line := range lines {
		m := envKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if v, ok := remaining[m[1]]; ok {
			lines[i] = m[1] + "=" + v
			delete(remaining, m[1])
		}
	}
	if len(remaining) > 0 {
		keys := make([]string, 0, len(remaining))
		for k := range remaining {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		if comment != "" {
			lines = append(lines, comment)
		}
		for _, k := range keys {
			lines = append(lines, k+"="+remaining[k])
		}
	}
	// #nosec G306 G703 -- <root>/.env of the app the user is running in; local dev config, not secrets.
	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

var envKeyLine = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=`)

// WaitReady polls <baseURL>/ready until it answers 200, printing one dot
// per poll to progress. It returns the last status seen when timeout
// elapses so the caller can say what the runtime was doing.
func WaitReady(ctx context.Context, baseURL string, timeout time.Duration, progress io.Writer) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	var last string
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/ready", nil)
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Sprintf("GET /ready -> %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else {
			last = err.Error()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("runtime at %s did not become ready within %s (last: %s)", baseURL, timeout, last)
		}
		if progress != nil {
			fmt.Fprint(progress, ".")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// WaitAgent polls <baseURL>/api/v1/agents until an agent with nodeID is
// registered, or timeout elapses. Registration lags the runtime by a few
// seconds, and the demo call the starter makes needs it. A timeout is not
// an error for the caller: it returns false and the app reports the state.
func WaitAgent(ctx context.Context, baseURL, nodeID string, timeout time.Duration, progress io.Writer) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/agents", nil)
		resp, err := client.Do(req)
		if err == nil {
			var payload struct {
				Agents []struct {
					NodeID string `json:"node_id"`
				} `json:"agents"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload)
			_ = resp.Body.Close()
			if decodeErr == nil {
				for _, a := range payload.Agents {
					if a.NodeID == nodeID {
						return true
					}
				}
			}
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		if progress != nil {
			fmt.Fprint(progress, ".")
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}
