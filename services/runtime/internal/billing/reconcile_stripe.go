// SPDX-License-Identifier: Apache-2.0

// reconcile_stripe.go — Stripe-specific subscription read used only by the
// drift-reconciliation cron.
//
// This capability is NOT part of the provider-neutral Client interface: the
// reconcile cron type-asserts a Client to subscriptionReader and no-ops when
// the active adapter doesn't implement it (stub, lago, remote). That keeps
// drift reconciliation a Stripe-only feature without forcing every adapter
// to grow a stub, and without expanding the surface the webhook path relies
// on.
package billing

import (
	"context"
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	stripesub "github.com/stripe/stripe-go/v82/subscription"
)

// subscriptionReader reads the current subscription snapshot for a provider
// customer id. Implemented only by realStripeClient. found=false means the
// customer has no subscription (the reconciler treats that as canceled).
type subscriptionReader interface {
	GetSubscription(ctx context.Context, customerID string) (remote RemoteSubscription, found bool, err error)
}

// GetSubscription returns the customer's most-recent subscription snapshot.
// It lists across all statuses (limit 1) so a canceled/past_due subscription
// is still visible for reconciliation.
func (c *realStripeClient) GetSubscription(_ context.Context, customerID string) (RemoteSubscription, bool, error) {
	if customerID == "" {
		return RemoteSubscription{}, false, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	params := &stripe.SubscriptionListParams{
		Customer: stripe.String(customerID),
		Status:   stripe.String("all"),
	}
	params.Limit = stripe.Int64(1)
	it := stripesub.List(params)
	for it.Next() {
		sub := it.Subscription()
		out := RemoteSubscription{
			StripeCustomerID: customerID,
			Status:           string(sub.Status),
		}
		if sub.TrialEnd > 0 {
			out.TrialEnd = time.Unix(sub.TrialEnd, 0).UTC().Format(time.RFC3339Nano)
		}
		if sub.Items != nil && len(sub.Items.Data) > 0 {
			item := sub.Items.Data[0]
			if item.CurrentPeriodEnd > 0 {
				out.CurrentPeriodEnd = time.Unix(item.CurrentPeriodEnd, 0).UTC().Format(time.RFC3339Nano)
			}
			if item.Price != nil {
				out.StripePriceID = item.Price.ID
			}
		}
		return out, true, nil
	}
	if err := it.Err(); err != nil {
		return RemoteSubscription{}, false, fmt.Errorf("%w: list subscriptions: %v", ErrStripeUnavailable, err)
	}
	return RemoteSubscription{}, false, nil
}
