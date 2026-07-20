// SPDX-License-Identifier: Apache-2.0

// enforce.go — the pure core of budget/quota enforcement.
//
// Multi-replica correctness
// -------------------------
// Budget enforcement holds NO in-memory spend counters. Every decision reads
// the running spend from the shared suite_cost_events ledger in Postgres
// (Budgets.Spent / Budgets.SpentByKey), and every admitted call's cost is
// written back to that same ledger by the Recorder. Two runtime replicas
// therefore enforce against the SAME committed spend: the moment replica A
// records a call, replica B's next check observes it. No per-process state can
// diverge across replicas — this is what makes horizontal scaling correct.
//
// The only residual cross-replica window is the check→record gap for calls in
// flight simultaneously (a TOCTOU that likewise exists between two goroutines
// inside a single replica): both may read spend just under the cap and both
// admit. That window is bounded — the gateway passes a small per-call estimate
// and the ledger converges as soon as each call's real cost is recorded — and
// is the documented, accepted fail-open behaviour, not a correctness bug.
//
// The deciders below are the pure functions both replicas run. Keeping the
// decision separate from the DB read makes the "shared committed spend ⇒ same
// decision on every replica" property unit-testable without a live database
// (see enforce_test.go).

package cost

// budgetDecision reports whether a call is admitted given the applicable
// monthly cap and the committed period spend (both read from the shared
// ledger). estimatedUSD is the caller's best guess at the upcoming call's cost.
// Admission requires the projected total to stay at or below the cap.
func budgetDecision(capUSD, committedSpendUSD, estimatedUSD float64) bool {
	return committedSpendUSD+estimatedUSD <= capUSD
}

// keyBudgetDecision reports whether a call is admitted given a per-key lifetime
// cap and the committed lifetime spend for that key. A non-positive cap means
// "uncapped" and always admits.
func keyBudgetDecision(capUSD, committedSpendUSD, estimatedUSD float64) bool {
	if capUSD <= 0 {
		return true
	}
	return committedSpendUSD+estimatedUSD <= capUSD
}
