// SPDX-License-Identifier: Apache-2.0

// better-auth config for the customer-facing app.
//
// SHARED INSTANCE: the customer-app and the operator dashboard read/write
// the SAME better-auth tables ("user", "session", "account", "verification")
// in the SAME Postgres database. A user that signs up here is a tenant
// CUSTOMER, distinct from a dashboard OPERATOR — but the two live side by
// side in one user table. We separate them by tenant membership: customers
// are owners of their own auto-provisioned suite_tenants row; operators
// are not.
//
// On first signup, the user.create.after hook auto-provisions the
// customer's tenant chain (suite_users, suite_tenants, suite_memberships,
// suite_api_keys, suite_billing_customers). See ./provisioning.ts.

import { betterAuth } from "better-auth"
import { Pool } from "pg"

import { provisionTenant, lookupCustomerContext } from "@/lib/provisioning"

function makeAuth() {
  const databaseUrl =
    process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL
  if (!databaseUrl) {
    return null
  }
  const pool = new Pool({ connectionString: databaseUrl })

  // Origins the customer-app accepts cross-origin auth requests from.
  // Defaults cover the docker-compose host-port pair (the customer-app
  // ships on :34000 by convention; the operator dashboard ships on
  // :33000). Operators override via BETTER_AUTH_URL or
  // BETTER_AUTH_TRUSTED_ORIGINS (comma-separated).
  const extraOrigins = (process.env.BETTER_AUTH_TRUSTED_ORIGINS ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
  const trustedOrigins = Array.from(
    new Set(
      [
        "http://localhost:3000",
        "http://localhost:33000",
        "http://localhost:34000",
        "http://127.0.0.1:3000",
        "http://127.0.0.1:33000",
        "http://127.0.0.1:34000",
        process.env.BETTER_AUTH_URL,
        ...extraOrigins,
      ].filter((s): s is string => Boolean(s)),
    ),
  )

  return betterAuth({
    database: pool,
    secret: process.env.AF_STACK_AUTH_SECRET ?? "dev-secret-change-me",
    baseURL: process.env.BETTER_AUTH_URL,
    trustedOrigins,
    emailAndPassword: {
      enabled: true,
      autoSignIn: true,
      minPasswordLength: 8,
    },
    session: {
      cookieCache: {
        enabled: true,
        maxAge: 5 * 60,
      },
    },
    databaseHooks: {
      user: {
        create: {
          // Runs AFTER better-auth has persisted the row in "user".
          // Auto-provision the tenant chain. We swallow errors here to
          // avoid bricking the signup; the post-signup page will retry
          // via lookupCustomerContext if needed.
          after: async (user) => {
            try {
              const existing = await lookupCustomerContext(user.email)
              if (existing) return
              // Hook path: create the tenant scaffold WITHOUT a fresh
              // key. The signup form follows up with a one-shot key
              // mint route that returns the plaintext.
              await provisionTenant({
                email: user.email,
                name: user.name,
                withApiKey: false,
              })
            } catch (err) {
              console.error("[customer-app] provisioning failed:", err)
            }
          },
        },
      },
    },
  })
}

const instance = makeAuth()

export const auth =
  instance ??
  (new Proxy(
    {},
    {
      get() {
        throw new Error(
          "DATABASE_URL or AF_STACK_DATABASE_URL must be set for better-auth.",
        )
      },
    },
  ) as ReturnType<typeof betterAuth>)

export type Auth = ReturnType<typeof betterAuth>
