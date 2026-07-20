// SPDX-License-Identifier: Apache-2.0

package billing

import "testing"

func strptr(s string) *string { return &s }

// Contract: the entitlement plan follows the status.
//
//	active / trialing / past_due → keep the subscribed plan.
//	terminal (canceled/unpaid/incomplete_expired/paused/incomplete) → free.
//	unknown status → degrade (fail safe).
func TestEntitlementPlan(t *testing.T) {
	cases := []struct {
		status, sub, want string
	}{
		{StatusActive, "pro", "pro"},
		{StatusTrialing, "pro", "pro"},
		{StatusPastDue, "pro", "pro"}, // grace: dunning keeps entitlements
		{StatusCanceled, "pro", "free"},
		{StatusUnpaid, "pro", "free"},
		{StatusIncompleteExpired, "pro", "free"},
		{StatusPaused, "pro", "free"},
		{StatusIncomplete, "pro", "free"},
		{"something_new", "pro", "free"}, // unknown → degrade
		{StatusActive, "", "free"},       // no subscribed plan → free
	}
	for _, c := range cases {
		if got := EntitlementPlan(c.status, c.sub, "free"); got != c.want {
			t.Errorf("EntitlementPlan(%q,%q) = %q, want %q", c.status, c.sub, got, c.want)
		}
	}
}

// Contract: the status classifiers partition the Stripe status space.
func TestStatusClassifiers(t *testing.T) {
	if !StatusIsActive(StatusActive) || !StatusIsActive(StatusTrialing) {
		t.Error("active/trialing must classify as active")
	}
	if StatusIsActive(StatusPastDue) {
		t.Error("past_due is not 'active'")
	}
	if !StatusInGrace(StatusPastDue) {
		t.Error("past_due must be a grace state")
	}
	for _, s := range []string{StatusCanceled, StatusUnpaid, StatusIncompleteExpired, StatusPaused, StatusIncomplete} {
		if !StatusIsTerminal(s) {
			t.Errorf("%q must classify as terminal", s)
		}
	}
	if StatusIsTerminal(StatusActive) || StatusIsTerminal(StatusPastDue) {
		t.Error("active/past_due must not classify as terminal")
	}
}

// Contract: Reconcile drives the local row to the desired state and reports
// whether entitlements degraded — exercised across the full drift set with
// fabricated Stripe snapshots (no live Stripe).
func TestReconcile_DriftCases(t *testing.T) {
	cases := []struct {
		name         string
		current      Customer
		remote       RemoteSubscription
		wantPlan     string
		wantStatus   string
		wantDegraded bool
		wantChanged  bool
	}{
		{
			name:        "trial starts",
			current:     Customer{Plan: "free"},
			remote:      RemoteSubscription{Status: StatusTrialing, Plan: "pro"},
			wantPlan:    "pro",
			wantStatus:  StatusTrialing,
			wantChanged: true,
		},
		{
			name:        "trial converts to active",
			current:     Customer{Plan: "pro", SubscriptionStatus: strptr(StatusTrialing)},
			remote:      RemoteSubscription{Status: StatusActive, Plan: "pro"},
			wantPlan:    "pro",
			wantStatus:  StatusActive,
			wantChanged: true,
		},
		{
			name:        "active steady state — no change",
			current:     Customer{Plan: "pro", SubscriptionStatus: strptr(StatusActive)},
			remote:      RemoteSubscription{Status: StatusActive, Plan: "pro"},
			wantPlan:    "pro",
			wantStatus:  StatusActive,
			wantChanged: false,
		},
		{
			name:        "payment fails → past_due keeps plan (dunning)",
			current:     Customer{Plan: "pro", SubscriptionStatus: strptr(StatusActive)},
			remote:      RemoteSubscription{Status: StatusPastDue, Plan: "pro"},
			wantPlan:    "pro",
			wantStatus:  StatusPastDue,
			wantChanged: true,
		},
		{
			name:         "dunning exhausted → unpaid degrades to free",
			current:      Customer{Plan: "pro", SubscriptionStatus: strptr(StatusPastDue)},
			remote:       RemoteSubscription{Status: StatusUnpaid, Plan: "pro"},
			wantPlan:     "free",
			wantStatus:   StatusUnpaid,
			wantDegraded: true,
			wantChanged:  true,
		},
		{
			name:         "cancellation degrades to free",
			current:      Customer{Plan: "pro", SubscriptionStatus: strptr(StatusActive)},
			remote:       RemoteSubscription{Status: StatusCanceled, Plan: "pro"},
			wantPlan:     "free",
			wantStatus:   StatusCanceled,
			wantDegraded: true,
			wantChanged:  true,
		},
		{
			// Degraded describes the desired STATE (entitlements at default
			// due to terminal failure); Changed is false because the row is
			// already there. The caller acts only on the transition
			// (Changed && Degraded), so this is a no-op.
			name:         "already-degraded canceled → no change",
			current:      Customer{Plan: "free", SubscriptionStatus: strptr(StatusCanceled)},
			remote:       RemoteSubscription{Status: StatusCanceled, Plan: "pro"},
			wantPlan:     "free",
			wantStatus:   StatusCanceled,
			wantDegraded: true,
			wantChanged:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Reconcile(c.current, c.remote, "free")
			if got.Plan != c.wantPlan {
				t.Errorf("Plan = %q, want %q", got.Plan, c.wantPlan)
			}
			if got.Status != c.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, c.wantStatus)
			}
			if got.Degraded != c.wantDegraded {
				t.Errorf("Degraded = %v, want %v", got.Degraded, c.wantDegraded)
			}
			if got.Changed != c.wantChanged {
				t.Errorf("Changed = %v, want %v", got.Changed, c.wantChanged)
			}
		})
	}
}
