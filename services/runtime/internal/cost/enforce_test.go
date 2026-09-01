// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"sync"
	"testing"
)

// fakeLedger stands in for the shared suite_cost_events ledger: a single
// committed-spend counter guarded by a mutex, shared by both "replicas". A DB
// transaction serialises commits the same way this mutex does.
type fakeLedger struct {
	mu    sync.Mutex
	spent float64
}

func (l *fakeLedger) committed() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spent
}

// enforcer models one runtime replica. It shares the ledger with every other
// replica (there is no per-replica spend state) and runs the real production
// decider keyBudgetDecision, exactly like the gateway pre-call gate.
type enforcer struct {
	id     string
	cap    float64
	ledger *fakeLedger
}

// attempt performs a check-then-record against the shared ledger under the
// ledger lock (modelling a serialised DB reserve). Returns whether the call was
// admitted.
func (e *enforcer) attempt(cost float64) bool {
	e.ledger.mu.Lock()
	defer e.ledger.mu.Unlock()
	if !keyBudgetDecision(e.cap, e.ledger.spent, cost) {
		return false
	}
	e.ledger.spent += cost
	return true
}

// ─── Pure decider semantics ───────────────────────────────────────────────

func TestBudgetDeciderBoundary(t *testing.T) {
	// At exactly the cap the call is still admitted (<=).
	if !budgetDecision(10, 9.999, 0.001) {
		t.Error("spend landing exactly on the cap should admit")
	}
	if budgetDecision(10, 10, 0.001) {
		t.Error("spend over the cap should reject")
	}
}

func TestKeyBudgetDeciderUncapped(t *testing.T) {
	if !keyBudgetDecision(0, 1_000_000, 5) {
		t.Error("zero cap means uncapped; should always admit")
	}
	if !keyBudgetDecision(-1, 1_000_000, 5) {
		t.Error("negative cap means uncapped; should always admit")
	}
}

// ─── Two-replica shared-state enforcement ─────────────────────────────────

// TestTwoEnforcersShareCommittedSpend proves the enforcement is driven by
// shared ledger state, not per-replica memory: a call admitted+recorded by
// replica A is immediately visible to replica B's next check.
func TestTwoEnforcersShareCommittedSpend(t *testing.T) {
	ledger := &fakeLedger{spent: 9.0}
	a := &enforcer{id: "replica-a", cap: 10, ledger: ledger}
	b := &enforcer{id: "replica-b", cap: 10, ledger: ledger}

	// Replica A admits a $0.50 call (9.0 + 0.5 = 9.5 <= 10) and records it.
	if !a.attempt(0.50) {
		t.Fatal("replica A should admit the first $0.50 call")
	}
	// Replica B — a *different* instance sharing the SAME ledger — sees $9.50
	// and admits its own $0.50 (9.5 + 0.5 = 10 <= 10).
	if !b.attempt(0.50) {
		t.Fatal("replica B should admit up to the cap using shared committed spend")
	}
	// The ledger is now exactly at the cap; replica B's next call must reject —
	// it observed replica A's + its own recorded spend.
	if b.attempt(0.50) {
		t.Fatal("replica B should reject once shared committed spend reached the cap")
	}
	if got := ledger.committed(); got != 10.0 {
		t.Fatalf("shared committed spend = %v, want exactly 10 (no double-spend past cap)", got)
	}
}

// TestConcurrentEnforcersNeverExceedCap runs many concurrent attempts across
// two replicas against one shared ledger and asserts the cap is never breached
// and the admitted total is exact — the correctness a shared, serialised store
// buys over per-replica counters.
func TestConcurrentEnforcersNeverExceedCap(t *testing.T) {
	const budgetCap = 5.0
	const callCost = 1.0
	const attemptsPerReplica = 100

	ledger := &fakeLedger{}
	replicas := []*enforcer{
		{id: "a", cap: budgetCap, ledger: ledger},
		{id: "b", cap: budgetCap, ledger: ledger},
	}

	var admitted int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range replicas {
		for i := 0; i < attemptsPerReplica; i++ {
			wg.Add(1)
			go func(e *enforcer) {
				defer wg.Done()
				if e.attempt(callCost) {
					mu.Lock()
					admitted++
					mu.Unlock()
				}
			}(r)
		}
	}
	wg.Wait()

	if got := ledger.committed(); got > budgetCap {
		t.Fatalf("shared committed spend = %v exceeded cap %v — enforcement is not shared/atomic", got, budgetCap)
	}
	// Exactly cap/callCost calls should have been admitted, no more, no fewer.
	if wantAdmitted := int64(budgetCap / callCost); admitted != wantAdmitted {
		t.Fatalf("admitted %d calls, want exactly %d (cap/cost)", admitted, wantAdmitted)
	}
}
