// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	if err := validate(cfg); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
	if cfg.Server.HTTPAddr != ":8080" {
		t.Errorf("expected http addr :8080, got %q", cfg.Server.HTTPAddr)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("expected json log format, got %q", cfg.Logging.Format)
	}
}

func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
server:
  http_addr: ":9999"
agentfield:
  url: http://test-af:8081
logging:
  level: debug
  format: text
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.HTTPAddr != ":9999" {
		t.Errorf("expected :9999, got %q", cfg.Server.HTTPAddr)
	}
	if cfg.AgentField.URL != "http://test-af:8081" {
		t.Errorf("expected http://test-af:8081, got %q", cfg.AgentField.URL)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected debug, got %q", cfg.Logging.Level)
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
server:
  http_addr: ":8080"
agentfield:
  url: http://af-from-yaml:8081
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AF_STACK_HTTP_ADDR", ":7777")
	t.Setenv("AF_STACK_AGENTFIELD_URL", "http://af-from-env:8081")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.HTTPAddr != ":7777" {
		t.Errorf("env should win, got %q", cfg.Server.HTTPAddr)
	}
	if cfg.AgentField.URL != "http://af-from-env:8081" {
		t.Errorf("env should win, got %q", cfg.AgentField.URL)
	}
}

func TestValidateRejectsBadLevel(t *testing.T) {
	cfg := Default()
	cfg.Logging.Level = "garbage"
	if err := validate(cfg); err == nil {
		t.Error("expected validation to reject bad log level")
	}
}

func TestLoadMissingFileIsOK(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("empty path should yield defaults, got %v", err)
	}
	if cfg.Server.HTTPAddr != ":8080" {
		t.Errorf("expected default :8080, got %q", cfg.Server.HTTPAddr)
	}
}

func TestSandboxDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Sandbox.Adapter != "docker" {
		t.Errorf("default sandbox adapter = %q, want docker", cfg.Sandbox.Adapter)
	}
}

func TestSandboxEnvOverrides(t *testing.T) {
	t.Setenv("AF_STACK_AGENTFIELD_URL", "http://af:8081")
	t.Setenv("AF_STACK_SANDBOX_ADAPTER", "E2B")
	t.Setenv("E2B_API_KEY", "sk-test-123")
	t.Setenv("AF_STACK_E2B_BASE_URL", "https://api.e2b.dev")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Sandbox.Adapter != "e2b" {
		t.Errorf("adapter = %q, want e2b (lowercased)", cfg.Sandbox.Adapter)
	}
	if cfg.Sandbox.E2BAPIKey != "sk-test-123" {
		t.Errorf("E2BAPIKey = %q, want sk-test-123", cfg.Sandbox.E2BAPIKey)
	}
	if cfg.Sandbox.E2BBaseURL != "https://api.e2b.dev" {
		t.Errorf("E2BBaseURL = %q, want https://api.e2b.dev", cfg.Sandbox.E2BBaseURL)
	}
}