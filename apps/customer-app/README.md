# Customer App — SupportDesk AI

Customer-facing starter app for BackAI. It is the product surface a user edits
into their own AI SaaS. The operator dashboard is separate: customers use this
app, while operators inspect tenants, costs, requests, billing, and runs in
admin.

For fork customization rules, see [`EDITING.md`](EDITING.md).

## What it is

The dashboard answers the question _"what does the operator who runs BackAI
see?"_. This app answers _"what does the end user of the AI product see?"_.
Same platform, opposite side of the LLM gateway.

A customer can:

1. Sign up at `/sign-up`. A tenant, owner membership, billing customer row,
   and an `af_…` API key are auto-provisioned in one transaction.
2. See their workspace at `/dashboard` — AI actions today, cost today, recent
   calls, masked API key, copy-pastable curl + Python quickstart.
3. Draft a support reply on `/code-helper`. The request goes through the LLM
   gateway (`/api/v1/llm/chat/completions`) using their tenant context, streams
   back, and the cost lands in `suite_cost_events` against their tenant.
4. View live usage meters + open the Stripe Customer Portal on `/billing`.
5. Manage their API keys (issue, revoke) on `/api-key`.

## How it relates to the dashboard

|                   | Admin dashboard                  | Customer app                |
| ----------------- | -------------------------------- | --------------------------- |
| Audience          | Operator (whoever runs BackAI)   | Tenant customer             |
| Better-auth users | Same `user` table                | Same `user` table           |
| Tenant scope      | All tenants                      | One tenant (customer's own) |
| API key creation  | `POST /api/v1/admin/keys`        | Auto on signup + `/api-key` |
| Cost view         | All tenants' calls               | Their own tenant only       |
| Brand             | "BackAI Admin"                   | "SupportDesk AI"            |

Both apps share:

- The same Postgres database (`afstack`)
- The same better-auth `user`/`session`/`account`/`verification` tables
- The same `AF_STACK_AUTH_SECRET` so session cookies are interchangeable
  if the cookie domain ever overlaps (in local dev they live on different
  ports of `localhost`, so cookies are not shared by default)

The customer-app NEVER calls the operator's admin REST endpoints
(`/api/v1/admin/*`). All provisioning happens via direct PG inserts into
`suite_users`, `suite_tenants`, `suite_memberships`, `suite_api_keys`,
and `suite_billing_customers`.

## Customer signup flow

1. POST `/api/auth/sign-up/email` (better-auth) → row in `"user"`.
2. `databaseHooks.user.create.after` runs, calling
   `lib/provisioning.ts::provisionTenant({ withApiKey: false })`. This
   creates `suite_users` (UUID, keyed by email), `suite_tenants` (slug =
   `<email-local>-<rand>`), `suite_memberships` (role = owner), and
   `suite_billing_customers` (plan = free, status = stub) in a single
   transaction.
3. better-auth auto-signs-in (sets the session cookie).
4. The sign-up React form posts to `/api/customer/onboarding-key`,
   which mints a fresh `af_<prefix>_<secret>` token, persists its
   bcrypt hash to `suite_api_keys`, and returns the plaintext exactly
   once.
5. The form pops a Dialog with the full token. The customer copies it.
   They never see the plaintext again — only the prefix on the
   dashboard.
6. Continue to `/dashboard`.

If the auth hook fails for any reason, `requireCustomerContext()` in
`lib/session.ts` will lazily re-run provisioning on first access to any
authenticated route (without minting a new key — the customer can still
issue one from `/api-key`).

## Running locally

The override file at the repo root wires this in. From the af-stack root:

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml \
  up -d customer-app
```

Verify:

```bash
# Should redirect to /sign-in
curl -i http://localhost:34000/

# Should create a tenant + membership + billing row, sign in via cookie
curl http://localhost:34000/api/auth/sign-up/email \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@test.com","password":"test12345678","name":"Alice"}'
```

## Tech notes

- Next.js 16, same shadcn UI primitives as the dashboard (`base-nova`).
- Forms: react-hook-form + zod + `Field` + `FieldGroup` pattern.
- Dark mode default. Purple primary (`--primary: oklch(0.55 0.22 285)`).
- Markdown answers rendered via `react-markdown` + `react-syntax-highlighter`.
- The proxy at `src/app/api/v1/[...path]/route.ts` forwards the better-auth
  session cookie to the runtime; the runtime's `tenant_resolver.go` reads
  it and scopes data to the customer's tenant.
- `lib/api.ts` mirrors only the zod schemas the customer-app needs
  (cost, cost events, billing). The Dockerfile copy scope is just this
  app — cross-app imports would break the build.
