// SPDX-License-Identifier: Apache-2.0

package appmetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRegisterAndObserve(t *testing.T) {
	ResetForTest()
	reg := prometheus.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ObserveCostUSD("", "openrouter/gpt-4o-mini", "", 0.25)
	ObserveLLMRequest("tenant-1", "openrouter/gpt-4o-mini", 200, 0.12)
	ObserveLLMRequest("tenant-1", "openrouter/gpt-4o-mini", 502, 0)
	ObserveSandboxRun("docker", "done")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	seen := map[string]bool{}
	var sawPlatform, sawDocker bool
	for _, family := range families {
		seen[family.GetName()] = true
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "tenant" && label.GetValue() == "platform" {
					sawPlatform = true
				}
				if label.GetName() == "adapter" && label.GetValue() == "docker" {
					sawDocker = true
				}
			}
		}
	}
	for _, name := range []string{
		"backai_cost_usd_total",
		"backai_llm_requests_total",
		"backai_llm_ttft_seconds",
		"backai_sandbox_runs_total",
	} {
		if !seen[name] {
			t.Fatalf("missing metric family %s in %+v", name, seen)
		}
	}
	if !sawPlatform || !sawDocker {
		t.Fatalf("labels not normalized sawPlatform=%v sawDocker=%v", sawPlatform, sawDocker)
	}
}
