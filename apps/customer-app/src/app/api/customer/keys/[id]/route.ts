// SPDX-License-Identifier: Apache-2.0

// DELETE /api/customer/keys/<id> — revokes the customer's own API key.
//
// Authorisation: the session must belong to a customer who is a member
// of the tenant that owns the key. We never let one tenant revoke
// another's keys.

import { NextResponse } from "next/server"
import { headers } from "next/headers"

import { auth } from "@/lib/auth"
import { getPool } from "@/lib/db"
import { lookupCustomerContext } from "@/lib/provisioning"

export async function DELETE(
  _req: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params

  const session = await auth.api.getSession({ headers: await headers() })
  if (!session) {
    return NextResponse.json(
      { error: { code: "UNAUTHORIZED", message: "Sign in required" } },
      { status: 401 },
    )
  }
  const ctx = await lookupCustomerContext(session.user.email)
  if (!ctx) {
    return NextResponse.json(
      { error: { code: "NO_TENANT", message: "No tenant for this user" } },
      { status: 403 },
    )
  }

  const pool = getPool()
  const result = await pool.query<{ id: string }>(
    `update suite_api_keys
       set revoked_at = now()
     where id = $1::uuid
       and tenant_id = $2::uuid
       and revoked_at is null
     returning id::text`,
    [id, ctx.tenantId],
  )
  if (result.rows.length === 0) {
    return NextResponse.json(
      { error: { code: "NOT_FOUND", message: "Key not found" } },
      { status: 404 },
    )
  }
  return NextResponse.json({ revoked: true, id: result.rows[0].id })
}
