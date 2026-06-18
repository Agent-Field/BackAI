// SPDX-License-Identifier: Apache-2.0

import Link from "next/link"

import { Button } from "@/components/ui/button"

import { TenantDetailShell } from "@/components/tenant-detail/tenant-detail-shell"
import { fetchTenantDetailSnapshot } from "@/lib/tenant-detail/data"

export const dynamic = "force-dynamic"

interface TenantDetailPageProps {
  params: Promise<{ id: string }>
}

export default async function TenantDetailPage({
  params,
}: TenantDetailPageProps) {
  const { id } = await params
  const snapshot = await fetchTenantDetailSnapshot(id)
  if (snapshot === null) {
    return (
      <div className="flex flex-col items-center justify-center gap-stack px-page-x py-20 text-center">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          Tenant not found
        </h1>
        <p className="max-w-md text-meta text-muted-foreground">
          Tenant <span className="font-mono">{id}</span> may have been deleted
          or never existed.
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 gap-inline text-meta"
          render={<Link href="/people/tenants" />}
        >
          Back to tenants
        </Button>
      </div>
    )
  }
  return <TenantDetailShell initialSnapshot={snapshot} />
}
