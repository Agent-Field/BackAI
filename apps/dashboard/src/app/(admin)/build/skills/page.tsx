// Skills page — installed AF skillkit bundles.
//
// Server component that pre-fetches the skills list so the first paint
// is hydrated with real data (or the empty state when no DB is wired —
// the runtime returns { skills: [] } in that case). The inner
// SkillsView is a client component that owns the install / uninstall /
// attach dialog state.

import { PageHeader } from "@/components/layout/page-header"

import { api, type SkillList } from "@/lib/api"

import { SkillsView } from "./_components/skills-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  // Best-effort prefetch. If the runtime is down or the endpoint
  // returns an envelope error we render the empty state and let the
  // client re-fetch when the operator hits "Refresh".
  let initial: SkillList = { skills: [] }
  try {
    initial = await api.skills.list()
  } catch {
    initial = { skills: [] }
  }
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Skills"
        description="AF skillkit bundles installed into the runtime. Install from a registry URL, a local path, or one of the embedded defaults; attach onto an agent or harness when ready."
      />
      <SkillsView initial={initial} />
    </div>
  )
}
