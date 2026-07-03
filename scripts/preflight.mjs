#!/usr/bin/env node

// BackAI preflight — port conflict detection and (with --fix) automatic
// allocation.
//
//   node scripts/preflight.mjs          # detect conflicts, print guidance, exit 1
//   node scripts/preflight.mjs --fix    # auto-allocate free host ports into .env,
//                                        # set COMPOSE_PROJECT_NAME, then exit 0
//
// `af-stack dev` runs the --fix variant automatically before `docker compose
// up`, so multiple BackAI apps can run side by side without manual port math.
// The point of the summary it prints is that an agent (or human) can read
// stdout and know exactly what is running and where — no guessing.

import { execFileSync } from "node:child_process"
import { existsSync, readFileSync, writeFileSync } from "node:fs"
import net from "node:net"
import path from "node:path"

const FIX = process.argv.includes("--fix")
const ENV_PATH = ".env"

const env = loadEnv()

// Each entry is one published host port. `target` is the in-container port
// docker maps to; `env` is the .env override key; `value` is the currently
// configured host port (from .env / process env, else the documented default).
const ports = [
  { service: "postgres", target: 5432, env: "POSTGRES_PORT", def: "5432", label: "Postgres" },
  { service: "minio", target: 9000, env: "MINIO_PORT", def: "9000", label: "MinIO API" },
  {
    service: "minio",
    target: 9001,
    env: "MINIO_CONSOLE_PORT",
    def: "9001",
    label: "MinIO console",
  },
  { service: "litellm", target: 4000, env: "LITELLM_PORT", def: "4000", label: "LiteLLM" },
  {
    service: "agentfield",
    target: 8080,
    env: "AGENTFIELD_PORT",
    def: "8081",
    label: "AgentField control plane",
  },
  {
    service: "runtime",
    target: 8080,
    env: "AF_STACK_PORT",
    def: "8080",
    label: "BackAI runtime (API)",
  },
  {
    service: "runtime",
    target: 9090,
    env: "AF_STACK_METRICS_PORT",
    def: "9090",
    label: "BackAI metrics",
  },
  {
    service: "dashboard",
    target: 3000,
    env: "AF_STACK_DASHBOARD_PORT",
    def: "33000",
    label: "Admin dashboard",
  },
  {
    service: "customer-app",
    target: 3000,
    env: "AF_STACK_CUSTOMER_APP_PORT",
    def: "34000",
    label: "Customer app",
  },
  { service: "svix-server", target: 8071, env: "SVIX_PORT", def: "8071", label: "Svix webhooks" },
].map((p) => ({ ...p, value: env[p.env] ?? p.def }))

const dockerInspectErrors = []

const checks = ports.map((port) => ({ ...port, port: Number.parseInt(port.value, 10) }))

const invalid = checks.filter((c) => !Number.isInteger(c.port) || c.port <= 0 || c.port > 65535)
if (invalid.length > 0) {
  console.error("BackAI preflight failed: invalid port values.")
  for (const check of invalid) console.error(`- ${check.env}=${check.value}`)
  process.exit(1)
}

if (FIX) {
  await runFix(checks)
} else {
  await runAdvisory(checks)
}

// ─── Advisory mode (default): detect + guide, do not mutate .env ───────────

async function runAdvisory(items) {
  const duplicatePorts = findDuplicatePorts(items)
  if (duplicatePorts.length > 0) {
    console.error(
      "BackAI preflight failed: multiple services are configured to publish the same host port.",
    )
    console.error("")
    for (const group of duplicatePorts) {
      console.error(`- localhost:${group.port}`)
      for (const check of group.checks)
        console.error(`  ${check.label}: ${check.env}=${check.value}`)
    }
    console.error("")
    console.error(
      "Fix automatically with:  node scripts/preflight.mjs --fix   (or run `af-stack dev`)",
    )
    process.exit(1)
  }

  const conflicts = []
  for (const check of items) {
    if (await canBind(check.port)) continue
    if (isCurrentComposePort(check)) continue
    conflicts.push(check)
  }

  if (conflicts.length > 0) {
    console.error("BackAI preflight failed: one or more local ports are already in use.")
    console.error("")
    for (const conflict of conflicts) {
      console.error(`- ${conflict.label}: localhost:${conflict.port} (${conflict.env})`)
    }
    console.error("")
    console.error(
      "Auto-allocate free ports with:  node scripts/preflight.mjs --fix   (or run `af-stack dev`)",
    )
    console.error("BackAI does not stop other local services automatically.")
    process.exit(1)
  }

  console.log("BackAI preflight passed: required local ports are available.")
  printEndpointMap(items, env.COMPOSE_PROJECT_NAME ?? defaultProjectName())
}

// ─── Fix mode: allocate free host ports and write them to .env ─────────────

