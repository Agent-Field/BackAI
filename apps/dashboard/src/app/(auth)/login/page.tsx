// SPDX-License-Identifier: Apache-2.0

import { getDashboardSSOConfig } from "@/lib/sso"
import { LoginForm } from "./login-form"

// Server component. A default operator account is seeded at boot
// (lib/bootstrap-operator.ts) and its credentials are documented in the
// README quickstart, so there is no first-run setup wizard to divert to —
// we always render the sign-in form.
export const dynamic = "force-dynamic"

export default async function LoginPage() {
  const sso = getDashboardSSOConfig()
  return <LoginForm sso={{ enabled: sso.enabled, label: sso.label }} />
}
