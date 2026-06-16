// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"strings"
)

const (
	PresetLean              = "lean"
	PresetFullObservability = "full-observability"
	PresetProduction        = "production"
	PresetCustom            = "custom"
)

type FeatureRule struct {
	Feature         string
	Requires        []string
	Conflicts       []string
	MutexGroup      string
	RequiresEnv     []string
	RequiresPGGrant []string
	RequiresPGConf  []string
	BackendOptions  []string
}

var featureRules = []FeatureRule{
	{Feature: "metrics.container_metrics", Requires: []string{"metrics.enabled"}},
	{Feature: "llm_gateway.spend_tracking", Requires: []string{"llm_gateway.virtual_keys"}},
	{Feature: "llm_gateway.virtual_keys", RequiresEnv: []string{"LITELLM_DATABASE_URL"}},
	{Feature: "errors.backend=glitchtip", RequiresEnv: []string{"SENTRY_DSN", "AF_STACK_ERRORS_GLITCHTIP_TOKEN"}},
	{Feature: "db_health", RequiresPGGrant: []string{"pg_read_all_stats"}, RequiresPGConf: []string{"shared_preload_libraries:pg_stat_statements"}},
	{Feature: "search_index_stats", RequiresPGGrant: []string{"pg_read_all_stats"}},
	{Feature: "logs.adapter", BackendOptions: []string{"ring", "loki", "remote"}},
	{Feature: "traces.adapter", BackendOptions: []string{"empty", "tempo", "remote"}},
	{Feature: "metrics.backend", BackendOptions: []string{"none", "prometheus", "remote"}},
	{Feature: "errors.backend", BackendOptions: []string{"logfilter", "glitchtip", "remote"}},
}

var presets = map[string]Features{
	PresetLean: {
		DBHealth:              FeatureBool{Enabled: true},
		ProviderHealthPolling: FeatureBool{Enabled: true},
		NotificationsMute:     FeatureBool{Enabled: true},
		BrandOverride:         FeatureBool{Enabled: true},
		SearchIndexStats:      FeatureBool{Enabled: true},
		CronManualTrigger:     FeatureBool{Enabled: true},
		CacheFlush:            FeatureBool{Enabled: true},
		APIKeyRotate:          FeatureBool{Enabled: true},
		LLMGateway:            LLMGatewayFeature{VirtualKeys: false, SpendTracking: false},
		Logs:                  LogsFeature{Enabled: false, Adapter: "ring"},
		Traces:                TracesFeature{Enabled: false, Adapter: "empty"},
		Metrics:               MetricsFeature{Enabled: false, Backend: "none", ContainerMetrics: false},
		Errors:                ErrorsFeature{Enabled: false, Backend: "logfilter"},
	},
}

func init() {
	full := presets[PresetLean]
	full.Logs = LogsFeature{Enabled: true, Adapter: "loki"}
	full.Traces = TracesFeature{Enabled: true, Adapter: "tempo"}
	full.Metrics = MetricsFeature{Enabled: true, Backend: "prometheus", ContainerMetrics: true}
	full.Errors = ErrorsFeature{Enabled: true, Backend: "glitchtip"}
	presets[PresetFullObservability] = full

	prod := full
	prod.LLMGateway = LLMGatewayFeature{VirtualKeys: true, SpendTracking: true}
	presets[PresetProduction] = prod

	presets[PresetCustom] = Features{}
}

func PresetFeatures(name string) (Features, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = PresetLean
	}
	f, ok := presets[name]
	if !ok {
		return Features{}, fmt.Errorf("preset must be lean|full-observability|production|custom, got %q", name)
	}
	return f, nil
}

func FeatureRules() []FeatureRule {
	out := make([]FeatureRule, len(featureRules))
	copy(out, featureRules)
	return out
}
