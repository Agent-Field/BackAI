// prober.go — the runtime side of harness detection.
//
// A single Service instance fans out across the provider registry,
// running per-provider probes that do three things in order:
//
//  1. exec.LookPath on each candidate binary. None resolved -> Missing.
//  2. Run "<binary> <versionArgs...>" with a 5s timeout. Failure ->
//     Errored + last_error captured for the dashboard.
//  3. Check os.Getenv for each required env var. Any missing -> NeedsAuth.
//
// Anything that passes all three lands in Ready.
//
// The Service caches the last probe result in-memory keyed by provider
// so the dashboard's list view doesn't fork four CLIs per page-load.
// Probe() forces a re-run; Get() returns the cached value (or fresh-probes
// when no cache).
package harnesses

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/harnesses/probes"
)

// probeTimeout caps each <binary> --version invocation. Five seconds is
// generous; in practice the calls return in tens of milliseconds. The
// upper bound mostly protects against a CLI that's installed but
// permanently broken (e.g. a node script with a missing runtime).
const probeTimeout = 5 * time.Second

// Service is the public surface the REST handlers + CLI consume. Safe
// for concurrent use; the mutex guards the cache map.
type Service struct {
	log   *slog.Logger
	mu    sync.RWMutex
	cache map[Provider]Harness
}

// NewService constructs a Service with an empty cache. Call ListAll(ctx)
// once at boot to warm the cache; subsequent Get() calls then return
// instantly.
func NewService(log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		log:   log,
		cache: make(map[Provider]Harness),
	}
}

// ListAll probes every known provider in order, populates the cache, and
// returns the resulting Harness slice. Always returns len(AllProviders)
// entries (one per provider) — a missing harness is just a row with
// Status=Missing, never an absent entry.
func (s *Service) ListAll(ctx context.Context) []Harness {
	out := make([]Harness, 0, len(AllProviders))
	for _, p := range AllProviders {
		h := s.probeAndCache(ctx, p)
		out = append(out, h)
	}
	return out
}

// Get returns the cached probe for a single provider, fresh-probing if
// the cache is empty. Returns ErrUnknownProvider when p isn't one of
// the four supported harnesses.
func (s *Service) Get(ctx context.Context, p Provider) (Harness, error) {
	if !p.IsValid() {
		return Harness{}, fmt.Errorf("%w: %q", ErrUnknownProvider, string(p))
	}
	s.mu.RLock()
	h, ok := s.cache[p]
	s.mu.RUnlock()
	if ok {
		return h, nil
	}
	// First-touch: probe + cache.
	return s.probeAndCache(ctx, p), nil
}

// Probe forces a fresh probe for the given provider and updates the
// cache. The dashboard's "Probe" button hits this; everything else
// reads from the cache.
func (s *Service) Probe(ctx context.Context, p Provider) (Harness, error) {
	if !p.IsValid() {
		return Harness{}, fmt.Errorf("%w: %q", ErrUnknownProvider, string(p))
	}
	return s.probeAndCache(ctx, p), nil
}

// probeAndCache runs one probe and replaces the cached entry. The
// returned Harness is the freshly-computed value, not a copy of any
// previously-cached state.
func (s *Service) probeAndCache(ctx context.Context, p Provider) Harness {
	h := s.runProbe(ctx, p)
	s.mu.Lock()
	s.cache[p] = h
	s.mu.Unlock()
	return h
}

// runProbe is the pure probe — no caching, no locks. It returns a fully
// populated Harness so callers can decide whether to persist it.
func (s *Service) runProbe(ctx context.Context, p Provider) Harness {
	pr, ok := probes.For(string(p))
	if !ok {
		// Defensive: AllProviders should keep this in sync with the
		// registry. If we get here, something is mis-wired in the
		// probes package.
		return Harness{
			Provider:    p,
			IsInstalled: false,
			Status:      StatusErrored,
			LastError:   strPtr(fmt.Sprintf("no probe registered for provider %q", p)),
			RequiredEnv: []string{},
		}
	}

	h := Harness{
		Provider:      p,
		IsInstalled:   false,
		RequiredEnv:   append([]string{}, pr.RequiredEnv...),
		InstallDocURL: optStr(pr.InstallDocURL),
	}
	// RequiredEnv should always emit as a JSON array (never null), so
	// give it an empty slice when the registry left it nil.
	if h.RequiredEnv == nil {
		h.RequiredEnv = []string{}
	}

	binary := resolveBinary(pr.Binary)
	if binary == "" {
		h.Status = StatusMissing
		return h
	}
	h.IsInstalled = true
	h.BinaryPath = strPtr(binary)

	version, err := runVersion(ctx, binary, pr.VersionArgs)
	if err != nil {
		h.Status = StatusErrored
		h.LastError = strPtr(err.Error())
		s.log.Debug("harness: version probe failed",
			"provider", p, "binary", binary, "error", err)
		return h
	}
	if version != "" {
		h.Version = strPtr(version)
	}

	if missing := missingEnv(pr.RequiredEnv); len(missing) > 0 {
		h.Status = StatusNeedsAuth
		h.LastError = strPtr(
			"required env not set: " + strings.Join(missing, ", "),
		)
		return h
	}

	h.Status = StatusReady
	return h
}

// resolveBinary returns the first candidate that resolves via
// exec.LookPath, or "" when none does.
func resolveBinary(candidates []string) string {
	for _, name := range candidates {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// runVersion runs "<binary> <args...>" with a 5s timeout and returns
// the trimmed stdout. An empty args slice yields an empty stdout (some
// CLIs print nothing when no subcommand is given); callers treat empty
// version as "unknown but reachable" rather than as an error.
func runVersion(ctx context.Context, binary string, args []string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// exec.ExitError captures stderr; fold it into the message so
		// operators can see why their CLI is broken without ssh'ing in.
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		} else {
			errMsg = fmt.Sprintf("%s (stderr: %s)", err.Error(), truncate(errMsg, 200))
		}
		return "", fmt.Errorf("%s %s: %s", binary, strings.Join(args, " "), errMsg)
	}

	// Some CLIs (claude, codex) print to stderr; check both.
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = strings.TrimSpace(stderr.String())
	}
	// Squash to one line — version output is occasionally multi-line
	// (banner + version) and the dashboard renders this verbatim.
	if idx := strings.IndexByte(out, '\n'); idx >= 0 {
		out = strings.TrimSpace(out[:idx])
	}
	return out, nil
}

// missingEnv returns the subset of required env vars that are empty in
// the current process environment. Whitespace-only values count as
// missing — they're never a real API key.
func missingEnv(required []string) []string {
	var missing []string
	for _, name := range required {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.TrimSpace(os.Getenv(name)) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// truncate caps s to n runes, appending "…" when shortened.
func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// strPtr returns a pointer to s. Used to make nullable JSON fields
// emit a value rather than null.
func strPtr(s string) *string { return &s }

// optStr returns a *string for non-empty s, else nil. Use when a
// nullable field's empty value should serialise as JSON null.
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
