// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"strings"
	"testing"
)

// A3: ValidateSelections must catch an AF_STACK_*_ADAPTER env value that names
// an adapter the slot doesn't implement, and must accept valid/unset values.
func TestValidateSelections(t *testing.T) {
	r := New()
	r.Register(Slot{
		ID: "sandbox", SwapMethod: "env_var", SwapEnv: "AF_STACK_SANDBOX_ADAPTER",
		AvailableBuiltin: []string{"docker", "gvisor", "e2b"}, Probe: AlwaysHealthy,
	})
	r.Register(Slot{
		ID: "storage", SwapMethod: "env_var", SwapEnv: "AF_STACK_S3_ADAPTER",
		AvailableBuiltin: []string{"minio", "s3"}, Probe: AlwaysHealthy,
	})
	// Remote-only slot with no closed value set — must never be flagged.
	r.Register(Slot{
		ID: "custom", SwapMethod: "env_var", SwapEnv: "AF_STACK_CUSTOM_ADAPTER",
		AvailableBuiltin: nil, Probe: AlwaysHealthy,
	})

	t.Run("valid + unset pass", func(t *testing.T) {
		env := map[string]string{
			"AF_STACK_SANDBOX_ADAPTER": "e2b", // valid
			"AF_STACK_S3_ADAPTER":      "",    // unset -> default
			"AF_STACK_CUSTOM_ADAPTER":  "whatever-remote",
		}
		if errs := r.ValidateSelections(func(k string) string { return env[k] }); len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
	})

	t.Run("invalid value is flagged", func(t *testing.T) {
		env := map[string]string{"AF_STACK_SANDBOX_ADAPTER": "kubernetes"}
		errs := r.ValidateSelections(func(k string) string { return env[k] })
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].SlotID != "sandbox" || errs[0].Value != "kubernetes" {
			t.Fatalf("wrong error: %+v", errs[0])
		}
		// The message must name the env var and the valid options (clarity).
		msg := errs[0].Error()
		for _, want := range []string{"AF_STACK_SANDBOX_ADAPTER", "kubernetes", "docker", "e2b"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("error message %q missing %q", msg, want)
			}
		}
	})

	t.Run("whitespace is trimmed", func(t *testing.T) {
		env := map[string]string{"AF_STACK_S3_ADAPTER": "  s3  "}
		if errs := r.ValidateSelections(func(k string) string { return env[k] }); len(errs) != 0 {
			t.Fatalf("expected trimmed 's3' to be valid, got %v", errs)
		}
	})
}
