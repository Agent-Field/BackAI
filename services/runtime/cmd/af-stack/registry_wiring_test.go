// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// newTestRegistry builds the adapter registry with an all-nil dependency set.
// The slot descriptors (tier/available-builtin/swap metadata) are static and
// don't need live services, so this exercises exactly the metadata honesty we
// care about.
func newTestRegistry(t *testing.T) map[string]registry.SlotView {
	t.Helper()
	r := buildAdapterRegistry(
		config.Config{}, // cfg
		nil,             // database
		nil,             // afClient
		nil,             // store
		nil,             // vault
		"",              // secretsRemoteName
		nil,             // jobsManager
		nil,             // llmGW
		nil,             // litellmAdmin
		nil,             // sandboxSvc
		nil,             // toolsRegistry
		nil,             // notificationsSvc
		nil,             // webhooksSvc
		nil,             // billingSvc
		nil,             // logsStore
		nil,             // tracesStore
		nil,             // metricsStore
		nil,             // errorsStore
	)
	views := map[string]registry.SlotView{}
	for _, v := range r.List(context.Background()).Slots {
		views[v.Slot] = v
	}
	return views
}

// TestRegistry_RealSlotsAreEnvSwappable asserts the slots this change made real
// advertise an env-var swap with a non-empty built-in set and the exact
// SwapEnv the factories actually read.
func TestRegistry_RealSlotsAreEnvSwappable(t *testing.T) {
	views := newTestRegistry(t)
	want := map[string]string{
		"storage":       "AF_STACK_S3_ADAPTER",
		"secrets":       "AF_STACK_SECRETS_ADAPTER",
		"llm-chat":      "AF_STACK_LLM_GATEWAY_ADAPTER",
		"notifications": "AF_STACK_NOTIFICATIONS_ADAPTER",
	}
	for slot, env := range want {
		v, ok := views[slot]
		if !ok {
			t.Fatalf("slot %q not registered", slot)
		}
		if v.SwapMethod != "env_var" {
			t.Errorf("slot %q: SwapMethod=%q, want env_var", slot, v.SwapMethod)
		}
		if v.SwapEnv != env {
			t.Errorf("slot %q: SwapEnv=%q, want %q", slot, v.SwapEnv, env)
		}
		if len(v.AvailableBuiltin) == 0 {
			t.Errorf("slot %q: AvailableBuiltin is empty; ValidateSelections cannot guard it", slot)
		}
	}
}

// TestRegistry_KnownBuiltins pins the selectable value sets so the dashboard /
// CLI and ValidateSelections stay in sync with the factories.
func TestRegistry_KnownBuiltins(t *testing.T) {
	views := newTestRegistry(t)
	cases := map[string][]string{
		"storage":       {"minio", "s3", "remote"},
		"secrets":       {"vault", "remote"},
		"llm-chat":      {"demo", "litellm", "remote"},
		"notifications": {"log", "resend", "slack", "sms", "twilio", "push", "fcm", "remote"},
		"auth":          {"better-auth"},
	}
	for slot, want := range cases {
		v := views[slot]
		if got := v.AvailableBuiltin; !equalStrings(got, want) {
			t.Errorf("slot %q: AvailableBuiltin=%v, want %v", slot, got, want)
		}
	}
}

// TestRegistry_NoUnreadSwapEnv is the honesty invariant: every slot that
// advertises an env-var swap with a SwapEnv must also expose a non-empty
// AvailableBuiltin, so ValidateSelections fail-fasts on a bad value instead of
// the env being a silent no-op. Catches multimodal (now SwapMethod "none") and
// auth (now has a built-in list) regressing.
func TestRegistry_NoUnreadSwapEnv(t *testing.T) {
	for slot, v := range newTestRegistry(t) {
		if v.SwapMethod == "env_var" && v.SwapEnv != "" && len(v.AvailableBuiltin) == 0 {
			t.Errorf("slot %q advertises SwapEnv=%q but has no AvailableBuiltin — that env is an unvalidated no-op", slot, v.SwapEnv)
		}
	}
}

// TestRegistry_MultimodalHasNoSwapEnv locks in the multimodal honesty fix: it
// no longer advertises AF_STACK_MULTIMODAL_ADAPTER (nothing reads it).
func TestRegistry_MultimodalHasNoSwapEnv(t *testing.T) {
	v := newTestRegistry(t)["multimodal"]
	if v.SwapEnv != "" {
		t.Errorf("multimodal SwapEnv=%q, want empty (unread env removed)", v.SwapEnv)
	}
	if v.SwapMethod != "none" {
		t.Errorf("multimodal SwapMethod=%q, want none", v.SwapMethod)
	}
}

// TestRegistry_ValidateSelections checks the newly-populated slots are guarded:
// a bogus value errors, a listed value passes.
func TestRegistry_ValidateSelections(t *testing.T) {
	r := buildAdapterRegistry(
		config.Config{}, nil, nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)

	// Bogus storage adapter -> one SelectionError for the storage slot.
	bogus := func(k string) string {
		if k == "AF_STACK_S3_ADAPTER" {
			return "nope"
		}
		return ""
	}
	errs := r.ValidateSelections(bogus)
	if len(errs) != 1 || errs[0].SlotID != "storage" {
		t.Fatalf("expected one storage SelectionError, got %+v", errs)
	}

	// A listed value on every real slot -> no errors.
	valid := func(k string) string {
		switch k {
		case "AF_STACK_S3_ADAPTER":
			return "remote"
		case "AF_STACK_SECRETS_ADAPTER":
			return "vault"
		case "AF_STACK_LLM_GATEWAY_ADAPTER":
			return "litellm"
		case "AF_STACK_NOTIFICATIONS_ADAPTER":
			return "slack"
		case "AF_STACK_AUTH_ADAPTER":
			return "better-auth"
		}
		return ""
	}
	if errs := r.ValidateSelections(valid); len(errs) != 0 {
		t.Fatalf("valid selections should pass, got %+v", errs)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
