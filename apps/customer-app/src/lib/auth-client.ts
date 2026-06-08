// SPDX-License-Identifier: Apache-2.0

// Client-side auth helpers — used in React Server / Client Components.

import { createAuthClient } from "better-auth/react"
import { genericOAuthClient } from "better-auth/client/plugins"

export const authClient = createAuthClient({
  plugins: [genericOAuthClient()],
})

export const { signIn, signUp, signOut, useSession } = authClient
