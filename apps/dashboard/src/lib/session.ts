// SPDX-License-Identifier: Apache-2.0

// Server-only session helpers.

import { headers } from "next/headers"
import { redirect } from "next/navigation"
import { auth } from "@/lib/auth"

async function queryOperatorCount(): Promise<number> {
  const { Pool } = await import("pg")
  const pool = new Pool({
    connectionString: process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL,
  })
  try {
    const result = await pool.query<{ count: string }>(
      "select count(*)::text as count from suite_operators",
    )
    return Number(result.rows[0]?.count ?? "0")
  } finally {
    await pool.end()
  }
}

async function isOperator(user: { id: string; email: string }): Promise<boolean> {
  const { Pool } = await import("pg")
  const pool = new Pool({
    connectionString: process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL,
  })
  try {
    const result = await pool.query<{ ok: boolean }>(
      `select exists (
         select 1 from suite_operators
         where user_id = $1 or lower(email) = lower($2)
       ) as ok`,
      [user.id, user.email],
    )
    return result.rows[0]?.ok === true
  } catch {
    return false
  } finally {
    await pool.end()
  }
}

export async function getServerSession() {
  return auth.api.getSession({ headers: await headers() })
}

export async function requireOperator() {
  const session = await getServerSession()
  if (!session) {
    redirect("/login")
  }
  if (!(await isOperator(session.user))) {
    redirect("/login?error=operator_required")
  }
  return session
}

/**
 * Returns the total number of registered operators. The first dashboard
 * signup inserts suite_operators; customer-app users do not count.
 */
export async function operatorCount(): Promise<number> {
  try {
    return await queryOperatorCount()
  } catch {
    // Tables may not exist yet on first boot before migrations.
    return 0
  }
}
