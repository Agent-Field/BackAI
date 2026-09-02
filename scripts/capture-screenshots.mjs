#!/usr/bin/env node
/**
 * Capture the three hero screenshots used in the README:
 *
 *   docs/assets/dashboard-screenshots/setup.png    - first-run wizard
 *   docs/assets/dashboard-screenshots/home.png     - signed-in operator Home with live data
 *   docs/assets/dashboard-screenshots/cost.png     - Operate / Cost dashboard
 *
 * Assumes:
 *   - the compose stack is up and an operator account already exists
 *   - dashboard is reachable at DASHBOARD_URL (default http://localhost:33000)
 *   - email/password of the operator are passed as env (OP_EMAIL, OP_PASSWORD)
 *
 * Used by Phase 4.5; the README hero block embeds these images.
 */

import { chromium } from "playwright"
import { mkdir } from "node:fs/promises"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)
const REPO_ROOT = resolve(__dirname, "..")
const OUT_DIR = resolve(REPO_ROOT, "docs/assets/dashboard-screenshots")

const DASHBOARD_URL = process.env.DASHBOARD_URL ?? "http://localhost:33000"
const OP_EMAIL = process.env.OP_EMAIL ?? "operator@example.com"
const OP_PASSWORD = process.env.OP_PASSWORD ?? "af-stack-demo-pwd"

const VIEWPORT = { width: 1440, height: 900 }
const DEVICE_SCALE_FACTOR = 2

