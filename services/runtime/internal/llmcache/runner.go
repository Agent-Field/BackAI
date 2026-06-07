// SPDX-License-Identifier: Apache-2.0

package llmcache

import (
	"context"
	"time"
)

// RunEviction is a blocking loop that calls Evict every `interval` until
// ctx is cancelled. Designed to be spawned in a goroutine at boot:
//
//	go cache.RunEviction(ctx, 5*time.Minute)
//
// On the first tick it logs the number of rows removed. Errors are
// logged but never returned — a transient PG hiccup must not silently
// kill the eviction loop, the next tick simply tries again.
//
// interval <= 0 is normalised to a sane default (5 minutes) rather than
// turning into a busy loop.
func (c *Cache) RunEviction(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	c.log.Info("llmcache: eviction loop started", "interval", interval.String())
	for {
		select {
		case <-ctx.Done():
			c.log.Info("llmcache: eviction loop stopped",
				"reason", ctx.Err().Error())
			return
		case <-t.C:
			c.tickEvict(ctx)
		}
	}
}

// tickEvict runs one eviction pass with its own short timeout so a slow
// DELETE doesn't pin the eviction goroutine across multiple intervals.
func (c *Cache) tickEvict(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	removed, err := c.Evict(ctx)
	if err != nil {
		c.log.Warn("llmcache: eviction tick failed", "error", err)
		return
	}
	if removed > 0 {
		c.log.Info("llmcache: evicted expired entries", "count", removed)
	}
}