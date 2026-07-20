// SPDX-License-Identifier: Apache-2.0

// Package appmetrics owns AF Stack application-level Prometheus collectors.
//
// Collectors are registered against the runtime's custom Prometheus registry
// in internal/observability, not prometheus.DefaultRegisterer.
package appmetrics

import (
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultTenant = "platform"
	defaultModel  = "none"
	defaultAgent  = "none"
	maxLabelLen   = 160
)

var (
	costUSDTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backai_cost_usd_total",
		Help: "Total estimated BackAI spend in USD by tenant, model, and agent.",
	}, []string{"tenant", "model", "agent"})

	llmRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backai_llm_requests_total",
		Help: "Total LLM gateway requests by tenant, model, and status.",
	}, []string{"tenant", "model", "status"})

	llmTTFTSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "backai_llm_ttft_seconds",
		Help:    "Time to first streamed LLM token in seconds.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"model"})

	sandboxRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backai_sandbox_runs_total",
		Help: "Total sandbox runs by adapter and terminal status.",
	}, []string{"adapter", "status"})

	runsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backai_runs_total",
		Help: "Total AgentField execute runs by agent and terminal status.",
	}, []string{"agent", "status"})

	// R7 production operating contract metrics — the signals the default
	// Prometheus alert rules (deploy/prometheus/alerts.yml) fire on.

	budgetRejectionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backai_budget_rejections_total",
		Help: "Total LLM gateway calls rejected by budget enforcement (402), by tenant and reason.",
	}, []string{"tenant", "reason"})

	jobsQueueOldestAgeSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backai_jobs_queue_oldest_age_seconds",
		Help: "Age in seconds of the oldest not-yet-completed background job (0 when the queue is empty or unsampled).",
	})

	dbPoolAcquiredConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backai_db_pool_acquired_connections",
		Help: "Currently acquired (in-use) connections in the runtime Postgres pool.",
	})

	dbPoolMaxConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backai_db_pool_max_connections",
		Help: "Maximum connections the runtime Postgres pool may open.",
	})

	backupTestLastSuccessTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backai_backup_test_last_success_timestamp",
		Help: "Unix timestamp of the last successful backup/restore verification (0 when it has never succeeded or is disabled).",
	})

	initOnce sync.Once
)

func Register(reg *prometheus.Registry) error {
	if reg == nil {
		return nil
	}
	initOnce.Do(initZeroSeries)
	for _, collector := range []prometheus.Collector{
		costUSDTotal,
		llmRequestsTotal,
		llmTTFTSeconds,
		sandboxRunsTotal,
		runsTotal,
		budgetRejectionsTotal,
		jobsQueueOldestAgeSeconds,
		dbPoolAcquiredConnections,
		dbPoolMaxConnections,
		backupTestLastSuccessTimestamp,
	} {
		if err := reg.Register(collector); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
				continue
			}
			return err
		}
	}
	return nil
}

func ObserveCostUSD(tenant, model, agent string, costUSD float64) {
	if costUSD < 0 {
		costUSD = 0
	}
	costUSDTotal.WithLabelValues(normalizeTenant(tenant), normalizeLabel(model, defaultModel), normalizeLabel(agent, defaultAgent)).Add(costUSD)
}

func ObserveLLMRequest(tenant, model string, statusCode int, ttftSeconds float64) {
	status := "error"
	if statusCode >= 200 && statusCode < 400 {
		status = "success"
	}
	model = normalizeLabel(model, defaultModel)
	llmRequestsTotal.WithLabelValues(normalizeTenant(tenant), model, status).Inc()
	if ttftSeconds > 0 {
		llmTTFTSeconds.WithLabelValues(model).Observe(ttftSeconds)
	}
}

func ObserveSandboxRun(adapter, status string) {
	sandboxRunsTotal.WithLabelValues(normalizeLabel(adapter, defaultAgent), normalizeLabel(status, "unknown")).Inc()
}

func ObserveRun(agent, status string) {
	runsTotal.WithLabelValues(normalizeLabel(agent, defaultAgent), normalizeLabel(status, "unknown")).Inc()
}

// ObserveBudgetRejection records one budget-enforcement rejection (HTTP 402).
// reason is "tenant" (monthly budget) or "key" (per-key lifetime cap).
func ObserveBudgetRejection(tenant, reason string) {
	budgetRejectionsTotal.WithLabelValues(normalizeTenant(tenant), normalizeLabel(reason, "unknown")).Inc()
}

// SetJobsQueueOldestAge sets the age (seconds) of the oldest not-yet-completed
// background job. Sampled periodically by the runtime.
func SetJobsQueueOldestAge(seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	jobsQueueOldestAgeSeconds.Set(seconds)
}

// SetDBPoolStats sets the Postgres pool saturation gauges. Sampled periodically.
func SetDBPoolStats(acquired, max int) {
	dbPoolAcquiredConnections.Set(float64(acquired))
	dbPoolMaxConnections.Set(float64(max))
}

// SetBackupTestLastSuccess records the Unix timestamp of the last successful
// backup/restore verification. Called by the backup-test cron.
func SetBackupTestLastSuccess(unixSeconds int64) {
	backupTestLastSuccessTimestamp.Set(float64(unixSeconds))
}

func initZeroSeries() {
	costUSDTotal.WithLabelValues(defaultTenant, defaultModel, defaultAgent).Add(0)
	llmRequestsTotal.WithLabelValues(defaultTenant, defaultModel, "success").Add(0)
	llmRequestsTotal.WithLabelValues(defaultTenant, defaultModel, "error").Add(0)
	llmTTFTSeconds.WithLabelValues(defaultModel)
	sandboxRunsTotal.WithLabelValues(defaultAgent, "done").Add(0)
	sandboxRunsTotal.WithLabelValues(defaultAgent, "failed").Add(0)
	runsTotal.WithLabelValues(defaultAgent, "succeeded").Add(0)
	runsTotal.WithLabelValues(defaultAgent, "failed").Add(0)
	budgetRejectionsTotal.WithLabelValues(defaultTenant, "tenant").Add(0)
	budgetRejectionsTotal.WithLabelValues(defaultTenant, "key").Add(0)
	jobsQueueOldestAgeSeconds.Set(0)
	dbPoolAcquiredConnections.Set(0)
	dbPoolMaxConnections.Set(0)
	backupTestLastSuccessTimestamp.Set(0)
}

func normalizeTenant(v string) string {
	v = normalizeLabel(v, defaultTenant)
	if v == "" {
		return defaultTenant
	}
	return v
}

func normalizeLabel(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		v = fallback
	}
	if len(v) > maxLabelLen {
		v = v[:maxLabelLen] + ":" + strconv.Itoa(len(v))
	}
	return v
}

func ResetForTest() {
	costUSDTotal.Reset()
	llmRequestsTotal.Reset()
	llmTTFTSeconds.Reset()
	sandboxRunsTotal.Reset()
	runsTotal.Reset()
	budgetRejectionsTotal.Reset()
	initZeroSeries()
}
