// SPDX-License-Identifier: Apache-2.0

import type { APIKey, Tenant } from "@/lib/api"

// Pure helpers for the API keys surface. Kept out of data.ts so client
// components can import them (data.ts is server-only).

// Keys minted on the zero-uuid tenant with an operator scope act as
// OPERATOR keys — usable by the af-stack CLI against the admin API.
export const OPERATOR_TENANT_ID = "00000000-0000-0000-0000-000000000000"

export function isOperatorKey(key: APIKey): boolean {
  return (
    key.tenant_id === OPERATOR_TENANT_ID &&
    key.scopes.some((s) => s === "operator" || s.startsWith("operator:"))
  )
}

export function tenantLabel(
  tenantId: string,
  tenants: Tenant[],
): string {
  if (tenantId === OPERATOR_TENANT_ID) return "operator (zero-uuid)"
  const tenant = tenants.find((t) => t.id === tenantId)
  return tenant ? tenant.name || tenant.slug : tenantId.slice(0, 8)
}

// Comma-separated scopes string → trimmed, de-duplicated array. Used by
// the issue-key dialog; empty input = no scopes.
export function parseScopes(raw: string): string[] {
  return [
    ...new Set(
      raw
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0),
    ),
  ]
}
