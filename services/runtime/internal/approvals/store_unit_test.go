// SPDX-License-Identifier: Apache-2.0

package approvals

import "testing"

func TestValidateKind(t *testing.T) {
	if err := validateKind("deploy_to_prod"); err != nil {
		t.Fatalf("valid kind failed: %v", err)
	}
	if err := validateKind(""); err == nil {
		t.Fatal("empty kind should fail")
	}
}

func TestValidStatus(t *testing.T) {
	for _, status := range []string{StatusPending, StatusApproved, StatusDenied, StatusCancelled} {
		if !validStatus(status) {
			t.Fatalf("status %q should be valid", status)
		}
	}
	if validStatus("maybe") {
		t.Fatal("unexpected valid status")
	}
}

func TestClamp(t *testing.T) {
	if got := clamp(0, 1, 100, 25); got != 25 {
		t.Fatalf("default = %d", got)
	}
	if got := clamp(500, 1, 100, 25); got != 100 {
		t.Fatalf("max = %d", got)
	}
}
