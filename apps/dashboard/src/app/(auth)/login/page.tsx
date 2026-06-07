// SPDX-License-Identifier: Apache-2.0

import { redirect } from "next/navigation"

import { operatorCount } from "@/lib/session"
import { LoginForm } from "./login-form"

// Server component — checks first-run state before rendering the form.
// If no operators exist yet, divert to the setup wizard so we don't ask
// people to "sign in" to an empty deployment.
export const dynamic = "force-dynamic"

export default async function LoginPage() {
  const count = await operatorCount()
  if (count === 0) {
    redirect("/setup")
  }
  return <LoginForm />
}