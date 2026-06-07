// SPDX-License-Identifier: Apache-2.0

// Better-auth catch-all route handler.
// Handles signup, login, OAuth callbacks, magic links, session reads, signout.

import { auth } from "@/lib/auth"
import { toNextJsHandler } from "better-auth/next-js"

export const { GET, POST } = toNextJsHandler(auth)