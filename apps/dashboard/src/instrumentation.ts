// SPDX-License-Identifier: Apache-2.0

// Next.js instrumentation hook — runs once when the server process starts
// (never at build time). We use it to seed the default operator account on
// first boot so the operator console is usable immediately, with no signup
// wizard. See src/lib/bootstrap-operator.ts.

export async function register() {
  // Only the Node.js server runtime can touch Postgres; skip the edge runtime.
  if (process.env.NEXT_RUNTIME !== "nodejs") {
    return
  }
  const { seedDefaultOperator } = await import("@/lib/bootstrap-operator")
  await seedDefaultOperator()
}
