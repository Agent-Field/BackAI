// SPDX-License-Identifier: Apache-2.0

// Auth gate for the customer-app.
//
// Unauthenticated visitors trying to reach an /(app)/* route are redirected
// to /sign-in (with the original URL as ?next).

import { NextResponse, type NextRequest } from "next/server"
import { getSessionCookie } from "better-auth/cookies"

const PUBLIC_PREFIXES = ["/sign-in", "/sign-up", "/api/", "/_next", "/favicon"]

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl

  // Personal mode (AF_STACK_MODE=personal): single-user app, no customer
  // sign-in. Skip the auth gate entirely so the app opens straight off the
  // bat. Read server-side only so it stays a true runtime toggle
  // (NEXT_PUBLIC_* would bake the value at build time).
  if (process.env.AF_STACK_MODE === "personal") {
    return NextResponse.next()
  }

  if (PUBLIC_PREFIXES.some((p) => pathname.startsWith(p))) {
    return NextResponse.next()
  }

  // Root: send to the customer help center if signed in, sign-in otherwise.
  const sessionCookie = getSessionCookie(request)
  if (!sessionCookie) {
    const url = request.nextUrl.clone()
    url.pathname = "/sign-in"
    if (pathname !== "/") {
      url.searchParams.set("next", pathname)
    }
    return NextResponse.redirect(url)
  }
  if (pathname === "/") {
    const url = request.nextUrl.clone()
    url.pathname = "/dashboard"
    return NextResponse.redirect(url)
  }
  return NextResponse.next()
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|.*\\.png|.*\\.svg).*)"],
}
