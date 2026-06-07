package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeClock makes time deterministic so we don't have to wall-clock
// sleep for refill assertions.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// drain consumes the entire burst capacity for (tenant, route) so the
// bucket is empty when the test goes to assert denials.
func drain(t *testing.T, lim Limiter, tenant, route string) {
	t.Helper()
	for i := 0; i < defaultBurst; i++ {
		allowed, _, err := lim.Allow(context.Background(), tenant, route)
		if err != nil {
			t.Fatalf("drain Allow err: %v", err)
		}
		if !allowed {
			t.Fatalf("drain unexpectedly denied at i=%d", i)
		}
	}
}

func TestAllowAcceptsBurstThenDenies(t *testing.T) {
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now))
	drain(t, lim, "tenant-a", "/api/v1/agents")
	allowed, retry, err := lim.Allow(context.Background(), "tenant-a", "/api/v1/agents")
	if err != nil {
		t.Fatalf("Allow err: %v", err)
	}
	if allowed {
		t.Errorf("expected deny after burst drain")
	}
	if retry <= 0 {
		t.Errorf("expected positive Retry-After, got %v", retry)
	}
}

func TestRefillRestoresCapacityAfterTimeAdvances(t *testing.T) {
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now))
	drain(t, lim, "tenant-a", "/api/v1/agents")
	// Default is 60 rpm = 1 token/sec. Advance 30s → 30 tokens worth of
	// budget restored, but the bucket caps at defaultBurst (10), so the
	// next Allow MUST pass.
	clk.Advance(30 * time.Second)
	allowed, _, _ := lim.Allow(context.Background(), "tenant-a", "/api/v1/agents")
	if !allowed {
		t.Errorf("expected allow after 30s refill")
	}
}

func TestPerTenantIsolation(t *testing.T) {
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now))

	// Drain tenant-a.
	drain(t, lim, "tenant-a", "/api/v1/agents")
	denied, _, _ := lim.Allow(context.Background(), "tenant-a", "/api/v1/agents")
	if denied {
		t.Fatalf("tenant-a should be throttled after burst drain")
	}

	// Tenant-b is untouched.
	allowed, _, _ := lim.Allow(context.Background(), "tenant-b", "/api/v1/agents")
	if !allowed {
		t.Errorf("tenant-b leaked tenant-a's throttle state")
	}
}

func TestPerRouteIsolation(t *testing.T) {
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now))

	drain(t, lim, "tenant-a", "/api/v1/agents")
	// Different route → own bucket.
	allowed, _, _ := lim.Allow(context.Background(), "tenant-a", "/api/v1/storage/upload")
	if !allowed {
		t.Errorf("/storage/upload throttle leaked from /agents bucket")
	}
}

func TestQuotaOverrideRaisesBurstAndRate(t *testing.T) {
	clk := newFakeClock()
	// tenant-pro gets 120 rpm = 2/s.
	src := func(_ context.Context, tid string) (int, bool) {
		if tid == "tenant-pro" {
			return 120, true
		}
		return 0, false
	}
	lim := NewInMemory(WithClock(clk.Now), WithQuotaSource(src))

	// Drain both buckets.
	drain(t, lim, "tenant-pro", "/r")
	drain(t, lim, "tenant-free", "/r")

	// Advance 1s. tenant-pro should have 2 tokens; tenant-free 1.
	clk.Advance(time.Second)

	proPassed := 0
	for i := 0; i < 3; i++ {
		ok, _, _ := lim.Allow(context.Background(), "tenant-pro", "/r")
		if ok {
			proPassed++
		}
	}
	freePassed := 0
	for i := 0; i < 3; i++ {
		ok, _, _ := lim.Allow(context.Background(), "tenant-free", "/r")
		if ok {
			freePassed++
		}
	}
	if proPassed != 2 {
		t.Errorf("tenant-pro: expected 2 passes after 1s, got %d", proPassed)
	}
	if freePassed != 1 {
		t.Errorf("tenant-free: expected 1 pass after 1s, got %d", freePassed)
	}
}

