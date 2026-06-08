# Auth Bootstrap

AF Stack ships two Next.js auth surfaces that share the same better-auth
tables:

- `apps/dashboard/` for operators
- `apps/customer-app/` for tenant customers

Because both apps share `"user"`, operator access is controlled by the
explicit `suite_operators` allow-list. Customer sign-ups do not become
operators just because they created a better-auth user.

## First Operator

On a fresh deploy:

1. The dashboard sees `suite_operators` is empty and redirects to
   `/setup`.
2. `/setup` sends the user to `/signup`.
3. The dashboard better-auth `user.create.after` hook mirrors the user
   into `suite_users`, grants default-tenant owner membership, and inserts
   the first row in `suite_operators`.
4. Subsequent dashboard sign-ups create auth users, but they are not
   operators unless explicitly allowed.

Dashboard admin routes call `requireOperator()`, which checks both the
better-auth session and `suite_operators`.

## Operator RBAC

AF Stack uses Casbin in the runtime for operator/admin authorization.
The dashboard still performs the first session gate with
`requireOperator()`, but every `/api/v1/admin/*` runtime request also
resolves the forwarded better-auth session cookie and checks
`suite_operators.role` before the handler can run a cross-tenant query or
opt into `app.bypass_rls`.

Current built-in roles:

| Role | Allowed |
| --- | --- |
| `owner` | Read, write, and delete all admin resources |
| `admin` | Read all admin resources, including privacy exports; create/update tenants, memberships, API keys, and budgets; cannot delete tenants, remove memberships, revoke API keys, or erase users |

Resources covered by the built-in policy:

- `admin:tenants`
- `admin:users`
- `admin:memberships`
- `admin:keys`
- `admin:budgets`
- `admin:audit`
- `admin:privacy`

PG RLS still enforces tenant isolation for tenant-scoped data. RBAC is
the operator permission layer above RLS, and only admin handlers that pass
Casbin checks can use the documented `app.bypass_rls` admin path.

## CLI Fallback

If setup is broken or you need to pre-authorize an operator email:

```bash
DATABASE_URL=postgres://... af-stack operator create \
  --email founder@example.com \
  --name "Founder"
```

The command creates or updates:

- `suite_users`
- `suite_operators`

If a better-auth `"user"` row already exists for the email, the operator
record links to its `user_id`. If it does not exist yet, the email is
still allowed; once that person signs up with the same email, dashboard
access succeeds by email match.

The CLI does not write better-auth password hashes. Use the dashboard
sign-up or magic-link flow for credentials.

## Customer App

Customer sign-up is separate:

- `apps/customer-app/src/lib/auth.ts` provisions a tenant, owner
  membership, billing customer row, and one-shot API key.
- Customer users are tenant owners in their product tenant.
- Customer users are not dashboard operators unless their email is also
  present in `suite_operators`.

## Enterprise SSO / SAML

AF Stack's dashboard SSO entrypoint is OIDC. SAML is supported through a
broker:

- **Self-hosted**: Authentik accepts SAML from the enterprise IdP and
  exposes an OIDC application to AF Stack.
- **Managed**: WorkOS owns the SAML/OIDC broker and exposes the same
  OIDC shape to AF Stack.

This keeps SAML XML parsing, signing-certificate rollover, and IdP
metadata drift out of the Next.js app. AF Stack keeps only better-auth
sessions in Postgres.

Set these env vars on the dashboard and customer-app services:

```bash
BETTER_AUTH_URL=https://admin.example.com
AF_STACK_SSO_LABEL="Company SSO"
AF_STACK_SSO_ISSUER=https://auth.example.com/application/o/af-stack/
AF_STACK_SSO_CLIENT_ID=...
AF_STACK_SSO_CLIENT_SECRET=...
AF_STACK_SSO_SCOPES="openid email profile"
```

`AF_STACK_SSO_DISCOVERY_URL` is optional. When omitted, AF Stack uses:

```text
<AF_STACK_SSO_ISSUER>/.well-known/openid-configuration
```

Register these redirect URIs in Authentik, WorkOS, or your OIDC broker:

```text
https://admin.example.com/api/auth/oauth2/callback/enterprise-sso
https://app.example.com/api/auth/oauth2/callback/enterprise-sso
```

The provider must return an email claim. New SSO users are mirrored into
`suite_users` by the same better-auth hook as email/password users. They
do not become dashboard operators unless they are the first bootstrap
user or their email is present in `suite_operators`.
