import { chromium } from "playwright"
const browser = await chromium.launch()
const ctx = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
  colorScheme: "dark",
})
const page = await ctx.newPage()
const signin = await ctx.request.post("http://localhost:33000/api/auth/sign-in/email", {
  data: { email: "operator@example.com", password: "af-stack-demo-pwd" }
})
console.log("signin:", signin.status())
await page.goto("http://localhost:33000/build/database", { waitUntil: "networkidle" })
await page.waitForTimeout(2000)
const sizes = await page.evaluate(() => ({
  bodyScrollWidth: document.body.scrollWidth,
  htmlScrollWidth: document.documentElement.scrollWidth,
  viewportWidth: window.innerWidth,
}))
console.log(sizes)
await page.screenshot({ path: "/Users/santoshkumarradha/Documents/agentfield/code/platform/af-stack/dashboard-screenshots/db-debug.png", fullPage: false })
await browser.close()
