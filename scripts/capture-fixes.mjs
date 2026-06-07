import { chromium } from "playwright"
import { resolve } from "node:path"

const OUT = "/Users/santoshkumarradha/Documents/agentfield/code/platform/af-stack/dashboard-screenshots"
const URL = "http://localhost:33000"

const browser = await chromium.launch()
const ctx = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
  colorScheme: "dark",
})
const page = await ctx.newPage()
page.setDefaultTimeout(15_000)

const signin = await ctx.request.post(`${URL}/api/auth/sign-in/email`, {
  data: { email: "operator@example.com", password: "af-stack-demo-pwd" },
})
if (!signin.ok()) throw new Error("signin failed")

for (const [route, name] of [
  ["/build/modules", "build-modules"],
  ["/build/auth", "build-auth"],
  ["/build/billing", "build-billing"],
  ["/build/webhooks", "build-webhooks"],
  ["/build/agents", "build-agents"],
  ["/build/integrations", "build-integrations"],
  ["/build/harnesses", "build-harnesses"],
]) {
  console.log("→", route)
  await page.goto(`${URL}${route}`, { waitUntil: "networkidle" })
  await page.waitForTimeout(1500)
  await page.screenshot({ path: resolve(OUT, `${name}.png`), fullPage: false })
}

await browser.close()
console.log("done")
