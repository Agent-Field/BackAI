package secrets

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnvExampleBootsKMS pins the quickstart contract: the committed
// .env.example, copied verbatim to .env (which `af-stack dev`, `af-stack
// mode`, the README, and AGENTS.md all do), must yield a KMS cipher the
// runtime can boot with.
//
// The runtime refuses to start when AF_STACK_KMS_KEY is set to something it
// cannot load (kmsBootDecision in cmd/af-stack), so an example value that is
// neither the dev sentinel nor 32 hex-encoded bytes turns every fresh clone
// into a crash loop. That is exactly what shipped while the example said
// "change-me-to-a-real-key".
func TestEnvExampleBootsKMS(t *testing.T) {
	values := readEnvExample(t)
	for _, k := range []string{
		"AF_STACK_KMS_PROVIDER",
		"AF_STACK_KMS_KEY",
		"AF_STACK_KMS_ENCRYPTED_DATA_KEY",
		"AF_STACK_KMS_ENCRYPTED_DATA_KEY_FILE",
	} {
		t.Setenv(k, values[k])
	}

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := LoadCipher(context.Background(), quiet)
	if err != nil {
		t.Fatalf(".env.example AF_STACK_KMS_KEY=%q does not boot the runtime: %v\n"+
			"use the dev sentinel %q or 32 random bytes hex-encoded",
			values["AF_STACK_KMS_KEY"], err, devKEKSentinel)
	}
	if err := c.Preflight(); err != nil {
		t.Fatalf("cipher built from .env.example fails preflight: %v", err)
	}
}

// readEnvExample parses the repo's .env.example into KEY=VALUE pairs,
// skipping comments and blank lines — the same view docker compose and the
// CLI's .env seeding take of the file.
func readEnvExample(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", ".env.example")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	values := map[string]string{}
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
		t.Fatalf("read %s: %v", path, err)
	}
	return values
}
