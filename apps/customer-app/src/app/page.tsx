// SPDX-License-Identifier: Apache-2.0

import { redirect } from "next/navigation"

// Middleware redirects unauthenticated visitors to /sign-in and authed
// visitors to /dashboard. This page is here as a fallback for the rare
// case where middleware passes through to the route handler.
export default function Home() {
  redirect("/dashboard")
}
