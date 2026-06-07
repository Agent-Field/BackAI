-- +goose Up
-- Phase 11.3 — Skills install/list/attach.
--
-- Skills are AF skillkit bundles installed into the runtime. Each one
-- declares prompt overlays + optional tool bindings; agents attach them
-- when needed. The dashboard's Build > Skills tab reads from these
-- tables; the SDK exposes app.admin.skills.{list, install, uninstall,
-- attach}.
--
-- Two tables:
--
--   suite_skills            — one row per installed skill bundle. id is
--                             the bundle identifier (e.g.
--                             "af-skill://acme/security-audit@1.2.0",
--                             a local path, or "embedded:default"). The
--                             source column records where the bundle
--                             came from so the dashboard can show
--                             provenance. tenant_id NULL means the skill
--                             is available to every tenant (used for
--                             embedded defaults + operator-installed
--                             shared bundles).
--
--   suite_skill_attachments — many-to-many between skills and agents.
--                             agent is a free-form slug (an AF agent
--                             ns.func id or a harness provider id) the
--                             skill is bound onto. Deleting a skill
--                             cascades into this table.
--
-- RLS mirrors the pattern in 00004_rls.sql but adds the "shared bundle"
-- case: rows with tenant_id IS NULL are visible to every tenant. The
-- admin escape hatch (app.bypass_rls=on) still wins so cross-tenant
-- listings work from the dashboard.

create table if not exists suite_skills (
  id            text primary key,
  name          text not null,
  version       text not null,
  source        text not null,
  description   text,
  harnesses     jsonb not null default '[]'::jsonb,
  tags          jsonb not null default '[]'::jsonb,
  tenant_id     uuid,
  installed_at  timestamptz not null default now()
);

create index if not exists suite_skills_tenant_installed_idx
  on suite_skills (tenant_id, installed_at desc);

create table if not exists suite_skill_attachments (
  skill_id     text not null references suite_skills(id) on delete cascade,
  agent        text not null,
  attached_at  timestamptz not null default now(),
  primary key (skill_id, agent)
);

create index if not exists suite_skill_attachments_agent_idx
  on suite_skill_attachments (agent);

-- RLS — per-tenant isolation with two extra carve-outs:
--   1. tenant_id IS NULL  ⇒ row is a shared bundle, visible to all
--   2. app.bypass_rls=on  ⇒ admin escape hatch for dashboard cross-tenant
--      listings (set transactionally; see 00004_rls.sql)

alter table suite_skills enable row level security;
alter table suite_skills force row level security;

create policy tenant_isolation on suite_skills
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

-- Attachments inherit tenancy from their skill — we gate visibility on
-- the join against suite_skills rather than denormalising tenant_id.
-- A plain enable+force here keeps the table from leaking via direct
-- SELECTs while the policy permits everything (the join through
-- suite_skills is what actually scopes results in practice).

alter table suite_skill_attachments enable row level security;
alter table suite_skill_attachments force row level security;

create policy attachment_passthrough on suite_skill_attachments
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or exists (
      select 1 from suite_skills s
       where s.id = suite_skill_attachments.skill_id
         and (
           s.tenant_id is null
           or s.tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
         )
    )
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or exists (
      select 1 from suite_skills s
       where s.id = suite_skill_attachments.skill_id
         and (
           s.tenant_id is null
           or s.tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
         )
    )
  );

-- +goose Down
drop policy if exists attachment_passthrough on suite_skill_attachments;
alter table suite_skill_attachments no force row level security;
alter table suite_skill_attachments disable row level security;

drop policy if exists tenant_isolation on suite_skills;
alter table suite_skills no force row level security;
alter table suite_skills disable row level security;

drop index if exists suite_skill_attachments_agent_idx;
drop table if exists suite_skill_attachments;

drop index if exists suite_skills_tenant_installed_idx;
drop table if exists suite_skills;
