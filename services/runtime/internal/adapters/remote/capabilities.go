// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"sync"
	"time"
)

// DefaultCapabilityTTL is how long a fetched capabilities response is
// considered fresh. The Client uses this if the operator hasn't
// configured a per-adapter override.
const DefaultCapabilityTTL = 5 * time.Minute

// capabilityCache memoises GET /v1/capabilities per adapter URL. We use
// a simple time-bounded cache rather than singleflight because the
// payload is small (a few KiB) and the fetch is rare; under load the
// optimisation isn't worth the complexity.
type capabilityCache struct {
	ttl time.Duration

	mu      sync.RWMutex
	entries map[string]capabilityEntry
}

type capabilityEntry struct {
	resp      CapabilityResponse
	fetchedAt time.Time
	err       error
}

func newCapabilityCache() *capabilityCache {
	return &capabilityCache{
		ttl:     DefaultCapabilityTTL,
		entries: make(map[string]capabilityEntry, 8),
	}
}

// get returns a cached capabilities response or invokes fetch and
// caches the result. Failures are NOT cached (a transient failure
// shouldn't poison the cache for 5 minutes).
func (c *capabilityCache) get(ctx context.Context, key string, fetch func() (CapabilityResponse, error)) (CapabilityResponse, error) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && time.Since(e.fetchedAt) < c.ttl {
		return e.resp, nil
	}

	resp, err := fetch()
	if err != nil {
		return CapabilityResponse{}, err
	}

	c.mu.Lock()
	c.entries[key] = capabilityEntry{resp: resp, fetchedAt: time.Now()}
	c.mu.Unlock()
	return resp, nil
}

// invalidate forces a re-fetch on next call. Used by tests + future
// operator-driven cache flush.
func (c *capabilityCache) invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}
