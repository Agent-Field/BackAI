// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultFeatureConfigPath = "backai.config.yaml"

// FeatureConfig is the operator-facing backai.config.yaml shape.
type FeatureConfig struct {
	Preset   string   `yaml:"preset" json:"preset"`
	Features Features `yaml:"features" json:"features"`
}

// RawFeatureConfig preserves pointer fields so preset overrides can be
// distinguished from omitted values.
type RawFeatureConfig struct {
	Preset   string      `yaml:"preset" json:"preset"`
	Features RawFeatures `yaml:"features" json:"features"`
}

type Features struct {
	DBHealth              FeatureBool       `yaml:"db_health" json:"db_health"`
	ProviderHealthPolling FeatureBool       `yaml:"provider_health_polling" json:"provider_health_polling"`
	NotificationsMute     FeatureBool       `yaml:"notifications_mute" json:"notifications_mute"`
	BrandOverride         FeatureBool       `yaml:"brand_override" json:"brand_override"`
	SearchIndexStats      FeatureBool       `yaml:"search_index_stats" json:"search_index_stats"`
	CronManualTrigger     FeatureBool       `yaml:"cron_manual_trigger" json:"cron_manual_trigger"`
	CacheFlush            FeatureBool       `yaml:"cache_flush" json:"cache_flush"`
	APIKeyRotate          FeatureBool       `yaml:"api_key_rotate" json:"api_key_rotate"`
	LLMGateway            LLMGatewayFeature `yaml:"llm_gateway" json:"llm_gateway"`
	Logs                  LogsFeature       `yaml:"logs" json:"logs"`
	Traces                TracesFeature     `yaml:"traces" json:"traces"`
	Metrics               MetricsFeature    `yaml:"metrics" json:"metrics"`
	Errors                ErrorsFeature     `yaml:"errors" json:"errors"`
}

type RawFeatures struct {
	DBHealth              *FeatureBool       `yaml:"db_health" json:"db_health"`
	ProviderHealthPolling *FeatureBool       `yaml:"provider_health_polling" json:"provider_health_polling"`
	NotificationsMute     *FeatureBool       `yaml:"notifications_mute" json:"notifications_mute"`
	BrandOverride         *FeatureBool       `yaml:"brand_override" json:"brand_override"`
	SearchIndexStats      *FeatureBool       `yaml:"search_index_stats" json:"search_index_stats"`
	CronManualTrigger     *FeatureBool       `yaml:"cron_manual_trigger" json:"cron_manual_trigger"`
	CacheFlush            *FeatureBool       `yaml:"cache_flush" json:"cache_flush"`
	APIKeyRotate          *FeatureBool       `yaml:"api_key_rotate" json:"api_key_rotate"`
	LLMGateway            *LLMGatewayFeature `yaml:"llm_gateway" json:"llm_gateway"`
	Logs                  *LogsFeature       `yaml:"logs" json:"logs"`
	Traces                *TracesFeature     `yaml:"traces" json:"traces"`
	Metrics               *MetricsFeature    `yaml:"metrics" json:"metrics"`
	Errors                *ErrorsFeature     `yaml:"errors" json:"errors"`
}

type FeatureBool struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type LLMGatewayFeature struct {
	VirtualKeys   bool `yaml:"virtual_keys" json:"virtual_keys"`
	SpendTracking bool `yaml:"spend_tracking" json:"spend_tracking"`
}

type LogsFeature struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Adapter string `yaml:"adapter" json:"adapter"`
	// Backend is accepted for Block 2 config-file compatibility. Block 3
	// standardises this slot on adapter to match the runtime env convention.
	Backend string            `yaml:"backend,omitempty" json:"-"`
	Loki    LogsLokiFeature   `yaml:"loki" json:"loki"`
	Remote  LogsRemoteFeature `yaml:"remote" json:"remote"`
}

type LogsLokiFeature struct {
	URL    string `yaml:"url" json:"url"`
	Tenant string `yaml:"tenant" json:"tenant"`
}

type LogsRemoteFeature struct {
	URL   string `yaml:"url" json:"url"`
	Token string `yaml:"token" json:"token"`
}

type TracesFeature struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Backend string `yaml:"backend" json:"backend"`
}

type MetricsFeature struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Backend          string `yaml:"backend" json:"backend"`
	ContainerMetrics bool   `yaml:"container_metrics" json:"container_metrics"`
}

type ErrorsFeature struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Backend string `yaml:"backend" json:"backend"`
}

