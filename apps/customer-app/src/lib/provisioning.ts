// SPDX-License-Identifier: Apache-2.0

// Tenant auto-provisioning for customer signups.
//
// When a customer signs up via the customer-app's /sign-up form, we run a
// single atomic Postgres transaction that creates:
//
//   1. suite_users row (mirrors the better-auth user, keyed by email)
//   2. suite_tenants row (slug derived from email + tail suffix)
//   3. suite_memberships row (the user as "owner" of the new tenant)
//   4. suite_api_keys row (one auto-provisioned key, scope "llm:invoke")
//   5. suite_billing_customers row (free plan, stub mode)
//
// The plaintext API-key token is returned exactly once. The caller is
// expected to hand it to the user via a single-use Dialog and never
// surface it again — only the bcrypt hash lives in Postgres.
//
// The token shape mirrors the runtime's tenancy/manager.go:
//   af_<15-char base32 prefix>_<48-char base32 secret>
// Prefix is the lookup index; secret is bcrypt-verified at gateway time.

import bcrypt from "bcryptjs"
import { randomBytes } from "crypto"
import { getPool } from "@/lib/db"

const KEY_PREFIX_BYTES = 9
const KEY_SECRET_BYTES = 30
const KEY_TOKEN_PREFIX = "af_"
const BCRYPT_COST = 12

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

function emailToSlug(email: string): string {
  const local = email.split("@")[0] ?? "user"
  const cleaned = local
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
  const safeBase = cleaned.length === 0 ? "user" : cleaned
  // Ensure first char is a letter or digit, never a hyphen.
  const head = /^[a-z0-9]/.test(safeBase) ? safeBase : "u" + safeBase
  const suffix = randomBytes(3).toString("hex")
  return `${head}-${suffix}`.slice(0, 63)
}

export type ProvisionResult = {
  tenantId: string
  tenantSlug: string
  tenantName: string
  suiteUserId: string
  // Set only when the transaction also issued a fresh API key (signup
  // path). Returning customers will see these as null — they read
  // existing keys from the masked prefix on the dashboard.
  apiKeyId: string | null
  apiKeyPrefix: string | null
  // Plaintext token. Show once, never store.
  apiKeyToken: string | null
}

export type ProvisionInput = {
  email: string
  name?: string | null
  // When true, the transaction also mints a fresh af_... API key and
  // returns its plaintext form. Use this on the customer signup path.
  // When false (the better-auth user.create.after hook calls it this
  // way), only the tenant/membership/billing rows are created.
  withApiKey: boolean
}

/**
 * Atomic transaction that creates a tenant, membership, and billing-
 * customer row for a newly-signed-up customer. When withApiKey is true,
 * it also mints an `af_<prefix>_<secret>` key and returns the plaintext
 * once. Otherwise, apiKey* fields are null.
 *
 * Idempotent on email: if the customer already has a suite_users row
 * with at least one tenant, returns the existing tenant context. When
 * withApiKey is true, a *new* key is always minted (so the post-signup
 * flow always has something to display).
 */
