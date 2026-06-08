// SPDX-License-Identifier: Apache-2.0

import { getCustomerSSOConfig } from "@/lib/sso"
import { SignInForm } from "./sign-in-form"

export const dynamic = "force-dynamic"

export default function SignInPage() {
  const sso = getCustomerSSOConfig()
  return <SignInForm sso={{ enabled: sso.enabled, label: sso.label }} />
}
