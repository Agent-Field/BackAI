// SPDX-License-Identifier: Apache-2.0

package jobcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// goldenScaffold renders a scaffold into a temp dir and compares the file to
// its golden fixture. Set AF_UPDATE_GOLDEN=1 to (re)write the fixtures.
func goldenScaffold(t *testing.T, lang, ext, golden string) {
	t.Helper()
	tmp := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := runNew(tmp, []string{"resize-image", "--lang", lang}, &stdout, &stderr); err != nil {
		t.Fatalf("runNew(%s): %v (stderr=%s)", lang, err, stderr.String())
	}
	got, err := os.ReadFile(filepath.Join(tmp, "jobs", "resize-image"+ext))
	if err != nil {
		t.Fatalf("scaffold not written: %v", err)
	}
	goldenPath := filepath.Join("testdata", golden)
	if os.Getenv("AF_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with AF_UPDATE_GOLDEN=1 to create)", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s scaffold drifted from %s:\n--- got ---\n%s", lang, goldenPath, got)
	}
}

// Contract: `job new <name> --lang py` produces the golden Python worker.
func TestJobNew_PythonGolden(t *testing.T) { goldenScaffold(t, "py", ".py", "resize-image.py.golden") }

// Contract: `job new <name> --lang ts` produces the golden TypeScript worker.
func TestJobNew_TypeScriptGolden(t *testing.T) {
	goldenScaffold(t, "ts", ".ts", "resize-image.ts.golden")
}

// Contract: default language is Python and the file registers the slugified
// kind + the jobs:work next-steps guidance is printed.
func TestJobNew_DefaultsToPython(t *testing.T) {
	tmp := t.TempDir()
	var stdout bytes.Buffer
	if err := runNew(tmp, []string{"Resize Image"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(tmp, "jobs", "resize-image.py"))
	if err != nil {
		t.Fatalf("slugified file not created: %v", err)
	}
	if !bytes.Contains(body, []byte(`@worker.register("resize-image")`)) {
		t.Errorf("handler not registered for slug kind:\n%s", body)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("jobs:work")) {
		t.Errorf("next-steps must mention the jobs:work scope, got:\n%s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("python jobs/resize-image.py")) {
		t.Errorf("next-steps must show how to run it, got:\n%s", stdout.String())
	}
}

// Contract: --json emits {"created": ["jobs/<slug>.ts"]} and nothing else.
func TestJobNew_JSONEnvelope(t *testing.T) {
	tmp := t.TempDir()
	var stdout bytes.Buffer
	if err := runNew(tmp, []string{"resize-image", "--lang", "ts", "--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	var got newResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v (%s)", err, stdout.String())
	}
	if len(got.Created) != 1 || got.Created[0] != "jobs/resize-image.ts" {
		t.Fatalf("created = %v, want [jobs/resize-image.ts]", got.Created)
	}
}

// Contract: arg/flag validation maps to the documented exit codes.
func TestJobNew_Validation(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no name", []string{}, output.ExitUsage},
		{"two names", []string{"a", "b"}, output.ExitUsage},
		{"unknown lang", []string{"x", "--lang", "rust"}, output.ExitUsage},
		{"no usable chars", []string{"@@@"}, output.ExitValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runNew(tmp, tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if code := output.ExitCode(err); code != tc.want {
				t.Fatalf("exit = %d, want %d (err=%v)", code, tc.want, err)
			}
		})
	}
}

// Contract: an existing job file is never overwritten; the collision is a
// validation error (exit 5).
func TestJobNew_RefusesOverwrite(t *testing.T) {
	tmp := t.TempDir()
	if err := runNew(tmp, []string{"resize-image"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("first scaffold: %v", err)
	}
	err := runNew(tmp, []string{"resize-image"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code := output.ExitCode(err); code != output.ExitValidation {
		t.Fatalf("overwrite exit = %d, want %d (err=%v)", code, output.ExitValidation, err)
	}
}

// Contract: the top-level dispatcher rejects an unknown subcommand (exit 2).
func TestJobRun_UnknownSubcommand(t *testing.T) {
	if code := output.ExitCode(Run([]string{"bogus"}, &bytes.Buffer{}, &bytes.Buffer{})); code != output.ExitUsage {
		t.Fatalf("unknown-subcommand exit = %d, want %d", code, output.ExitUsage)
	}
}
