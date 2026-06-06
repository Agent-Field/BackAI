// better-auth configuration wired against the dashboard's Postgres database.
//
// We share the same database as the runtime (see TECH-SPEC §3). better-auth
// manages four tables itself: `user`, `session`, `account`, `verification`.
// They're created by the runtime migration 00002_better_auth.sql.
//
// At build time DATABASE_URL may be missing — we tolerate that with a stub
// that throws at request time, so the build can prerender static pages
// without a live database.

import { betterAuth } from "better-auth"
import { magicLink } from "better-auth/plugins"
import { Pool } from "pg"

function makeAuth() {
  const databaseUrl =
    process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL
  if (!databaseUrl) {
    return null
  }
  const pool = new Pool({ connectionString: databaseUrl })

  return betterAuth({
    database: pool,
    secret: process.env.AF_STACK_AUTH_SECRET ?? "dev-secret-change-me",
    emailAndPassword: {
      enabled: true,
      autoSignIn: true,
      minPasswordLength: 8,
    },
    socialProviders: {
      google:
        process.env.GOOGLE_CLIENT_ID && process.env.GOOGLE_CLIENT_SECRET
          ? {
              clientId: process.env.GOOGLE_CLIENT_ID,
              clientSecret: process.env.GOOGLE_CLIENT_SECRET,
            }
          : undefined,
    },
    plugins: [
      magicLink({
        sendMagicLink: async ({ email, url }) => {
          // Dev fallback: log the link. The notifications module wires
          // real delivery in Phase 10.
          console.log(`[magic-link] ${email}: ${url}`)
        },
      }),
    ],
    session: {
      cookieCache: {
        enabled: true,
        maxAge: 5 * 60,
      },
    },
  })
}

const instance = makeAuth()

// `auth` is the live better-auth instance when DATABASE_URL is set, and a
// trap that throws a useful error at request time when it isn't (so the
// build can still prerender static pages).
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
