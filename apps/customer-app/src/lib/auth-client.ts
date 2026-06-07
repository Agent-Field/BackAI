// SPDX-License-Identifier: Apache-2.0

// Client-side auth helpers — used in React Server / Client Components.

import { createAuthClient } from "better-auth/react"

export const authClient = createAuthClient()

export const { signIn, signUp, signOut, useSession } = authClient
