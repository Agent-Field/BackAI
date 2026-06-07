// SPDX-License-Identifier: Apache-2.0

// Server-only session helpers.

import { headers } from "next/headers"
import { redirect } from "next/navigation"
import { auth } from "@/lib/auth"

export async function getServerSession() {
  return auth.api.getSession({ headers: await headers() })
}

export async function requireOperator() {
  const session = await getServerSession()
  if (!session) {
    redirect("/login")
  }
  return session
}

/**
 * Returns the total number of registered operators. The very first signup
 * becomes the operator; subsequent signups go through invites (Phase 6).
 */
export async function operatorCount(): Promise<number> {
  const { Pool } = await import("pg")
  const pool = new Pool({
    connectionString: process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL,
  })
  try {
    const result = await pool.query<{ count: string }>(
      'select count(*)::text as count from "user"',
    )
    return Number(result.rows[0]?.count ?? "0")
    // Column case is preserved by quoting in the better-auth migration; this
    // query only counts rows so column-case doesn't matter here.
  } catch {
    // Tables may not exist yet on first boot before migrations.
    return 0
  } finally {
    await pool.end()
  }
}