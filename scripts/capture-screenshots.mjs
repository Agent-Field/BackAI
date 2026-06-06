#!/usr/bin/env node
/**
 * Capture the three hero screenshots used in the README:
 *
 *   dashboard-screenshots/setup.png    - first-run wizard
 *   dashboard-screenshots/home.png     - signed-in operator Home with live data
 *   dashboard-screenshots/cost.png     - Operate / Cost dashboard
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
const OUT_DIR = resolve(REPO_ROOT, "dashboard-screenshots")

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

  // ── 5. Runs (bonus screenshot — shows live data nicely)
  console.log("→ Runs")
  await page.goto(`${DASHBOARD_URL}/operate/runs`, { waitUntil: "networkidle" })
  await page.waitForTimeout(2000)
  await page.screenshot({
    path: resolve(OUT_DIR, "runs.png"),
    fullPage: false,
  })

  await browser.close()
  console.log(`saved screenshots to ${OUT_DIR}`)
}

capture().catch((err) => {
  console.error(err)
  process.exit(1)
})
