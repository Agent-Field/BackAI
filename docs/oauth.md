# OAuth-on-behalf-of-user

AF Stack ships built-in OAuth so backend agents can act on behalf of
customers in third-party APIs. The first shipped providers are GitHub
and Google. The shape is Composio-like, but native to the stack.

## When to use this

You're building an agent that does something for the user inside a
third-party SaaS:

- "Schedule a meeting on my Google Calendar."
- "Open a PR on my behalf in GitHub."
- "Read files from my Google Drive."

Your customer clicks **Connect GitHub** on the integrations page once.
After that, agents can call:

```python
from af_stack import suite

token = await suite.oauth.token("github", user_id="...")
# Use it to call GitHub's REST API as that user.
```

## Architecture (one paragraph)

The runtime ships an OAuth dance per provider. Connect-button on the
customer-app → `POST /api/v1/oauth/{provider}/authorize` returns the
provider's consent URL with a signed state nonce. The user consents at
the provider; the provider redirects back to `GET /oauth/callback/
{provider}`; the runtime verifies state (HMAC), exchanges the code for
tokens, encrypts the tokens into the **secrets vault** (`suite_secrets`),
and stores opaque **references** in `suite_oauth_tokens`. Tokens never
live in plain columns — even if the metadata table leaks, the attacker
gets scopes + expiry, not bytes they can authenticate with.

Agents retrieve a fresh token via `POST /api/v1/oauth/token` (gated by
`X-AF-Stack-Internal: 1`, which CORS prevents browsers from sending).
The Manager transparently refreshes on near-expiry using the
provider's refresh flow when available.

## How OAuth-OBO differs from operator/customer sign-in

- **better-auth's `GOOGLE_CLIENT_ID`** (`apps/dashboard/src/lib/auth.ts`)
  handles SIGN-IN. It puts a session cookie in the user's browser.
- **`OAUTH_GOOGLE_CLIENT_ID`** (this module) handles AGENT acting AS the
  user inside Google's APIs. Different scopes, different tokens,
  different storage. Don't conflate.

Two providers with the same name in your OAuth dashboard is fine — they
serve different purposes.

## Configuring a provider

A provider is "configured" when both env vars are set:

```bash
OAUTH_GITHUB_CLIENT_ID=Iv1.abc123
OAUTH_GITHUB_CLIENT_SECRET=ghs_xyz...
```

Repeat per shipped provider (`GITHUB`, `GOOGLE`). Restart the runtime;
the integrations page/API provider list now reports the provider as
configured.

The runtime also needs:

- `AF_STACK_PUBLIC_URL` (e.g. `https://app.example.com`) — what the
  runtime advertises to providers as the `redirect_uri`. Must match the
  URL registered in each provider's OAuth app settings.
- `AF_STACK_OAUTH_ALLOWED_RETURN_ORIGINS` — comma-separated list of
  origins the customer-app may pass as `return_to` (open-redirect
  protection). Localhost dev origins always pass; production must
  enumerate.
- `AF_STACK_AUTH_SECRET` — signs the OAuth state nonce (same secret
  better-auth uses; one secret to rotate).

## Provider status

| Provider | Status | Notes |
|---|---|---|
| GitHub | Shipped | User-token flow (not GitHub App); no refresh |
| Google | Shipped | offline access + auto-refresh on expiry |
| Notion | Not exposed | Add an adapter + migration before documenting as configured |
| Slack | Not exposed | Add an adapter + migration before documenting as configured |
| Linear | Not exposed | Add an adapter + migration before documenting as configured |

## Adding a new provider

1. Add a package at `services/runtime/internal/oauth/adapters/<name>/`.
   Keep it outside `AllProviderNames` until exchange/refresh/revoke are
   implemented and tested.
2. Implement `Name()`, `AuthorizeURL`, `Exchange`, `Refresh` (or return
   `oauthtypes.ErrRefreshNotSupported`), `Revoke`, `DefaultScopes()`.
