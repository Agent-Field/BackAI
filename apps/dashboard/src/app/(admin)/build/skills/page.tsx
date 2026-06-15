// SPDX-License-Identifier: Apache-2.0

// Skills page — Build → Skills. Catalog of installed AF skillkit
// bundles, backed by GET /api/v1/skills. Server component: fetched
// once per render (force-dynamic + no-store via lib/api.ts). When the
// runtime isn't reachable we render a clean empty-state shell rather
// than crashing.

import { Boxes, Cpu, Tags } from "lucide-react"

import { api, ApiError, type SkillList } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

async function loadSkills(): Promise<{
  data: SkillList
  error: string | null
}> {
  try {
    const data = await api.skills.list()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load skills"
    return { data: { skills: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadSkills()
  const skills = data.skills

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Skills"
        description="Installed AF skillkit bundles. Backed by /api/v1/skills."
      />

      {error && skills.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Skills aren&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : skills.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <Boxes className="size-3.5" />
          No skills installed.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Skill
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Source
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Harnesses
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Tags
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {skills.map((skill) => (
                <TableRow key={skill.id} className="hover:bg-muted/30">
                  <TableCell>
                    <span className="text-sm font-medium">{skill.name}</span>
                    <span className="text-muted-foreground ml-2 text-xs tabular-nums">
                      {skill.version}
                    </span>
                    {skill.description ? (
                      <p className="text-muted-foreground mt-0.5 text-xs">
                        {skill.description}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {skill.source}
                  </TableCell>
                  <TableCell>
                    {skill.harnesses.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1">
                        <Cpu className="text-muted-foreground size-3.5" />
                        {skill.harnesses.map((h) => (
                          <Badge key={h} variant="secondary" className="text-xs">
                            {h}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground text-xs">—</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {skill.tags.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1">
                        <Tags className="text-muted-foreground size-3.5" />
                        {skill.tags.map((t) => (
                          <Badge key={t} variant="outline" className="text-xs">
                            {t}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground text-xs">—</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
