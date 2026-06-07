// worker.go — background drain of suite_notifications.
//
// The worker wakes every ~2s, atomically claims up to MaxBatchSize
// queued+ready rows (status='queued' AND scheduled_at <= now()), and
// dispatches each through the Service's adapter in parallel. Errors
// are recorded in the row's last_error column; the worker never
// crashes the runtime.
//
// Design choices:
//
//   - 2s tick. Fast enough that the dashboard's 5s refresh shows live
//     transitions, slow enough that an idle runtime barely touches the
//     DB.
//   - Atomic claim via FOR UPDATE SKIP LOCKED. Lets future multi-pod
//     deployments scale workers horizontally without double-sending.
//   - Per-dispatch goroutine with a bounded waitgroup. Caps fan-out at
//     MaxBatchSize so a backlog can't exhaust the connection pool.
//   - Best-effort context. We use the worker's parent ctx for both the
//     DB claim and the adapter call; cancellation drains cleanly on
//     SIGTERM.
package notifications

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Worker drains suite_notifications via Service.dispatchOne.
type Worker struct {
	svc      *Service
	log      *slog.Logger
	interval time.Duration
}

// NewWorker constructs a Worker. svc must be non-nil; the worker
// no-ops when svc.AdapterAvailable() returns false (so dev runs with
// no adapter still boot).
func NewWorker(svc *Service, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	return &Worker{
		svc:      svc,
		log:      log,
		interval: 2 * time.Second,
	}
}

// WithInterval overrides the poll cadence (mainly for tests).
func (w *Worker) WithInterval(d time.Duration) *Worker {
	if d > 0 {
		w.interval = d
	}
	return w
}

// Run blocks until ctx is cancelled, polling for queued rows on each
// tick. Safe to call as `go worker.Run(ctx)`.
func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.svc == nil || !w.svc.AdapterAvailable() {
		w.log.Info("notifications worker: no adapter wired; worker idle")
		return
	}
	rec := w.svc.Recorder()
	if rec == nil || !rec.HasPool() {
		w.log.Info("notifications worker: no DB wired; worker idle")
		return
	}
	adapterName := w.svc.AdapterName()
	w.log.Info("notifications worker started",
		"adapter", adapterName,
		"interval", w.interval.String(),
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("notifications worker stopping", "reason", ctx.Err())
			return
		case <-ticker.C:
			w.drainOnce(ctx, adapterName)
		}
	}
}

// drainOnce claims up to MaxBatchSize queued rows and dispatches them
// concurrently. Errors are logged but never propagate — the next tick
// will retry rows that the claim left in 'sending' status because of
// a crash (operators can also flip them back to 'queued' manually).
func (w *Worker) drainOnce(ctx context.Context, adapterName string) {
	rec := w.svc.Recorder()
	claimed, err := rec.ClaimQueued(ctx, adapterName, MaxBatchSize)
	if err != nil {
		w.log.Warn("notifications worker: claim failed", "error", err)
		return
	}
	if len(claimed) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, n := range claimed {
		wg.Add(1)
		go func(item Notification) {
			defer wg.Done()
			w.svc.dispatchOne(ctx, item)
		}(n)
	}
	wg.Wait()
}
