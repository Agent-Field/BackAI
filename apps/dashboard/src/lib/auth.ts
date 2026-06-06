// better-auth configuration wired against the dashboard's Postgres database.
//
// We share the same database as the runtime (see TECH-SPEC §3), and let
// better-auth manage its own `user` / `session` / `account` / `verification`
// tables. Operator data is mirrored into `suite_users` by the dashboard so
// that the runtime sees newly created operators too.
//
// Operator role: the first signup becomes the operator. Subsequent signups
// require an invitation (the dashboard gates this; better-auth is not aware).
//
// Lazy initialisation: we don't construct the auth instance until something
// actually calls it. This lets the dashboard build during CI without needing
// a live database.

import { betterAuth } from "better-auth"
import { magicLink } from "better-auth/plugins"
import { Pool } from "pg"

let _auth: ReturnType<typeof buildAuth> | undefined

function buildAuth() {
  const databaseUrl =
    process.env.DATABASE_URL ?? process.env.AF_STACK_DATABASE_URL
  if (!databaseUrl) {
    throw new Error(
      "DATABASE_URL or AF_STACK_DATABASE_URL must be set for better-auth.",
    )
  }
  const pool = new Pool({ connectionString: databaseUrl })

  return betterAuth({
    database: pool,
    secret: process.env.AF_STACK_AUTH_SECRET ?? "dev-secret-change-me",
    emailAndPassword: {
      enabled: true,
      autoSignIn: true,
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
          // Dev fallback: log the link. Notifications module wires real
          // delivery in Phase 10.
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

export const auth = new Proxy(
  {} as ReturnType<typeof buildAuth>,
  {
    get(_target, prop, receiver) {
      _auth ??= buildAuth()
      return Reflect.get(_auth, prop, receiver)
    },
  },
)

export type Auth = ReturnType<typeof buildAuth>
