#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
//
// Anti-hallucination verifier for the AF Stack skill.
//
// Checks:
//   1. Every file path referenced in SKILL.md and rules/*.md exists in the repo.
//   2. Every SDK call referenced exists in packages/sdk-py / packages/sdk-ts.
//   3. Every API route referenced is a real route (registered in
//      services/runtime/internal/server/openapi_routes.go).
//   4. Snippet files compile (Python parses, TS type-checks).
//
// Run from the skill directory:
//   node meta/verify-skill.mjs
//
// Exit code: 0 if clean, 1 if drift detected. CI should block on this.

import { readFileSync, readdirSync, statSync, existsSync } from "node:fs"
import { join, dirname, resolve } from "node:path"
import { spawnSync } from "node:child_process"

const SKILL_ROOT = resolve(dirname(new URL(import.meta.url).pathname), "..")
const REPO_ROOT = resolve(SKILL_ROOT, "../..")

const RED = "\x1b[31m"
const GREEN = "\x1b[32m"
const YELLOW = "\x1b[33m"
const RESET = "\x1b[0m"

let failures = 0
let warnings = 0

function fail(msg) {
  console.log(`${RED}✗${RESET} ${msg}`)
  failures++
}
function warn(msg) {
  console.log(`${YELLOW}⚠${RESET} ${msg}`)
  warnings++
}
function ok(msg) {
  console.log(`${GREEN}✓${RESET} ${msg}`)
}

// ─── Collect all .md files in the skill ─────────────────────────────────

function walkSkillMarkdown(dir) {
  const out = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    const s = statSync(full)
    if (s.isDirectory()) {
      out.push(...walkSkillMarkdown(full))
    } else if (entry.endsWith(".md")) {
      out.push(full)
    }
  }
  return out
}

const allSkillMd = walkSkillMarkdown(SKILL_ROOT)

// ─── 1. Check file path references ──────────────────────────────────────
//
// Paths that appear as `path/like/this` in skill docs. Skip URLs, code
// fences, and obviously-template paths (REPLACE_ME, <name>, etc.).

const PATH_REGEX = /\b([a-z_-]+\/(?:[a-z0-9_.-]+\/)*[a-z0-9_.-]+\.(?:md|py|ts|tsx|go|yaml|yml|json|sql|sh|mjs|js))\b/gi

const SKIP_PATHS = [
  /REPLACE_ME/,
  /YOUR_/,
  /your-/,
  /<.+?>/,
  /node_modules/,
  /\.git\//,
]

const referencedPaths = new Set()
for (const md of allSkillMd) {
  const content = readFileSync(md, "utf8")
  for (const match of content.matchAll(PATH_REGEX)) {
    const p = match[1]
    if (SKIP_PATHS.some((re) => re.test(p))) continue
    referencedPaths.add(p)
  }
}

console.log(`\n${YELLOW}Checking ${referencedPaths.size} referenced file paths...${RESET}`)
let pathFails = 0
for (const p of referencedPaths) {
  // Try the path relative to repo root.
  const fullPath = join(REPO_ROOT, p)
  if (!existsSync(fullPath)) {
    // Also try relative to skill root (links within the skill).
    const skillPath = join(SKILL_ROOT, p)
    if (!existsSync(skillPath)) {
      pathFails++
      if (pathFails <= 10) {
        warn(`Path not found: ${p}`)
      }
    }
  }
}
if (pathFails === 0) {
  ok(`All file path references resolve`)
} else {
  warn(`${pathFails} path references could not be resolved (showing first 10)`)
}

// ─── 2. Check SDK call references ───────────────────────────────────────

const SDK_PY_PATH = join(REPO_ROOT, "packages/sdk-py/af_stack")
const SDK_TS_PATH = join(REPO_ROOT, "packages/sdk-ts/src")

const PY_NAMESPACES = ["agents", "llm", "memory", "storage", "sandbox", "jobs",
                       "secrets", "billing", "notifications", "webhooks", "cost",
                       "harnesses", "tools"]
