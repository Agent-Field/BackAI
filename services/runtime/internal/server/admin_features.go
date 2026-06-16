// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/probe"
)

type featureCapabilityStatus string

const (
	featureStatusOK            featureCapabilityStatus = "ok"
	featureStatusDegraded      featureCapabilityStatus = "degraded"
	featureStatusNotConfigured featureCapabilityStatus = "not_configured"
	featureStatusUnavailable   featureCapabilityStatus = "unavailable"
)

type adminFeatureDetail struct {
	Key      string `json:"key"`
	Value    any    `json:"value"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type adminFeatureEntry struct {
	Enabled          *bool                   `json:"enabled,omitempty"`
	VirtualKeys      *bool                   `json:"virtual_keys,omitempty"`
	SpendTracking    *bool                   `json:"spend_tracking,omitempty"`
	Backend          string                  `json:"backend,omitempty"`
	ContainerMetrics *bool                   `json:"container_metrics,omitempty"`
	CapabilityStatus featureCapabilityStatus `json:"capability_status"`
	Details          []adminFeatureDetail    `json:"details,omitempty"`
}

type adminFeaturesResponse struct {
	Preset            string                       `json:"preset"`
	Features          map[string]adminFeatureEntry `json:"features"`
	ValidatorWarnings []config.ValidationError     `json:"validator_warnings"`
}

func (s *Server) registerAdminFeaturesRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/features", s.handleAdminFeatures)
}

func (s *Server) registerAdminFeaturesOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/features", openapi.RouteMeta{
		Summary: "List configured platform features and capability probe status",
		Tags:    []string{"admin"},
	})
}

func (s *Server) handleAdminFeatures(w http.ResponseWriter, r *http.Request) {
	cfg := s.featureConfig
	if cfg.Preset == "" {
		lean, _ := config.PresetFeatures(config.PresetLean)
		cfg = config.FeatureConfig{Preset: config.PresetLean, Features: lean}
	}
	probes := map[string]probe.Result{}
	if s.probeRegistry != nil {
		probes = s.probeRegistry.Snapshot()
	}
	f := cfg.Features
	features := map[string]adminFeatureEntry{
		"db_health": featureBoolEntry(f.DBHealth.Enabled, statusFromDBProbes(probes), probeDetails(probes,
			probe.PGStatStatementsProbeID,
			probe.PGRoleReadAllStatsProbeID,
		)),
		"provider_health_polling": featureBoolEntry(f.ProviderHealthPolling.Enabled, featureStatusOK, nil),
		"notifications_mute":      featureBoolEntry(f.NotificationsMute.Enabled, featureStatusOK, nil),
		"brand_override":          featureBoolEntry(f.BrandOverride.Enabled, featureStatusOK, nil),
		"search_index_stats": featureBoolEntry(f.SearchIndexStats.Enabled, statusFromProbe(probes[probe.PGRoleReadAllStatsProbeID], f.SearchIndexStats.Enabled), probeDetails(probes,
			probe.PGRoleReadAllStatsProbeID,
		)),
		"cron_manual_trigger": featureBoolEntry(f.CronManualTrigger.Enabled, featureStatusOK, nil),
		"cache_flush":         featureBoolEntry(f.CacheFlush.Enabled, featureStatusOK, nil),
		"api_key_rotate":      featureBoolEntry(f.APIKeyRotate.Enabled, featureStatusOK, nil),
		"llm_gateway":         llmGatewayFeatureEntry(f.LLMGateway, probes),
		"logs":                backendFeatureEntry(f.Logs.Enabled, f.Logs.Backend),
		"traces":              backendFeatureEntry(f.Traces.Enabled, f.Traces.Backend),
		"metrics":             metricsFeatureEntry(f.Metrics),
		"errors":              backendFeatureEntry(f.Errors.Enabled, f.Errors.Backend),
	}
	warnings := s.featureWarnings
	if warnings == nil {
		warnings = []config.ValidationError{}
	}
	writeJSON(w, http.StatusOK, adminFeaturesResponse{
		Preset:            cfg.Preset,
		Features:          features,
		ValidatorWarnings: warnings,
	})
}

func featureBoolEntry(enabled bool, status featureCapabilityStatus, details []adminFeatureDetail) adminFeatureEntry {
	if !enabled {
		status = featureStatusNotConfigured
	}
	return adminFeatureEntry{Enabled: boolPtr(enabled), CapabilityStatus: status, Details: details}
}

func backendFeatureEntry(enabled bool, backend string) adminFeatureEntry {
	status := featureStatusOK
	if !enabled {
		status = featureStatusNotConfigured
	}
	return adminFeatureEntry{Enabled: boolPtr(enabled), Backend: backend, CapabilityStatus: status}
}

func metricsFeatureEntry(f config.MetricsFeature) adminFeatureEntry {
	entry := backendFeatureEntry(f.Enabled, f.Backend)
	entry.ContainerMetrics = boolPtr(f.ContainerMetrics)
	return entry
}

func llmGatewayFeatureEntry(f config.LLMGatewayFeature, probes map[string]probe.Result) adminFeatureEntry {
	details := probeDetails(probes, probe.LiteLLMVirtualKeysProbeID, probe.LiteLLMSpendTrackingProbeID)
	status := featureStatusDegraded
	if res, ok := probes[probe.LiteLLMVirtualKeysProbeID]; ok {
		status = statusFromProbe(res, true)
		if status == featureStatusUnavailable {
			status = featureStatusDegraded
		}
	}
	return adminFeatureEntry{
		VirtualKeys:      boolPtr(f.VirtualKeys),
		SpendTracking:    boolPtr(f.SpendTracking),
		CapabilityStatus: status,
		Details:          details,
	}
}

func statusFromDBProbes(probes map[string]probe.Result) featureCapabilityStatus {
	stat := statusFromProbe(probes[probe.PGStatStatementsProbeID], true)
	role := statusFromProbe(probes[probe.PGRoleReadAllStatsProbeID], true)
	if stat == featureStatusOK && role == featureStatusOK {
		return featureStatusOK
	}
	if stat == featureStatusUnavailable || role == featureStatusUnavailable {
		return featureStatusUnavailable
	}
	return featureStatusDegraded
}

func statusFromProbe(res probe.Result, enabled bool) featureCapabilityStatus {
	if !enabled {
		return featureStatusNotConfigured
	}
	active, _ := res.Value.(bool)
	if active && res.Severity == probe.SeverityOK {
		return featureStatusOK
	}
	if res.ProbeID == "" {
		return featureStatusUnavailable
	}
	if res.Severity == probe.SeverityUnavailable {
		return featureStatusUnavailable
	}
	return featureStatusDegraded
}

func probeDetails(probes map[string]probe.Result, ids ...string) []adminFeatureDetail {
	out := []adminFeatureDetail{}
	for _, id := range ids {
		res, ok := probes[id]
		if !ok {
			continue
		}
		key := res.Capability
		if dot := strings.LastIndexByte(key, '.'); dot >= 0 && dot+1 < len(key) {
			key = key[dot+1:]
		}
		out = append(out, adminFeatureDetail{
			Key:      key,
			Value:    res.Value,
			Severity: string(res.Severity),
			Message:  res.Detail,
		})
	}
	return out
}

func boolPtr(v bool) *bool { return &v }
