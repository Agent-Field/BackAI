// SPDX-License-Identifier: Apache-2.0

// Client-side auth helpers. Use these in React Server / Client Components.

import { createAuthClient } from "better-auth/react"
import { genericOAuthClient, magicLinkClient } from "better-auth/client/plugins"

export const authClient = createAuthClient({
  plugins: [genericOAuthClient(), magicLinkClient()],
})

export const { signIn, signUp, signOut, useSession } = authClient
