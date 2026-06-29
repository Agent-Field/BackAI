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

func initZeroSeries() {
	costUSDTotal.WithLabelValues(defaultTenant, defaultModel, defaultAgent).Add(0)
	llmRequestsTotal.WithLabelValues(defaultTenant, defaultModel, "success").Add(0)
	llmRequestsTotal.WithLabelValues(defaultTenant, defaultModel, "error").Add(0)
	llmTTFTSeconds.WithLabelValues(defaultModel)
	sandboxRunsTotal.WithLabelValues(defaultAgent, "done").Add(0)
	sandboxRunsTotal.WithLabelValues(defaultAgent, "failed").Add(0)
	runsTotal.WithLabelValues(defaultAgent, "succeeded").Add(0)
	runsTotal.WithLabelValues(defaultAgent, "failed").Add(0)
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
	initZeroSeries()
}
