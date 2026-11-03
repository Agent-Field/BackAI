// SPDX-License-Identifier: Apache-2.0

import type { NextConfig } from "next"

const nextConfig: NextConfig = {
  // /api/v1/* is proxied to the runtime via the dynamic Route Handler at
  // src/app/api/v1/[...path]/route.ts. RUNTIME_URL is resolved at request
  // time so docker-compose env vars take effect without a rebuild.
  //
  // better-auth 1.7 moved generic-OAuth callbacks from
  // /api/auth/oauth2/callback/:id to /api/auth/callback/:id. Keep the old
  // path working so already-registered IdP redirect URIs still land.
  async rewrites() {
    return [
      {
        source: "/api/auth/oauth2/callback/:id",
        destination: "/api/auth/callback/:id",
      },
    ]
  },
}

export default nextConfig
