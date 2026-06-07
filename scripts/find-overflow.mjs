import { chromium } from "playwright"
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: "dark" })
const page = await ctx.newPage()
await ctx.request.post("http://localhost:33000/api/auth/sign-in/email", {
  data: { email: "operator@example.com", password: "af-stack-demo-pwd" }
})
await page.goto("http://localhost:33000/build/database", { waitUntil: "networkidle" })
await page.waitForTimeout(2000)
const offenders = await page.evaluate(() => {
  const out = []
  const vw = window.innerWidth
  const all = document.querySelectorAll("*")
  for (const el of all) {
    const r = el.getBoundingClientRect()
    if (r.right > vw + 5) {
      // grab tag + classes (truncated)
      out.push({
        tag: el.tagName,
        cls: (el.className && el.className.toString ? el.className.toString().slice(0,80) : ""),
        right: Math.round(r.right),
        width: Math.round(r.width),
        path: (() => {
          let p = el, parts = []
          while (p && p.nodeType === 1 && parts.length < 5) {
            const c = p.className && p.className.toString ? p.className.toString().split(/\s+/).slice(0,2).join(".") : ""
            parts.unshift(`${p.tagName.toLowerCase()}${c ? "."+c : ""}`)
            p = p.parentElement
          }
          return parts.join(" > ")
        })(),
      })
    }
    if (out.length > 25) break
  }
  return out
})
console.log(JSON.stringify(offenders, null, 2))
await browser.close()
