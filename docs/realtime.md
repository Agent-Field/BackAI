# Realtime

AF Stack exposes a small Postgres-backed realtime bridge:

```text
Postgres NOTIFY suite_realtime, '<json payload>'
        -> runtime WebSocket /api/v1/realtime
        -> suite.realtime.subscribe(table, filter)
```

This is for application data owned by AF Stack or workload modules. It
does not stream AgentField runs, spans, traces, sessions, or memory.
Those AI-stateful streams stay in AgentField.

## Subscribe

TypeScript:

```ts
import { realtime } from "@af-stack/sdk"

const socket = realtime.subscribe("public.messages", { room_id: "room_123" })

socket.addEventListener("message", (event) => {
  const payload = JSON.parse(event.data)
  console.log(payload.record)
})
```

The SDK uses `AF_STACK_URL` and `AF_STACK_API_KEY` by default. Browser
apps using dashboard/customer-app session cookies can omit the API key.
Server runtimes without a global `WebSocket` can pass a constructor:

```ts
realtime.subscribe("public.messages", {}, { WebSocket: MyWebSocket })
```

Raw WebSocket URL:

```text
ws://localhost:8080/api/v1/realtime?table=public.messages&filter={"room_id":"room_123"}&api_key=af_...
```

## Payload Contract

Every Postgres notification payload must be JSON:

```json
{
  "table": "public.messages",
  "op": "insert",
  "tenant_id": "00000000-0000-0000-0000-000000000001",
  "record": {
    "id": "msg_123",
    "room_id": "room_123",
    "body": "hello"
  },
  "old": null,
  "at": "2026-06-07T12:00:00Z"
}
```

Rules:

- `table` must match the subscriber's `table`.
- When multi-tenancy is enabled, `tenant_id` must match the caller's
  resolved tenant.
- `filter` is a JSON object matched against `record` by exact key/value.
- The runtime forwards the original JSON payload unchanged.

## Trigger Example

Workload modules can add table-specific triggers in their migrations:

```sql
create or replace function notify_messages_realtime()
returns trigger
language plpgsql
as $$
declare
  payload jsonb;
begin
  payload = jsonb_build_object(
    'table', 'public.messages',
    'op', lower(TG_OP),
    'tenant_id', coalesce(new.tenant_id, old.tenant_id),
    'record', case when TG_OP = 'DELETE' then null else to_jsonb(new) end,
    'old', case when TG_OP = 'INSERT' then null else to_jsonb(old) end,
    'at', now()
  );

  perform pg_notify('suite_realtime', payload::text);
  return coalesce(new, old);
end;
$$;

create trigger messages_realtime_notify
after insert or update or delete on public.messages
for each row execute function notify_messages_realtime();
```

For high-volume realtime, move the same contract behind a NATS or
Centrifugo adapter later. The v1 default is Postgres because it keeps a
fresh fork to one database and one runtime.
