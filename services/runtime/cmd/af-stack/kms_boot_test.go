// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"testing"
)

// TestKMSBootDecision locks the S6 secrets-vault boot policy: a KEK that
// fails to load or preflight is fatal only when the operator explicitly
// configured KMS (so a silently-disabled vault can't pass as a healthy
// boot); with no KMS configured it degrades to a disabled vault; a clean
// load always serves.
func TestKMSBootDecision(t *testing.T) {
	boom := errors.New("kek broken")
	cases := []struct {
		name          string
		loadErr       error
		kmsConfigured bool
		want          kmsDecision
	}{
		{"clean load, kms configured -> serve", nil, true, kmsServe},
		{"clean load, dev -> serve", nil, false, kmsServe},
		{"load fails, kms configured -> fatal", boom, true, kmsFatal},
		{"load fails, no kms configured -> degrade", boom, false, kmsDegrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kmsBootDecision(tc.loadErr, tc.kmsConfigured); got != tc.want {
				t.Fatalf("kmsBootDecision(%v, %v) = %d, want %d",
					tc.loadErr, tc.kmsConfigured, got, tc.want)
			}
		})
	}
}
