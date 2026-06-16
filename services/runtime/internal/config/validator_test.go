// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeatureConfigExampleValidates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "backai.config.yaml.example"))
	if err != nil {
		t.Fatal(err)
	}
	_, issues, err := ParseFeatureConfig(data, func(string) string { return "" })
	if err != nil {
		t.Fatalf("ParseFeatureConfig: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
}

func TestFeatureConfigRejectsContainerMetricsWithoutMetrics(t *testing.T) {
	raw := RawFeatureConfig{
		Preset: PresetLean,
		Features: RawFeatures{Metrics: &MetricsFeature{
			Enabled:          false,
			Backend:          "none",
			ContainerMetrics: true,
		}},
	}
	_, issues, err := ResolveFeatureConfig(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !containsIssue(issues, "metrics.container_metrics", "requires features.metrics.enabled") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestFeatureConfigRejectsGlitchTipWithoutEnv(t *testing.T) {
	raw := RawFeatureConfig{
		Preset: PresetLean,
		Features: RawFeatures{Errors: &ErrorsFeature{
			Enabled: true,
			Backend: "glitchtip",
		}},
	}
	_, issues, err := ResolveFeatureConfig(raw, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if !containsIssue(issues, "errors.backend=glitchtip", "SENTRY_DSN") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestFeatureConfigRejectsUnknownField(t *testing.T) {
	_, _, err := ParseFeatureConfig([]byte(`
preset: lean
features:
  dbHealth: { enabled: true }
`), nil)
	if err == nil || !strings.Contains(err.Error(), "field dbHealth not found") {
		t.Fatalf("err = %v, want unknown field", err)
	}
}

func TestFeatureConfigRejectsBadLogsAdapter(t *testing.T) {
	_, _, err := ParseFeatureConfig([]byte(`
preset: lean
features:
  logs:
    enabled: false
    adapter: foo
`), nil)
	if err == nil || !strings.Contains(err.Error(), "features.logs.adapter") {
		t.Fatalf("err = %v, want adapter enum error", err)
	}
}

func TestFeatureConfigAcceptsLegacyLogsBackend(t *testing.T) {
	cfg, _, err := ParseFeatureConfig([]byte(`
preset: lean
features:
  logs:
    enabled: false
    backend: loki
`), nil)
	if err != nil {
		t.Fatalf("ParseFeatureConfig: %v", err)
	}
	if cfg.Features.Logs.Adapter != "loki" {
		t.Fatalf("logs adapter=%q want loki", cfg.Features.Logs.Adapter)
	}
}

func TestFeatureConfigPresetLean(t *testing.T) {
	f, err := PresetFeatures(PresetLean)
	if err != nil {
		t.Fatal(err)
	}
	if !f.DBHealth.Enabled || f.Logs.Enabled || f.Metrics.ContainerMetrics {
		t.Fatalf("lean = %+v", f)
	}
}

func TestFeatureConfigFullObservabilityOverrideErrorsOff(t *testing.T) {
	raw := RawFeatureConfig{
		Preset: PresetFullObservability,
		Features: RawFeatures{Errors: &ErrorsFeature{
			Enabled: false,
			Backend: "logfilter",
		}},
	}
	cfg, issues, err := ResolveFeatureConfig(raw, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
	if !cfg.Features.Logs.Enabled || cfg.Features.Errors.Enabled {
		t.Fatalf("merged = %+v", cfg.Features)
	}
}

func TestFeatureConfigCustomRequiresEveryFeature(t *testing.T) {
	_, _, err := ParseFeatureConfig([]byte(`
preset: custom
features:
  db_health: { enabled: true }
`), nil)
	if err == nil || !strings.Contains(err.Error(), "preset custom requires every feature") {
		t.Fatalf("err = %v", err)
	}
}

func containsIssue(issues []ValidationError, feature, fragment string) bool {
	for _, issue := range issues {
		if issue.Feature == feature && strings.Contains(issue.Message, fragment) {
			return true
		}
	}
	return false
}
