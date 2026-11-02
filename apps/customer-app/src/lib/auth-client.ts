// SPDX-License-Identifier: Apache-2.0

// Client-side auth helpers — used in React Server / Client Components.
//
// better-auth 1.7 folded generic OAuth into the social-provider path, so
// the client no longer needs genericOAuthClient(). SSO uses signIn.social.

import { createAuthClient } from "better-auth/react"

export const authClient = createAuthClient()

export const { signIn, signUp, signOut, useSession } = authClient
