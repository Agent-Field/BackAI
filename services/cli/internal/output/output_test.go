// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

// Contract: every error class maps to its documented, stable exit code.
// This is the table downstream scripts branch on — it must not drift.
func TestExitCodeTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"plain-error", errors.New("boom"), ExitGeneric},
		{"usage-fault", Usage("bad flag"), ExitUsage},
		{"auth-fault", Auth("no key"), ExitAuth},
		{"notfound-fault", NotFound("missing"), ExitNotFound},
		{"validation-fault", Invalid("bad input"), ExitValidation},
		{"remote-fault", Remote("down"), ExitRemote},
		{"generic-fault", Fail("nope"), ExitGeneric},
		{"flag-help", flag.ErrHelp, ExitUsage},
		{"wrapped-fault", fmt.Errorf("ctx: %w", Auth("no key")), ExitAuth},
		{"api-400", &client.APIError{Status: 400}, ExitValidation},
		{"api-401", &client.APIError{Status: 401}, ExitAuth},
		{"api-403", &client.APIError{Status: 403}, ExitAuth},
		{"api-404", &client.APIError{Status: 404}, ExitNotFound},
		{"api-409", &client.APIError{Status: 409}, ExitValidation},
		{"api-422", &client.APIError{Status: 422}, ExitValidation},
		{"api-402", &client.APIError{Status: 402}, ExitRemote},
		{"api-429", &client.APIError{Status: 429}, ExitRemote},
		{"api-500", &client.APIError{Status: 500}, ExitRemote},
		{"api-503", &client.APIError{Status: 503}, ExitRemote},
		{"wrapped-api", fmt.Errorf("call: %w", &client.APIError{Status: 404}), ExitNotFound},
		{"fault-wins-over-api", Wrap(ExitValidation, "x", &client.APIError{Status: 500}), ExitValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.want {
				t.Fatalf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// Contract: a Fault preserves its wrapped error for errors.As, so an
// APIError still unwraps through it (the Fault's explicit code wins for
// exit mapping, but the underlying cause is not lost).
func TestFaultUnwrap(t *testing.T) {
	api := &client.APIError{Status: 404, Code: "NOT_FOUND", Message: "gone"}
	f := Wrap(ExitNotFound, "fetch failed", api)
	var got *client.APIError
	if !errors.As(f, &got) {
		t.Fatal("expected APIError to unwrap through Fault")
	}
	if got.Status != 404 {
		t.Fatalf("unwrapped status = %d, want 404", got.Status)
	}
	if !strings.Contains(f.Error(), "fetch failed") || !strings.Contains(f.Error(), "gone") {
		t.Fatalf("Fault.Error() = %q, want both msg and cause", f.Error())
	}
}

// Contract: Result emits stable, parseable JSON under --json and human text
// otherwise — from a single call site.
func TestResultDualRepresentation(t *testing.T) {
	machine := map[string]any{"count": 2, "items": []string{"a", "b"}}
	human := func(w io.Writer) error {
		_, err := fmt.Fprintln(w, "count: 2")
		return err
	}

	var jbuf bytes.Buffer
	if err := Result(&jbuf, true, machine, human); err != nil {
		t.Fatalf("Result json: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(jbuf.Bytes(), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, jbuf.String())
	}
	if decoded["count"].(float64) != 2 {
		t.Fatalf("decoded count = %v, want 2", decoded["count"])
	}
	if !strings.HasSuffix(jbuf.String(), "\n") {
		t.Fatal("JSON output must end with a newline")
	}

	var hbuf bytes.Buffer
	if err := Result(&hbuf, false, machine, human); err != nil {
		t.Fatalf("Result human: %v", err)
	}
	if strings.TrimSpace(hbuf.String()) != "count: 2" {
		t.Fatalf("human output = %q, want 'count: 2'", hbuf.String())
	}
	if strings.Contains(hbuf.String(), "{") {
		t.Fatal("human output must not contain JSON")
	}
}
