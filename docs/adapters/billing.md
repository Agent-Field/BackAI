# Billing Adapters

Billing adapters power customer billing, metered usage, and customer
portal links.

## Active selector

`AF_STACK_BILLING_ADAPTER` selects the active external billing provider:

Supported today:

| Adapter | Use |
|---|---|
| `none` | No external billing provider |
| `stripe` | Stripe billing and customer portal |
| `lago` | Lago billing and customer portal |

When set to `none`, the operator dashboard hides
`Customers -> Customer Billing` and the customer app hides `/billing`.
Direct visits show an empty state with a link back to this document.

## Provider env

```bash
AF_STACK_BILLING_ADAPTER=stripe
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SECRET=
```

```bash
AF_STACK_BILLING_ADAPTER=lago
LAGO_API_URL=http://lago-api:3000
LAGO_API_KEY=
```

If required provider credentials are unset, the selected adapter runs in
deterministic stub mode for local development.
