// SPDX-License-Identifier: Apache-2.0

// Auth gate for the dashboard.
//
// Unauthenticated visitors trying to reach an admin route are redirected to
// /login (with the original URL as ?next so we can bounce them back after
// signing in). Auth API routes always pass through.
//
// First-run setup: if no operator has signed up yet, every route (except
// /setup) redirects to /setup. This is handled by the server-side check
// in `app/(admin)/layout.tsx` rather than here, because Edge middleware
// can't query Postgres cheaply.

import { NextResponse, type NextRequest } from "next/server"
import { getSessionCookie } from "better-auth/cookies"

const PUBLIC_PREFIXES = [
  "/login",
  "/signup",
  "/setup",
  // Allow ALL /api/* through — better-auth has its own session handling,
  // and the rewrites in next.config.ts proxy /api/v1/* to the runtime
  // which has its own auth boundary (Phase 6).
  "/api/",
  "/_next",
  "/favicon",
]

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl

  // Static assets and auth API always pass.
  if (PUBLIC_PREFIXES.some((p) => pathname.startsWith(p))) {
    return NextResponse.next()
  }

  const sessionCookie = getSessionCookie(request)
  if (!sessionCookie) {
    const url = request.nextUrl.clone()
    url.pathname = "/login"
    url.searchParams.set("next", pathname)
    return NextResponse.redirect(url)
  }
  return NextResponse.next()
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|.*\\.png|.*\\.svg).*)"],
}