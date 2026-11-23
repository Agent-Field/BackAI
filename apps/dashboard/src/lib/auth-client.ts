// SPDX-License-Identifier: Apache-2.0

// Client-side auth helpers. Use these in React Server / Client Components.
//
// better-auth 1.7 folded generic OAuth into the social-provider path, so
// the client no longer needs genericOAuthClient(). SSO uses signIn.social.

import { createAuthClient } from "better-auth/react"
import { magicLinkClient } from "better-auth/client/plugins"

export const authClient = createAuthClient({
  plugins: [magicLinkClient()],
})

export const { signIn, signUp, signOut, useSession } = authClient
