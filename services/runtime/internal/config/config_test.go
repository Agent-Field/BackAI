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

func TestLogsEnvOverrides(t *testing.T) {
	t.Setenv("AF_STACK_LOGS_ADAPTER", "LOKI")
	t.Setenv("AF_STACK_LOGS_LOKI_URL", "http://loki:3100")
	t.Setenv("AF_STACK_LOGS_LOKI_TENANT", "tenant-a")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logs.Adapter != "loki" {
		t.Fatalf("adapter=%q want loki", cfg.Logs.Adapter)
	}
	if cfg.Logs.Loki.URL != "http://loki:3100" || cfg.Logs.Loki.Tenant != "tenant-a" {
		t.Fatalf("loki config=%+v", cfg.Logs.Loki)
	}
}

func TestLogsAdapterRequiresURL(t *testing.T) {
	cfg := Default()
	cfg.Logs.Adapter = "loki"
	if err := validate(cfg); err == nil {
		t.Fatal("expected logs.adapter=loki without url to fail")
	}
	cfg = Default()
	cfg.Logs.Adapter = "remote"
	if err := validate(cfg); err == nil {
		t.Fatal("expected logs.adapter=remote without url to fail")
	}
}

func TestTracesEnvOverrides(t *testing.T) {
	t.Setenv("AF_STACK_TRACES_ADAPTER", "TEMPO")
	t.Setenv("AF_STACK_TRACES_TEMPO_URL", "http://tempo:3200")
	t.Setenv("AF_STACK_TRACES_TEMPO_TENANT", "tenant-a")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Traces.Adapter != "tempo" {
		t.Fatalf("adapter=%q want tempo", cfg.Traces.Adapter)
	}
	if cfg.Traces.Tempo.URL != "http://tempo:3200" || cfg.Traces.Tempo.Tenant != "tenant-a" {
		t.Fatalf("tempo config=%+v", cfg.Traces.Tempo)
	}
}

func TestTracesAdapterRequiresURL(t *testing.T) {
	cfg := Default()
	cfg.Traces.Adapter = "tempo"
	if err := validate(cfg); err == nil {
		t.Fatal("expected traces.adapter=tempo without url to fail")
	}
	cfg = Default()
	cfg.Traces.Adapter = "remote"
	if err := validate(cfg); err == nil {
		t.Fatal("expected traces.adapter=remote without url to fail")
	}
}

func TestMetricsEnvOverrides(t *testing.T) {
	t.Setenv("AF_STACK_METRICS_ADAPTER", "PROMETHEUS")
	t.Setenv("AF_STACK_METRICS_PROMETHEUS_URL", "http://prometheus:9090")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Metrics.Adapter != "prometheus" {
		t.Fatalf("adapter=%q want prometheus", cfg.Metrics.Adapter)
	}
	if cfg.Metrics.Prometheus.URL != "http://prometheus:9090" {
		t.Fatalf("prometheus config=%+v", cfg.Metrics.Prometheus)
	}
}

func TestMetricsAdapterRequiresURL(t *testing.T) {
	cfg := Default()
	cfg.Metrics.Adapter = "prometheus"
	if err := validate(cfg); err == nil {
		t.Fatal("expected metrics.adapter=prometheus without url to fail")
	}
	cfg = Default()
	cfg.Metrics.Adapter = "remote"
	if err := validate(cfg); err == nil {
		t.Fatal("expected metrics.adapter=remote without url to fail")
	}
}

func TestErrorsEnvOverrides(t *testing.T) {
	t.Setenv("AF_STACK_ERRORS_ADAPTER", "GLITCHTIP")
	t.Setenv("AF_STACK_ERRORS_GLITCHTIP_URL", "http://glitchtip:8000")
	t.Setenv("AF_STACK_ERRORS_GLITCHTIP_ORG", "backai")
	t.Setenv("AF_STACK_ERRORS_GLITCHTIP_TOKEN", "token")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Errors.Adapter != "glitchtip" {
		t.Fatalf("adapter=%q want glitchtip", cfg.Errors.Adapter)
	}
	if cfg.Errors.GlitchTip.URL != "http://glitchtip:8000" || cfg.Errors.GlitchTip.Org != "backai" || cfg.Errors.GlitchTip.Token != "token" {
		t.Fatalf("glitchtip config=%+v", cfg.Errors.GlitchTip)
	}
}

func TestErrorsAdapterRequiresConfig(t *testing.T) {
	cfg := Default()
	cfg.Errors.Adapter = "glitchtip"
	if err := validate(cfg); err == nil {
		t.Fatal("expected errors.adapter=glitchtip without url/org/token to fail")
	}
	cfg = Default()
	cfg.Errors.Adapter = "remote"
	if err := validate(cfg); err == nil {
		t.Fatal("expected errors.adapter=remote without url to fail")
	}
}

