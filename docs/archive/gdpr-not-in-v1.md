# GDPR Data Rights

BackAI exposes operator-only endpoints for exporting and erasing data
held by the BackAI backend:

- `GET /api/v1/admin/users/{id}/export`
- `POST /api/v1/admin/users/{id}/erase`

Both endpoints require an operator session. The runtime resolves the
forwarded dashboard better-auth cookie, loads `suite_operators.role`,
and checks the Casbin `admin:privacy` resource before running the
operation.

## Scope

These endpoints cover BackAI-owned app/backend records:

- `suite_users`
- better-auth users, accounts, and sessions
- tenant memberships
- API key ownership metadata
- gateway request metadata
- audit log and user activity rows
- OAuth connection metadata and vault references
- feature flag and tool adapter update attribution
- Shipwright task attribution
- approval request/decision attribution

They do not export, erase, or mutate AgentField-owned execution state.
AgentField runs, spans, traces, sessions, and memory remain in
AgentField. BackAI only exports references such as execution IDs when
those IDs are stored in BackAI tables.

## Export Contract

The export endpoint returns JSON grouped by source collection. Secret
values and OAuth token plaintext are never returned. OAuth exports include
metadata and vault references only.

Response shape:

```json
{
  "exported_at": "2026-06-07T00:00:00Z",
  "user_id": "00000000-0000-0000-0000-000000000000",
  "agentfield_notice": "BackAI export includes BackAI app-auth/backend records only...",
  "redaction_contract": "Secret values and OAuth token plaintext are never exported...",
  "data": {
    "suite_user": [],
    "better_auth_user": [],
    "memberships": []
  }
}
```

## Erasure Contract

The erase endpoint removes direct account/session records and anonymizes
or clears references that must remain for tenant, audit, billing, or
operational history.

The operation:

- deletes OAuth token vault rows referenced by the user's OAuth
  connections
- deletes OAuth connections, memberships, better-auth sessions, accounts,
  and user rows
- removes matching operator rows
- clears nullable user attribution columns on retained records
- anonymizes `suite_users.email` to
  `erased-{uuid}@erased.af-stack.local`
- sets `suite_users.name` and `suite_users.avatar_url` to `null`
- sets `suite_users.deleted_at` when it is not already set
- writes a `gdpr.user.erase` audit event

The response includes per-step row counts so operators can keep evidence
for data-rights fulfillment.

Only `owner` can erase users with the built-in policy. `admin` can read
privacy exports but cannot run erasure.
