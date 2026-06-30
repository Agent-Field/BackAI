// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// TestBuildHostConfigHardening locks the S3 contract: every sandbox container
// drops all Linux capabilities, forbids privilege escalation, and has a pid
// cap — regardless of the run spec.
func TestBuildHostConfigHardening(t *testing.T) {
	hc := buildHostConfig(sandbox.RunSpec{
		Image:    "python:3.12-slim",
		MemoryGB: 2,
		CPU:      1,
		Network:  sandbox.NetworkOpen,
	}, "")

	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Fatalf("CapDrop = %v, want [ALL]", hc.CapDrop)
	}
	found := false
	for _, o := range hc.SecurityOpt {
		if o == "no-new-privileges" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SecurityOpt = %v, want it to contain no-new-privileges", hc.SecurityOpt)
	}
	if hc.Resources.PidsLimit == nil || *hc.Resources.PidsLimit != defaultSandboxPidsLimit {
		t.Fatalf("PidsLimit = %v, want %d", hc.Resources.PidsLimit, defaultSandboxPidsLimit)
	}
}

// TestBuildHostConfigNetworkIsolated verifies the isolated profile gets no
// network (escape can't reach the host network), and that hardening still holds.
func TestBuildHostConfigNetworkIsolated(t *testing.T) {
	hc := buildHostConfig(sandbox.RunSpec{MemoryGB: 1, CPU: 1, Network: sandbox.NetworkIsolated}, "")
	if string(hc.NetworkMode) != "none" {
		t.Fatalf("NetworkMode = %q, want none", hc.NetworkMode)
	}
	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Fatalf("CapDrop = %v, want [ALL]", hc.CapDrop)
	}
}
