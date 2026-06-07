// SPDX-License-Identifier: Apache-2.0

// metrics_ring.go — in-memory ring buffer of recent HTTP request
// durations + per-route counters.
//
// Used by /api/v1/metrics/summary to compute p95 latency and the
// top-routes table without depending on Prometheus. The buffer caps
// at MetricsRingSize samples — enough to compute a stable p95 over a
// 5-10 minute window at typical dashboard QPS but small enough to
// keep the lock cheap.
//
// Per-route counters are unbounded in keys (the dashboard caller picks
// top-10) but bounded in keys-per-route — each value is a fixed-size
// struct.
//
// Concurrency: one sync.Mutex guards both structures. Locks are held
// only across in-memory work, never across I/O.
package server

import (
	"sort"
	"sync"
)

// MetricsRingSize is the cap on remembered request durations. ~2000 is
// large enough for a stable p95 over a 5-10 minute window at typical
// dashboard request rates but small enough that the snapshot copy +
// sort stays sub-millisecond.
const MetricsRingSize = 2048

// routeStat is the per-route summary the ring keeps.
type routeStat struct {
	route   string
	count   int64
	totalMS int64
	errors  int64
}

// metricsRing is a fixed-capacity circular buffer of durations.
type metricsRing struct {
	mu      sync.Mutex
	durMS   []int64
	idx     int
	full    bool
	total   int64
	routes  map[string]*routeStat
}

// newMetricsRing constructs a ring of the given capacity. Capacity is
// rounded up to MetricsRingSize if smaller.
func newMetricsRing(capacity int) *metricsRing {
	if capacity < 16 {
		capacity = MetricsRingSize
	}
	return &metricsRing{
		durMS:  make([]int64, capacity),
		routes: map[string]*routeStat{},
	}
}

// observe records a completed HTTP request.
//
// `route` is the normalised path (we use the URL path verbatim — the
// dashboard renders the raw routes so we keep them honest). statusCode
// is the final response code; anything 5xx counts toward the error
// tally (4xx are client problems, not runtime health concerns).
func (r *metricsRing) observe(route string, durationMS int64, statusCode int) {
	if r == nil {
		return
	}
	if durationMS < 0 {
		durationMS = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.durMS[r.idx] = durationMS
	r.idx = (r.idx + 1) % len(r.durMS)
	if r.idx == 0 {
		r.full = true
	}
	r.total++
	st, ok := r.routes[route]
	if !ok {
		st = &routeStat{route: route}
		r.routes[route] = st
	}
	st.count++
	st.totalMS += durationMS
	if statusCode >= 500 {
		st.errors++
	}
}

// totalRequests returns the lifetime count.
func (r *metricsRing) totalRequests() int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}

// p95 returns the 95th percentile of the recent window in milliseconds.
// Returns 0 when fewer than two samples have been recorded.
func (r *metricsRing) p95() float64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.idx
	if r.full {
		n = len(r.durMS)
	}
	if n < 2 {
		return 0
	}
	// Snapshot + sort. The window is bounded so this is microseconds.
	snap := make([]int64, n)
	copy(snap, r.durMS[:n])
	sort.Slice(snap, func(i, j int) bool { return snap[i] < snap[j] })
	// floor(0.95 * (n-1)) is the standard "nearest-rank" interpretation.
	idx := int(0.95 * float64(n-1))
	if idx >= n {
		idx = n - 1
	}
	return float64(snap[idx])
}

// routeStats returns a snapshot copy of every per-route counter. The
// caller is responsible for sorting + truncating.
func (r *metricsRing) routeStats() []routeStat {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]routeStat, 0, len(r.routes))
	for _, st := range r.routes {
		out = append(out, *st)
	}
	return out
}