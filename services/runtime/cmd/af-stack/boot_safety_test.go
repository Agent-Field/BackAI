// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// TestEffectiveMTEnabled locks the mode-aware isolation predicate the boot
// gate uses: multi-tenancy counts as enforced only in a non-personal
// deployment with the module on — mirroring the resolver's
// multiTenancyEnabled().
func TestEffectiveMTEnabled(t *testing.T) {
	mk := func(mode string, mt bool) config.Config {
		c := config.Default()
		c.Mode = mode
		c.Modules.Enabled = map[string]bool{"multi-tenancy": mt}
		return c
	}
	cases := []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{"saas + mt on -> enforcing", mk(config.ModeSaaS, true), true},
		{"saas + mt off -> not enforcing", mk(config.ModeSaaS, false), false},
		{"personal + mt on -> relaxed", mk(config.ModePersonal, true), false},
		{"personal + mt off -> relaxed", mk(config.ModePersonal, false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveMTEnabled(tc.cfg); got != tc.want {
				t.Fatalf("effectiveMTEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSaaSBootRejectsUnsafeRole composes the mode-aware predicate with the
// RLS boot policy: an unsafe (RLS-bypassing) serving role in a SaaS +
// multi-tenancy deployment is fatal, while personal mode stays relaxed.
// This is the "boot validation rejects an unsafe SaaS config combo"
// acceptance, expressed against the pure decision so it needs no DB.
func TestSaaSBootRejectsUnsafeRole(t *testing.T) {
	saasMT := config.Default()
	saasMT.Mode = config.ModeSaaS
	saasMT.Modules.Enabled = map[string]bool{"multi-tenancy": true}

	personalMT := config.Default()
	personalMT.Mode = config.ModePersonal
	personalMT.Modules.Enabled = map[string]bool{"multi-tenancy": true}

	const roleCanBypass = true
	const noOverride = false

	// SaaS + MT + bypassing role + no override -> fatal.
	if got := rlsBootDecision(roleCanBypass, effectiveMTEnabled(saasMT), noOverride); got != rlsFatal {
		t.Errorf("saas+mt+unsafe role: decision = %d, want fatal(%d)", got, rlsFatal)
	}
	// Personal mode is relaxed even with the module flag set + unsafe role.
	if got := rlsBootDecision(roleCanBypass, effectiveMTEnabled(personalMT), noOverride); got != rlsWarn {
		t.Errorf("personal+unsafe role: decision = %d, want warn(%d)", got, rlsWarn)
	}
}

// TestRLSUnsafeForReady locks the /ready flag: unsafe only when isolation
// is intended AND the role bypasses RLS.
func TestRLSUnsafeForReady(t *testing.T) {
	cases := []struct {
		canBypass, mtEnabled, want bool
	}{
		{true, true, true},
		{true, false, false},
		{false, true, false},
		{false, false, false},
	}
	for _, tc := range cases {
		if got := rlsUnsafeForReady(tc.canBypass, tc.mtEnabled); got != tc.want {
			t.Errorf("rlsUnsafeForReady(%v,%v) = %v, want %v", tc.canBypass, tc.mtEnabled, got, tc.want)
		}
	}
}
