// SPDX-License-Identifier: Apache-2.0

package connections

import "sync"

// refreshGroup deduplicates concurrent OAuth token refreshes per connection
// id. When N goroutines hit Handle for the same connection with an expired
// token, exactly one performs the network refresh; the rest block on it and
// share the result. This is the "single-flight" the R5 acceptance requires,
// kept in-package (no golang.org/x/sync dependency) so the behaviour is
// fully owned and deterministically testable.
type refreshGroup struct {
	mu sync.Mutex
	m  map[string]*refreshCall
}

type refreshCall struct {
	wg  sync.WaitGroup
	res refreshResult
}

type refreshResult struct {
	cred credential
	err  error
}

// do runs fn for key, ensuring only one in-flight execution per key. The
// returned shared flag is true for callers that waited on another's
// execution (i.e. did NOT run fn) — surfaced for test assertions.
func (g *refreshGroup) do(key string, fn func() refreshResult) (refreshResult, bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*refreshCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.res, true
	}
	c := &refreshCall{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.res = fn()
	c.wg.Done()

	g.mu.Lock()
	// Only delete if it's still our call — a later refresh would install a
	// fresh entry, but within one do() cycle this is always ours.
	if g.m[key] == c {
		delete(g.m, key)
	}
	g.mu.Unlock()

	return c.res, false
}
