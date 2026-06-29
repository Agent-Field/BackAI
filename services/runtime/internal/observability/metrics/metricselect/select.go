// SPDX-License-Identifier: Apache-2.0

// Package metricselect constructs the active metrics.Store from typed config.
package metricselect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
	prommetrics "github.com/Agent-Field/backai/services/runtime/internal/observability/metrics/adapters/prometheus"
	remotemetrics "github.com/Agent-Field/backai/services/runtime/internal/observability/metrics/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics/none"
)

func Select(ctx context.Context, cfg config.Config) (metrics.Store, error) {
	switch Adapter(cfg) {
	case "none":
		return none.New(), nil
	case "prometheus":
		return prommetrics.New(ctx, prommetrics.Config{
			BaseURL: cfg.Metrics.Prometheus.URL,
		})
	case "remote":
		return remotemetrics.New(ctx, remotemetrics.Config{
			BaseURL: cfg.Metrics.Remote.URL,
			Token:   cfg.Metrics.Remote.Token,
			Timeout: 30 * time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported metrics adapter %q", cfg.Metrics.Adapter)
	}
}

func Adapter(cfg config.Config) string {
	adapter := strings.ToLower(strings.TrimSpace(cfg.Metrics.Adapter))
	if adapter == "" {
		return "none"
	}
	return adapter
}

func Name(cfg config.Config, store metrics.Store) string {
	switch Adapter(cfg) {
	case "remote":
		if named, ok := store.(interface{ AdapterName() string }); ok {
			return "remote: " + named.AdapterName()
		}
		return "remote"
	case "prometheus":
		return "Prometheus"
	default:
		return "none"
	}
}
