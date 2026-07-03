// SPDX-License-Identifier: Apache-2.0

// Package telemetry implements anonymous, opt-out usage telemetry for the
// af-stack CLI.
//
// Design constraints (see TELEMETRY.md):
//
//   - ANONYMOUS. An event carries only: the subcommand name, the CLI version,
//     the OS/arch, success/fail, a coarse duration bucket, a random per-machine
//     id, and a timestamp. It NEVER carries command arguments, flag values,
//     file paths, the working directory, environment values, or any other PII.
//   - OPT-OUT. `--no-telemetry`, or AF_STACK_TELEMETRY in {0,false,off,no},
//     disables it entirely (no network, no first-run notice).
//   - SINK OFF BY DEFAULT. The destination URL is read from
//     AF_STACK_TELEMETRY_URL, falling back to the build-time DefaultURL (set
//     via -ldflags). In the open-source build DefaultURL is empty, so nothing
//     is ever sent unless an operator/distribution sets a URL.
//   - NEVER BLOCKS OR FAILS A COMMAND. Emit posts synchronously with a tight
//     timeout and swallows every error; a command's exit code is never affected
//     by telemetry.
package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultURL is the collection endpoint baked in at build time via
//
//	-ldflags "-X github.com/Agent-Field/backai/services/cli/internal/telemetry.DefaultURL=https://..."
//
// It is empty in the open-source build, which keeps the sink off by default.
// AF_STACK_TELEMETRY_URL overrides it at runtime.
var DefaultURL = ""

// schemaID identifies the event shape so a collector can version it.
const schemaID = "af-stack.cli/v1"

// postTimeout caps how long Emit will wait on the network so telemetry can
// never noticeably slow a command down.
const postTimeout = 1500 * time.Millisecond

// Event is the exact JSON payload sent to the collector. Every field here is
// non-identifying by construction — keep it that way (see TELEMETRY.md).
type Event struct {
	Schema     string `json:"schema"`
	Command    string `json:"command"`
	CLIVersion string `json:"cli_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Success    bool   `json:"success"`
	DurationMs int64  `json:"duration_ms"`
	AnonID     string `json:"anon_id"`
	Timestamp  string `json:"ts"`
}

// Client emits telemetry events. The zero value is not usable; build one with
// New.
type Client struct {
	url        string
	enabled    bool
	cliVersion string
	anonID     string
	httpClient *http.Client
	now        func() time.Time
	stderr     io.Writer
	configDir  string
}

// New builds a telemetry client from the environment and the global
// --no-telemetry flag. It prints a one-time first-run notice to stderr the
// first time telemetry is active on a machine.
//
// optOutFlag is the parsed value of the global --no-telemetry flag.
func New(cliVersion string, optOutFlag bool, stderr io.Writer) *Client {
	c := &Client{
		cliVersion: cliVersion,
		httpClient: &http.Client{Timeout: postTimeout},
		now:        time.Now,
		stderr:     stderr,
		configDir:  configDir(),
	}

	if optOutFlag || envOptOut() {
		// Opted out: stay fully dark — no URL, no notice, no id file read.
		return c
	}

	url := strings.TrimSpace(os.Getenv("AF_STACK_TELEMETRY_URL"))
	if url == "" {
		url = strings.TrimSpace(DefaultURL)
	}
	if url == "" {
		// No sink configured: telemetry is off. Nothing to notice, nothing
		// to send. This is the default open-source posture.
		return c
	}

	c.url = url
	c.enabled = true
	c.anonID = c.loadOrCreateAnonID()
	c.maybePrintFirstRunNotice()
	return c
}

// Emit records one event for a completed command. It is a no-op unless a sink
// is configured and the user has not opted out. It never returns an error and
// never panics; failures are swallowed by design.
func (c *Client) Emit(ctx context.Context, command string, success bool, dur time.Duration) {
	if c == nil || !c.enabled || c.url == "" {
		return
	}
	defer func() { _ = recover() }()

	ev := Event{
		Schema:     schemaID,
		Command:    sanitizeCommand(command),
		CLIVersion: c.cliVersion,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Success:    success,
		DurationMs: bucketDuration(dur),
		AnonID:     c.anonID,
		Timestamp:  c.now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(ev)
	if err != nil {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, postTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "af-stack-cli/"+c.cliVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// sanitizeCommand keeps only a bare subcommand token so an argument can never
// leak in via the command field. Anything unexpected collapses to "unknown".
func sanitizeCommand(command string) string {
	command = strings.TrimSpace(strings.ToLower(command))
	if command == "" {
		return "unknown"
	}
	for _, r := range command {
		if (r < 'a' || r > 'z') && r != '-' {
			return "unknown"
		}
	}
	return command
}

// bucketDuration rounds to a coarse bucket so timing can't be used to
// fingerprint a specific invocation.
func bucketDuration(d time.Duration) int64 {
	ms := d.Milliseconds()
	switch {
	case ms < 0:
		return 0
	case ms < 100:
		return ms / 10 * 10 // nearest 10ms under 100ms
	case ms < 1000:
		return ms / 100 * 100 // nearest 100ms under 1s
	default:
		return ms / 1000 * 1000 // nearest 1s above
	}
}

func envOptOut() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_TELEMETRY"))) {
	case "0", "false", "off", "no":
		return true
	default:
		return false
	}
}

func configDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".af-stack")
	}
	return ""
}

// loadOrCreateAnonID returns a stable random per-machine id, generating and
// persisting one on first use. If the filesystem is unavailable it falls back
// to an ephemeral id rather than failing.
func (c *Client) loadOrCreateAnonID() string {
	if c.configDir == "" {
		return randomID()
	}
	path := filepath.Join(c.configDir, "anonymous_id")
	if raw, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id
		}
	}
	id := randomID()
	if err := os.MkdirAll(c.configDir, 0o700); err == nil {
		_ = os.WriteFile(path, []byte(id+"\n"), 0o600)
	}
	return id
}

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand should never fail; fall back to a time seed so we still
		// emit something non-empty rather than crashing.
		return fmt.Sprintf("anon-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// maybePrintFirstRunNotice prints the telemetry notice once per machine.
func (c *Client) maybePrintFirstRunNotice() {
	if c.configDir == "" || c.stderr == nil {
		return
	}
	marker := filepath.Join(c.configDir, "telemetry-notice")
	if _, err := os.Stat(marker); err == nil {
		return // already shown
	}
	fmt.Fprintln(c.stderr, strings.TrimSpace(`
af-stack collects anonymous usage telemetry (the command name, CLI version, OS,
and success/failure — never arguments, paths, or personal data) to help
prioritize the roadmap. Opt out any time with --no-telemetry or
AF_STACK_TELEMETRY=0. Details: TELEMETRY.md`))
	if err := os.MkdirAll(c.configDir, 0o700); err == nil {
		_ = os.WriteFile(marker, []byte(c.now().UTC().Format(time.RFC3339)+"\n"), 0o600)
	}
}
