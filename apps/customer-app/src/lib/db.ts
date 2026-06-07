// SPDX-License-Identifier: Apache-2.0

// Shared Postgres pool for the customer-app. Same database as the dashboard
// (the runtime owns the schema; we just read/write the suite_* tables and
// the better-auth tables that the runtime migration creates).

import { Pool } from "pg"

declare global {
  // eslint-disable-next-line no-var
  var __customerAppPool: Pool | undefined
}

export function getPool(): Pool {
  if (!global.__customerAppPool) {
    const connectionString =
      process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL
    if (!connectionString) {
      throw new Error("DATABASE_URL or AF_STACK_DATABASE_URL must be set")
    }
    global.__customerAppPool = new Pool({ connectionString })
  }
  return global.__customerAppPool
}
