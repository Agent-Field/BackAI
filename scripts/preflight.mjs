#!/usr/bin/env node

import { execFileSync } from "node:child_process"
import { existsSync, readFileSync } from "node:fs"
import net from "node:net"

const env = loadEnv()

const ports = [
  {
    service: "postgres",
    target: 5432,
    env: "POSTGRES_PORT",
    value: env.POSTGRES_PORT ?? "5432",
    label: "Postgres",
    example: "POSTGRES_PORT=55432",
  },
  {
    service: "minio",
    target: 9000,
    env: "MINIO_PORT",
    value: env.MINIO_PORT ?? "9000",
    label: "MinIO API",
    example: "MINIO_PORT=39000",
  },
  {
    service: "minio",
    target: 9001,
    env: "MINIO_CONSOLE_PORT",
    value: env.MINIO_CONSOLE_PORT ?? "9001",
    label: "MinIO console",
    example: "MINIO_CONSOLE_PORT=39001",
  },
  {
    service: "litellm",
    target: 4000,
    env: "LITELLM_PORT",
    value: env.LITELLM_PORT ?? "4000",
    label: "LiteLLM",
    example: "LITELLM_PORT=34002",
  },
  {
    service: "agentfield",
    target: 8080,
    env: "AGENTFIELD_PORT",
    value: env.AGENTFIELD_PORT ?? "8081",
    label: "AgentField control plane",
    example: "AGENTFIELD_PORT=38081",
  },
  {
    service: "runtime",
    target: 8080,
    env: "AF_STACK_PORT",
    value: env.AF_STACK_PORT ?? "8080",
    label: "BackAI runtime",
    example: "AF_STACK_PORT=38080",
  },
  {
    service: "runtime",
    target: 9090,
    env: "AF_STACK_METRICS_PORT",
    value: env.AF_STACK_METRICS_PORT ?? "9090",
    label: "BackAI metrics",
    example: "AF_STACK_METRICS_PORT=39090",
  },
  {
    service: "dashboard",
    target: 3000,
    env: "AF_STACK_DASHBOARD_PORT",
    value: env.AF_STACK_DASHBOARD_PORT ?? "33000",
    label: "Admin dashboard",
    example: "AF_STACK_DASHBOARD_PORT=33001",
  },
  {
    service: "customer-app",
    target: 3000,
    env: "AF_STACK_CUSTOMER_APP_PORT",
    value: env.AF_STACK_CUSTOMER_APP_PORT ?? "34000",
    label: "Customer app",
    example: "AF_STACK_CUSTOMER_APP_PORT=34001",
  },
  {
    service: "svix-server",
    target: 8071,
    env: "SVIX_PORT",
    value: env.SVIX_PORT ?? "8071",
    label: "Svix webhooks",
    example: "SVIX_PORT=38071",
  },
]

const checks = ports.map((port) => ({
  ...port,
  port: Number.parseInt(port.value, 10),
}))

const invalid = checks.filter((check) => !Number.isInteger(check.port) || check.port <= 0 || check.port > 65535)
if (invalid.length > 0) {
  console.error("BackAI preflight failed: invalid port values.")
  for (const check of invalid) {
    console.error(`- ${check.env}=${check.value}`)
  }
  process.exit(1)
}

const duplicatePorts = findDuplicatePorts(checks)
if (duplicatePorts.length > 0) {
  console.error("BackAI preflight failed: multiple BackAI services are configured to publish the same host port.")
  console.error("")
  for (const group of duplicatePorts) {
    console.error(`- localhost:${group.port}`)
    for (const check of group.checks) {
      console.error(`  ${check.label}: ${check.env}=${check.value}`)
    }
    console.error(`  Choose a different port, for example: ${group.checks[0].example} docker compose up`)
  }
  console.error("")
  console.error("BackAI does not stop other local services automatically. Pick unused host ports with env overrides.")
  process.exit(1)
}

const conflicts = []
for (const check of checks) {
  if (await canBind(check.port)) {
    continue
  }
  if (isCurrentComposePort(check)) {
    continue
  }
  conflicts.push(check)
}

if (conflicts.length > 0) {
  console.error("BackAI preflight failed: one or more local ports are already in use.")
  console.error("")
  for (const conflict of conflicts) {
    console.error(`- ${conflict.label}: localhost:${conflict.port} (${conflict.env})`)
    console.error(`  Try: ${conflict.example} docker compose up`)
  }
  console.error("")
  console.error("BackAI does not stop other local services automatically. Change the port override or stop the conflicting service yourself.")
  process.exit(1)
}

console.log("BackAI preflight passed: required local ports are available.")

function loadEnv() {
  const values = {}
  if (existsSync(".env")) {
    const lines = readFileSync(".env", "utf8").split(/\r?\n/)
    for (const line of lines) {
      const trimmed = line.trim()
      if (!trimmed || trimmed.startsWith("#") || !trimmed.includes("=")) {
        continue
      }
      const index = trimmed.indexOf("=")
      const key = trimmed.slice(0, index).trim()
      const value = trimmed.slice(index + 1).trim().replace(/^['"]|['"]$/g, "")
      values[key] = value
    }
  }
  return { ...values, ...process.env }
}

function canBind(port) {
  return new Promise((resolve) => {
    const server = net.createServer()
    server.once("error", () => resolve(false))
    server.once("listening", () => {
      server.close(() => resolve(true))
    })
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
    const output = execFileSync("docker", ["compose", "port", check.service, String(check.target)], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim()
    if (!output) {
      return false
    }
    return output
      .split(/\r?\n/)
      .some((line) => line.trim().endsWith(`:${check.port}`) || line.trim().endsWith(`]:${check.port}`))
  } catch {
    return false
  }
}
