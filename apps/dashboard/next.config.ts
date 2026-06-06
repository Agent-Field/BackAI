import type { NextConfig } from "next"

const nextConfig: NextConfig = {
  // /api/v1/* is proxied to the runtime via the dynamic Route Handler at
  // src/app/api/v1/[...path]/route.ts (so RUNTIME_URL is resolved at
  // request time, not baked at build time).
}

export default nextConfig
