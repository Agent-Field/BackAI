import { describe, it, expect } from "vitest"
import { ctx, withCtx } from "../src/ctx.js"

describe("ctx", () => {
  it("defaults to nulls outside withCtx", () => {
    expect(ctx.tenantId).toBeNull()
    expect(ctx.userId).toBeNull()
    expect(ctx.apiKeyId).toBeNull()
    expect(ctx.requestId).toBeNull()
  })

  it("exposes values inside withCtx", async () => {
    await withCtx(
      { tenantId: "t-1", userId: "u-1", apiKeyId: "k-1", requestId: "r-1" },
      async () => {
        expect(ctx.tenantId).toBe("t-1")
        expect(ctx.userId).toBe("u-1")
        expect(ctx.apiKeyId).toBe("k-1")
        expect(ctx.requestId).toBe("r-1")
      },
    )
  })

  it("restores frame after withCtx returns", async () => {
    await withCtx({ tenantId: "t-1" }, async () => {
      expect(ctx.tenantId).toBe("t-1")
    })
    expect(ctx.tenantId).toBeNull()
  })

  it("propagates across awaits and setTimeout", async () => {
    await withCtx({ tenantId: "t-async", requestId: "r-async" }, async () => {
      await new Promise((r) => setTimeout(r, 5))
      expect(ctx.tenantId).toBe("t-async")
      expect(ctx.requestId).toBe("r-async")
    })
  })

  it("isolates concurrent frames", async () => {
    const results: Array<string | null> = []
    await Promise.all([
      withCtx({ tenantId: "t-a" }, async () => {
        await new Promise((r) => setTimeout(r, 10))
        results.push(ctx.tenantId)
      }),
      withCtx({ tenantId: "t-b" }, async () => {
        await new Promise((r) => setTimeout(r, 5))
        results.push(ctx.tenantId)
      }),
    ])
    expect(results.sort()).toEqual(["t-a", "t-b"])
  })

  it("nested withCtx merges and restores", async () => {
    await withCtx({ tenantId: "outer", userId: "u-outer" }, async () => {
      await withCtx({ tenantId: "inner" }, async () => {
        expect(ctx.tenantId).toBe("inner")
        expect(ctx.userId).toBe("u-outer")
      })
      expect(ctx.tenantId).toBe("outer")
    })
  })

  it("ignores writes via proxy", () => {
    const mut = ctx as unknown as { tenantId: string }
    mut.tenantId = "nope"
    expect(ctx.tenantId).toBeNull()
  })
})