func TestLRUEvictsOldestWhenCapExceeded(t *testing.T) {
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now), WithMaxBuckets(5))

	for i := 0; i < 5; i++ {
		_, _, _ = lim.Allow(context.Background(),
			"tenant-"+strconv.Itoa(i), "/r")
	}
	if got := lim.Stats().BucketCount; got != 5 {
		t.Errorf("BucketCount = %d, want 5", got)
	}
	if got := lim.Stats().EvictionCount; got != 0 {
		t.Errorf("EvictionCount = %d, want 0 before overflow", got)
	}

	// Force 3 more buckets — oldest 3 should evict.
	for i := 5; i < 8; i++ {
		_, _, _ = lim.Allow(context.Background(),
			"tenant-"+strconv.Itoa(i), "/r")
	}
	st := lim.Stats()
	if st.BucketCount != 5 {
		t.Errorf("after overflow BucketCount = %d, want 5", st.BucketCount)
	}
	if st.EvictionCount != 3 {
		t.Errorf("EvictionCount = %d, want 3", st.EvictionCount)
	}
}

func TestLRUTouchKeepsMRUAlive(t *testing.T) {
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now), WithMaxBuckets(3))

	// Insert A, B, C.
	for _, tid := range []string{"A", "B", "C"} {
		_, _, _ = lim.Allow(context.Background(), tid, "/r")
	}
	// Touch A → A becomes MRU.
	_, _, _ = lim.Allow(context.Background(), "A", "/r")
	// Now insert D → B evicts (oldest), A stays.
	_, _, _ = lim.Allow(context.Background(), "D", "/r")

	// A's bucket should retain its partially-drained state. After 4
	// Allow calls on A, the bucket should NOT be a freshly created one
	// (which would happen if A had been evicted + re-inserted). Verify
	// indirectly: drain A completely from current state, denial behaviour
	// should kick in faster than a fresh bucket.
	deniedAfter := 0
	for i := 0; i < defaultBurst+5; i++ {
		ok, _, _ := lim.Allow(context.Background(), "A", "/r")
		if !ok {
			deniedAfter = i
			break
		}
	}
	if deniedAfter == 0 {
		t.Errorf("expected A to eventually deny")
	}
	if deniedAfter > defaultBurst {
		t.Errorf("A appears to have been evicted+recreated: drained at i=%d, want <= %d",
			deniedAfter, defaultBurst)
	}
}

func TestTenLargeScaleEviction(t *testing.T) {
	// Stress: insert N+1 unique tenants into a cap-N limiter and confirm
	// exactly 1 eviction. Catches off-by-one bugs in the LRU.
	const N = 200
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now), WithMaxBuckets(N))

	for i := 0; i < N+1; i++ {
		_, _, _ = lim.Allow(context.Background(),
			fmt.Sprintf("tenant-%05d", i), "/r")
	}
	st := lim.Stats()
	if st.BucketCount != N {
		t.Errorf("BucketCount = %d, want %d", st.BucketCount, N)
	}
	if st.EvictionCount != 1 {
		t.Errorf("EvictionCount = %d, want 1", st.EvictionCount)
	}
}

func TestEmptyTenantUsesDefault(t *testing.T) {
	// Pre-auth requests carry no tenant; the limiter should still apply
	// a (shared) bucket so a hostile client can't bypass throttling by
	// dropping their bearer.
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now))
	for i := 0; i < defaultBurst; i++ {
		allowed, _, _ := lim.Allow(context.Background(), "", "/r")
		if !allowed {
			t.Fatalf("burst slot %d denied unexpectedly", i)
		}
	}
	allowed, _, _ := lim.Allow(context.Background(), "", "/r")
	if allowed {
		t.Errorf("expected empty-tenant burst to be exhausted")
	}
}

func TestConcurrentAllowDoesNotRace(t *testing.T) {
	// The mu inside InMemory + rate.Limiter must keep concurrent callers
	// race-free. Run with -race to catch regressions.
	lim := NewInMemory()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			tid := fmt.Sprintf("tenant-%d", i%4)
			for j := 0; j < 50; j++ {
				_, _, _ = lim.Allow(context.Background(), tid, "/r")
			}
		}()
	}
	wg.Wait()
}

func TestStatsSnapshot(t *testing.T) {
	clk := newFakeClock()
	lim := NewInMemory(WithClock(clk.Now), WithMaxBuckets(4))
	if st := lim.Stats(); st.BucketCount != 0 || st.EvictionCount != 0 {
		t.Errorf("fresh limiter stats = %+v, want zero", st)
	}
	_, _, _ = lim.Allow(context.Background(), "x", "/r")
	if st := lim.Stats(); st.BucketCount != 1 {
		t.Errorf("after 1 insert BucketCount = %d", st.BucketCount)
	}
}
