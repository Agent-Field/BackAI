// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/appmetrics"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
)

// prodMetricsSampleInterval is how often the background sampler refreshes the
// gauges that can't be event-driven (queue backlog age, pool saturation).
const prodMetricsSampleInterval = 30 * time.Second

// startProductionMetricsSampler spawns a background goroutine that periodically
// refreshes the R7 operating-contract gauges the default alert rules watch:
// Postgres pool saturation and the oldest not-yet-run background job's age.
// No-op without a database. The goroutine exits when ctx is cancelled (drain).
func startProductionMetricsSampler(ctx context.Context, database *db.DB, log *slog.Logger) {
	if database == nil || database.Pool == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(prodMetricsSampleInterval)
		defer ticker.Stop()
		sampleProductionMetrics(ctx, database) // prime immediately
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sampleProductionMetrics(ctx, database)
			}
		}
	}()
	log.Info("production metrics sampler started", "interval", prodMetricsSampleInterval)
}

// sampleProductionMetrics refreshes the pool + queue-age gauges once.
func sampleProductionMetrics(ctx context.Context, database *db.DB) {
	stats := database.Stats()
	appmetrics.SetDBPoolStats(stats.AcquiredConns, stats.MaxConns)

	qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// Age of the oldest *past-due* job still awaiting a worker (available or
	// retryable, scheduled_at already elapsed). river_job carries no tenant
	// dimension, so no RLS scoping is involved. A query error (jobs disabled →
	// river_job absent) leaves the gauge at its previous value.
	var ageSeconds *float64
	err := database.Pool.QueryRow(qctx, `
		select extract(epoch from (now() - min(scheduled_at)))
		  from river_job
		 where state in ('available', 'retryable')
		   and scheduled_at <= now()
	`).Scan(&ageSeconds)
	if err != nil {
		return
	}
	if ageSeconds != nil && *ageSeconds > 0 {
		appmetrics.SetJobsQueueOldestAge(*ageSeconds)
	} else {
		appmetrics.SetJobsQueueOldestAge(0)
	}
}
