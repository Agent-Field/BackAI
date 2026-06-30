// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// TestRLSBootDecision locks the S2 tenant-isolation boot policy: a DB role that
// can bypass row-level security is fatal only when multi-tenancy is enabled and
// not explicitly overridden; otherwise it warns; a safe role is always OK.
func TestRLSBootDecision(t *testing.T) {
	cases := []struct {
		name                      string
		canBypass, mtEnabled, ovr bool
		want                      rlsDecision
	}{
		{"safe role, mt on", false, true, false, rlsOK},
		{"safe role, mt off", false, false, false, rlsOK},
		{"bypass role, mt on, no override -> fatal", true, true, false, rlsFatal},
		{"bypass role, mt on, override -> warn", true, true, true, rlsWarn},
		{"bypass role, mt off -> warn", true, false, false, rlsWarn},
		{"bypass role, mt off, override -> warn", true, false, true, rlsWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rlsBootDecision(tc.canBypass, tc.mtEnabled, tc.ovr); got != tc.want {
				t.Fatalf("rlsBootDecision(%v,%v,%v) = %d, want %d",
					tc.canBypass, tc.mtEnabled, tc.ovr, got, tc.want)
			}
		})
	}
}
