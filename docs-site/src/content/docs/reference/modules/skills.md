---
title: Module — Skills
description: AF skillkit install / list / attach. Prompt overlays + tool bindings per harness.
sidebar:
  order: 14
---

AF skillkit install/list/attach layer. A Skill is a manifest declaring prompt overlays, optional tool bindings, target harnesses, and tags.

## What it does

`skills.Installer` reads the manifest from a Skill's `Source` (registry URL, local path, `embedded:<name>`) and writes a row into `suite_skills`. List, uninstall, attach dispatch through `skills.Store`.

`Attach` binds an installed skill to a target harness (via the [Harnesses](./harnesses/) module). The dashboard reads + writes through the REST surface; the SDKs share the same wire contract.

Wire shapes (`SkillSchema`, `SkillListSchema`, `InstallSkillInputSchema`, `AttachSkillInputSchema`) mirror `apps/dashboard/src/lib/api.ts`.

When no store is wired, mutating endpoints return `503`; list returns an empty page.

## Configuration

```yaml
modules:
  enabled:
    skills: true
```

Env override:

```bash
AF_STACK_MODULE_SKILLS=true
```

## REST endpoints

Registered in `services/runtime/internal/server/skills.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/skills` | List installed skills. |
| `POST` | `/api/v1/skills` | Install a skill from `source`. |
| `DELETE` | `/api/v1/skills/{id}` | Uninstall a skill. |
| `POST` | `/api/v1/skills/attach` | Attach a skill to a target harness. |

## Database tables

Owned by migration `00013_skills.sql`:

- `suite_skills` — id, tenant, name, version, source, manifest JSONB, installed_at.
- `suite_skill_attachments` — skill_id → harness_provider binding.

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_MODULE_SKILLS` | Enable / disable. |

## Code map

- `interface.go` — wire types.
- `installer.go` — manifest fetch + parse for each source type.
- `attach.go` — bind to a harness.
- `store.go` — Postgres queries.

## Related

- Attaches to providers enumerated by [Harnesses](./harnesses/).
