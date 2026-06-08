// SPDX-License-Identifier: Apache-2.0

// suite.oauth.* — OAuth-on-behalf-of-user operations.
//
// Lets backend agents act on behalf of the user in third-party APIs.
// The first shipped providers are Google and GitHub. The runtime owns
// the OAuth dance + vault; this SDK is the client surface.
//
// - authorizeUrl(provider, opts) -> string  : consent URL to redirect to
// - connected()                  -> ConnectionList
// - token(provider, opts)        -> string   : fresh access token (internal-only)
// - disconnect(provider, opts)   -> boolean : revoke + soft-delete
//
// `token()` sends an `X-AF-Stack-Internal: 1` header. The header is NOT
// in the runtime's CORS allow-list, so browsers cannot reach this
// endpoint — only server-side callers (workload modules, agents, this
// SDK from a Node process) can.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

export const ConnectionSchema = z.object({
  provider: z.string(),
  scopes: z.array(z.string()).default([]),
  connected_at: z.string(),
  expires_at: z.string().nullable().optional(),
})
export type Connection = z.infer<typeof ConnectionSchema>

export const ConnectionListSchema = z.object({
  connections: z.array(ConnectionSchema).default([]),
})
export type ConnectionList = z.infer<typeof ConnectionListSchema>

export const AccessTokenSchema = z.object({
  provider: z.string(),
  user_id: z.string(),
  access_token: z.string(),
  scopes: z.array(z.string()).default([]),
  expires_at: z.string().nullable().optional(),
})
export type AccessToken = z.infer<typeof AccessTokenSchema>

const AuthorizeResponseSchema = z.object({
  provider: z.string(),
  authorization_url: z.string(),
  redirect_uri: z.string(),
  scopes: z.array(z.string()).default([]),
})

export interface AuthorizeUrlOptions extends HttpOptions {
  scopes?: string[]
  returnTo?: string
}

export interface TokenOptions extends HttpOptions {
  userId?: string
}

export interface DisconnectOptions extends HttpOptions {
  userId?: string
}

/**
 * Return the provider's consent URL the user should be redirected to.
 *
 * The runtime signs an HMAC state nonce that round-trips through the
 * provider's consent page; on callback it's verified to defeat CSRF.
 * `returnTo` is validated against an allowed-origin list.
 */
export async function authorizeUrl(
  provider: string,
  opts: AuthorizeUrlOptions = {},
): Promise<string> {
  if (typeof provider !== "string" || provider.length === 0) {
    throw new Error("provider must be a non-empty string")
  }
  const { scopes, returnTo, ...http } = opts
  const body: Record<string, unknown> = {}
  if (scopes !== undefined) body.scopes = scopes
  if (returnTo !== undefined) body.return_to = returnTo
  const raw = await request<unknown>(
    "POST",
    `/oauth/${encodeURIComponent(provider)}/authorize`,
    body,
    http,
  )
  return AuthorizeResponseSchema.parse(raw).authorization_url
}

/** List active OAuth connections for the current user. */
export async function connected(opts: HttpOptions = {}): Promise<ConnectionList> {
  const raw = await request<unknown>("GET", "/oauth/connections", null, opts)
  return ConnectionListSchema.parse(raw)
}

/**
 * Return a fresh access token for `provider`. Auto-refreshes if the
 * stored token is within 60s of expiry.
 *
 * Internal-only: the runtime requires `X-AF-Stack-Internal: 1`. Browsers
 * cannot send this header cross-origin thanks to the CORS allow-list,
 * so only server-side callers reach it.
 */
export async function token(
  provider: string,
  opts: TokenOptions = {},
): Promise<string> {
  if (typeof provider !== "string" || provider.length === 0) {
    throw new Error("provider must be a non-empty string")
  }
  const { userId, headers, ...rest } = opts
  const merged: HttpOptions = {
    ...rest,
    headers: {
      ...(headers ?? {}),
      "X-AF-Stack-Internal": "1",
    },
  }
  const body: Record<string, unknown> = { provider }
  if (userId !== undefined) body.user_id = userId
  const raw = await request<unknown>("POST", "/oauth/token", body, merged)
  return AccessTokenSchema.parse(raw).access_token
}

/**
 * Disconnect the user's OAuth grant for `provider`. Best-effort upstream
 * revoke + local soft-delete. Returns true on success.
 */
export async function disconnect(
  provider: string,
  opts: DisconnectOptions = {},
): Promise<boolean> {
  if (typeof provider !== "string" || provider.length === 0) {
    throw new Error("provider must be a non-empty string")
  }
  const { userId, ...http } = opts
  let path = `/oauth/${encodeURIComponent(provider)}`
  if (userId !== undefined && userId !== "") {
    path += `?user_id=${encodeURIComponent(userId)}`
  }
  const raw = await request<{ disconnected?: boolean } | null>(
    "DELETE",
    path,
    null,
    http,
  )
  if (raw === null || raw === undefined) return true
  return Boolean(raw.disconnected ?? true)
}

/** Namespace object — `suite.oauth.*`. */
export const oauth = {
  authorizeUrl,
  connected,
  token,
  disconnect,
} as const
