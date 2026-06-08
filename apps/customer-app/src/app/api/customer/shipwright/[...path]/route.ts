// SPDX-License-Identifier: Apache-2.0
//
// Proxy /api/customer/shipwright/* -> shipwright-api sidecar.
// Forwards the session-derived tenant + user as the standard
// x-af-stack-tenant-id + x-af-stack-user-id headers.

import { NextResponse } from "next/server"
import { headers } from "next/headers"

import { auth } from "@/lib/auth"
import { lookupCustomerContext } from "@/lib/provisioning"

const SHIPWRIGHT_URL =
  process.env.SHIPWRIGHT_URL ??
  process.env.NEXT_PUBLIC_SHIPWRIGHT_URL ??
  "http://shipwright-api:8000"

const TENANT_HEADER = "x-af-stack-tenant-id"
const USER_HEADER = "x-af-stack-user-id"

type Ctx = { params: Promise<{ path?: string[] }> }

async function proxy(req: Request, ctx: Ctx) {
  const session = await auth.api.getSession({ headers: await headers() })
  if (!session) {
    return NextResponse.json(
      { error: { code: "UNAUTHORIZED", message: "Sign in required" } },
      { status: 401 },
    )
  }
  const tenant = await lookupCustomerContext(session.user.email)
  if (!tenant) {
    return NextResponse.json(
      { error: { code: "NO_TENANT", message: "No tenant for this user" } },
      { status: 403 },
    )
  }

  const { path } = await ctx.params
  const segments = (path ?? []).join("/")
  const url = new URL(req.url)
  const target = `${SHIPWRIGHT_URL}/${segments}${url.search}`

  const fwd = new Headers()
  fwd.set(TENANT_HEADER, tenant.tenantId)
  fwd.set(USER_HEADER, tenant.suiteUserId)
  const contentType = req.headers.get("content-type")
  if (contentType) fwd.set("content-type", contentType)

  const body =
    req.method === "GET" || req.method === "HEAD" ? undefined : await req.text()

  let upstream: Response
  try {
    upstream = await fetch(target, {
      method: req.method,
      headers: fwd,
      body,
      cache: "no-store",
    })
  } catch (err) {
    return NextResponse.json(
      {
        error: {
          code: "UPSTREAM_UNREACHABLE",
          message: err instanceof Error ? err.message : "Connection failed",
        },
      },
      { status: 502 },
    )
  }

  const responseBody = await upstream.text()
  return new NextResponse(responseBody, {
    status: upstream.status,
    headers: {
      "content-type": upstream.headers.get("content-type") ?? "application/json",
    },
  })
}

export const GET = proxy
export const POST = proxy
export const DELETE = proxy
export const PATCH = proxy
