// SPDX-License-Identifier: Apache-2.0

import { KeyRound } from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { getPool } from "@/lib/db"
import { requireCustomerContext } from "@/lib/session"
import { ApiKeyManager } from "./api-key-manager"

export const dynamic = "force-dynamic"

type ApiKeyRow = {
  id: string
  prefix: string
  name: string | null
  scopes: string[]
  created_at: string
  last_used_at: string | null
  revoked_at: string | null
}

async function listKeys(tenantId: string): Promise<ApiKeyRow[]> {
  const pool = getPool()
  const result = await pool.query<ApiKeyRow>(
    `select id::text, prefix, name, scopes,
            created_at::text, last_used_at::text, revoked_at::text
       from suite_api_keys
      where tenant_id = $1
      order by created_at desc`,
    [tenantId],
  )
  return result.rows
}

export default async function ApiKeyPage() {
  const { ctx } = await requireCustomerContext()
  const keys = await listKeys(ctx.tenantId)

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight flex items-center gap-2">
          <KeyRound className="size-5" />
          API Keys
        </h1>
        <p className="text-muted-foreground text-sm">
          Issue keys for your tenant. Full token shown only at creation.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Your keys</CardTitle>
          <CardDescription>
            Use the prefix to identify a key. Revoke if leaked.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ApiKeyManager initialKeys={keys} tenantId={ctx.tenantId} />
        </CardContent>
      </Card>
    </div>
  )
}
