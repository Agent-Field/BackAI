// SPDX-License-Identifier: Apache-2.0

// worker.go — background loop that drains the outbound webhook outbox.
//
// Phase 10.3 owns this file. The worker is the engine behind the
// "fire-and-forget" semantics of webhooks.send(): the HTTP handler
// returns immediately after inserting the row, and this loop is
// responsible for the actual POST + retry behaviour.
//
// Design choices:
//
//   - Pull-based, not push-based. We don't fan out per-row goroutines;
//     instead the worker claims a small batch on every tick and walks it
//     sequentially. This keeps the connection pool tame and makes it
//     trivial to multi-process the runtime (FOR UPDATE SKIP LOCKED in
//     DeliveryStore.ClaimQueued lets N workers race safely).
//
//   - One pool per runtime. The DeliveryStore takes a *pgxpool so we
//     don't need a separate connection here.
//
//   - Per-row deadline. Deliver() runs with a 30s context; the worker
//     loop itself respects the caller's ctx for graceful shutdown.
package webhooks

import (
	"context"
	"log/slog"
	"time"
)

// OutboundWorker drains the suite_webhook_deliveries outbox.
//
// Run blocks until ctx is cancelled; intended to be started in a
// goroutine from main.go alongside the HTTP server.
type OutboundWorker struct {
	svc      *OutboundService
	store    *DeliveryStore
	log      *slog.Logger
	interval time.Duration
	batch    int
}

// NewOutboundWorker constructs a worker. Both nil checks degrade
// gracefully: a worker without a service or store is a no-op (Run
// returns immediately).
func NewOutboundWorker(svc *OutboundService, store *DeliveryStore, log *slog.Logger) *OutboundWorker {
	if log == nil {
		log = slog.Default()
	}
	return &OutboundWorker{
		svc:      svc,
		store:    store,
		log:      log,
		interval: PollInterval,
		batch:    BatchSize,
	}
}

// Run is the main loop. It exits when ctx is cancelled; any in-flight
// Deliver call is allowed to finish (its own timeout caps the wait).
func (w *OutboundWorker) Run(ctx context.Context) {
	if w == nil || w.svc == nil || w.store == nil || !w.store.HasPool() {
		w.log.Info("webhooks: outbound worker not configured; exiting")
		return
	}
	w.log.Info("webhooks: outbound worker started",
		"interval", w.interval, "batch", w.batch)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run one immediate pass so a freshly-enqueued row doesn't have to
	// wait the full interval. After that, settle into the ticker
	// cadence.
	w.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("webhooks: outbound worker stopping")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick claims a batch and walks it. Failures from individual deliveries
// don't abort the batch — Deliver records the failure on the row, the
// next tick picks it up again once scheduled_at lands.
func (w *OutboundWorker) tick(ctx context.Context) {
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := w.store.ClaimQueued(claimCtx, w.batch)
	if err != nil {
		// A DB blip shouldn't crash the worker. Log + back off until
		// the next tick.
		w.log.Warn("webhooks: claim queued failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	w.log.Debug("webhooks: claimed batch", "count", len(rows))

	for i := range rows {
		row := rows[i]
		// Per-row context so a hanging POST can't block the rest of
		// the batch. The OutboundService also has its own per-POST
		// timeout (DefaultTimeout); the worker context is an outer
		// belt-and-braces cap.
		rowCtx, rowCancel := context.WithTimeout(ctx, DefaultTimeout+5*time.Second)
		// We pass the row's ID rather than the row itself because
		// Deliver re-loads to handle the case where the row was
		// modified between the claim and the POST.
		if err := w.svc.Deliver(rowCtx, row.ID); err != nil {
			w.log.Warn("webhooks: deliver failed",
				"id", row.ID, "error", err)
		}
		rowCancel()

		// Cooperative cancellation: if the parent ctx is done we
		// stop after the current row rather than burning through the
		// whole batch.
		if ctx.Err() != nil {
			return
		}
	}
}