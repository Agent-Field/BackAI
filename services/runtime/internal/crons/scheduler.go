// SPDX-License-Identifier: Apache-2.0

// scheduler.go — the 60-second tick loop that dispatches due crons.
//
// One goroutine, no parallelism inside the tick. Per-row enqueue is fast
// (it just inserts a River row); per-tick concurrency would only help if
// individual enqueues stalled, and at that point the right fix is to
// shrink the batch size, not race with ourselves.
//
// The scheduler is driven by Store.ClaimDue: that helper opens a single
// transaction, FOR UPDATE SKIP LOCKED's the working set, and lets the
// scheduler dispatch each row inside the same transaction so the
// updates land atomically. If multiple af-stack processes run against
// the same DB, SKIP LOCKED guarantees no double-firing.
package crons

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler dispatches due crons on a fixed interval.
type Scheduler struct {
	store    *Store
	enqueuer JobEnqueuer
	log      *slog.Logger
	interval time.Duration
	batch    int
}

// SchedulerConfig is the constructor input.
type SchedulerConfig struct {
	Store    *Store
	Enqueuer JobEnqueuer
	Logger   *slog.Logger
	// Interval is the tick frequency. Defaults to 60s. Tests can shrink
	// this to drive the loop without sleeping.
	Interval time.Duration
	// Batch is the per-tick claim size. Defaults to 100.
	Batch int
}

// NewScheduler constructs a Scheduler. Returns nil when either store or
// enqueuer is missing — main.go uses the nil to skip starting the
// goroutine in degraded boot modes (no DB / no jobs).
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.Store == nil || cfg.Enqueuer == nil {
		return nil
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	batch := cfg.Batch
	if batch <= 0 {
		batch = 100
	}
	return &Scheduler{
		store:    cfg.Store,
		enqueuer: cfg.Enqueuer,
		log:      log,
		interval: interval,
		batch:    batch,
	}
}

// Run blocks until ctx is cancelled. Ticks every Interval; each tick
// claims due rows and dispatches them.
//
// The first tick fires after Interval (not immediately) so the runtime
// doesn't hammer the DB at boot before everything else is up. Callers
// that want an at-boot dispatch should call Tick() directly before
// Run().
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	t := time.NewTicker(s.interval)
	defer t.Stop()
	s.log.Info("crons: scheduler started",
		"interval", s.interval, "batch", s.batch)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("crons: scheduler stopping")
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Tick runs one dispatch pass. Exported so tests + callers can drive
// the loop manually without waiting on the ticker.
func (s *Scheduler) Tick(ctx context.Context) int {
	if s == nil || s.store == nil {
		return 0
	}
	tickCtx, cancel := context.WithTimeout(ctx, s.interval-time.Second)
	if s.interval <= 2*time.Second {
		// Use the parent ctx for very short intervals (tests) so the
		// helper isn't capped below a usable window.
		cancel()
		tickCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	processed, err := s.store.ClaimDue(tickCtx, s.batch, func(ctx context.Context, row DueRow) (bool, error) {
		// Per-row enqueue. We always advance next_run_at — even if
		// the enqueue fails (the store handler logs it), the cron
		// keeps its cadence rather than firing twice once the
		// underlying issue is fixed.
		tenant := ""
		if row.TenantID != nil {
			tenant = *row.TenantID
		}
		if err := s.enqueuer.Enqueue(ctx, row.JobName, row.Args, tenant); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		s.log.Warn("crons: tick failed", "error", err)
	}
	if processed > 0 {
		s.log.Debug("crons: tick processed", "rows", processed)
	}
	return processed
}