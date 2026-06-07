-- +goose Up
-- Phase 11.1 — MCP (Model Context Protocol) server registry.
--
-- An MCP server is an external process (stdio) or HTTP+SSE endpoint that
-- exposes a tool catalogue to suite agents. The runtime hosts long-lived
-- connections (one per row here) and proxies tool calls via a shared
-- Pool. Tools are discovered on connect and refreshed every 5 minutes.
--
-- Row lifecycle (status column):
--
--   connecting -> the Pool is trying to open the JSON-RPC connection.
--   ready      -> handshake + initial tools/list succeeded.
--   errored    -> last connect or call failed; last_error has the
--                 truncated message (see runtime mcp/pool.go).
--   disabled   -> operator flipped is_enabled=false; the pool closes the
--                 underlying adapter but keeps the row for re-enable.
--
-- Per-tenant scoping:
--   tenant_id NULL means "available to every tenant" — the canonical
--   path for shared infra (e.g. GitHub MCP). A UUID scopes the row to
--   one tenant. The RLS policy below grants SELECTs on either case so
--   tenant operators see the union of {their tenant, NULL} without any
--   extra glue in the handlers.
--
-- The wire schema (MCPServerSchema in apps/dashboard/src/lib/api.ts)
-- mirrors this table 1:1. command is a JSON array (NOT a text column)
-- so the dashboard's command-editor round-trips structured data without
-- any quoting / shell-escaping ambiguity. env is jsonb so values prefixed
-- "secret:<key>" stay opaque strings until the runtime resolves them via
-- the secrets vault before spawning the child process.

create table if not exists suite_mcp_servers (
  -- Stable slug used in tool ids and URLs. Lowercased letters, digits,
  -- dashes (validated at the API boundary). Primary key so a re-install
  -- via the dashboard's "Add MCP server" form upserts cleanly.
  name text primary key,

  -- "stdio": spawn a child process; the runtime talks JSON-RPC framed
  --          with `Content-Length: N\r\n\r\n<json>` over stdin/stdout.
  -- "sse"  : long-lived HTTP+SSE; client sends JSON-RPC requests via
  --          POST to <url>/messages and receives responses on <url>/sse.
  transport text not null check (transport in ('stdio','sse')),

  -- stdio: argv-style command, e.g. ["uvx","mcp-server-github"].
  --        Empty array is rejected at the API boundary.
  -- sse  : ignored (defaulted to []).
  command jsonb not null default '[]'::jsonb,

  -- sse: base URL, e.g. "https://mcp.acme.com" (the runtime appends
  --      /messages and /sse).
  -- stdio: NULL.
  url text,

  -- Process / SSE environment. Values prefixed "secret:<key>" are
  -- resolved against the secrets vault (per tenant_id) at connect time.
  -- Stored opaquely so the dashboard's secret-ref ergonomics work
  -- without leaking material into this table.
  env jsonb not null default '{}'::jsonb,

  -- Per-tenant scoping. NULL = visible to every tenant (the canonical
  -- "shared infra" path). A UUID scopes to that tenant only.
  tenant_id uuid references suite_tenants(id) on delete set null,

  -- Human description shown in the dashboard list.
  description text not null default '',

  -- Operator toggle. Pool.Reconcile() closes the adapter and flips
  -- status to 'disabled' when this goes false; flips back to
  -- 'connecting' on re-enable.
  is_enabled boolean not null default true,

  -- Wire status column. Drives the dashboard's coloured chip.
  status text not null default 'connecting'
    check (status in ('connecting','ready','errored','disabled')),

  -- Most-recent error message when status='errored'. Truncated to ~500
  -- chars so the dashboard isn't drowned in stack traces.
  last_error text,

  -- Last successful tools/list count. Refreshed on connect + every 5
  -- minutes. Cheap counter the dashboard can show without re-fetching
  -- the catalogue.
  tools_count int not null default 0,

  installed_at timestamptz not null default now(),
  last_connected_at timestamptz
);

-- The pool's Reconcile loop scans by (is_enabled, status) to pick up
-- newly-enabled rows and to surface errored servers in the dashboard's
-- "needs attention" filter.
create index if not exists suite_mcp_servers_enabled_status_idx
  on suite_mcp_servers (is_enabled, status);

-- Row Level Security — same shape as suite_secrets (migration 00004)
-- and suite_webhook_endpoints (migration 00010). The NULL tenant_id
-- branch makes shared rows visible to every tenant: an operator on
-- tenant T sees (their rows) UNION (every NULL row). Admin operations
-- bypass with app.bypass_rls='on'.
alter table suite_mcp_servers enable row level security;
alter table suite_mcp_servers force row level security;

create policy tenant_isolation on suite_mcp_servers
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_mcp_servers;
alter table suite_mcp_servers no force row level security;
alter table suite_mcp_servers disable row level security;
drop index if exists suite_mcp_servers_enabled_status_idx;
drop table if exists suite_mcp_servers;
