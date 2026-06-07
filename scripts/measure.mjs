import { chromium } from "playwright"
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: "dark" })
const page = await ctx.newPage()
await ctx.request.post("http://localhost:33000/api/auth/sign-in/email", {
  data: { email: "operator@example.com", password: "af-stack-demo-pwd" }
})
await page.goto("http://localhost:33000/build/database", { waitUntil: "networkidle" })
await page.waitForTimeout(2000)
const sizes = await page.evaluate(() => {
  const measure = (s) => {
    const el = document.querySelector(s)
    if (!el) return null
    const r = el.getBoundingClientRect()
    return { left: Math.round(r.left), right: Math.round(r.right), width: Math.round(r.width) }
  }
  return {
    body: measure("body"),
    wrapper: measure("[data-slot=sidebar-wrapper]"),
    sidebarGap: measure("[data-slot=sidebar-gap]"),
    sidebarContainer: measure("[data-slot=sidebar-container]"),
    inset: measure("[data-slot=sidebar-inset]"),
    children: measure("[data-slot=sidebar-inset] > div:nth-child(2)"),
  }
})
console.log(JSON.stringify(sizes, null, 2))
await browser.close()