func TestErrorsBackendAlias(t *testing.T) {
	cfg := Default()
	cfg.Errors.Adapter = ""
	cfg.Errors.Backend = "remote"
	cfg.Errors.Remote.URL = "http://adapter:8080"
	applyEnvOverrides(&cfg)
	if cfg.Errors.Adapter != "remote" {
		t.Fatalf("adapter=%q want remote from backend alias", cfg.Errors.Adapter)
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// ─── Deployment mode (AF_STACK_MODE) ──────────────────────────────────────

func TestModeDefaultsToSaaS(t *testing.T) {
	cfg := Default()
	if cfg.Mode != ModeSaaS {
		t.Errorf("default mode = %q, want %q", cfg.Mode, ModeSaaS)
	}
	if cfg.PersonalMode() {
		t.Error("default config should not be personal mode")
	}
}

func TestModeEnvOverride(t *testing.T) {
	t.Setenv("AF_STACK_AGENTFIELD_URL", "http://af:8081")
	t.Setenv("AF_STACK_MODE", "personal")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.PersonalMode() {
		t.Errorf("mode = %q, want personal", cfg.Mode)
	}
}

func TestModeInvalidRejected(t *testing.T) {
	cfg := Default()
	cfg.Mode = "bogus"
	if err := validate(cfg); err == nil {
		t.Fatal("validate should reject an unknown mode")
	}
}

func TestModeEmptyIsAllowed(t *testing.T) {
	// An empty mode is treated as saas (back-compat with older configs).
	cfg := Default()
	cfg.Mode = ""
	if err := validate(cfg); err != nil {
		t.Fatalf("empty mode should validate: %v", err)
	}
	if cfg.PersonalMode() {
		t.Error("empty mode should not be personal")
	}
}

func TestBillingEnabledMatrix(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		billing *bool // nil = unset in Modules.Enabled
		want    bool
	}{
		{"saas default (unset) => on", ModeSaaS, nil, true},
		{"saas explicit true => on", ModeSaaS, boolPtr(true), true},
		{"saas explicit false => off", ModeSaaS, boolPtr(false), false},
		{"personal unset => off", ModePersonal, nil, false},
		{"personal even if flag true => off", ModePersonal, boolPtr(true), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Mode = tc.mode
			if tc.billing != nil {
				cfg.Modules.Enabled = map[string]bool{"billing": *tc.billing}
			}
			if got := cfg.BillingEnabled(); got != tc.want {
				t.Errorf("BillingEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func TestProductionHardeningMatrix(t *testing.T) {
	cases := []struct {
		name string
		mode string
		env  string
		want bool
	}{
		{"saas + production => armed", ModeSaaS, "production", true},
		{"empty mode (=saas) + production => armed", "", "production", true},
		{"saas + dev => off", ModeSaaS, "dev", false},
		{"saas + empty env => off", ModeSaaS, "", false},
		{"personal + production => off", ModePersonal, "production", false},
		{"saas + PRODUCTION (case) => armed", ModeSaaS, "PRODUCTION", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Mode = tc.mode
			cfg.Env = tc.env
			if got := cfg.ProductionHardening(); got != tc.want {
				t.Errorf("ProductionHardening() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnvOverrideAFStackEnv(t *testing.T) {
	t.Setenv("AF_STACK_ENV", "Production")
	cfg := Default()
	applyEnvOverrides(&cfg)
	if !cfg.ProductionEnv() {
		t.Errorf("AF_STACK_ENV=Production should set ProductionEnv, Env=%q", cfg.Env)
	}
}

func TestSandboxNetworkPolicyDefaultsIsolated(t *testing.T) {
	cfg := Default()
	if cfg.SandboxNetworkPolicy() != NetworkPolicyIsolated {
		t.Errorf("default sandbox network policy = %q, want isolated", cfg.SandboxNetworkPolicy())
	}
	cfg.Sandbox.NetworkPolicy = ""
	if cfg.SandboxNetworkPolicy() != NetworkPolicyIsolated {
		t.Errorf("empty sandbox network policy should normalise to isolated, got %q", cfg.SandboxNetworkPolicy())
	}
	cfg.Sandbox.NetworkPolicy = "OPEN"
	if cfg.SandboxNetworkPolicy() != "open" {
		t.Errorf("sandbox network policy should lowercase, got %q", cfg.SandboxNetworkPolicy())
	}
}

func TestSandboxNetworkPolicyEnvOverride(t *testing.T) {
	t.Setenv("AF_STACK_SANDBOX_NETWORK_POLICY", "open")
	cfg := Default()
	applyEnvOverrides(&cfg)
	if cfg.SandboxNetworkPolicy() != "open" {
		t.Errorf("AF_STACK_SANDBOX_NETWORK_POLICY=open should override, got %q", cfg.SandboxNetworkPolicy())
	}
}
