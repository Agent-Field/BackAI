# Adapter Protocol Audit — Notifications / Secrets / Billing v1

Audit of three BackAI v1 adapter protocols against real-world connector
APIs (Resend, Postmark, SendGrid, Twilio, Slack, OneSignal; Vault, AWS
Secrets Manager, Azure KV, GCP SM, Doppler, 1Password; Stripe, Lago,
Paddle, LemonSqueezy, Polar, Chargebee).

**Frame**: gaps that block a third-party adapter from plugging in vs.
gaps that just mean "v2 will be nicer." BackAI is targeting indie
hackers and early-stage teams — not Fortune 500 procurement.

---

## 1. Notifications v1

### Must-fix gaps

**1a. `supports_attachments=true` is a lie today.** The capability is
declared but `POST /v1/messages` has no `attachments` field. A Resend
or SendGrid adapter that legitimately supports attachments has nowhere
to receive them. Either:
- Drop the capability flag entirely from v1, OR
- Add a minimal `attachments: [{filename, content_base64, content_type}]`
  array (capped at, say, 10 MB total via `max_attachment_bytes`).

Recommendation: **drop the flag**. Indie users sending welcome emails
don't need attachments; serious senders use templates. Reserving a flag
that no endpoint honors is worse than not having it.

**1b. `to` is a flat array — no `cc` / `bcc`.** Every real email
provider (Resend, Postmark, SendGrid, Mailgun) takes `to`/`cc`/`bcc`
separately. A third-party Postmark adapter has no way to express a `cc`
without abusing the `to` field. Cheap fix: add optional `cc: []` and
`bcc: []`.

**1c. SMS `from` semantics are ambiguous.** Twilio requires either a
phone number, an Alphanumeric Sender ID, or a Messaging Service SID.
The protocol says `from` is a string "SMS sender id" but doesn't
distinguish. A Twilio adapter will work because the string is opaque,
but it's worth a one-line clarification: `from` is provider-specific
and opaque to the runtime.

### Nice-to-have (defer to v2)

- **Bulk send** (single API for N distinct recipients with per-recipient
  vars). Resend batch endpoint, SendGrid `personalizations`. v1's
  `to: []` is fan-out-to-same-content, which is fine. Indie users send
  one welcome email at a time. Defer.
- **Scheduled send** (`send_at`). Resend, SendGrid, Postmark support it.
  Easy to add as an optional field later. Not a blocker.
- **Bounce / complaint inbound webhooks.** The current model is "poll
  `/v1/messages/{id}`." Real providers push bounces via webhook. This
  is a structural mismatch but the protocol notes `bounced` / `complained`
  in the GET response, which is enough for v1. Inbound bounce webhooks
  are a v2 feature (cross-cutting with the billing webhook pattern).
- **Reply / threading**. Postmark `InReplyTo`, SendGrid `headers`. Only
  matters for inbound-email use cases (support inboxes). Out of scope
  for transactional v1.
- **Slack / push specifics.** Slack webhooks take `blocks` (rich layout);
  OneSignal takes `headings`/`contents` per locale + `data` payload.
  The current `body_text`/`body_html`/`subject` is awkward for both,
  but a Slack adapter can stuff JSON into `body_text` and survive.
  Defer; revisit when someone actually wants to ship a non-email
  adapter.

### Verdict: **Ship with must-fix 1a + 1b only**

Drop `supports_attachments` (or add the array). Add `cc`/`bcc`.
Everything else is fine for indie use. Twilio, Resend, Postmark,
SendGrid adapters all build cleanly against this with those two
changes.

---

## 2. Secrets v1

### Must-fix gaps

**2a. No version-specific read.** The protocol declares
`supports_versioning: true` and `version_retention_count` but
`POST /v1/secrets/{key}/reveal` always returns the current version.
Vault KV v2 supports `?version=N`, AWS Secrets Manager has
`VersionStage`/`VersionId`, Azure KV has version-pinned URIs. Without
a `version` query param, an operator who rotated a key by mistake
cannot recover the old value through the protocol — they have to go
into Vault directly, defeating the abstraction.

Fix: `POST /v1/secrets/{key}/reveal` accepts `{"version": N}` in body
(omit = latest). Similarly `GET /v1/secrets/{key}?version=N` for
metadata. One-line additions.

**2b. `reveal` shape forces value-bearing rotation paths to lie.**
Section 5 says "the new plaintext is NOT returned" from rotate, but
Mode A (adapter generates) is useless if the operator can't see the
new value. The protocol allows MAY-return — fine — but the conformance
checklist on line 269 says `/reveal` returns the value, which only
works if the adapter retains it. State this clearly: Mode A adapters
MUST either return the value in the rotate response OR persist it so
`/reveal` works. Otherwise the generated secret is unrecoverable.

### Nice-to-have (defer to v2)

- **Lease / TTL secrets** (Vault dynamic creds). Vault's headline
  feature is short-lived DB credentials with auto-revoke. BackAI's
  current model is "static secret in a box" — Vault works fine for
  that, just degraded. A v2 protocol can add `lease_id` / `ttl_seconds`
  in the reveal response and `POST /v1/secrets/{key}/renew`. **Don't
  ship in v1** — adds a state machine and changes runtime caching
  semantics.
- **Dynamic secrets** (DB credentials generated on demand). Same as
  above — different shape entirely (request-driven, not stored).
  Different slot or v2.
- **Encryption-as-a-service** (Vault Transit `encrypt`/`decrypt`).
  Not a secrets-store concern. Belongs in its own future `crypto` slot.
  Don't conflate.
