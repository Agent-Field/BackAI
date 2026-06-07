// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from "vitest"
import { VERSION } from "../src/index.js"

describe("@af-stack/sdk", () => {
  it("exports a VERSION constant", () => {
    expect(VERSION).toBeTypeOf("string")
    expect(VERSION).not.toBe("")
  })
})