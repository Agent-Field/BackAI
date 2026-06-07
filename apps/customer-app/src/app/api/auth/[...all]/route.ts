// SPDX-License-Identifier: Apache-2.0

// Better-auth catch-all route. Handles signup, signin, signout, sessions.

import { auth } from "@/lib/auth"
import { toNextJsHandler } from "better-auth/next-js"

export const { GET, POST } = toNextJsHandler(auth)
