// SPDX-License-Identifier: Apache-2.0

// First-run bootstrap: seed a default operator account so a fresh
// deployment is usable the moment `docker compose up` finishes — no signup
// wizard, no chicken-and-egg.
//
// Why this exists
// ----------------
// Operator status lives in `suite_operators`, a table separate from
// better-auth's `user` / `account`. Previously the *first* dashboard signup
// populated it as a side effect, inside one all-or-nothing transaction that
// also wrote `suite_users` + `suite_memberships`. Any partial failure rolled
// the whole thing back, so the deployment was left with zero operators — and
// every route (/login, /, …) bounced to /setup forever. Seeding a known
// account removes that failure mode entirely.
//
// Credentials come from env and are documented in the README quickstart,
// AGENTS.md and .env.example:
//   AF_STACK_DEFAULT_OPERATOR_EMAIL    (default: operator@af-stack.local)
//   AF_STACK_DEFAULT_OPERATOR_PASSWORD (default: changeme123)
//   AF_STACK_DEFAULT_OPERATOR_NAME     (default: Default Operator)
//
// The seed is gated on an EMPTY `suite_operators` table, so it runs exactly
// once. It never clobbers a changed password or a hand-rolled operator set:
// once any operator exists, this is a no-op on every subsequent boot.

import { randomUUID } from "node:crypto"
import { hashPassword } from "better-auth/crypto"
import { Pool, type PoolClient } from "pg"

const DEFAULT_EMAIL =
  process.env.AF_STACK_DEFAULT_OPERATOR_EMAIL?.trim() || "operator@af-stack.local"
const DEFAULT_PASSWORD = process.env.AF_STACK_DEFAULT_OPERATOR_PASSWORD?.trim() || "changeme123"
const DEFAULT_NAME = process.env.AF_STACK_DEFAULT_OPERATOR_NAME?.trim() || "Default Operator"

// Migrations run in the runtime container, which the dashboard only
// `depends_on: service_started` — not healthy. So the auth/operator tables
// may not exist for the first few seconds after boot. Retry until they do.
async function withRetry(fn: () => Promise<void>, attempts = 20, delayMs = 3000): Promise<void> {
  let lastErr: unknown
  for (let i = 0; i < attempts; i++) {
    try {
      await fn()
      return
    } catch (e) {
      lastErr = e
      await new Promise((resolve) => setTimeout(resolve, delayMs))
    }
  }
  throw lastErr
}

async function ensureDefaultOperatorMembership(client: PoolClient): Promise<void> {
  await client.query(
    `insert into suite_memberships (tenant_id, user_id, role, accepted_at)
     select '00000000-0000-0000-0000-000000000000'::uuid, u.id, 'owner', now()
     from suite_users u
     where lower(u.email) = lower($1)
     on conflict (tenant_id, user_id) do update
       set accepted_at = coalesce(suite_memberships.accepted_at, excluded.accepted_at)`,
    [DEFAULT_EMAIL],
  )
}

export async function seedDefaultOperator(): Promise<void> {
  if ((process.env.AF_STACK_DEFAULT_OPERATOR_DISABLED ?? "").trim().toLowerCase() === "true") {
    return
  }
  const connectionString = process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL
  if (!connectionString) {
    console.warn("[operator-seed] no DATABASE_URL set; skipping default operator seed")
    return
  }

  const pool = new Pool({ connectionString, max: 2 })
  try {
    await withRetry(async () => {
      const client = await pool.connect()
      try {
        // Probe the tables migrations are responsible for. A missing table
        // throws here and the whole attempt is retried.
        await client.query('select 1 from "user" limit 1')
        await client.query("select 1 from suite_operators limit 1")

        await client.query("begin")
        // Serialize concurrent dashboard replicas racing the same seed.
        await client.query(
          "select pg_advisory_xact_lock(hashtext('af_stack_default_operator_seed'))",
        )

        const { rows } = await client.query<{ count: string }>(
          "select count(*)::text as count from suite_operators",
        )
        if (Number(rows[0]?.count ?? "0") > 0) {
          // Already bootstrapped — never re-seed or clobber a changed
          // password, but repair the default tenant membership if an older
          // seed created only the operator + suite user rows.
          await ensureDefaultOperatorMembership(client)
          await client.query("commit")
          return
        }

        const passwordHash = await hashPassword(DEFAULT_PASSWORD)

        // 1. better-auth user row (idempotent on email).
        await client.query(
          `insert into "user" ("id", "name", "email", "emailVerified", "createdAt", "updatedAt")
           values ($1, $2, $3, true, now(), now())
           on conflict ("email") do nothing`,
          [randomUUID(), DEFAULT_NAME, DEFAULT_EMAIL],
        )
        const userRow = await client.query<{ id: string }>(
          'select "id" from "user" where lower("email") = lower($1)',
          [DEFAULT_EMAIL],
        )
        const userId = userRow.rows[0]?.id
        if (!userId) {
          throw new Error("[operator-seed] user row missing after insert")
        }

        // 2. Credential account holding the password hash. better-auth keys
        //    email/password accounts as providerId='credential',
        //    accountId=<user id>. Skip if the user already has one.
        await client.query(
          `insert into "account" ("id", "accountId", "providerId", "userId", "password", "createdAt", "updatedAt")
           select $1, $2, 'credential', $3, $4, now(), now()
           where not exists (
             select 1 from "account" where "userId" = $3 and "providerId" = 'credential'
           )`,
          [randomUUID(), userId, userId, passwordHash],
        )

        // 3. Operator allow-list entry — what actually gates the console.
        await client.query(
          `insert into suite_operators ("user_id", "email", "name", "role")
           values ($1, $2, $3, 'owner')
           on conflict ("email") do update set "user_id" = excluded."user_id"`,
          [userId, DEFAULT_EMAIL, DEFAULT_NAME],
        )

        // 4. Mirror into suite_users so the runtime's tenant_resolver can
        //    join on email (otherwise protected /api/v1/* return 401).
        await client.query(
          `insert into suite_users ("email", "name") values ($1, $2)
           on conflict ("email") do nothing`,
          [DEFAULT_EMAIL, DEFAULT_NAME],
        )
        await ensureDefaultOperatorMembership(client)

        await client.query("commit")
        console.log(
          `[operator-seed] seeded default operator ${DEFAULT_EMAIL} — change this password after first login`,
        )
      } catch (e) {
        await client.query("rollback").catch(() => {})
        throw e
      } finally {
        client.release()
      }
    })
  } catch (e) {
    // Never crash the dashboard process over the seed — log loudly and let
    // it boot. A restart re-attempts.
    console.error("[operator-seed] failed to seed default operator:", e)
  } finally {
    await pool.end().catch(() => {})
  }
}
