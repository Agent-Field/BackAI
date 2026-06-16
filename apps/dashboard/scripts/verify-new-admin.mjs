#!/usr/bin/env node
import { readFileSync } from "node:fs"
import { resolve } from "node:path"

const root = resolve(import.meta.dirname, "..")
const nav = readFileSync(resolve(root, "src/lib/new-admin/navigation.ts"), "utf8")
const model = readFileSync(resolve(root, "src/lib/new-admin/page-model.ts"), "utf8")
const designDoc = readFileSync(resolve(root, "../../development/admin-design-patterns-v1.md"), "utf8")
const gapDoc = readFileSync(resolve(root, "../../development/admin-api-gap-registry-v1.md"), "utf8")

const requiredRoutes = [
  "/",
  "/operate/runs",
  "/operate/cost",
  "/operate/errors",
  "/operate/traces",
  "/operate/queue",
  "/operate/cache",
  "/operate/sandbox-runs",
  "/operate/webhooks",
  "/operate/notifications",
  "/operate/approvals",
  "/operate/activity",
  "/operate/health",
  "/operate/logs",
  "/build/agents",
  "/build/reasoners",
  "/build/tools",
  "/build/skills",
  "/build/harnesses",
  "/build/crons",
  "/build/sandboxes",
  "/build/modules",
  "/build/data/tables",
  "/build/data/sql",
  "/build/data/memory",
  "/build/data/storage",
  "/build/data/search",
  "/build/feature-flags",
  "/build/api-explorer",
  "/customers/tenants",
  "/customers/api-keys",
  "/customers/members",
  "/customers/sessions",
  "/customers/budgets",
  "/customers/audit",
  "/customers/oauth",
  "/customers/billing",
  "/setup/adapters",
  "/setup/auth-providers",
  "/setup/llm-providers",
  "/setup/sandbox",
  "/setup/webhook-subscribers",
  "/setup/notifications",
  "/setup/secrets",
  "/setup/observability",
  "/setup/billing-adapter",
  "/setup/deploy-targets",
  "/brand",
]

function assert(condition, message) {
  if (!condition) {
    console.error(`new-admin contract failed: ${message}`)
    process.exitCode = 1
  }
}

for (const route of requiredRoutes) {
  assert(nav.includes(`href: "${route}"`), `missing nav route ${route}`)
  assert(model.includes(`"${route}": {`), `missing page model for ${route}`)
}

assert(!nav.includes('title: "Develop"'), "Develop group must not exist in new admin")
assert(!nav.includes("/develop/"), "Develop routes must not exist in new admin")
assert(nav.includes('dataTruth: "missing"'), "navigation must record missing API states")
assert(nav.includes('dataTruth: "degraded"'), "navigation must record degraded API states")
assert(model.includes("statusCard("), "page model must render data contract panels")
assert(designDoc.includes("Page Archetypes"), "design pattern doc must define page archetypes")
assert(designDoc.includes("Anti-Duplication Rule"), "design pattern doc must define anti-duplication rule")
assert(gapDoc.includes("Deploy targets"), "API gap registry must include deploy target gap")
assert(gapDoc.includes("Brand"), "API gap registry must include brand endpoint gap")

if (!process.exitCode) {
  console.log(`new-admin contract ok: ${requiredRoutes.length} routes verified`)
}