// LoadFeatureConfig reads backai.config.yaml. Missing path returns the lean
// preset so fresh checkouts keep booting without a config file.
func LoadFeatureConfig(path string) (FeatureConfig, []ValidationError, error) {
	if path == "" {
		path = DefaultFeatureConfigPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			raw := RawFeatureConfig{Preset: PresetLean}
			return ResolveFeatureConfig(raw, nil)
		}
		return FeatureConfig{}, nil, fmt.Errorf("read feature config %s: %w", path, err)
	}
	return ParseFeatureConfig(data, nil)
}

func ParseFeatureConfig(data []byte, env EnvLookup) (FeatureConfig, []ValidationError, error) {
	var raw RawFeatureConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return FeatureConfig{}, nil, fmt.Errorf("Layer 1 (schema): %w", err)
	}
	return ResolveFeatureConfig(raw, env)
}

func ResolveFeatureConfig(raw RawFeatureConfig, env EnvLookup) (FeatureConfig, []ValidationError, error) {
	preset := raw.Preset
	if preset == "" {
		preset = PresetLean
	}
	if preset == PresetCustom {
		if missing := raw.Features.missingFields(); len(missing) > 0 {
			return FeatureConfig{}, nil, fmt.Errorf("Layer 1 (schema): preset custom requires every feature; missing %s", joinList(missing))
		}
	}
	base, err := PresetFeatures(preset)
	if err != nil {
		return FeatureConfig{}, nil, fmt.Errorf("Layer 1 (schema): %w", err)
	}
	merged := mergeFeatureOverrides(base, raw.Features)
	if err := validateFeatureEnums(merged); err != nil {
		return FeatureConfig{}, nil, fmt.Errorf("Layer 1 (schema): %w", err)
	}
	cfg := FeatureConfig{Preset: preset, Features: merged}
	issues := ValidateFeatures(cfg, env)
	return cfg, issues, nil
}

func mergeFeatureOverrides(base Features, raw RawFeatures) Features {
	if raw.DBHealth != nil {
		base.DBHealth = *raw.DBHealth
	}
	if raw.ProviderHealthPolling != nil {
		base.ProviderHealthPolling = *raw.ProviderHealthPolling
	}
	if raw.NotificationsMute != nil {
		base.NotificationsMute = *raw.NotificationsMute
	}
	if raw.BrandOverride != nil {
		base.BrandOverride = *raw.BrandOverride
	}
	if raw.SearchIndexStats != nil {
		base.SearchIndexStats = *raw.SearchIndexStats
	}
	if raw.CronManualTrigger != nil {
		base.CronManualTrigger = *raw.CronManualTrigger
	}
	if raw.CacheFlush != nil {
		base.CacheFlush = *raw.CacheFlush
	}
	if raw.APIKeyRotate != nil {
		base.APIKeyRotate = *raw.APIKeyRotate
	}
	if raw.LLMGateway != nil {
		base.LLMGateway = *raw.LLMGateway
	}
	if raw.Logs != nil {
		if raw.Logs.Adapter == "" && raw.Logs.Backend != "" {
			raw.Logs.Adapter = raw.Logs.Backend
		}
		base.Logs = *raw.Logs
	}
	if raw.Traces != nil {
		base.Traces = *raw.Traces
	}
	if raw.Metrics != nil {
		base.Metrics = *raw.Metrics
	}
	if raw.Errors != nil {
		base.Errors = *raw.Errors
	}
	return base
}

func (r RawFeatures) missingFields() []string {
	var missing []string
	if r.DBHealth == nil {
		missing = append(missing, "features.db_health")
	}
	if r.ProviderHealthPolling == nil {
		missing = append(missing, "features.provider_health_polling")
	}
	if r.NotificationsMute == nil {
		missing = append(missing, "features.notifications_mute")
	}
	if r.BrandOverride == nil {
		missing = append(missing, "features.brand_override")
	}
	if r.SearchIndexStats == nil {
		missing = append(missing, "features.search_index_stats")
	}
	if r.CronManualTrigger == nil {
		missing = append(missing, "features.cron_manual_trigger")
	}
	if r.CacheFlush == nil {
		missing = append(missing, "features.cache_flush")
	}
	if r.APIKeyRotate == nil {
		missing = append(missing, "features.api_key_rotate")
	}
	if r.LLMGateway == nil {
		missing = append(missing, "features.llm_gateway")
	}
	if r.Logs == nil {
		missing = append(missing, "features.logs")
	}
	if r.Traces == nil {
		missing = append(missing, "features.traces")
	}
	if r.Metrics == nil {
		missing = append(missing, "features.metrics")
	}
	if r.Errors == nil {
		missing = append(missing, "features.errors")
	}
	return missing
}
