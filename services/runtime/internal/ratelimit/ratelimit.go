// SPDX-License-Identifier: Apache-2.0

// Package ratelimit implements per-tenant request throttling for the
// public gateway.
//
// Architecture
//
// One token bucket per (tenant_id, route) pair. The token bucket model
// gives us a steady refill rate (requests-per-minute / second) plus a
// small burst capacity for benign traffic spikes (e.g. a UI page that
// fires three concurrent API calls).
//
// In-process (v1) uses golang.org/x/time/rate and stores buckets in an
// LRU cache so a long-tail of cold tenants can't exhaust memory. A
// Redis-backed adapter is sketched as the v2 target so multi-replica
// deployments share a single counter — see the Limiter interface comment
// below.
//
// Quota Overrides
//
// Per-tenant overrides come from a QuotaSource func — the server wires
// it to read suite_tenants.quota jsonb ("rpm" key when present). Lookups
// are cached briefly so we don't hammer Postgres on every request.
package ratelimit

import (
	"container/list"
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// DefaultRPM is the per-tenant baseline when no override is present in
// suite_tenants.quota. Sized so a normal dashboard session never hits it
// under load.
const DefaultRPM = 60

// defaultBurst is the bucket capacity. It governs how many requests a
// quiet tenant can fire back-to-back before the steady refill rate
// kicks in. We keep it small so a buggy client can't blow through 5
// minutes of quota in one tight loop.
const defaultBurst = 10

// defaultMaxBuckets is the LRU cap. ~10k tenants × 5 routes ≈ 50k
// buckets at the bound, well below the OOM threshold for a typical
// runtime container; pick the number lower if you expect many more
// routes.
const defaultMaxBuckets = 10_000

// Limiter is the polymorphic API. The in-process InMemory limiter is the
// v1 impl. A Redis-backed Limiter is a v2 TODO: same interface, server
// wires whichever it has, no handler code changes.
//
// Allow returns:
//
//	allowed     — true when the request fits within the bucket
//	retryAfter  — when allowed=false, the wall-clock delay the client
//	              should wait before retrying. Zero on success.
//	err         — non-nil only on infrastructure error (e.g. Redis down).
//	              Handlers SHOULD fail-open on err (accept the request)
//	              so a Redis outage doesn't take down the gateway.
type Limiter interface {
	Allow(ctx context.Context, tenantID, route string) (allowed bool, retryAfter time.Duration, err error)
}

// QuotaSource resolves the per-tenant requests-per-minute override.
// Return (0, false) when no override exists; the limiter falls back to
// DefaultRPM. Errors should be logged by the source; we treat any
// missing/error case as "use default" so a bad DB read can't strand a
// tenant.
type QuotaSource func(ctx context.Context, tenantID string) (rpm int, ok bool)

// Stats exposes counters useful for ops + tests.
type Stats struct {
	// BucketCount is the live size of the LRU at the time of the call.
	BucketCount int
	// EvictionCount is the cumulative number of buckets evicted because
	// the LRU was full. A monotonically growing counter; ops dashboards
	// should alert when the per-minute delta jumps.
	EvictionCount int64
}

// ─── In-memory implementation ─────────────────────────────────────────────

// bucket is one entry in the LRU. A *rate.Limiter is cheap (~64 bytes)
// so we don't bother pooling them.
type bucket struct {
	key     string
	limiter *rate.Limiter
}

// InMemory is the v1 limiter: process-local LRU of token buckets.
//
// Thread-safe; a single sync.Mutex covers the LRU + the rate.Limiter
// lookup. rate.Limiter has its own internal lock so we don't need to
// hold ours across Allow() calls — but we DO need it for LRU access.
type InMemory struct {
	mu     sync.Mutex
	lru    *list.List               // front = most recently used
	index  map[string]*list.Element // key -> element in lru
	cap    int
	quotas QuotaSource

	// now is overridable for tests — production uses time.Now.
	now func() time.Time

	stats Stats
}

// Option configures an InMemory at construction time.
type Option func(*InMemory)

// WithQuotaSource installs a per-tenant rpm override resolver.
func WithQuotaSource(qs QuotaSource) Option {
	return func(im *InMemory) { im.quotas = qs }
}

// WithMaxBuckets overrides the LRU cap (default 10k).
func WithMaxBuckets(n int) Option {
	return func(im *InMemory) {
		if n > 0 {
			im.cap = n
		}
	}
}

// WithClock injects a custom clock. Tests use this to advance time
// without sleeping.
func WithClock(now func() time.Time) Option {
	return func(im *InMemory) {
		if now != nil {
			im.now = now
		}
	}
}

// NewInMemory builds an in-process limiter.
func NewInMemory(opts ...Option) *InMemory {
	im := &InMemory{
		lru:   list.New(),
		index: make(map[string]*list.Element),
		cap:   defaultMaxBuckets,
		now:   time.Now,
	}
	for _, o := range opts {
		o(im)
	}
	return im
}

// Allow implements Limiter.
//
// We don't return an error in v1; a Redis adapter would be the first to
// need it (network blip → fail open).
func (im *InMemory) Allow(ctx context.Context, tenantID, route string) (bool, time.Duration, error) {
	rpm := DefaultRPM
	if im.quotas != nil && tenantID != "" {
		if v, ok := im.quotas(ctx, tenantID); ok && v > 0 {
			rpm = v
		}
	}

	limiter := im.getOrCreate(tenantID, route, rpm)
	res := limiter.ReserveN(im.now(), 1)
	if !res.OK() {
		// rate.Reserve only returns !OK when the burst capacity is
		// smaller than the requested token count, which shouldn't
		// happen here (we always request 1). Treat as deny with a
		// short cooldown.
		return false, time.Second, nil
	}
	delay := res.DelayFrom(im.now())
	if delay > 0 {
		// We could just block — but middleware should be non-blocking.
		// Cancel the reservation and tell the caller to retry.
		res.Cancel()
		return false, delay, nil
	}
	return true, 0, nil
}

// Stats returns a snapshot of internal counters.
func (im *InMemory) Stats() Stats {
	im.mu.Lock()
	defer im.mu.Unlock()
	return Stats{
		BucketCount:   im.lru.Len(),
		EvictionCount: im.stats.EvictionCount,
	}
}

// getOrCreate looks up the bucket for (tenant, route). On miss, allocates
// a new rate.Limiter and inserts it at the LRU front. On hit, marks the
// existing entry MRU. Holds im.mu the whole way so concurrent inserts
// don't race on the same key.
func (im *InMemory) getOrCreate(tenantID, route string, rpm int) *rate.Limiter {
	key := tenantID + "|" + route

	im.mu.Lock()
	defer im.mu.Unlock()

	if el, ok := im.index[key]; ok {
		im.lru.MoveToFront(el)
		b := el.Value.(*bucket)
		// If the quota changed since this bucket was created (rare,
		// tenant plan upgraded mid-flight), drop the bucket so the next
		// call re-creates it with the new rate. Cheap: tiny allocation.
		want := rateLimit(rpm)
		if b.limiter.Limit() != want {
			b.limiter.SetLimit(want)
		}
		return b.limiter
	}

	// Miss → allocate.
	lim := rate.NewLimiter(rateLimit(rpm), defaultBurst)
	b := &bucket{key: key, limiter: lim}
	el := im.lru.PushFront(b)
	im.index[key] = el

	// LRU eviction. Done in a loop to be tolerant of a cap that was
	// lowered at runtime via WithMaxBuckets (uncommon, but possible
	// when an admin tunes the runtime).
	for im.lru.Len() > im.cap {
		oldest := im.lru.Back()
		if oldest == nil {
			break
		}
		ob := oldest.Value.(*bucket)
		delete(im.index, ob.key)
		im.lru.Remove(oldest)
		im.stats.EvictionCount++
	}
	return lim
}

// rateLimit converts rpm to a per-second rate.Limit. Refill is steady,
// not bursty: a 60 rpm tenant gets one token per second.
func rateLimit(rpm int) rate.Limit {
	if rpm <= 0 {
		rpm = DefaultRPM
	}
	return rate.Limit(float64(rpm) / 60.0)
}

// ─── Redis adapter — v2 TODO ──────────────────────────────────────────────
//
// Sketch of what the v2 Redis adapter looks like. We keep it as a
// commented stub so the contract is clear for whoever picks this up:
//
//   type Redis struct {
//       client redis.UniversalClient
//       quotas QuotaSource
//       prefix string
//   }
//
//   func (r *Redis) Allow(ctx context.Context, tenantID, route string) (bool, time.Duration, error) {
//       // Atomic INCR + EXPIRE per (tenant, route) minute window.
//       // Round trip: 1 RTT to Redis using GCRA or a simple sliding
//       // window. Fail-open on err — return (true, 0, err) and let the
//       // caller decide whether to log + alert.
//   }
//
// Wire-up: server.Deps.RateLimiter accepts any Limiter, so swapping
// InMemory → Redis is a one-line change in main.go. No handler edits.