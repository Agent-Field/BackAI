// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// A3: the remote sandbox shim is wired into newSandbox. A "remote" selection
// with no URL must fail fast with a clear, actionable message (not the generic
// "unknown adapter"); an unknown adapter must now list remote as an option.
func TestNewSandboxRemoteRequiresURL(t *testing.T) {
	_, err := newSandbox(config.SandboxConfig{Adapter: "remote"}, nil, nil)
	if err == nil {
		t.Fatal("expected an error for adapter=remote with no URL")
	}
	if !strings.Contains(err.Error(), "AF_STACK_SANDBOX_ADAPTER_URL") {
		t.Fatalf("error should name the missing env var, got: %v", err)
	}
}

func TestNewSandboxUnknownAdapterListsRemote(t *testing.T) {
	_, err := newSandbox(config.SandboxConfig{Adapter: "kubernetes"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("unknown-adapter error should list remote as an option, got: %v", err)
	}
}