- **Policy / access control surface.** AWS IAM, Vault policies, 1Pwd
  vault-level ACLs. The runtime has its own tenant/role model; the
  adapter doesn't need to expose upstream policies. Pass-through is
  fine.
- **Audit log retrieval.** Current protocol says the adapter MUST
  audit-log reveals, but offers no `GET /v1/audit` to surface them.
  For v1, the runtime's own request log is sufficient (it sees every
  `/reveal` call). Adapter-internal audit is for upstream compliance,
  not BackAI UX.

### Verdict: **Ship with must-fix 2a + a clarification on 2b**

Add `version` to reveal/metadata. Tighten rotate-mode-A wording. Done.
Vault KV v2, AWS Secrets Manager, Azure KV, GCP SM, Doppler, 1Password
all build against this — the dynamic-secrets feature gap is acceptable
because the protocol is honest about being a static-secret store.

---

## 3. Billing v1

### Must-fix gaps

**3a. No subscription endpoint at all.** The customer response carries
`plan`, `trial_ends_at`, `current_period_end`, `subscription_status`
as read fields, but there's no `POST /v1/subscriptions` or `PATCH
/v1/customers/{id}` to actually create/change a subscription. The
implicit model is "subscription happens in the customer portal" —
which is valid for Stripe Checkout / Paddle / LemonSqueezy flows
where the portal handles it. **This is actually OK for indie v1**
as long as it's documented.

Action: add one sentence to §1 saying "subscription lifecycle happens
via the customer portal (§3) and inbound webhooks (§4); the adapter
does not expose programmatic plan changes in v1." That converts a
gap into an explicit design choice. Without that, integrators will
hunt for the missing endpoint.

**3b. `supports_refunds` and `supports_disputes` flags with no
endpoints.** Same problem as notifications `supports_attachments`. The
flags declare capability but `POST /v1/refunds` doesn't exist. A
Stripe adapter that fully supports refunds has nothing to wire to.
Either drop the flags or add minimal endpoints.

Recommendation: **drop both flags from v1 capabilities**. Indie users
handle refunds in the Stripe dashboard. Reserved-but-unused flags rot.

**3c. Webhook verification has no event-type allow-list / filtering
contract.** The response returns `decoded` as an opaque object. The
runtime has to know that for Stripe, `checkout.session.completed` means
"subscription started" and for Lago, the event name is different. The
protocol pushes all event-type semantics to the runtime. This is
probably the right call (adapter normalizes signature, runtime
normalizes meaning) but it should be stated: the runtime is
responsible for mapping provider event types to BackAI internal
events. Otherwise an adapter author thinks they need to remap and
gets it wrong.

### Nice-to-have (defer to v2)

- **Multi-currency.** `default_currency` is declared but `POST
  /v1/customers` doesn't accept a `currency`. Stripe lets you set per-
  customer currency. For v1, default-currency-only is fine — most
  indie products are USD-only. Defer.
- **Tax calculation** (Stripe Tax, Paddle MoR-included). Paddle/
  LemonSqueezy handle tax automatically as merchant-of-record; Stripe
  needs explicit Tax setup. The runtime doesn't need to know — tax
  shows up on the invoice. Out of scope for v1.
- **Coupons / discount codes.** Belongs at the checkout / portal
  layer, not the adapter API. Stripe Coupons, Lago Coupons — both have
  rich semantics. Defer.
- **Dispute / chargeback handling.** Inbound webhook event types cover
  this — a `charge.dispute.created` arrives via the existing webhook
  pipeline. The runtime can surface it without a dedicated endpoint.
- **Usage ingestion shape.** `POST /v1/usage` is single-event. Lago
  and Stripe Meters both support batch ingestion. For indie throughput
  (< 1000 events/min) the single-event API is fine; HTTP/2 keep-alive
  makes it cheap. Batch is a v2 optimization.

### Verdict: **Ship with must-fix 3b + documentation tightening on 3a/3c**

Drop `supports_refunds` and `supports_disputes` from v1 capabilities.
Add one paragraph each on (a) "subscription via portal, not
programmatic" and (c) "event-type semantics live in runtime." All six
target providers (Stripe, Lago, Paddle, LemonSqueezy, Polar, Chargebee)
build cleanly: customers + portal + webhook verification + optional
metered usage covers their core flow. Plan changes, refunds, disputes
all route through the portal + inbound webhooks today, which is
already supported.

---

## Cross-cutting observation

The three protocols share one anti-pattern: **capability flags that
declare features with no corresponding endpoint** (`supports_attachments`,
`supports_refunds`, `supports_disputes`, and arguably
`supports_versioning` without `?version=N`). These create false
promises to integrators. Rule for v1: **a capability flag is only
valid if there's an endpoint or field it gates.** Reserved-for-v2
flags belong in v2.

## Summary

| Slot | Verdict | Must-fix count |
|---|---|---|
| Notifications | Ship after small fix | 2 (drop `supports_attachments` OR add field; add `cc`/`bcc`) |
| Secrets | Ship after small fix | 1.5 (add `version` to reveal/metadata; clarify rotate-A wording) |
| Billing | Ship after small fix | 1 + 2 doc-only (drop refund/dispute flags; document portal-driven subs + runtime event mapping) |

All three are within striking distance of v1-ready. None of the gaps
demand a structural redesign. Indie-hacker scope is the right altitude:
the protocols handle the 80% case (transactional email, static secret
storage, subscription via Stripe Checkout / Paddle portal) and degrade
gracefully on the 20% (no attachments, no dynamic creds, no
programmatic plan changes). Fix the false-promise capability flags,
ship.
