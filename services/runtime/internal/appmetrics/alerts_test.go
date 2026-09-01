// SPDX-License-Identifier: Apache-2.0

package appmetrics

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v3"
)

// alertsFile is the shipped default alert rules, located relative to this test
// file (CWD-independent) so the test runs from anywhere.
func alertsFile(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	// internal/appmetrics -> repo root is five levels up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(root, "deploy", "prometheus", "alerts.yml")
}

// Minimal shape of the Prometheus rule-file format we validate structurally.
type alertRuleFile struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Alert       string            `yaml:"alert"`
			Expr        string            `yaml:"expr"`
			For         string            `yaml:"for"`
			Labels      map[string]string `yaml:"labels"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

func loadAlerts(t *testing.T) alertRuleFile {
	t.Helper()
	data, err := os.ReadFile(alertsFile(t))
	if err != nil {
		t.Fatalf("read alerts.yml: %v", err)
	}
	var f alertRuleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("alerts.yml is not valid YAML: %v", err)
	}
	return f
}

// TestAlertsParseAndAreWellFormed validates the rule file's structure: promtool
// isn't available in CI, so we assert the schema Prometheus requires — groups
// with names, rules with a non-empty alert+expr, a valid severity label, and
// summary+description annotations.
func TestAlertsParseAndAreWellFormed(t *testing.T) {
	f := loadAlerts(t)
	if len(f.Groups) == 0 {
		t.Fatal("alerts.yml has no groups")
	}
	validSeverity := map[string]bool{"warning": true, "critical": true, "info": true}
	seenAlerts := map[string]bool{}
	total := 0
	for _, g := range f.Groups {
		if g.Name == "" {
			t.Error("a group is missing its name")
		}
		if len(g.Rules) == 0 {
			t.Errorf("group %q has no rules", g.Name)
		}
		for _, r := range g.Rules {
			total++
			if r.Alert == "" {
				t.Errorf("group %q has a rule with no alert name", g.Name)
			}
			if seenAlerts[r.Alert] {
				t.Errorf("duplicate alert name %q", r.Alert)
			}
			seenAlerts[r.Alert] = true
			if r.Expr == "" {
				t.Errorf("alert %q has an empty expr", r.Alert)
			}
			if !validSeverity[r.Labels["severity"]] {
				t.Errorf("alert %q has invalid/missing severity label %q", r.Alert, r.Labels["severity"])
			}
			if r.Annotations["summary"] == "" || r.Annotations["description"] == "" {
				t.Errorf("alert %q must have summary + description annotations", r.Alert)
			}
		}
	}
	if total == 0 {
		t.Fatal("no alert rules found")
	}
}

// TestAlertsCoverRequiredSignals asserts the operating-contract coverage the
// PRD requires is present (by alert name).
func TestAlertsCoverRequiredSignals(t *testing.T) {
	f := loadAlerts(t)
	names := map[string]bool{}
	for _, g := range f.Groups {
		for _, r := range g.Rules {
			names[r.Alert] = true
		}
	}
	required := []string{
		"BackAIReadinessFlapping",
		"BackAILLMProviderFailureRateHigh",
		"BackAIJobsQueueBacklog",
		"BackAIBudgetRejectionsSpike",
		"BackAIWebhookDeliveryFailureRateHigh",
		"BackAIDBConnectionSaturation",
		"BackAIBackupTestStale",
	}
	for _, want := range required {
		if !names[want] {
			t.Errorf("alerts.yml is missing required alert %q", want)
		}
	}
}

// TestAlertsReferenceOnlyExistingMetrics guards against alerting on a metric the
// runtime doesn't export: every backai_* token in an expr must be a metric
// family the appmetrics registry actually registers.
func TestAlertsReferenceOnlyExistingMetrics(t *testing.T) {
	ResetForTest()
	reg := prometheus.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	registered := map[string]bool{}
	for _, fam := range families {
		registered[fam.GetName()] = true
	}

	metricToken := regexp.MustCompile(`backai_[a-z0-9_]+`)
	f := loadAlerts(t)
	for _, g := range f.Groups {
		for _, r := range g.Rules {
			for _, tok := range metricToken.FindAllString(r.Expr, -1) {
				if !registered[tok] {
					t.Errorf("alert %q references metric %q that appmetrics does not register", r.Alert, tok)
				}
			}
		}
	}
}
