// suite.admin.skills.* — AF skillkit bundles installed into the runtime.
//
// Maps to the Phase 11.3 skills endpoints in
// `apps/dashboard/src/lib/api.ts`:
//
//   GET    /api/v1/skills?tenant=&harness=
//   POST   /api/v1/skills            (body: { source, tenant_id? })
//   DELETE /api/v1/skills/{id}
//   POST   /api/v1/skills/attach     (body: { skill_id, agent })
//
// A Skill is an AF skillkit bundle — a manifest declaring prompt
// overlays, optional tool bindings, target harnesses, and tags.
// install() reads the manifest from its source (registry URL, local
// path, or "embedded:<name>") and persists a row in suite_skills;
// attach() binds the skill onto a specific agent or harness id.
//
// The endpoints surface a `SuiteError` with `code === "SKILLS_NOT_CONFIGURED"`
// when the runtime has no database wired — handle that as the empty-state
// in your UI.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const SkillSchema = z.object({
  // Bundle id ("af-skill://acme/security-audit@1.2.0", "embedded:default",
  // or "local:/abs/path").
  id: z.string(),
  name: z.string(),
  version: z.string(),
  // Where the bundle came from — registry URL, local path, or
  // "embedded:<name>" for the built-in defaults.
  source: z.string(),
  description: z.string().nullable(),
  // List of harnesses this skill targets ("claude-code", "codex", "any").
  harnesses: z.array(z.string()),
  // Tags from the skill manifest — surface for the dashboard filter.
  tags: z.array(z.string()),
  // Null = shared bundle visible to every tenant.
  tenantId: z.string().nullable(),
  installedAt: z.string(),
})
export type Skill = z.infer<typeof SkillSchema>

export const SkillListSchema = z.object({
  skills: z.array(SkillSchema),
})
export type SkillList = z.infer<typeof SkillListSchema>

export interface InstallSkillInput {
  /**
   * Source string. Accepts three formats:
   *   - "af-skill://<vendor>/<name>@<version>" — registry URL
   *   - "/abs/path" or "./relative" — local manifest dir / file
   *   - "embedded:<name>" — runtime-bundled skill (e.g. "embedded:default")
   */
  source: string
  /** Scope the install to a single tenant. Omit for a shared bundle. */
  tenantId?: string
}

export interface AttachSkillInput {
  skillId: string
  /** AF agent ns.func id or a harness provider id. */
  agent: string
}

export interface ListSkillsOptions extends HttpOptions {
  /**
   * Tenant id, the literal "shared" (matches only the cross-tenant
   * bundles), or omit to defer scoping to the server-side RLS policy.
   */
  tenant?: string
  /**
   * Filter by membership in the skill's harnesses list. "any"
   * short-circuits and returns every skill.
   */
  harness?: string
}

// ---------- public API ----------

/** List installed skills with optional tenant + harness filters. */
export async function list(
  opts: ListSkillsOptions = {},
): Promise<SkillList> {
  const { tenant, harness, ...http } = opts
  const qs = new URLSearchParams()
  if (tenant !== undefined && tenant !== "") qs.set("tenant", tenant)
  if (harness !== undefined && harness !== "") qs.set("harness", harness)
  const query = qs.toString()
  const path = query.length > 0 ? `/skills?${query}` : "/skills"
  const raw = await request<unknown>("GET", path, null, http)
  return SkillListSchema.parse(camelizeSkillList(raw))
}

/** Install a skill from a source string. */
export async function install(
  input: InstallSkillInput,
  opts: HttpOptions = {},
): Promise<Skill> {
  if (typeof input.source !== "string" || input.source.length === 0) {
    throw new Error("input.source must be a non-empty string")
  }
  const body: Record<string, unknown> = { source: input.source }
  if (input.tenantId !== undefined && input.tenantId !== "") {
    body.tenant_id = input.tenantId
  }
  const raw = await request<unknown>("POST", "/skills", body, opts)
  return SkillSchema.parse(camelizeSkill(raw))
}

/** Uninstall a skill by id. Attachments cascade-delete server-side. */
export async function uninstall(
  id: string,
  opts: HttpOptions = {},
): Promise<void> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("id must be a non-empty string")
  }
  await request<unknown>(
    "DELETE",
    `/skills/${encodeURIComponent(id)}`,
    null,
    opts,
  )
}

/**
 * Attach an installed skill onto an agent. Re-attaching the same
 * (skill_id, agent) pair is a no-op server-side.
 */
export async function attach(
  input: AttachSkillInput,
  opts: HttpOptions = {},
): Promise<void> {
  if (typeof input.skillId !== "string" || input.skillId.length === 0) {
    throw new Error("input.skillId must be a non-empty string")
  }
  if (typeof input.agent !== "string" || input.agent.length === 0) {
    throw new Error("input.agent must be a non-empty string")
  }
  const body = { skill_id: input.skillId, agent: input.agent }
  await request<unknown>("POST", "/skills/attach", body, opts)
}

// ---------- helpers ----------

/**
 * Translate the snake_case wire envelope for a single skill into the
 * camelCase shape declared by `SkillSchema`. We do this by hand (rather
 * than using `toCamel`) so the `harnesses` and `tags` arrays — which
 * may contain caller-supplied strings — pass through verbatim.
 */
function camelizeSkill(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    name: src.name,
    version: src.version,
    source: src.source,
    description: src.description ?? null,
    harnesses: Array.isArray(src.harnesses) ? src.harnesses : [],
    tags: Array.isArray(src.tags) ? src.tags : [],
    tenantId: src.tenant_id ?? src.tenantId ?? null,
    installedAt: src.installed_at ?? src.installedAt,
  }
}

function camelizeSkillList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const skillsArr = Array.isArray(src.skills)
    ? src.skills.map(camelizeSkill)
    : src.skills
  return { skills: skillsArr ?? [] }
}

/** Namespace object — the shape `suite.admin.skills` is built from. */
export const skills = {
  list,
  install,
  uninstall,
  attach,
} as const
