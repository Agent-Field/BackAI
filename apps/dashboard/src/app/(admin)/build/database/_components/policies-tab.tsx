"use client"

// PoliciesTab — renders row-level security policies for the selected
// table. Each policy is rendered as a Card with its command type,
// permissive/restrictive flag, applicable roles, and the using/check
// expressions in a monospace pre block.

import { AlertCircle, ShieldCheck } from "lucide-react"

import type { DBTableDetail, RLSPolicy } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"

type PoliciesTabProps = {
  detail: DBTableDetail | null
  loading: boolean
  error: string | null
}

export function PoliciesTab({ detail, loading, error }: PoliciesTabProps) {
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertCircle />
        <AlertTitle>Couldn&apos;t load policies</AlertTitle>
        <AlertDescription>
          <span className="font-mono text-xs">{error}</span>
        </AlertDescription>
      </Alert>
    )
  }

  if (loading && !detail) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  if (!detail) return null

  if (detail.policies.length === 0) {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ShieldCheck />
          </EmptyMedia>
          <EmptyTitle>No RLS policies on this table</EmptyTitle>
          <EmptyDescription>
            {detail.rls_enabled
              ? "Row-level security is enabled but no policies have been defined yet."
              : "Row-level security is off. Without policies, all rows are visible to roles with table access."}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {detail.policies.map((p) => (
        <PolicyCard key={p.name} policy={p} />
      ))}
    </div>
  )
}

function PolicyCard({ policy }: { policy: RLSPolicy }) {
  return (
    <Card className="gap-3">
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="font-mono text-sm">{policy.name}</CardTitle>
          <Badge variant="secondary" className="uppercase">
            {policy.cmd}
          </Badge>
          <Badge variant={policy.permissive ? "outline" : "destructive"}>
            {policy.permissive ? "permissive" : "restrictive"}
          </Badge>
        </div>
        <CardDescription>
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-muted-foreground text-xs">Roles:</span>
            {policy.roles.length === 0 ? (
              <Badge variant="ghost">public</Badge>
            ) : (
              policy.roles.map((r) => (
                <Badge key={r} variant="ghost" className="font-mono">
                  {r}
                </Badge>
              ))
            )}
          </div>
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <ExpressionBlock
          label="USING"
          expression={policy.using_expression}
        />
        <ExpressionBlock
          label="WITH CHECK"
          expression={policy.with_check_expression}
        />
      </CardContent>
    </Card>
  )
}

function ExpressionBlock({
  label,
  expression,
}: {
  label: string
  expression: string | null
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
        {label}
      </span>
      {expression ? (
        <pre className="bg-muted/50 overflow-x-auto rounded-md p-3 font-mono text-xs">
          {expression}
        </pre>
      ) : (
        <span className="text-muted-foreground text-xs">—</span>
      )}
    </div>
  )
}