export async function provisionTenant(
  input: ProvisionInput,
): Promise<ProvisionResult> {
  const pool = getPool()
  const client = await pool.connect()
  try {
    await client.query("begin")

    // 1. Suite user (upsert by email). suite_users.id is uuid; the
    //    better-auth "user".id is text. Link is by email.
    const upsertUser = await client.query<{ id: string }>(
      `insert into suite_users (email, name)
       values ($1, $2)
       on conflict (email) do update set name = coalesce(excluded.name, suite_users.name)
       returning id::text`,
      [input.email, input.name ?? null],
    )
    const suiteUserId = upsertUser.rows[0].id

    // 2. Tenant. If this user already owns one, reuse it. Otherwise
    //    create a fresh row with a slug derived from the email.
    let tenantId: string
    let tenantSlug: string
    let tenantName: string

    const existing = await client.query<{
      id: string
      slug: string
      name: string
    }>(
      `select t.id::text, t.slug, t.name
         from suite_memberships m
         join suite_tenants t on t.id = m.tenant_id
         where m.user_id = $1 and t.deleted_at is null
         order by m.invited_at asc
         limit 1`,
      [suiteUserId],
    )
    if (existing.rows.length > 0) {
      tenantId = existing.rows[0].id
      tenantSlug = existing.rows[0].slug
      tenantName = existing.rows[0].name
    } else {
      const slug = emailToSlug(input.email)
      tenantName = (input.name && input.name.trim()) || input.email
      const tenantRow = await client.query<{ id: string; slug: string }>(
        `insert into suite_tenants (slug, name, plan)
         values ($1, $2, 'free')
         returning id::text, slug`,
        [slug, tenantName],
      )
      tenantId = tenantRow.rows[0].id
      tenantSlug = tenantRow.rows[0].slug

      // 3. Membership — the customer owns their own tenant.
      await client.query(
        `insert into suite_memberships (tenant_id, user_id, role, accepted_at)
         values ($1, $2, 'owner', now())
         on conflict (tenant_id, user_id) do nothing`,
        [tenantId, suiteUserId],
      )
    }

    // 4. Billing customer (stub: no Stripe id, free plan).
    await client.query(
      `insert into suite_billing_customers
         (tenant_id, email, plan, subscription_status)
       values ($1, $2, 'free', 'stub')
       on conflict (tenant_id) do nothing`,
      [tenantId, input.email],
    )

    // 5. Optional fresh API key — only on the signup flow. Token shape
    //    af_<15>_<48>; only the bcrypt hash of the secret is persisted.
    let apiKeyId: string | null = null
    let apiKeyPrefix: string | null = null
    let apiKeyToken: string | null = null
    if (input.withApiKey) {
      const prefix = randomToken(KEY_PREFIX_BYTES).slice(0, 15)
      const secret = randomToken(KEY_SECRET_BYTES).slice(0, 48)
      const hashedSecret = await bcrypt.hash(secret, BCRYPT_COST)
      const keyRow = await client.query<{ id: string }>(
        `insert into suite_api_keys
           (tenant_id, prefix, hashed_secret, name, scopes, created_by)
         values ($1, $2, $3, $4, $5::text[], $6)
         returning id::text`,
        [
          tenantId,
          prefix,
          hashedSecret,
          "Default key",
          ["llm:invoke"],
          suiteUserId,
        ],
      )
      apiKeyId = keyRow.rows[0].id
      apiKeyPrefix = prefix
      apiKeyToken = `${KEY_TOKEN_PREFIX}${prefix}_${secret}`
    }

    await client.query("commit")

    return {
      tenantId,
      tenantSlug,
      tenantName,
      suiteUserId,
      apiKeyId,
      apiKeyPrefix,
      apiKeyToken,
    }
  } catch (err) {
    await client.query("rollback").catch(() => {})
    throw err
  } finally {
    client.release()
  }
}

/**
 * Looks up the tenant + key prefix for a returning customer (the user
 * already exists). Used by the dashboard page to show their key, masked.
 */
export async function lookupCustomerContext(email: string): Promise<{
  tenantId: string
  tenantName: string
  tenantSlug: string
  suiteUserId: string
  apiKeyPrefix: string | null
} | null> {
  const pool = getPool()
  const result = await pool.query<{
    suite_user_id: string
    tenant_id: string
    tenant_name: string
    tenant_slug: string
    api_key_prefix: string | null
  }>(
    `select
       su.id::text as suite_user_id,
       t.id::text as tenant_id,
       t.name as tenant_name,
       t.slug as tenant_slug,
       (
         select k.prefix
         from suite_api_keys k
         where k.tenant_id = t.id and k.revoked_at is null
         order by k.created_at asc
         limit 1
       ) as api_key_prefix
     from suite_users su
     join suite_memberships m on m.user_id = su.id
     join suite_tenants t on t.id = m.tenant_id
     where su.email = $1
       and t.deleted_at is null
     order by m.invited_at asc
     limit 1`,
    [email],
  )
  if (result.rows.length === 0) return null
  const row = result.rows[0]
  return {
    suiteUserId: row.suite_user_id,
    tenantId: row.tenant_id,
    tenantName: row.tenant_name,
    tenantSlug: row.tenant_slug,
    apiKeyPrefix: row.api_key_prefix,
  }
}