3. Add the provider to `AllProviderNames` and the `NewFactoryFromEnv`
   switch in `services/runtime/internal/oauth/factory.go`.
4. Add the provider to the `suite_oauth_tokens.provider` CHECK
   constraint in a new migration.
5. Add an `OAUTH_<NAME>_CLIENT_ID` / `_SECRET` row to `.env.example`.
6. Add an HTTP-fake test under your adapter package — fake the token
   endpoint with `httptest.NewServer` and verify the TokenSet round
   trip.
7. Update `docs/oauth.md` (this file) with the provider's status.

## API surface

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/oauth/providers` | List configured providers |
| GET | `/api/v1/oauth/connections` | List the user's connections |
| POST | `/api/v1/oauth/{provider}/authorize` | Get consent URL |
| GET | `/oauth/callback/{provider}` | Provider redirect target |
| DELETE | `/api/v1/oauth/{provider}` | Disconnect (revoke + soft-delete) |
| POST | `/api/v1/oauth/token` | **Internal**: fresh access token |

The `/oauth/token` endpoint requires `X-AF-Stack-Internal: 1`. The CORS
allow-list does not include this header, so cross-origin browser
requests cannot send it; only server-side callers (agents, workload
modules, the AF Stack SDK from a Node process) reach it.

## SDK usage

### Python

```python
from af_stack import suite

# Customer-facing (browser flow): get the consent URL and redirect.
url = await suite.oauth.authorize_url("github", return_to="https://app.example.com/integrations")

# Agent-facing (server-side/API-key): grab a fresh token to call the API.
access = await suite.oauth.token("github", user_id="11111111-1111-1111-1111-111111111111")
import httpx
async with httpx.AsyncClient() as c:
    r = await c.get(
        "https://api.github.com/user/repos",
        headers={"Authorization": f"Bearer {access}"},
    )
```

### TypeScript

```ts
import { suite } from "@af-stack/sdk"

// Browser-side connect button.
const url = await suite.oauth.authorizeUrl("github", {
  returnTo: `${location.origin}/integrations`,
})
location.href = url

// Server-side/API-key fetch using the token.
const token = await suite.oauth.token("github", {
  userId: "11111111-1111-1111-1111-111111111111",
})
const r = await fetch("https://api.github.com/user/repos", {
  headers: { Authorization: `Bearer ${token}` },
})
```

## Security model

- **Token storage** — access + refresh tokens live in `suite_secrets`
  (AES-256-GCM, KEK from `AF_STACK_KMS_KEY`). The `suite_oauth_tokens`
  row stores only opaque references.
- **CSRF** — every `/authorize` call signs an HMAC state nonce over
  `(tenant_id, user_id, provider, return_to, issued_at, nonce)` using
  `AF_STACK_AUTH_SECRET`. The callback rejects any state that fails
  HMAC verify or is older than 10 minutes.
- **Open redirect** — `return_to` is validated twice: once at
  `/authorize` (early bail) and once on the callback (defense in depth)
  against the allow-list, plus a scheme check (only `http`/`https`).
- **Internal-only token retrieval** — `/oauth/token` requires
  `X-AF-Stack-Internal: 1`. This header is not in the CORS allow-list,
  so browsers can't reach the endpoint regardless of cookie state.
- **User targeting** — browser/session calls can list or disconnect only
  the resolved session user's grant. Backend/API-key callers may pass
  `user_id` when an agent is serving a specific user.
- **Audit** — `oauth.authorize`, `oauth.callback`, and
  `oauth.disconnect` all write to `suite_audit_log`. The token
  retrieval verb is deliberately NOT audited (would be too noisy —
  agents call it on every third-party request) — use provider-side
  audit logs for that level.
- **Revoke on disconnect** — best-effort upstream revoke + secret-vault
  delete + local row soft-delete. Local-only deletion still succeeds
  if the upstream revoke fails (operator might want to forget the
  grant even if the provider is unreachable).
