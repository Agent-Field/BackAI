-- +goose Up
-- Tenant-isolate suite_memory (mirrors 00016_search.sql).
--
-- suite_memory shipped with no tenant_id column and no RLS, and its
-- scoped get/put/delete/search paths trusted a client-supplied scope_id
-- — so a caller could read or write another tenant's entries by passing
-- a crafted scope_id. Add a real tenant_id dimension + FORCE RLS so the
-- tenant boundary is enforced at the database, independent of scope_id
-- (which reverts to a pure intra-tenant sub-namespace).

alter table suite_memory
  add column if not exists tenant_id uuid not null
    default '00000000-0000-0000-0000-000000000000'
    references suite_tenants(id) on delete cascade;

-- Backfill: rows written under scope=tenant already carry the owning
-- tenant uuid in scope_id — promote it so history stays visible to its
-- owner. Everything else lands under the default tenant.
update suite_memory
   set tenant_id = scope_id::uuid
 where scope = 'tenant'
   and scope_id ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';

-- Repoint the primary key so identical (scope, scope_id, key) can coexist
-- across tenants.
alter table suite_memory drop constraint suite_memory_pkey;
alter table suite_memory add primary key (tenant_id, scope, scope_id, key);

drop index if exists suite_memory_scope_idx;
create index if not exists suite_memory_tenant_scope_idx
  on suite_memory (tenant_id, scope, scope_id);

alter table suite_memory enable row level security;
alter table suite_memory force row level security;

create policy tenant_isolation on suite_memory
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_memory;
alter table suite_memory no force row level security;
alter table suite_memory disable row level security;
drop index if exists suite_memory_tenant_scope_idx;
alter table suite_memory drop constraint suite_memory_pkey;
alter table suite_memory add primary key (scope, scope_id, key);
create index if not exists suite_memory_scope_idx on suite_memory (scope, scope_id);
alter table suite_memory drop column if exists tenant_id;
