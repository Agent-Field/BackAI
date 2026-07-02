// Browser-grade auth E2E: one Chromium context (one cookie store, like a
// real user) drives BOTH bundled apps. Catches the class of bugs curl
// jars cannot: shared-host cookie collisions between the customer app and
// the operator dashboard, Next.js client-router stale-redirect login
// loops, and cross-service session cookie-name drift.
//
// Requires the compose stack up (customer app :34000, dashboard :33000)
// and playwright browsers installed:
//   node node_modules/.pnpm/playwright@1.61.1/node_modules/playwright/cli.js install chromium-headless-shell
// Run: node scripts/browser-auth-e2e.mjs
import { createRequire } from "node:module"
const require = createRequire(import.meta.url)
// pnpm keeps playwright un-hoisted; resolve it out of the store.
const { chromium } = require(
  new URL(
    "../node_modules/.pnpm/playwright@1.61.1/node_modules/playwright/index.js",
    import.meta.url,
  ).pathname,
)

const APP = process.env.APP_URL ?? "http://localhost:34000"
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

// 1. Customer signs up at :34000 → better-auth.* cookies now live on
//    the shared "localhost" host (the collision precondition).
const email = `e2e-${Date.now()}@auth-e2e.local`
const signup = await context.request.post(`${APP}/api/auth/sign-up/email`, {
  data: { email, password: "e2e-passw0rd!", name: "E2E Browser" },
})
signup.ok()
  ? pass(`customer signup ${email}`)
  : fail(`customer signup: HTTP ${signup.status()}`)

// 2. Same browser opens the operator dashboard → must land on plain
//    /login (no operator_required bounce from the customer session).
await page.goto(`${DASH}/`, { waitUntil: "domcontentloaded" })
await page.waitForURL(/login/, { timeout: 10000 }).catch(() => {})
const loginUrl = page.url()
loginUrl.includes("/login") && !loginUrl.includes("operator_required")
  ? pass(`dashboard shows clean login (${new URL(loginUrl).pathname})`)
  : fail(`expected clean /login, got ${loginUrl}`)

// 3. Operator signs in via the real form → must land on the dashboard
//    WITHOUT a manual refresh (the router.push stale-redirect loop).
await page.fill("#email", OP_EMAIL)
await page.fill("#password", OP_PASSWORD)
await Promise.all([
  page.waitForURL((u) => !u.pathname.startsWith("/login"), { timeout: 15000 }),
  page.click('button[type="submit"]'),
]).catch(() => {})
const afterLogin = new URL(page.url())
afterLogin.pathname === "/"
  ? pass("operator login lands on dashboard home, no manual refresh")
  : fail(`still on ${afterLogin.pathname} after sign-in (login loop)`)

// 4. Operator-gated write through the dashboard proxy — must be 2xx.
//    OPERATOR_FORBIDDEN here means the runtime resolved the wrong
//    session (customer cookie leaked through).
await page.waitForLoadState("domcontentloaded")
const reveal = await page.evaluate(async () => {
  const res = await fetch("/api/v1/admin/keys", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      name: "browser-auth-e2e",
      tenant_id: "00000000-0000-0000-0000-000000000000",
    }),
  })
  return { status: res.status, body: (await res.text()).slice(0, 200) }
})
reveal.status >= 200 && reveal.status < 300
  ? pass(`admin key mint via dashboard session (HTTP ${reveal.status})`)
  : fail(`admin key mint: HTTP ${reveal.status} ${reveal.body}`)

// 5. Operator-gated GET the dashboard UI uses everywhere.
const tenants = await page.evaluate(async () => {
  const res = await fetch("/api/v1/admin/tenants", { credentials: "include" })
  return res.status
})
tenants === 200
  ? pass("admin tenants list (HTTP 200)")
  : fail(`admin tenants list: HTTP ${tenants}`)

// 6. Customer app still works in the SAME browser: its session must
//    have survived the operator login (cookie isolation) and its
//    tenant-scoped calls must resolve the CUSTOMER, not the operator.
await page.goto(`${APP}/dashboard`, { waitUntil: "domcontentloaded" })
new URL(page.url()).pathname === "/dashboard"
  ? pass("customer app session intact alongside operator session")
  : fail(`customer app bounced to ${page.url()}`)
const onboarding = await page.evaluate(async () => {
  const res = await fetch("/api/customer/onboarding-key", {
    method: "POST",
    credentials: "include",
  })
  return res.status
})
onboarding === 200
  ? pass("customer tenant key mint resolves own tenant (HTTP 200)")
  : fail(`customer onboarding-key: HTTP ${onboarding}`)

// 7. Dashboard sign-out via the real footer link: must land on /login,
//    clear the operator cookies, and kill admin access.
await page.goto(`${DASH}/`, { waitUntil: "domcontentloaded" })
await page.click('a[href="/sign-out"]').catch(() => {})
await page.waitForURL(/login/, { timeout: 10000 }).catch(() => {})
const afterSignOut = await page.evaluate(async () => {
  const res = await fetch("/api/v1/admin/tenants", { credentials: "include" })
  return res.status
})
page.url().includes("/login") && afterSignOut !== 200
  ? pass(`dashboard sign-out: back on /login, admin access revoked (HTTP ${afterSignOut})`)
  : fail(`sign-out: url=${page.url()} admin=${afterSignOut}`)
const opCookies = (await context.cookies()).filter((c) =>
  c.name.includes("backai-operator"),
)
opCookies.length === 0
  ? pass("operator cookies cleared")
  : fail(`operator cookies left: ${opCookies.map((c) => c.name).join(",")}`)

// 8. Customer sign-out route (the nav-user "Log out" item navigates here).
await page.goto(`${APP}/sign-out`, { waitUntil: "domcontentloaded" })
const custAfter = await context.request.get(`${APP}/api/auth/get-session`)
const custBody = await custAfter.text()
custBody === "null" || custBody === ""
  ? pass("customer sign-out kills session")
  : fail(`customer session survived sign-out: ${custBody.slice(0, 80)}`)

await browser.close()
console.log(failed ? "\nBROWSER AUTH E2E: FAILURES ABOVE" : "\nBROWSER AUTH E2E: ALL PASS")
process.exit(failed)