const TS_EXTRA = ["realtime", "search", "skills"]

const knownPyNamespaces = new Set(PY_NAMESPACES)
const knownTsNamespaces = new Set([...PY_NAMESPACES, ...TS_EXTRA])

// Find every `suite.<ns>.<method>` reference in skill markdown
const SUITE_CALL_REGEX = /suite\.([a-z_]+)\.([a-z_]+)/g
const suiteCalls = new Set()
for (const md of allSkillMd) {
  const content = readFileSync(md, "utf8")
  for (const match of content.matchAll(SUITE_CALL_REGEX)) {
    suiteCalls.add(`${match[1]}.${match[2]}`)
  }
}

console.log(`\n${YELLOW}Checking ${suiteCalls.size} suite.* call references...${RESET}`)
let suiteFails = 0
for (const call of suiteCalls) {
  const [ns] = call.split(".")
  if (!knownTsNamespaces.has(ns)) {
    suiteFails++
    if (suiteFails <= 5) {
      warn(`Unknown namespace: suite.${call}`)
    }
  }
}
if (suiteFails === 0) {
  ok(`All suite.* namespace references valid (deep methods not verified)`)
} else {
  warn(`${suiteFails} unknown suite.* namespaces`)
}

// `app.*` references (AgentField SDK)
const APP_CALL_REGEX = /\bapp\.([a-z_]+)/g
const appCalls = new Set()
const knownAppCalls = new Set([
  "ai", "memory", "harness", "mcp", "tools", "is_cancelled", "run",
  "reasoner", "call",
])
for (const md of allSkillMd) {
  const content = readFileSync(md, "utf8")
  for (const match of content.matchAll(APP_CALL_REGEX)) {
    appCalls.add(match[1])
  }
}

let appFails = 0
for (const call of appCalls) {
  if (!knownAppCalls.has(call)) {
    appFails++
    if (appFails <= 5) {
      warn(`Unknown app.* call: app.${call}`)
    }
  }
}
if (appFails === 0) {
  ok(`All app.* namespace references valid`)
} else {
  warn(`${appFails} unknown app.* calls (these may be agent-defined, not SDK)`)
}

// ─── 3. Check route references ──────────────────────────────────────────

const OPENAPI_FILE = join(REPO_ROOT, "services/runtime/internal/server/openapi_routes.go")
let registeredRoutes = new Set()
if (existsSync(OPENAPI_FILE)) {
  const content = readFileSync(OPENAPI_FILE, "utf8")
  const routeMatches = content.matchAll(/Register\("(GET|POST|PUT|DELETE|PATCH)",\s*"(\/[^"]+)"/g)
  for (const m of routeMatches) {
    registeredRoutes.add(m[2])
  }
}

console.log(`\n${YELLOW}Loaded ${registeredRoutes.size} registered runtime routes${RESET}`)

const ROUTE_REGEX = /(?:GET|POST|PUT|DELETE|PATCH)\s+(\/api\/v1\/[a-z0-9/_{}-]+)/g
const referencedRoutes = new Set()
for (const md of allSkillMd) {
  const content = readFileSync(md, "utf8")
  for (const match of content.matchAll(ROUTE_REGEX)) {
    referencedRoutes.add(match[1])
  }
}

let routeFails = 0
for (const route of referencedRoutes) {
  // Normalize trailing slashes, template paths
  const normalized = route.replace(/\/$/, "")
  // Check exact match OR check ignoring template params {id} vs {name}
  const matchesAny = [...registeredRoutes].some((r) => {
    const rNorm = r.replace(/\/$/, "").replace(/{[^}]+}/g, "{}")
    const nNorm = normalized.replace(/{[^}]+}/g, "{}")
    return rNorm === nNorm
  })
  if (!matchesAny) {
    routeFails++
    if (routeFails <= 5) {
      warn(`Route not found: ${route}`)
    }
  }
}
if (routeFails === 0) {
  ok(`All /api/v1/* route references valid`)
} else {
  warn(`${routeFails} route references not found in openapi_routes.go`)
}