async function runFix(items) {
  // Ports we must not hand out: anything currently bound, plus ports we
  // assign during this run (so two services never collide with each other).
  const claimed = new Set()
  const overrides = {}

  for (const check of items) {
    const free = (await canBind(check.port)) && !claimed.has(check.port)
    const ownedByThisProject = !free && isCurrentComposePort(check)
    if (free || ownedByThisProject) {
      // Keep the configured port — stable across restarts.
      claimed.add(check.port)
      check.resolved = check.port
      continue
    }
    // Taken by something else: probe upward for the next free host port.
    const next = await nextFreePort(check.port + 1, claimed)
    claimed.add(next)
    check.resolved = next
    overrides[check.env] = String(next)
  }

  // Give the stack a stable, unique compose project name so container names
  // and `docker compose -p <name> logs` don't clash between apps.
  let project = env.COMPOSE_PROJECT_NAME
  if (!project || project.trim() === "") {
    project = defaultProjectName()
    overrides.COMPOSE_PROJECT_NAME = project
  }

  if (Object.keys(overrides).length > 0) {
    writeEnvOverrides(overrides)
    console.log(`BackAI preflight: allocated conflict-free ports (written to ${ENV_PATH}):`)
    for (const check of items) {
      if (overrides[check.env]) {
        console.log(`  ${check.label}: ${check.value} → ${check.resolved} (${check.env})`)
      }
    }
    if (overrides.COMPOSE_PROJECT_NAME) {
      console.log(`  Compose project: COMPOSE_PROJECT_NAME=${project}`)
    }
  } else {
    console.log("BackAI preflight: all required ports are free — no changes needed.")
  }

  printEndpointMap(items, project)
}

// ─── Shared helpers ────────────────────────────────────────────────────────

function printEndpointMap(items, project) {
  const byEnv = Object.fromEntries(items.map((c) => [c.env, c.resolved ?? c.port]))
  const P = (envKey) => byEnv[envKey]
  console.log("")
  console.log(`BackAI stack "${project}" — what runs where (host ports):`)
  console.log(`  Customer app    http://localhost:${P("AF_STACK_CUSTOMER_APP_PORT")}`)
  console.log(`  Admin console   http://localhost:${P("AF_STACK_DASHBOARD_PORT")}`)
  console.log(`  API runtime     http://localhost:${P("AF_STACK_PORT")}/api/v1`)
  console.log(`  Runtime health  http://localhost:${P("AF_STACK_PORT")}/health`)
  console.log(`  AgentField UI   http://localhost:${P("AGENTFIELD_PORT")}`)
  console.log(`  Metrics         http://localhost:${P("AF_STACK_METRICS_PORT")}/metrics`)
  console.log(`  LiteLLM         http://localhost:${P("LITELLM_PORT")}`)
  console.log(`  MinIO console   http://localhost:${P("MINIO_CONSOLE_PORT")}`)
  console.log(`  Postgres        localhost:${P("POSTGRES_PORT")}`)
  console.log("")
  console.log(
    `  Logs / control:  docker compose -p ${project} logs -f   ·   docker compose -p ${project} ps`,
  )
}

async function nextFreePort(start, claimed) {
  let p = start
  while (p <= 65535) {
    if (!claimed.has(p) && (await canBind(p))) return p
    p += 1
  }
  throw new Error("BackAI preflight: no free host port found")
}

function writeEnvOverrides(overrides) {
  let lines = []
  if (existsSync(ENV_PATH)) {
    lines = readFileSync(ENV_PATH, "utf8").split(/\r?\n/)
  } else if (existsSync(".env.example")) {
    // Seed from the documented template so the new .env keeps the full set.
    lines = readFileSync(".env.example", "utf8").split(/\r?\n/)
  }
  const remaining = { ...overrides }
  const out = lines.map((line) => {
    const m = line.match(/^\s*([A-Z0-9_]+)\s*=/)
    if (m && remaining[m[1]] !== undefined) {
      const key = m[1]
      const val = remaining[key]
      delete remaining[key]
      return `${key}=${val}`
    }
    return line
  })
  const appended = Object.entries(remaining).map(([k, v]) => `${k}=${v}`)
  if (appended.length > 0) {
    if (out.length > 0 && out[out.length - 1].trim() !== "") out.push("")
    out.push("# Auto-allocated by `af-stack dev` / preflight --fix to avoid port conflicts.")
    out.push(...appended)
  }
  let text = out.join("\n")
  if (!text.endsWith("\n")) text += "\n"
  // .env holds local dev config; ports are not secrets.
  writeFileSync(ENV_PATH, text)
}

function defaultProjectName() {
  const base = path.basename(process.cwd()).toLowerCase()
  const slug = base.replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "")
  return slug || "backai"
}

function loadEnv() {
  const values = {}
  if (existsSync(ENV_PATH)) {
    for (const line of readFileSync(ENV_PATH, "utf8").split(/\r?\n/)) {
      const trimmed = line.trim()
      if (!trimmed || trimmed.startsWith("#") || !trimmed.includes("=")) continue
      const index = trimmed.indexOf("=")
      const key = trimmed.slice(0, index).trim()
      const value = trimmed
        .slice(index + 1)
        .trim()
        .replace(/^['"]|['"]$/g, "")
      values[key] = value
    }
  }
  return { ...values, ...process.env }
}

function canBind(port) {
  return new Promise((resolve) => {
    const server = net.createServer()
    server.once("error", () => resolve(false))
    server.once("listening", () => server.close(() => resolve(true)))
    server.listen({ host: "0.0.0.0", port, exclusive: true })
  })
}

function findDuplicatePorts(items) {
  const byPort = new Map()
  for (const item of items) {
    const existing = byPort.get(item.port) ?? []
    existing.push(item)
    byPort.set(item.port, existing)
  }
  return Array.from(byPort.entries())
    .filter(([, matches]) => matches.length > 1)
    .map(([port, matches]) => ({ port, checks: matches }))
}

function isCurrentComposePort(check) {
  try {
    const output = execFileSync(
      "docker",
      ["compose", "port", check.service, String(check.target)],
      {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      },
    ).trim()
    if (!output) return false
    return output
      .split(/\r?\n/)
      .some(
        (line) => line.trim().endsWith(`:${check.port}`) || line.trim().endsWith(`]:${check.port}`),
      )
  } catch (error) {
    dockerInspectErrors.push({ check, error })
    return false
  }
}