async function capture() {
  await mkdir(OUT_DIR, { recursive: true })

  const browser = await chromium.launch()
  const context = await browser.newContext({
    viewport: VIEWPORT,
    deviceScaleFactor: DEVICE_SCALE_FACTOR,
    colorScheme: "dark",
  })
  const page = await context.newPage()
  page.setDefaultTimeout(15_000)

  // ── 1. Setup wizard (anonymous; works when no operator exists too)
  console.log("→ setup wizard")
  await page.goto(`${DASHBOARD_URL}/setup`, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(1000)
  await page.screenshot({
    path: resolve(OUT_DIR, "setup.png"),
    fullPage: false,
  })

  // ── 2. Sign in via the API to get the session cookie, then load Home
  console.log("→ signing in via API")
  const signInResp = await context.request.post(
    `${DASHBOARD_URL}/api/auth/sign-in/email`,
    {
      data: { email: OP_EMAIL, password: OP_PASSWORD },
    },
  )
  if (!signInResp.ok()) {
    throw new Error(
      `Sign-in failed (${signInResp.status()}): ${await signInResp.text()}`,
    )
  }

  // ── 3. Home (with live data from the recent LLM calls)
  console.log("→ Home")
  await page.goto(`${DASHBOARD_URL}/`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "home.png"),
    fullPage: false,
  })

  // ── 4. Cost dashboard
  console.log("→ Cost dashboard")
  await page.goto(`${DASHBOARD_URL}/operate/cost`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "cost.png"),
    fullPage: false,
  })

  // ── 4b. Cost dashboard with LIVE LLM traffic.
  //
  // The README hero block uses cost-live.png — it's the same page as
  // cost.png but captured AFTER the OpenAI-SDK e2e test has run, so the
  // live cost events table, model mix, and per-tenant spend bars are
  // populated with real (not synthetic) data.
  //
  // The caller is expected to:
  //   1. run scripts/test-openai-sdk.mjs first (a few chat calls)
  //   2. then run this capture script
  //
  // We don't gate the capture on that — if no live calls have happened
  // yet the file will still save (showing the empty state), which is a
  // useful canary that the e2e ran.
  console.log("→ Cost dashboard (live)")
  await page.goto(`${DASHBOARD_URL}/operate/cost`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2500)
  await page.screenshot({
    path: resolve(OUT_DIR, "cost-live.png"),
    fullPage: false,
  })

  // ── 5. Runs
  console.log("→ Runs")
  await page.goto(`${DASHBOARD_URL}/operate/runs`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "runs.png"),
    fullPage: false,
  })

  // ── 6. Queues (Phase 5: real River data)
  console.log("→ Queues")
  await page.goto(`${DASHBOARD_URL}/operate/queues`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "queues.png"),
    fullPage: false,
  })

  // ── 7. Build → Jobs (definitions list + enqueue Sheet)
  console.log("→ Jobs")
  await page.goto(`${DASHBOARD_URL}/build/jobs`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "jobs.png"),
    fullPage: false,
  })

  // ── 8. Build → Secrets
  console.log("→ Secrets")
  await page.goto(`${DASHBOARD_URL}/build/secrets`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "secrets.png"),
    fullPage: false,
  })

  // ── 9. Build → Storage
  console.log("→ Storage")
  await page.goto(`${DASHBOARD_URL}/build/storage`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "storage.png"),
    fullPage: false,
  })

  // ── 10-12. Customers/* (Phase 6 — only render real content when
  //          multi-tenancy is on; the pages render an empty-state CTA
  //          otherwise, which is also worth a screenshot.)
  console.log("→ Customers → Tenants")
  await page.goto(`${DASHBOARD_URL}/customers/tenants`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "customers-tenants.png"),
    fullPage: false,
  })

  console.log("→ Customers → API Keys")
  await page.goto(`${DASHBOARD_URL}/customers/api-keys`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "customers-api-keys.png"),
    fullPage: false,
  })

  console.log("→ Customers → Audit")
  await page.goto(`${DASHBOARD_URL}/customers/audit`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "customers-audit.png"),
    fullPage: false,
  })

  // ── 13. Build → Database (Phase 8.4)
  //
  // The DB studio renders a left table sidebar + tabs (Data / Structure /
  // Policies / SQL / Memory). When the runtime hasn't wired the
  // /api/v1/db/* endpoints yet (Phase 8.x in flight on a parallel
  // branch), the page renders an empty-state CTA instead — still worth
  // a screenshot as a canary.
  console.log("→ Build → Database")
  await page.goto(`${DASHBOARD_URL}/build/database`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  // Best-effort: select suite_tenants in the sidebar so the screenshot
  // shows real content. The sidebar may not have rendered if the runtime
  // endpoints are down — in that case we just capture the empty state.
  try {
    const tenants = page
      .locator(
        '[role="button"], [data-table], button, a',
        { hasText: /^suite_tenants$/ },
      )
      .first()
    await tenants.waitFor({ state: "visible", timeout: 3000 })
    await tenants.click()
    await page.waitForTimeout(1500)
  } catch {
    console.log("    (no suite_tenants entry found — capturing whatever rendered)")
  }
  await page.screenshot({
    path: resolve(OUT_DIR, "database.png"),
    fullPage: false,
  })

  // ── 14. Build → Database → Memory tab (Phase 8.4)
  //
  // Same route, just the Memory tab activated. The tab shows the per-
  // scope KV browser + vector search box.
  console.log("→ Build → Database → Memory tab")
  await page.goto(`${DASHBOARD_URL}/build/database`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(1500)
  try {
    const memoryTab = page
      .locator('button[role="tab"], [data-state]', { hasText: /^Memory$/i })
      .first()
    await memoryTab.waitFor({ state: "visible", timeout: 3000 })
    await memoryTab.click()
    await page.waitForTimeout(1500)
  } catch {
    console.log("    (no Memory tab found — capturing whatever rendered)")
  }
  await page.screenshot({
    path: resolve(OUT_DIR, "memory.png"),
    fullPage: false,
  })

  // ── 15. Operate → Sandbox Activity (Phase 9.4)
  //
  // The activity page lists recent sandbox runs (per-tenant when MT is on),
  // adapter pool stats (warm / active / queued), and cost-today rollups.
  // When the runtime hasn't wired /api/v1/sandbox/* yet the page falls
  // back to an empty-state CTA — still a useful canary screenshot.
  console.log("→ Operate → Sandbox Activity")
  await page.goto(`${DASHBOARD_URL}/operate/sandbox-activity`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "sandbox-activity.png"),
    fullPage: false,
  })

  // ── 16. Build → Sandboxes (config)
  //
  // The configuration page for the sandbox module: adapter selection
  // (docker / firecracker / e2b / modal), default limits (cpu, memory,
  // timeout), egress policy, and image allowlist.
  console.log("→ Build → Sandboxes")
  await page.goto(`${DASHBOARD_URL}/build/sandboxes`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "sandbox-config.png"),
    fullPage: false,
  })

  // ── 17. Operate → Notifications (Phase 10.1) — stats KPI + deliveries.
  console.log("→ Operate → Notifications")
  await page.goto(`${DASHBOARD_URL}/operate/notifications`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "notifications.png"),
    fullPage: false,
  })

  // ── 18. Operate → Webhook Activity (Phase 10.2 + 10.3) — unified feed.
  console.log("→ Operate → Webhook Activity")
  await page.goto(`${DASHBOARD_URL}/operate/webhook-activity`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "webhook-activity.png"),
    fullPage: false,
  })

  // ── 19. Customers → Billing (Phase 10.4) — per-tenant Stripe billing.
  console.log("→ Customers → Billing")
  await page.goto(`${DASHBOARD_URL}/customers/customer-billing`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "billing.png"),
    fullPage: false,
  })

  // ── 20. Build → MCP (Phase 11.1 + 11.2) — server table + tool browser.
  console.log("→ Build → MCP")
  await page.goto(`${DASHBOARD_URL}/build/mcp`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "mcp.png"),
    fullPage: false,
  })

  // ── 21. Build → Skills (Phase 11.3) — installed skill bundles.
  console.log("→ Build → Skills")
  await page.goto(`${DASHBOARD_URL}/build/skills`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "skills.png"),
    fullPage: false,
  })

  // ── 22. Build → Harnesses (Phase 11.4) — CLI tool probe grid.
  console.log("→ Build → Harnesses")
  await page.goto(`${DASHBOARD_URL}/build/harnesses`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "harnesses.png"),
    fullPage: false,
  })

  // ── 23. Customers → Tenant drilldown (Phase 12.1)
  //
  // Click the first tenant row from the list (skip if the page hasn't
  // wired drilldown navigation yet — falls back to the list page).
  console.log("→ Customers → Tenant drilldown")
  try {
    await page.goto(`${DASHBOARD_URL}/customers/tenants`, {
      waitUntil: "networkidle",
    })
    await page.waitForTimeout(1500)
    // First tenant row link.
    const firstTenant = page.locator('a[href*="/customers/tenants/"]').first()
    if (await firstTenant.count()) {
      await firstTenant.click()
      await page.waitForLoadState("networkidle")
      await page.waitForTimeout(2000)
    } else {
      console.log("    (no tenant link found — capturing list)")
    }
  } catch (e) {
    console.log("    (drilldown nav failed —", e.message, ")")
  }
  await page.screenshot({
    path: resolve(OUT_DIR, "tenant-drilldown.png"),
    fullPage: false,
  })

  // ── 24. Operate → Crons (Phase 12.2)
  console.log("→ Operate → Crons")
  await page.goto(`${DASHBOARD_URL}/operate/crons`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "crons.png"),
    fullPage: false,
  })

  // ── 25. Operate → Metrics (Phase 12.2)
  console.log("→ Operate → Metrics")
  await page.goto(`${DASHBOARD_URL}/operate/metrics`, {
    waitUntil: "networkidle",
  })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "metrics.png"),
    fullPage: false,
  })


  await browser.close()
  console.log(`saved screenshots to ${OUT_DIR}`)
}

capture().catch((err) => {
  console.error(err)
  process.exit(1)
})
