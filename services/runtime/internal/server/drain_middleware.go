// drain_middleware.go — request-counting middleware paired with the
// Drain controller.
//
// Sits on the outside of the handler chain so every inbound request is
// counted before any other middleware (rate-limit, tenant resolver,
// tracing) runs. The pairing with shutdown.go is straightforward:
//
//   - On entry, if drain.IsDraining() is true and the request is NOT a
//     probe (/health, /ready), reject with 503 + Connection: close +
//     the standard error envelope. This sheds load fast — load
//     balancers see the 503 and rotate the next request elsewhere.
//   - Otherwise, increment the active counter, run the handler, then
//     decrement on the way out (deferred so panics don't leak a slot).
//   - Probes (/health, /ready) skip the counter entirely. We do NOT
//     want a stuck readiness probe to keep the drain window open, and
//     /ready specifically needs to return 503-draining DURING drain —
//     which it can't if this middleware short-circuits it.
//
// The /metrics endpoint is also excluded because Prometheus scrapes
// every ~15s and a stuck scrape would hold the drain open for the
// entire scrape interval. /openapi.json is excluded for the same
// reason (the dashboard polls it on every page-load).
package server

import (
	"net/http"
)

// drainExcludedPaths are paths that bypass the drain counter entirely.
// Probes + scrape endpoints — anything that callers poll on a tight
// schedule and that must continue responding during drain so operators
// + load balancers can see the state.
var drainExcludedPaths = map[string]struct{}{
	"/health":       {},
	"/ready":        {},
	"/metrics":      {},
	"/openapi.json": {},
}

// isDrainExcluded reports whether the given URL path is exempt from
// drain accounting. Path-exact match only; we don't want a future
// /api/v1/health/* route to accidentally inherit this behaviour.
func isDrainExcluded(path string) bool {
	_, ok := drainExcludedPaths[path]
	return ok
}

// withDrain wraps next with the Drain integration:
//
//   - Excluded paths (/health, /ready, /metrics, /openapi.json) pass
//     through untouched.
//   - When drain.IsDraining() is true, new requests get a 503 with
//     Connection: close. The Connection: close hint tells well-behaved
//     clients (and load balancers) to drop the keep-alive connection
//     so the next request doesn't even reach us.
//   - Otherwise the request is counted (Inc/Dec) around next.ServeHTTP.
//     Dec is deferred so a panic in a downstream handler still releases
//     the slot.
func withDrain(drain *Drain, next http.Handler) http.Handler {
	if drain == nil {
		// Nil drain = tests / boot-mode without a controller. Pass
		// through so behavior matches pre-Phase-14.3.
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isDrainExcluded(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if drain.IsDraining() {
			// Tell the client (and any upstream LB) that we're going
			// away. Connection: close prevents keep-alive reuse, so
			// the LB rotates the next request to a healthy pod.
			w.Header().Set("Connection", "close")
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable,
				"DRAINING", "server is shutting down; retry on another instance", nil)
			return
		}
		drain.Inc()
		defer drain.Dec()
		next.ServeHTTP(w, r)
	})
}
