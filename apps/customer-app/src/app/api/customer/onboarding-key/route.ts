// SPDX-License-Identifier: Apache-2.0

// One-shot endpoint the signup page calls right after better-auth signup.
// Mints a fresh af_<prefix>_<secret> API key for the customer's tenant
// and returns the plaintext exactly once.
//
// Requires a valid better-auth session (the signup call has already
// set the session cookie via autoSignIn). We resolve the tenant from
// the session — no body required.

import { NextResponse } from "next/server"
import { headers } from "next/headers"

import { auth } from "@/lib/auth"
import { getPool } from "@/lib/db"
import { provisionTenant, lookupCustomerContext } from "@/lib/provisioning"
import bcrypt from "bcryptjs"
import { randomBytes } from "crypto"

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

function base32Encode(bytes: Buffer): string {
  let bits = 0
  let value = 0
  let out = ""
  for (const b of bytes) {
    value = (value << 8) | b
    bits += 8
    while (bits >= 5) {
      out += BASE32_ALPHABET[(value >>> (bits - 5)) & 31]
      bits -= 5
    }
  }
  if (bits > 0) {
    out += BASE32_ALPHABET[(value << (5 - bits)) & 31]
  }
  return out
}

function randomToken(bytes: number): string {
  return base32Encode(randomBytes(bytes)).toLowerCase()
}

export async function POST() {
  const session = await auth.api.getSession({ headers: await headers() })
  if (!session) {
    return NextResponse.json(
      { error: { code: "UNAUTHORIZED", message: "Sign in required" } },
      { status: 401 },
    )
  }

  // Ensure the tenant scaffold exists. The user.create.after hook should
  // have done this, but we tolerate the case where it didn't.
  let ctx = await lookupCustomerContext(session.user.email)
  if (!ctx) {
    const provisioned = await provisionTenant({
      email: session.user.email,
      name: session.user.name,
      withApiKey: true,
    })
    return NextResponse.json({
      tenant_id: provisioned.tenantId,
      tenant_name: provisioned.tenantName,
      api_key_prefix: provisioned.apiKeyPrefix,
      api_key_token: provisioned.apiKeyToken,
    })
  }

  // Mint the customer's onboarding key. We mint inline rather than
  // calling provisionTenant again because the tenant already exists.
  const pool = getPool()
  const client = await pool.connect()
  try {
    const prefix = randomToken(9).slice(0, 15)
    const secret = randomToken(30).slice(0, 48)
    const hashedSecret = await bcrypt.hash(secret, 12)
    await client.query(
      `insert into suite_api_keys
         (tenant_id, prefix, hashed_secret, name, scopes, created_by)
       values ($1, $2, $3, $4, $5::text[], $6)`,
      [
        ctx.tenantId,
        prefix,
        hashedSecret,
        "Default key",
        ["llm:invoke"],
        ctx.suiteUserId,
      ],
    )
    return NextResponse.json({
      tenant_id: ctx.tenantId,
      tenant_name: ctx.tenantName,
      api_key_prefix: prefix,
      api_key_token: `af_${prefix}_${secret}`,
    })
  } finally {
    client.release()
  }
}
