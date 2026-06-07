// SPDX-License-Identifier: Apache-2.0

import type { NextConfig } from "next"

const nextConfig: NextConfig = {
  // /api/v1/* is proxied to the runtime via the dynamic Route Handler at
  // src/app/api/v1/[...path]/route.ts. RUNTIME_URL is resolved at request
  // time so docker-compose env vars take effect without a rebuild.
}

export default nextConfig
