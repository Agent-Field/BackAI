// SPDX-License-Identifier: Apache-2.0

// Same-origin proxy from the customer-app to the runtime's REST API.
// Mirrors apps/dashboard/src/app/api/v1/[...path]/route.ts.
//
// The customer's better-auth session cookie is forwarded; the runtime's
// tenant_resolver uses it to scope reads to the customer's tenant.
// Do-not-touch zone for most forks: product pages should call this proxy,
// not reimplement runtime auth or tenant forwarding.

import type { NextRequest } from "next/server"

const RUNTIME_DEFAULT = "http://localhost:8080"

async function proxy(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params
  const runtime = process.env.RUNTIME_URL ?? RUNTIME_DEFAULT
  const search = req.nextUrl.search
  const target = `${runtime}/api/v1/${path.join("/")}${search}`

  const body = req.method === "GET" || req.method === "HEAD" ? undefined : await req.arrayBuffer()

  const upstreamHeaders = new Headers()
  req.headers.forEach((value, key) => {
    if (key.toLowerCase() === "host") return
    upstreamHeaders.set(key, value)
  })

  let upstream: Response
  try {
    upstream = await fetch(target, {
      method: req.method,
      headers: upstreamHeaders,
      body,
      // @ts-expect-error: Next.js wants a duplex hint for streamed bodies
      duplex: "half",
    })
  } catch (err) {
    return new Response(
      JSON.stringify({
        error: {
          code: "RUNTIME_UNREACHABLE",
          message: err instanceof Error ? err.message : "runtime unreachable",
        },
      }),
      { status: 502, headers: { "content-type": "application/json" } },
    )
  }

  const respHeaders = new Headers()
  upstream.headers.forEach((value, key) => {
    if (["content-encoding", "content-length", "transfer-encoding"].includes(key.toLowerCase())) {
      return
    }
    respHeaders.set(key, value)
  })

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: respHeaders,
  })
}

export {
  proxy as GET,
  proxy as POST,
  proxy as PUT,
  proxy as PATCH,
  proxy as DELETE,
  proxy as OPTIONS,
}
