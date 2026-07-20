// SPDX-License-Identifier: Apache-2.0

// Cross-language parity: the BackAI (TS) client surface matches
// packages/sdk-parity.json.
//
// Validation contract:
// * Every governed namespace in the manifest exists on the explicit `BackAI`
//   client.
// * For each governed namespace the set of public callable methods equals the
//   manifest's `ts` names exactly — no missing methods, no extras.
// * The set of governed namespaces on the client equals the manifest's set.
//
// Because both SDKs' parity tests compare their own client against the SAME
// manifest, a method added to only one language fails that language's test.

import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"
import { BackAI, GOVERNED_NAMESPACES } from "../src/index.js"

interface ManifestMethod {
  name: string
  py: string
  ts: string
}
interface Manifest {
  namespaces: Record<string, { methods: ManifestMethod[] }>
}

const manifest = JSON.parse(
  readFileSync(new URL("../../sdk-parity.json", import.meta.url), "utf-8"),
) as Manifest

function publicMethods(nsObj: Record<string, unknown>): Set<string> {
  return new Set(
    Object.keys(nsObj).filter((k) => !k.startsWith("_") && typeof nsObj[k] === "function"),
  )
}

describe("sdk-parity manifest", () => {
  it("is well-formed", () => {
    expect(Object.keys(manifest.namespaces).length).toBeGreaterThan(0)
    for (const [ns, spec] of Object.entries(manifest.namespaces)) {
      expect(spec.methods.length, `namespace ${ns} has no methods`).toBeGreaterThan(0)
      for (const m of spec.methods) {
        expect(m).toHaveProperty("name")
        expect(m).toHaveProperty("py")
        expect(m).toHaveProperty("ts")
      }
    }
  })
})

describe("BackAI client parity", () => {
  it("exposes exactly the manifest's governed namespaces", () => {
    expect(new Set(GOVERNED_NAMESPACES)).toEqual(new Set(Object.keys(manifest.namespaces)))
  })

  it("each namespace surface matches the manifest's ts names", () => {
    const client = new BackAI({ checkRuntimeVersion: false }) as unknown as Record<
      string,
      Record<string, unknown>
    >
    for (const [ns, spec] of Object.entries(manifest.namespaces)) {
      const nsObj = client[ns]
      expect(nsObj, `client missing namespace ${ns}`).toBeDefined()
      const expected = new Set(spec.methods.map((m) => m.ts))
      const actual = publicMethods(nsObj)
      expect(actual, `namespace ${ns} surface drift`).toEqual(expected)
    }
  })
})
