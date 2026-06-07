// SPDX-License-Identifier: Apache-2.0

// shutdown.go — Drain controller for graceful shutdown.
//
// The Drain controller is the central piece of Phase 14.3 graceful
// shutdown. It tracks two things:
//
//  1. Whether the server is currently in drain mode (set once via
//     Start() and never cleared — drain is one-way).
//  2. The count of in-flight HTTP requests, maintained by the
//     drain_middleware on the outside of the handler chain.
//
// When SIGTERM lands the main goroutine calls drain.Start(); from that
// moment on:
//
//   - /ready returns 503 {"status":"draining"} so Kubernetes / Fly /
//     load balancers stop routing new traffic to this pod.
//   - The drain_middleware rejects every NEW request with 503 +
//     Connection: close. In-flight requests are NOT cancelled — they
//     get to run to completion (or until the drain timeout fires).
//   - drain.Start() blocks until either (a) the active counter reaches
//     zero, or (b) the drain timeout expires.
//
// After Start() returns the main goroutine proceeds to httpServer.Shutdown
// and then ordered worker shutdown. See main.go for the full sequence.
//
// Design notes:
//
//   - Drain mode is atomic. We use sync/atomic for IsDraining() so the
//     hot path (every request) does a single atomic load — no mutex
//     contention.
//   - The in-flight counter is also atomic for the same reason.
//   - The "all in-flight done" signal uses a channel that drainOnce
//     closes once. We poll-and-check rather than condvar because the
//     counter is bumped by middleware that doesn't know about Drain.
//   - DrainStart is captured at Start() time and exposed so /ready can
//     compute the remaining drain window for Retry-After.
package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Drain controls graceful shutdown. Exported so tests can construct
// one directly; production code goes through Server.drain.
type Drain struct {
	// draining is 0 when normal, 1 once Start() has been called.
	// Atomic for lock-free reads from the request hot path.
	draining atomic.Bool

	// activeRequests counts in-flight HTTP requests, maintained by the
	// drain middleware. Excludes /health and /ready so probe traffic
	// never holds the drain open.
	activeRequests atomic.Int64

	// drainStart records when Start() was called. Used by /ready to
	// emit a Retry-After hint and a "since_s" field in the body.
	drainStart atomic.Int64 // unix-nano

	// timeout is the maximum wall-clock window Start() will wait for
	// in-flight requests to finish before returning.
	timeout time.Duration

	// readyOnce gates the ready() flip — only the first Start() takes
	// effect. Subsequent calls are no-ops (return immediately).
	startOnce sync.Once

	// done is closed exactly once when active hits zero (or when
	// Start's timeout expires). Used internally to coordinate the
	// poll-loop's exit.
	done chan struct{}
}

// NewDrain constructs a Drain controller with the given timeout. A
// zero timeout falls back to 30s (the documented default).
func NewDrain(timeout time.Duration) *Drain {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Drain{
		timeout: timeout,
		done:    make(chan struct{}),
	}
}

// IsDraining reports whether Start() has been called. Safe to call from
// any goroutine; lock-free.
func (d *Drain) IsDraining() bool {
	if d == nil {
		return false
	}
	return d.draining.Load()
}

// Active returns the current in-flight request count. Used by the
// /ready handler to surface a debug field in the response body.
func (d *Drain) Active() int64 {
	if d == nil {
		return 0
	}
	return d.activeRequests.Load()
}

// DrainStart returns the wall-clock time at which Start() was called.
// Zero when not draining. Used by /ready to compute Retry-After.
func (d *Drain) DrainStart() time.Time {
	if d == nil {
		return time.Time{}
	}
	ns := d.drainStart.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// Timeout returns the configured drain timeout.
func (d *Drain) Timeout() time.Duration {
	if d == nil {
		return 0
	}
	return d.timeout
}

// Inc bumps the active-request counter. Called by the drain
// middleware on request entry.
func (d *Drain) Inc() {
	if d == nil {
		return
	}
	d.activeRequests.Add(1)
}

// Dec decrements the active-request counter. Called by the drain
// middleware on request exit. Safe to call without a matching Inc
// (clamped at zero).
func (d *Drain) Dec() {
	if d == nil {
		return
	}
	n := d.activeRequests.Add(-1)
	if n < 0 {
		// Counter under-flowed — restore to zero. Shouldn't happen
		// in practice but cheap insurance against test-time misuse.
		d.activeRequests.Store(0)
	}
}

// Start flips the drain flag and blocks until either (a) the
// active-request counter reaches zero, or (b) the timeout expires.
//
// Returns nil when the drain completes cleanly (all requests done).
// Returns context.DeadlineExceeded when the timeout fires with
// requests still in flight — callers should log the active count and
// proceed to http.Server.Shutdown (which will cut off remaining
// connections at its own timeout).
//
// Start is idempotent — calling it twice returns immediately on the
// second call without re-blocking. This makes the SIGTERM handler
// safe against accidental duplicate signals.
func (d *Drain) Start(ctx context.Context) error {
	if d == nil {
		return nil
	}

	// Use startOnce so the second SIGTERM is a no-op rather than
	// crashing or re-blocking the main goroutine.
	first := false
	d.startOnce.Do(func() {
		first = true
		d.drainStart.Store(time.Now().UnixNano())
		d.draining.Store(true)
	})
	if !first {
		// Already draining — second caller waits for the same done
		// channel as the first.
		<-d.done
		return nil
	}

	// Poll the active counter on a short tick. We could use a
	// condvar instead but the polling overhead is negligible against
	// a 30s budget, and it keeps Inc/Dec on the request hot path
	// completely free of lock acquisition.
	deadline := time.NewTimer(d.timeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	// Fast path: counter already at zero (e.g. drain triggered during
	// a fully-idle test). Return immediately.
	if d.activeRequests.Load() <= 0 {
		close(d.done)
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			// Caller's context cancelled — surface that.
			close(d.done)
			return ctx.Err()
		case <-deadline.C:
			// Hard timeout. Callers log the active count and move
			// on to http.Server.Shutdown.
			close(d.done)
			return context.DeadlineExceeded
		case <-tick.C:
			if d.activeRequests.Load() <= 0 {
				close(d.done)
				return nil
			}
		}
	}
}