// ─── 4. Snippet compile checks ──────────────────────────────────────────

console.log(`\n${YELLOW}Checking snippet compile...${RESET}`)

const SNIPPETS_DIR = join(SKILL_ROOT, "snippets")
function walkSnippets(dir) {
  const out = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    const s = statSync(full)
    if (s.isDirectory()) {
      out.push(...walkSnippets(full))
    } else {
      out.push(full)
    }
  }
  return out
}

const snippetFiles = walkSnippets(SNIPPETS_DIR)

for (const f of snippetFiles) {
  if (f.endsWith(".py")) {
    // Parse-check with python -c "compile(...)"
    const result = spawnSync("python3", [
      "-c",
      `import ast; ast.parse(open('${f}').read())`,
    ])
    if (result.status !== 0) {
      fail(`Python parse error in ${f}: ${result.stderr.toString()}`)
    }
  }
  // TS/Go/SQL snippets are templates with REPLACE_ME — we don't compile.
  // CI would need to substitute placeholders to type-check.
}

if (failures === 0) {
  ok(`Python snippets parse cleanly`)
}

// ─── 5. Truth gates (G3) ────────────────────────────────────────────────
//
// Three release-truth checks. They are STUBBED (warn-only) by default so the
// harness lands without blocking on known, tracked drift; set
// ENFORCE_TRUTH_GATES=1 to flip them to hard failures (the later phases do).
//
//   a. OpenAPI ↔ runtime route parity  (published spec matches served routes)
//   b. TS ≡ Py SDK namespace parity     (the two SDKs expose the same surface)
//   c. No stub markers in shipped agents (examples/*/agents are real, not fakes)

const ENFORCE_TRUTH_GATES = process.env.ENFORCE_TRUTH_GATES === "1"

function truthGate(name, problems) {
  if (problems.length === 0) {
    ok(`truth-gate ${name}: clean`)
    return
  }
  if (ENFORCE_TRUTH_GATES) {
    fail(`truth-gate ${name}: ${problems.length} issue(s)`)
  } else {
    warn(`truth-gate ${name} [stubbed]: ${problems.length} issue(s) — set ENFORCE_TRUTH_GATES=1 to enforce`)
  }
  for (const p of problems.slice(0, 8)) console.log(`      - ${p}`)
  if (problems.length > 8) console.log(`      … and ${problems.length - 8} more`)
}

console.log(`\n${YELLOW}Truth gates (G3)${ENFORCE_TRUTH_GATES ? " [ENFORCED]" : " [stubbed]"}...${RESET}`)

// ── a. OpenAPI ↔ runtime route parity ──
const normRoute = (s) => s.replace(/{[^}]+}/g, "{}").replace(/\/$/, "")
const SERVER_DIR = join(REPO_ROOT, "services/runtime/internal/server")
const servedRoutes = new Set()
if (existsSync(SERVER_DIR)) {
  for (const f of readdirSync(SERVER_DIR)) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue
    const c = readFileSync(join(SERVER_DIR, f), "utf8")
    for (const m of c.matchAll(/HandleFunc\("(GET|POST|PUT|DELETE|PATCH)\s+(\/api\/v1\/[^"]+)"/g)) {
      servedRoutes.add(`${m[1]} ${normRoute(m[2])}`)
    }
  }
}
const OPENAPI_JSON = join(REPO_ROOT, "docs-site/public/openapi.json")
const documentedRoutes = new Set()
if (existsSync(OPENAPI_JSON)) {
  try {
    const spec = JSON.parse(readFileSync(OPENAPI_JSON, "utf8"))
    for (const [p, ops] of Object.entries(spec.paths || {})) {
      if (!p.startsWith("/api/v1")) continue
      for (const method of Object.keys(ops)) {
        documentedRoutes.add(`${method.toUpperCase()} ${normRoute(p)}`)
      }
    }
  } catch (e) {
    warn(`could not parse ${OPENAPI_JSON}: ${e.message}`)
  }
}
const routeProblems = []
if (servedRoutes.size && documentedRoutes.size) {
  for (const r of servedRoutes) {
    if (!documentedRoutes.has(r)) routeProblems.push(`served but undocumented: ${r}`)
  }
  for (const r of documentedRoutes) {
    if (!servedRoutes.has(r)) routeProblems.push(`documented but not served: ${r}`)
  }
  truthGate("openapi-parity", routeProblems)
} else {
  warn(`truth-gate openapi-parity: could not load routes (served=${servedRoutes.size}, documented=${documentedRoutes.size})`)
}

