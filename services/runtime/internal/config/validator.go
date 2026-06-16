// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

type ValidationLevel string

const (
	ValidationErrorLevel   ValidationLevel = "error"
	ValidationWarningLevel ValidationLevel = "warning"
)

type ValidationError struct {
	Feature     string          `json:"feature"`
	Level       ValidationLevel `json:"level"`
	Message     string          `json:"message"`
	Remediation string          `json:"remediation"`
}

type EnvLookup func(string) string

func ValidateFeatures(cfg FeatureConfig, env EnvLookup) []ValidationError {
	if env == nil {
		env = os.Getenv
	}
	var issues []ValidationError
	for _, rule := range featureRules {
		if len(rule.BackendOptions) > 0 {
			continue
		}
		if !featureEnabled(cfg.Features, rule.Feature) {
			continue
		}
		for _, req := range rule.Requires {
			if !featureEnabled(cfg.Features, req) {
				issues = append(issues, ValidationError{
					Feature:     rule.Feature,
					Level:       ValidationErrorLevel,
					Message:     fmt.Sprintf("features.%s=true requires features.%s=true", rule.Feature, req),
					Remediation: fmt.Sprintf("Enable features.%s or disable features.%s.", req, rule.Feature),
				})
			}
		}
		for _, envName := range rule.RequiresEnv {
			if strings.TrimSpace(env(envName)) == "" {
				issues = append(issues, ValidationError{
					Feature:     rule.Feature,
					Level:       ValidationErrorLevel,
					Message:     fmt.Sprintf("features.%s requires env %s", rule.Feature, envName),
					Remediation: fmt.Sprintf("Set %s, or disable features.%s.", envName, rule.Feature),
				})
			}
		}
	}
	return issues
}

func validateFeatureEnums(f Features) error {
	for _, rule := range featureRules {
		if len(rule.BackendOptions) == 0 {
			continue
		}
		value := featureStringValue(f, rule.Feature)
		if value != "" && !slices.Contains(rule.BackendOptions, value) {
			return fmt.Errorf("features.%s=%q is not supported; use one of: %s", rule.Feature, value, strings.Join(rule.BackendOptions, ", "))
		}
	}
	return nil
}

func featureEnabled(f Features, name string) bool {
	switch name {
	case "db_health":
		return f.DBHealth.Enabled
	case "provider_health_polling":
		return f.ProviderHealthPolling.Enabled
	case "notifications_mute":
		return f.NotificationsMute.Enabled
	case "brand_override":
		return f.BrandOverride.Enabled
	case "search_index_stats":
		return f.SearchIndexStats.Enabled
	case "cron_manual_trigger":
		return f.CronManualTrigger.Enabled
	case "cache_flush":
		return f.CacheFlush.Enabled
	case "api_key_rotate":
		return f.APIKeyRotate.Enabled
	case "llm_gateway.virtual_keys":
		return f.LLMGateway.VirtualKeys
	case "llm_gateway.spend_tracking":
		return f.LLMGateway.SpendTracking
	case "logs.enabled":
		return f.Logs.Enabled
	case "traces.enabled":
		return f.Traces.Enabled
	case "metrics.enabled":
		return f.Metrics.Enabled
	case "metrics.container_metrics":
		return f.Metrics.ContainerMetrics
	case "errors.enabled":
		return f.Errors.Enabled
	case "errors.backend=glitchtip":
		return f.Errors.Enabled && f.Errors.Backend == "glitchtip"
	default:
		return false
	}
}

func featureStringValue(f Features, name string) string {
	switch name {
	case "logs.adapter":
		if f.Logs.Adapter != "" {
			return f.Logs.Adapter
		}
		return f.Logs.Backend
	case "traces.adapter":
		if f.Traces.Adapter != "" {
			return f.Traces.Adapter
		}
		return f.Traces.Backend
	case "metrics.adapter":
		if f.Metrics.Adapter != "" {
			return f.Metrics.Adapter
		}
		return f.Metrics.Backend
	case "errors.backend":
		return f.Errors.Backend
	default:
		return ""
	}
}

func CountEnabledFeatures(f Features) int {
	count := 0
	for _, on := range []bool{
		f.DBHealth.Enabled,
		f.ProviderHealthPolling.Enabled,
		f.NotificationsMute.Enabled,
		f.BrandOverride.Enabled,
		f.SearchIndexStats.Enabled,
		f.CronManualTrigger.Enabled,
		f.CacheFlush.Enabled,
		f.APIKeyRotate.Enabled,
		f.LLMGateway.VirtualKeys,
		f.LLMGateway.SpendTracking,
		f.Logs.Enabled,
		f.Traces.Enabled,
		f.Metrics.Enabled,
		f.Metrics.ContainerMetrics,
		f.Errors.Enabled,
	} {
		if on {
			count++
		}
	}
	return count
}

func joinList(values []string) string {
	return strings.Join(values, ", ")
}
