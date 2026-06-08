// SPDX-License-Identifier: Apache-2.0

export const SSO_PROVIDER_ID = "enterprise-sso"

export type CustomerSSOConfig = {
  enabled: boolean
  label: string
  providerId: typeof SSO_PROVIDER_ID
  issuer?: string
  discoveryUrl?: string
  scopes: string[]
  missingEnv: string[]
}

function splitScopes(value: string | undefined): string[] {
  return (value ?? "openid email profile")
    .split(/[,\s]+/)
    .map((scope) => scope.trim())
    .filter(Boolean)
}

export function getCustomerSSOConfig(env: NodeJS.ProcessEnv = process.env): CustomerSSOConfig {
  const issuer = env.AF_STACK_SSO_ISSUER?.replace(/\/$/, "")
  const discoveryUrl =
    env.AF_STACK_SSO_DISCOVERY_URL ??
    (issuer ? `${issuer}/.well-known/openid-configuration` : undefined)

  const required = {
    AF_STACK_SSO_CLIENT_ID: env.AF_STACK_SSO_CLIENT_ID,
    AF_STACK_SSO_CLIENT_SECRET: env.AF_STACK_SSO_CLIENT_SECRET,
    AF_STACK_SSO_ISSUER: issuer,
  }
  const missingEnv = Object.entries(required)
    .filter(([, value]) => !value)
    .map(([key]) => key)

  return {
    enabled: missingEnv.length === 0,
    label: env.AF_STACK_SSO_LABEL || "Enterprise SSO",
    providerId: SSO_PROVIDER_ID,
    issuer,
    discoveryUrl,
    scopes: splitScopes(env.AF_STACK_SSO_SCOPES),
    missingEnv,
  }
}