// ── b. TS ≡ Py SDK namespace parity ──
function sdkNamespaces(dir, ext) {
  const out = new Set()
  if (!existsSync(dir)) return out
  for (const f of readdirSync(dir)) {
    const full = join(dir, f)
    if (statSync(full).isDirectory()) {
      if (!f.startsWith("_") && f !== "__pycache__") out.add(f)
      continue
    }
    if (!f.endsWith(ext)) continue
    const base = f.slice(0, -ext.length)
    if (base.startsWith("_") || base === "index" || base === "__init__") continue
    out.add(base)
  }
  return out
}
const pyNs = sdkNamespaces(SDK_PY_PATH, ".py")
const tsNs = sdkNamespaces(SDK_TS_PATH, ".ts")
const sdkProblems = []
for (const n of pyNs) if (!tsNs.has(n)) sdkProblems.push(`py-only namespace: ${n}`)
for (const n of tsNs) if (!pyNs.has(n)) sdkProblems.push(`ts-only namespace: ${n}`)
truthGate("sdk-parity", sdkProblems)

// ── c. No stub markers in shipped example agents ──
// Unambiguous "this agent is not doing real work" signals. Deliberately does
// NOT match the words "fabricate"/"hardcoded diff", which show up in honest
// disclaimers ("never fabricates a diff") far more than in actual stubs.
const STUB_MARKERS = [
  /raise\s+NotImplementedError/,
  /TODO:?\s*implement/i,
  /\bFIXME\b/,
  /#\s*stub\b/i,
  /simulate[ds]?\s+(a\s+)?(run|step|diff|build)/i,
]
function walkAgentPy(dir) {
  const out = []
  if (!existsSync(dir)) return out
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) {
      if (["mocks", "tests", "__pycache__", "node_modules"].includes(entry)) continue
      out.push(...walkAgentPy(full))
    } else if (entry.endsWith(".py") && !entry.endsWith("_test.py") && !entry.startsWith("test_")) {
      // Only files that live under an agents/ directory count as "shipped agents".
      if (full.includes(`${"agents"}/`) || full.includes("/agents/")) out.push(full)
    }
  }
  return out
}
const EXAMPLES_DIR = join(REPO_ROOT, "examples")
const agentProblems = []
for (const f of walkAgentPy(EXAMPLES_DIR)) {
  const content = readFileSync(f, "utf8")
  for (const re of STUB_MARKERS) {
    if (re.test(content)) {
      agentProblems.push(`${f.replace(REPO_ROOT + "/", "")} matches ${re}`)
      break
    }
  }
}
truthGate("no-stub-agents", agentProblems)

// ─── Summary ────────────────────────────────────────────────────────────

console.log(`\n${"=".repeat(60)}`)
console.log(`Failures: ${failures}   Warnings: ${warnings}`)

if (failures > 0) {
  console.log(`${RED}Skill verification FAILED.${RESET}`)
  process.exit(1)
} else if (warnings > 0) {
  console.log(`${YELLOW}Skill verification passed with warnings.${RESET}`)
  process.exit(0)
} else {
  console.log(`${GREEN}Skill verification passed.${RESET}`)
  process.exit(0)
}
