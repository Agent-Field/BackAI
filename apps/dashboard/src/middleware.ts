// SPDX-License-Identifier: Apache-2.0

// Auth gate for the dashboard.
//
// Unauthenticated visitors trying to reach an admin route are redirected to
// /login (with the original URL as ?next so we can bounce them back after
// signing in). Auth API routes always pass through.
//
// There is no first-run setup wizard: the default operator account is seeded
// at boot (lib/bootstrap-operator.ts), so /login is the only entry point.

import { NextResponse, type NextRequest } from "next/server"
import { getSessionCookie } from "better-auth/cookies"

const PUBLIC_PREFIXES = [
  "/login",
  // Allow ALL /api/* through — better-auth has its own session handling,
  // and the rewrites in next.config.ts proxy /api/v1/* to the runtime
  // which has its own auth boundary (Phase 6).
  "/api/",
  "/_next",
  "/favicon",
]

export function middleware(request: NextRequest) {
  const { pathname, search, searchParams } = request.nextUrl

  // Static assets and auth API always pass.
  if (PUBLIC_PREFIXES.some((p) => pathname.startsWith(p))) {
    return NextResponse.next()
  }

  if (process.env.NODE_ENV === "development" && searchParams.get("preview") === "1") {
    const response = NextResponse.next()
    response.cookies.set("better-auth.session_token", "dev-preview-session", {
      httpOnly: true,
      sameSite: "lax",
      path: "/",
    })
    return response
  }

  const sessionCookie = getSessionCookie(request)
  if (!sessionCookie) {
    const url = request.nextUrl.clone()
    url.pathname = "/login"
    url.searchParams.set("next", `${pathname}${search}`)
    return NextResponse.redirect(url)
  }
  return NextResponse.next()
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|.*\\.png|.*\\.svg).*)"],
}
