// Sidebar real-pages E2E: logs in as the operator once, then drives every
// newly shipped dashboard page in a real Chromium and asserts each renders
// its own content (not an error boundary, not a redirect to Home).
//
// Run: node scripts/sidebar-pages-e2e.mjs
import { createRequire } from "node:module"
const require = createRequire(import.meta.url)
const { chromium } = require(
  new URL(
    "../node_modules/.pnpm/playwright@1.61.1/node_modules/playwright/index.js",
    import.meta.url,
  ).pathname,
)

const DASH = process.env.DASH_URL ?? "http://localhost:33000"
const OP_EMAIL = process.env.OPERATOR_EMAIL ?? "operator@af-stack.local"
const OP_PASSWORD = process.env.OPERATOR_PASSWORD ?? "changeme123"

let failed = 0
const pass = (m) => console.log(`PASS  ${m}`)
const fail = (m) => {
  console.log(`FAIL  ${m}`)
  failed = 1
}

const browser = await chromium.launch()
const context = await browser.newContext()
const page = await context.newPage()

// Operator login via better-auth REST — same cookie the browser would get.
const login = await context.request.post(`${DASH}/api/auth/sign-in/email`, {
  data: { email: OP_EMAIL, password: OP_PASSWORD },
  headers: { Origin: DASH },
})
login.ok() ? pass("operator login") : fail(`operator login HTTP ${login.status()}`)

// page path -> [h1/header text to expect, content probe that proves real data or a real empty-state]
const PAGES = [
  ["/activity/errors", /Errors/i, /error groups|No error groups/i],
  ["/activity/logs", /Logs/i, /af-stack|level|No log lines/i],
  ["/activity/queue", /Queue/i, /pending|No jobs yet/i],
  ["/activity/webhooks", /Webhook/i, /Endpoints|Deliveries/i],
  ["/people/audit", /Audit/i, /api_key\.create|entries/i],
  ["/people/activity", /Activity/i, /No activity yet|entries|simulation\./i],
  ["/people/sessions", /Sessions/i, new RegExp(OP_EMAIL.replace(/[.@]/g, "\\$&"), "i")],
  ["/people/keys", /API keys/i, /prefix|Issue|No keys/i],
  ["/people/users", /Users/i, /@/],
  ["/build/agents", /Agents/i, /courtsim/i],
  ["/build/reasoners", /Reasoners/i, /courtsim\.|No reasoner calls/i],
]

for (const [path, headerRe, probeRe] of PAGES) {
  try {
    const resp = await page.goto(`${DASH}${path}`, { waitUntil: "networkidle", timeout: 30000 })
    const status = resp?.status() ?? 0
    const url = page.url()
    const body = await page.textContent("body")
    if (status >= 400) {
      fail(`${path}: HTTP ${status}`)
    } else if (!url.includes(path)) {
      fail(`${path}: redirected to ${url} (nav still comingSoon?)`)
    } else if (/Application error|Unhandled Runtime Error|500/i.test(body ?? "") && !probeRe.test(body ?? "")) {
      fail(`${path}: error boundary rendered`)
    } else if (!headerRe.test(body ?? "")) {
      fail(`${path}: header ${headerRe} not found`)
    } else if (!probeRe.test(body ?? "")) {
      fail(`${path}: content probe ${probeRe} not found — page may be a shell`)
    } else {
      pass(`${path}`)
    }
  } catch (err) {
    fail(`${path}: ${String(err).slice(0, 120)}`)
  }
}

// Top-bar tenant switcher — a Base UI Menu.GroupLabel used without a
// Menu.Group crashed this on open (MenuGroupContext missing). Guard it.
try {
  const tbErrors = []
  page.on("pageerror", (e) => tbErrors.push(e.message.split("\n")[0]))
  await page.goto(`${DASH}/`, { waitUntil: "networkidle", timeout: 30000 })
  await page.waitForTimeout(500)
  const hydrationOnLoad = tbErrors.some((m) => /Hydration|#418/i.test(m))
  await page.locator("header").getByRole("button").nth(1).click({ timeout: 5000 })
  await page.waitForTimeout(600)
  const items = await page.locator('[data-slot="dropdown-menu-item"]').count()
  const menuCrash = tbErrors.some((m) => /MenuGroupContext|Base UI|#31/i.test(m))
  const hasAllTenants = (await page.textContent("body"))?.includes("All tenants")
  if (menuCrash || items === 0) {
    fail(`tenant switcher: menu did not open (items=${items})${menuCrash ? " — Base UI crash" : ""}`)
  } else if (hydrationOnLoad) {
    fail("top bar: hydration mismatch on load (#418) — theme toggle?")
  } else if (!hasAllTenants) {
    fail("tenant switcher: no 'All tenants' option")
  } else {
    pass(`tenant switcher opens (${items - 1} tenants + All tenants, no hydration error)`)
    // Selecting a tenant must actually navigate to its drilldown.
    const tenantRow = page.locator('[data-slot="dropdown-menu-item"]').nth(1)
    await tenantRow.click({ timeout: 5000 })
    await page.waitForTimeout(1200)
    if (/\/people\/tenants\/[^/]+/.test(page.url())) {
      pass(`tenant select navigates → ${new URL(page.url()).pathname}`)
    } else {
      fail(`tenant select did nothing (still at ${new URL(page.url()).pathname})`)
    }
  }
} catch (err) {
  fail(`tenant switcher: ${String(err).slice(0, 120)}`)
}

await browser.close()
console.log(failed ? "\nSIDEBAR PAGES E2E: FAILURES ABOVE" : "\nSIDEBAR PAGES E2E: ALL PASS")
process.exit(failed)
