// SPDX-License-Identifier: Apache-2.0

// Same-origin proxy from the dashboard to the runtime's REST API.
//
// Without this, client components calling `api.runs()` would either:
//   1. Hit the dashboard's own port (and 404), or
//   2. Hit the runtime cross-origin and trigger CORS preflights.
//
// This handler resolves RUNTIME_URL at request time so docker-compose's
// runtime env vars take effect without rebuilds.
// Do-not-touch zone for most forks: dashboard pages and plugins should call
// this proxy instead of reimplementing runtime forwarding.

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
    if (key.toLowerCase() === "cookie") return
    upstreamHeaders.set(key, value)
  })
  // Forward ONLY the operator session cookies. On a shared host the
  // browser also sends the customer app's better-auth.* cookies here;
  // if those reach the runtime it resolves the wrong session and admin
  // calls fail with OPERATOR_FORBIDDEN.
  const operatorCookies = req.cookies
    .getAll()
    .filter((c) => c.name.includes("backai-operator"))
    .map((c) => `${c.name}=${c.value}`)
    .join("; ")
  if (operatorCookies) {
    upstreamHeaders.set("cookie", operatorCookies)
  }

  let upstream: Response
  try {
    upstream = await fetch(target, {
      method: req.method,
      headers: upstreamHeaders,
      body,
      // Long-running agents may take a while; don't add a tight timeout
      // here — the runtime / browser handle that.
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
