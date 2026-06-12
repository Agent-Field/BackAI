import { chromium } from "playwright"
const b = await chromium.launch()
const ctx = await b.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, colorScheme: "dark" })
const p = await ctx.newPage()
await ctx.request.post("http://localhost:33000/api/auth/sign-in/email", {
  data: { email: "operator@example.com", password: "af-stack-demo-pwd" }
})
await p.goto("http://localhost:33000/customers/audit", { waitUntil: "networkidle" })
await p.waitForTimeout(1500)
await p.screenshot({ path: "/Users/santoshkumarradha/Documents/agentfield/code/platform/af-stack/docs/assets/dashboard-screenshots/audit-after.png", fullPage: false })
// Click the From button to open the datepicker
const fromBtn = p.getByRole("button", { name: /^From$/i }).first()
try { await fromBtn.click({ timeout: 3000 }) } catch (e) { console.log("no from button", e.message) }
await p.waitForTimeout(700)
await p.screenshot({ path: "/Users/santoshkumarradha/Documents/agentfield/code/platform/af-stack/docs/assets/dashboard-screenshots/audit-datepicker.png", fullPage: false })
await b.close()
