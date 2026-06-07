// SPDX-License-Identifier: Apache-2.0

// GET/POST /sign-out — kills the better-auth session and bounces to sign-in.

import { NextResponse } from "next/server"
import { headers } from "next/headers"

import { auth } from "@/lib/auth"

async function handle() {
  try {
    await auth.api.signOut({ headers: await headers() })
  } catch (err) {
    console.error("[customer-app] sign-out failed:", err)
  }
  const url = new URL(
    "/sign-in",
    process.env.BETTER_AUTH_URL ?? "http://localhost:34000",
  )
  return NextResponse.redirect(url)
}

export { handle as GET, handle as POST }
