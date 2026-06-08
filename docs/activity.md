# User Activity Log

AF Stack includes a tenant-scoped activity log for product events in
the app you build on the stack.

Use it for customer-facing events:

- onboarding steps
- uploads, downloads, searches, and shares
- billing page views or plan changes
- domain actions from workload modules
- customer-visible timeline entries

Do not use it for AgentField memory, sessions, runs, spans, or traces.
Those remain in AgentField. Do not use it for operator/admin compliance
events either; those stay in `suite_audit_log`.

## Stack

- Table: `suite_user_activity`
- Isolation: `tenant_id` plus Postgres RLS
- Identity: `user_id` and `api_key_id` are filled from request context
  when available
- Querying: action, user, resource, time range, limit, offset

## TypeScript

```ts
import { suite } from "@af-stack/sdk"

await suite.activity.log("document.uploaded", {
  resourceType: "document",
  resourceId: "doc_123",
  metadata: {
    fileType: "pdf",
    bytes: 481920,
  },
})

const recent = await suite.activity.list({
  action: "document.uploaded",
  resourceType: "document",
  limit: 20,
})
```

## REST

Append an event:

```bash
curl -X POST "$AF_STACK_URL/api/v1/activity" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "action": "document.uploaded",
    "resource_type": "document",
    "resource_id": "doc_123",
    "metadata": {"file_type": "pdf", "bytes": 481920}
  }'
```

List events:

```bash
curl "$AF_STACK_URL/api/v1/activity?action=document.uploaded&limit=20" \
  -H "Authorization: Bearer $AF_STACK_API_KEY"
```
