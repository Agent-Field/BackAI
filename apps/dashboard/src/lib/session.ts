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

/**
 * Personal mode (AF_STACK_MODE=personal): single-user app, no operator
 * login. Read server-side only so it stays a true runtime toggle.
 */
export function isPersonalMode(): boolean {
  return process.env.AF_STACK_MODE === "personal"
}

// Synthetic operator session used in personal mode, where there is no
// login and therefore no real better-auth session. No caller reads its
// fields today (the layout only uses it as a redirect gate), but we shape
// it like a real session so any future consumer degrades gracefully.
function personalOperatorSession() {
  const now = new Date()
  return {
    session: {
      id: "personal",
      token: "personal",
      userId: "personal",
      expiresAt: new Date(now.getTime() + 24 * 60 * 60 * 1000),
      createdAt: now,
      updatedAt: now,
    },
    user: {
      id: "personal",
      email: "you@localhost",
      name: "Personal",
      emailVerified: true,
      createdAt: now,
      updatedAt: now,
    },
  }
}

export async function getServerSession() {
  if (isPersonalMode()) {
    return personalOperatorSession() as Awaited<ReturnType<typeof auth.api.getSession>>
  }
  return auth.api.getSession({ headers: await headers() })
}

export async function requireOperator() {
  // Personal mode has no login gate — return a synthetic operator so
  // operator-gated server components render without a real session.
  if (isPersonalMode()) {
    return personalOperatorSession() as NonNullable<Awaited<ReturnType<typeof auth.api.getSession>>>
  }
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
