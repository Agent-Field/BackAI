// SPDX-License-Identifier: Apache-2.0

// Tests cover the three probe outcomes (Missing / Errored / NeedsAuth /
// Ready) without depending on a real CLI being installed. The runProbe
// path is exercised end-to-end by pointing the probe at a small shell
// helper we write into a tempdir + prepending it to PATH.
package harnesses

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withPath prepends dir to PATH for the duration of the test.
func withPath(t *testing.T, dir string) {
	t.Helper()
	prev := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+prev); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
}

// writeStubBinary drops a chmod 0o755 shell script at <dir>/<name> that
// prints `versionOutput` to stdout when called with any args. On
// non-Unix we skip — the probe path is exercised on macOS/Linux CI.
func writeStubBinary(t *testing.T, dir, name, versionOutput string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub binary requires POSIX shell")
	}
	body := "#!/bin/sh\necho \"" + strings.ReplaceAll(versionOutput, "\"", "\\\"") + "\"\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

// writeFailingBinary drops a chmod 0o755 script that exits non-zero.
func writeFailingBinary(t *testing.T, dir, name, stderrMsg string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub binary requires POSIX shell")
	}
	body := "#!/bin/sh\necho \"" + strings.ReplaceAll(stderrMsg, "\"", "\\\"") + "\" 1>&2\nexit 1\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// clearProviderEnv unsets every env var any provider's probe might use.
// Tests opt back in via t.Setenv.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"} {
		t.Setenv(k, "")
	}
}

func TestProvider_IsValid(t *testing.T) {
	cases := []struct {
		in   Provider
		want bool
	}{
		{Claudecode, true},
		{Codex, true},
		{Gemini, true},
		{Opencode, true},
		{Provider("bogus"), false},
		{Provider(""), false},
	}
	for _, c := range cases {
		if got := c.in.IsValid(); got != c.want {
			t.Errorf("Provider(%q).IsValid() = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestService_ListAll_MissingByDefault(t *testing.T) {
	// Point PATH at an empty tmpdir so no real CLI resolves. We MUST
	// fully replace PATH (not just prepend) — if the host machine has
	// the real `claude` or `gemini` binaries installed those would
	// still resolve via the original PATH.
	dir := t.TempDir()
	prev := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
	_ = os.Setenv("PATH", dir)

	clearProviderEnv(t)

	svc := NewService(quietLogger())
	list := svc.ListAll(context.Background())
	if got, want := len(list), len(AllProviders); got != want {
		t.Fatalf("ListAll returned %d entries, want %d", got, want)
	}
	for _, h := range list {
		if h.Status != StatusMissing {
			t.Errorf("provider %s: status = %s, want %s", h.Provider, h.Status, StatusMissing)
		}
		if h.IsInstalled {
			t.Errorf("provider %s: is_installed = true, want false", h.Provider)
		}
		if h.BinaryPath != nil {
			t.Errorf("provider %s: binary_path = %v, want nil", h.Provider, *h.BinaryPath)
		}
		if h.RequiredEnv == nil {
			t.Errorf("provider %s: required_env nil; should be empty slice", h.Provider)
		}
	}
}

func TestService_Probe_ReadyWhenBinaryAndEnvPresent(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "claude", "claude 1.2.3")
	// Replace PATH so we don't accidentally match the host's real CLI.
	prev := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
	_ = os.Setenv("PATH", dir)
	clearProviderEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-123")

	svc := NewService(quietLogger())
	h, err := svc.Probe(context.Background(), Claudecode)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusReady {
		t.Errorf("status = %s, want %s (last_error=%v)", h.Status, StatusReady, h.LastError)
	}
	if !h.IsInstalled {
		t.Errorf("is_installed = false, want true")
	}
	if h.BinaryPath == nil || !strings.HasSuffix(*h.BinaryPath, "/claude") {
		t.Errorf("binary_path = %v, want suffix /claude", h.BinaryPath)
	}
	if h.Version == nil || *h.Version != "claude 1.2.3" {
		t.Errorf("version = %v, want \"claude 1.2.3\"", h.Version)
	}
}

func TestService_Probe_NeedsAuthWhenEnvMissing(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "codex", "codex 0.5.0")
	prev := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
	_ = os.Setenv("PATH", dir)
	clearProviderEnv(t)
	// Intentionally do NOT set OPENAI_API_KEY.

	svc := NewService(quietLogger())
	h, err := svc.Probe(context.Background(), Codex)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusNeedsAuth {
		t.Errorf("status = %s, want %s", h.Status, StatusNeedsAuth)
	}
	if !h.IsInstalled {
		t.Errorf("is_installed = false, want true")
	}
	if h.LastError == nil || !strings.Contains(*h.LastError, "OPENAI_API_KEY") {
		t.Errorf("last_error = %v, want to mention OPENAI_API_KEY", h.LastError)
	}
}

func TestService_Probe_ErroredWhenVersionFails(t *testing.T) {
	dir := t.TempDir()
	writeFailingBinary(t, dir, "gemini", "boom: config missing")
	prev := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
	_ = os.Setenv("PATH", dir)
	clearProviderEnv(t)
	t.Setenv("GEMINI_API_KEY", "key")

	svc := NewService(quietLogger())
	h, err := svc.Probe(context.Background(), Gemini)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusErrored {
		t.Errorf("status = %s, want %s", h.Status, StatusErrored)
	}
	if h.LastError == nil {
		t.Errorf("last_error nil; want stderr surfaced")
	}
}

func TestService_Probe_OpencodeReadyWithoutEnv(t *testing.T) {
	// opencode has no required env — once the binary is present and
	// version succeeds, the harness is Ready.
	dir := t.TempDir()
	writeStubBinary(t, dir, "opencode", "opencode 2.0.0")
	prev := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
	_ = os.Setenv("PATH", dir)
	clearProviderEnv(t)

	svc := NewService(quietLogger())
	h, err := svc.Probe(context.Background(), Opencode)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if h.Status != StatusReady {
		t.Errorf("status = %s, want %s (last_error=%v)", h.Status, StatusReady, h.LastError)
	}
}

func TestService_Get_UsesCache(t *testing.T) {
	dir := t.TempDir()
	writeStubBinary(t, dir, "claude", "v1")
	prev := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
	_ = os.Setenv("PATH", dir)
	clearProviderEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "k")

	svc := NewService(quietLogger())
	first, err := svc.Get(context.Background(), Claudecode)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if first.Status != StatusReady {
		t.Fatalf("first status = %s, want Ready", first.Status)
	}

	// Replace the stub with a failing binary; cache should still serve
	// the Ready value because Get reads cache after first-touch.
	writeFailingBinary(t, dir, "claude", "now broken")
	second, err := svc.Get(context.Background(), Claudecode)
	if err != nil {
		t.Fatalf("Get (cached): %v", err)
	}
	if second.Status != StatusReady {
		t.Errorf("cached status = %s, want Ready (cache should have served)", second.Status)
	}

	// Probe forces a re-run; should now be Errored.
	third, err := svc.Probe(context.Background(), Claudecode)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if third.Status != StatusErrored {
		t.Errorf("forced probe status = %s, want Errored", third.Status)
	}
}

func TestService_UnknownProvider(t *testing.T) {
	svc := NewService(quietLogger())
	if _, err := svc.Get(context.Background(), Provider("bogus")); err == nil {
		t.Errorf("Get(bogus) = nil error, want ErrUnknownProvider")
	}
	if _, err := svc.Probe(context.Background(), Provider("bogus")); err == nil {
		t.Errorf("Probe(bogus) = nil error, want ErrUnknownProvider")
	}
}