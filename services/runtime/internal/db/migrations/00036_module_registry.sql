-- +goose Up
-- Platform registry tracking which workload-module migrations have been
-- applied (PRD R2). This is platform-owned bookkeeping, NOT tenant-owned:
-- it records, per module, which versioned .sql files under
-- workload-modules/<id>/migrations/ have run. Like suite_tenants and the
-- goose version table, it intentionally carries no tenant_id / RLS — a
-- module's own data tables are the tenant-scoped, RLS-forced surface (the
-- runtime statically refuses to apply a module migration whose CREATE
-- TABLE lacks tenant_id + FORCE RLS + a policy).
--
-- Keyed by (module_id, version) so a module can advance its schema
-- independently and re-running boot is idempotent.
create table if not exists suite_module_migrations (
  module_id  text        not null,
  version    integer     not null,
  name       text        not null,
  applied_at timestamptz not null default now(),
  primary key (module_id, version)
);

create index if not exists suite_module_migrations_module_idx
  on suite_module_migrations (module_id);

-- +goose Down
drop table if exists suite_module_migrations;
