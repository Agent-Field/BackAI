// SPDX-License-Identifier: Apache-2.0

// GET/POST /sign-out — kills the operator's better-auth session and
// bounces to /login. Mirrors the customer app's sign-out route.

import { NextResponse } from "next/server"
import { headers } from "next/headers"

import { auth } from "@/lib/auth"

async function handle() {
  try {
    await auth.api.signOut({ headers: await headers() })
  } catch (err) {
    console.error("[dashboard] sign-out failed:", err)
  }
  const url = new URL(
    "/login",
    process.env.BETTER_AUTH_URL ?? "http://localhost:33000",
  )
  const res = NextResponse.redirect(url)
  // auth.api.signOut revokes the DB session, but without better-auth's
  // nextCookies plugin its clearing Set-Cookie headers never reach this
  // response — the browser keeps the (now dead) session_token plus the
  // signed session_data cache, which reads as "logged in" for up to
  // 5 minutes. Clear both explicitly.
  for (const name of [
    "backai-operator.session_token",
    "backai-operator.session_data",
    "__Secure-backai-operator.session_token",
    "__Secure-backai-operator.session_data",
  ]) {
    res.cookies.set(name, "", { maxAge: 0, path: "/" })
  }
  return res
}

export { handle as GET, handle as POST }
