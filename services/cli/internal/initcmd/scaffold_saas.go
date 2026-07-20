// SPDX-License-Identifier: Apache-2.0

package initcmd

// SaaSTemplateFiles returns the relative-path -> contents map for the full
// `--template saas` scaffold: a lean Vite + React + TS customer app with an
// auth-aware API client, one domain module (notes) with a tenant-isolated
// migration, one agent, tests, env + compose references, agent-orienting
// docs, and a machine-readable capabilities.json.
//
// Exported so `af-stack test`'s scaffold-typecheck gate can materialize the
// tree (and, in particular, offline-typecheck the dependency-free API
// client) without duplicating the template.
func SaaSTemplateFiles(displayName, slug string) map[string]string {
	return saasTemplate(displayName, slug)
}

// SaaSStandaloneClientPath is the file in the saas template that is written
// to be dependency-free (no React, no external imports) so it typechecks
// with a bare `tsc` and DOM lib — the target of the offline typecheck gate.
const SaaSStandaloneClientPath = "src/api/client.ts"

func saasTemplate(displayName, slug string) map[string]string {
	files := map[string]string{
		"package.json":                            saasPackageJSON(slug),
		"index.html":                              saasIndexHTML(displayName),
		"vite.config.ts":                          saasViteConfig,
		"vitest.config.ts":                        saasVitestConfig,
		"tsconfig.json":                           saasTSConfig,
		"tsconfig.node.json":                      saasTSConfigNode,
		".env.example":                            saasEnvExample,
		".gitignore":                              saasGitignore,
		"src/main.tsx":                            saasMainTSX,
		"src/App.tsx":                             saasAppTSX(displayName),
		"src/vite-env.d.ts":                       saasViteEnvDTS,
		"src/api/client.ts":                       saasClientTS,
		"src/api/client.test.ts":                  saasClientTestTS,
		"src/pages/Login.tsx":                     saasLoginTSX(displayName),
		"src/pages/Notes.tsx":                     saasNotesTSX,
		"modules/notes/backai.module.yaml":        saasNotesManifest,
		"modules/notes/migrations/00001_init.sql": saasNotesMigration,
		"modules/notes/README.md":                 saasNotesModuleReadme,
		"agents/notes-assistant/main.py":          saasAgentMain,
		"agents/notes-assistant/requirements.txt": "agentfield>=0.1.109\npydantic>=2\n",
		"agents/notes-assistant/Dockerfile":       saasAgentDockerfile,
		"docker-compose.reference.yml":            saasComposeReference,
		"capabilities.json":                       saasCapabilitiesJSON(displayName, slug),
		"README.md":                               saasReadme(displayName, slug),
		"AGENTS.md":                               saasAgentsMd(displayName),
		"CLAUDE.md":                               saasClaudeMd(displayName),
	}
	return files
}

func saasPackageJSON(slug string) string {
	return `{
  "name": "` + slug + `",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "description": "A SaaS app on the BackAI backend (customer app + notes module + agent).",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "typecheck": "tsc --noEmit",
    "test": "vitest run"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "typescript": "^5.6.3",
    "vite": "^5.4.11",
    "vitest": "^2.1.8"
  }
}
`
}

func saasIndexHTML(displayName string) string {
	return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>` + displayName + `</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
`
}

const saasViteConfig = `// SPDX-License-Identifier: Apache-2.0
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

// The dev server proxies /api/v1 to the BackAI runtime so the browser talks
// to one origin. Point VITE_AF_STACK_URL at your runtime (default :8080).
export default defineConfig({
  plugins: [react()],
  server: {
    port: Number(process.env.PORT ?? 34000),
    proxy: {
      "/api": {
        target: process.env.VITE_AF_STACK_URL ?? "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
})
`

const saasVitestConfig = `// SPDX-License-Identifier: Apache-2.0
import { defineConfig } from "vitest/config"

export default defineConfig({
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
})
`

const saasTSConfig = `{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true
  },
  "include": ["src"]
}
`

// saasTSConfigNode is a standalone config for the Vite/Vitest config files
// (Node environment). It is intentionally NOT referenced from tsconfig.json
// — project references require the referenced project to emit, which fights
// the app's noEmit typecheck. Vite loads these via esbuild, so this exists
// only for editors that want Node typings on the config files.
const saasTSConfigNode = `{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2023"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true,
    "strict": true,
    "noEmit": true
  },
  "include": ["vite.config.ts", "vitest.config.ts"]
}
`

const saasEnvExample = `# BackAI runtime base URL the app proxies /api/v1 to.
VITE_AF_STACK_URL=http://localhost:8080
# Dev server port for the customer app.
PORT=34000
`

const saasGitignore = `node_modules/
dist/
.env
*.local
`

const saasMainTSX = `// SPDX-License-Identifier: Apache-2.0
import React from "react"
import ReactDOM from "react-dom/client"

import App from "./App"

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
`

func saasAppTSX(displayName string) string {
	return `// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from "react"

import { getToken } from "./api/client"
import { Login } from "./pages/Login"
import { Notes } from "./pages/Notes"

// The app is auth-aware: no token -> Login; token present -> the notes
// domain page. Auth, tenancy, and billing all live in the backend; the app
// only carries the bearer token it received at login.
export default function App() {
  const [authed, setAuthed] = useState<boolean>(() => getToken() !== null)

  useEffect(() => {
    document.title = "` + displayName + `"
  }, [])

  if (!authed) {
    return <Login onAuthed={() => setAuthed(true)} />
  }
  return <Notes onSignOut={() => setAuthed(false)} />
}
`
}

const saasViteEnvDTS = `// SPDX-License-Identifier: Apache-2.0
/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_AF_STACK_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
`

// saasClientTS is written WITHOUT template literals or external imports so it
// typechecks with a bare tsc + DOM lib (the offline scaffold-typecheck gate).
const saasClientTS = `// SPDX-License-Identifier: Apache-2.0

// Auth-aware API client for the BackAI backend.
//
// One base URL, everything under /api/v1, bearer auth. The token is issued
// by the backend at login and stored client-side; every call carries it.
// Zero external dependencies so it runs (and typechecks) anywhere.

export interface ApiError {
  status: number
  code: string
  message: string
  requestId?: string
}

export interface WhoAmI {
  tenantId: string
  userId?: string
  email?: string
}

export interface Note {
  id: string
  title: string
  body: string
  createdAt: string
}

const TOKEN_KEY = "af_stack_token"

export function getToken(): string | null {
  try {
    return globalThis.localStorage ? globalThis.localStorage.getItem(TOKEN_KEY) : null
  } catch {
    return null
  }
}

export function setToken(token: string): void {
  try {
    if (globalThis.localStorage) {
      globalThis.localStorage.setItem(TOKEN_KEY, token)
    }
  } catch {
    /* non-browser context: ignore */
  }
}

export function clearToken(): void {
  try {
    if (globalThis.localStorage) {
      globalThis.localStorage.removeItem(TOKEN_KEY)
    }
  } catch {
    /* ignore */
  }
}

export function baseUrl(): string {
  const env = (import.meta as unknown as { env?: { VITE_AF_STACK_URL?: string } }).env
  return (env && env.VITE_AF_STACK_URL) || "http://localhost:8080"
}

export class HttpError extends Error {
  readonly error: ApiError
  constructor(error: ApiError) {
    super("[" + error.code + "] " + error.message + " (status=" + String(error.status) + ")")
    this.name = "HttpError"
    this.error = error
  }
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = getToken()
  const headers: Record<string, string> = { accept: "application/json" }
  if (init.body) {
    headers["content-type"] = "application/json"
  }
  if (token) {
    headers["authorization"] = "Bearer " + token
  }
  const res = await fetch(baseUrl() + "/api/v1" + path, { ...init, headers })
  const text = await res.text()
  if (!res.ok) {
    let apiErr: ApiError = { status: res.status, code: "HTTP_ERROR", message: text }
    try {
      const parsed = JSON.parse(text) as { error?: Partial<ApiError> }
      if (parsed.error) {
        apiErr = {
          status: res.status,
          code: parsed.error.code || "HTTP_ERROR",
          message: parsed.error.message || text,
          requestId: parsed.error.requestId,
        }
      }
    } catch {
      /* non-JSON error body: keep the raw text */
    }
    throw new HttpError(apiErr)
  }
  return (text ? (JSON.parse(text) as T) : (null as unknown as T))
}

export interface NotesList {
  notes: Note[]
}

// The notes domain surface. Routes mount under /workload/notes per the
// module manifest; the client never hardcodes a tenant — the backend derives
// it from the bearer token.
export const api = {
  whoami(): Promise<WhoAmI> {
    return request<WhoAmI>("/auth/whoami")
  },
  listNotes(): Promise<NotesList> {
    return request<NotesList>("/workload/notes/notes")
  },
  createNote(title: string, body: string): Promise<Note> {
    return request<Note>("/workload/notes/notes", {
      method: "POST",
      body: JSON.stringify({ title: title, body: body }),
    })
  },
}
`

const saasClientTestTS = `// SPDX-License-Identifier: Apache-2.0
import { afterEach, describe, expect, it, vi } from "vitest"

import { api, HttpError } from "./client"

// A minimal in-memory localStorage + fetch fake so the client's contract is
// tested with no network and no browser.
const store = new Map<string, string>()
;(globalThis as unknown as { localStorage: Storage }).localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
  clear: () => store.clear(),
  key: () => null,
  length: 0,
} as Storage

afterEach(() => {
  vi.restoreAllMocks()
  store.clear()
})

describe("api client", () => {
  it("prefixes /api/v1 and sends the bearer token", async () => {
    store.set("af_stack_token", "tok_123")
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ notes: [] }), { status: 200 }),
    )
    vi.stubGlobal("fetch", fetchMock)

    const out = await api.listNotes()
    expect(out.notes).toEqual([])
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain("/api/v1/workload/notes/notes")
    expect((init as RequestInit).headers).toMatchObject({ authorization: "Bearer tok_123" })
  })

  it("throws a structured HttpError on a runtime error envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({ error: { code: "BUDGET_EXCEEDED", message: "over budget" } }),
          { status: 402 },
        ),
      ),
    )
    await expect(api.listNotes()).rejects.toBeInstanceOf(HttpError)
  })
})
`

func saasLoginTSX(displayName string) string {
	return `// SPDX-License-Identifier: Apache-2.0
import { useState } from "react"

import { setToken } from "../api/client"

// Login stores the bearer token the backend issues. In keyless dev mode you
// can paste any non-empty token; in SaaS mode wire this form to the backend's
// auth flow (POST /api/v1/auth/login) and store the returned token.
export function Login({ onAuthed }: { onAuthed: () => void }) {
  const [token, setTokenValue] = useState("")

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!token.trim()) return
    setToken(token.trim())
    onAuthed()
  }

  return (
    <main style={{ maxWidth: 420, margin: "10vh auto", fontFamily: "system-ui" }}>
      <h1>` + displayName + `</h1>
      <p>Sign in with an API token from your BackAI backend.</p>
      <form onSubmit={submit}>
        <input
          aria-label="API token"
          type="password"
          value={token}
          onChange={(e) => setTokenValue(e.target.value)}
          placeholder="af_..."
          style={{ width: "100%", padding: 8, boxSizing: "border-box" }}
        />
        <button type="submit" style={{ marginTop: 12, padding: "8px 16px" }}>
          Continue
        </button>
      </form>
    </main>
  )
}
`
}

const saasNotesTSX = `// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from "react"

import { api, clearToken, HttpError, type Note } from "../api/client"

// The one domain page, wired to the notes workload module over /api/v1.
export function Notes({ onSignOut }: { onSignOut: () => void }) {
  const [notes, setNotes] = useState<Note[]>([])
  const [title, setTitle] = useState("")
  const [body, setBody] = useState("")
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const out = await api.listNotes()
      setNotes(out.notes)
      setError(null)
    } catch (e) {
      setError(e instanceof HttpError ? e.error.message : String(e))
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  async function add(e: React.FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    try {
      await api.createNote(title.trim(), body.trim())
      setTitle("")
      setBody("")
      await refresh()
    } catch (e) {
      setError(e instanceof HttpError ? e.error.message : String(e))
    }
  }

  function signOut() {
    clearToken()
    onSignOut()
  }

  return (
    <main style={{ maxWidth: 640, margin: "6vh auto", fontFamily: "system-ui" }}>
      <header style={{ display: "flex", justifyContent: "space-between" }}>
        <h1>Notes</h1>
        <button onClick={signOut}>Sign out</button>
      </header>
      {error ? <p style={{ color: "crimson" }}>{error}</p> : null}
      <form onSubmit={add} style={{ display: "grid", gap: 8, marginBottom: 24 }}>
        <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title" />
        <textarea value={body} onChange={(e) => setBody(e.target.value)} placeholder="Body" rows={3} />
        <button type="submit">Add note</button>
      </form>
      <ul>
        {notes.map((n) => (
          <li key={n.id}>
            <strong>{n.title}</strong> — {n.body}
          </li>
        ))}
      </ul>
    </main>
  )
}
`

const saasNotesManifest = `id: notes
name: Notes
version: 0.1.0
description: Per-tenant Markdown notes for the customer app.

# Served out of the box in the scaffold. Flip to false to park the module
# (routes disappear on the next boot; the table and its data stay).
enabled: true
migrations: migrations

# Declarative resources: the runtime auto-generates tenant-scoped CRUD at
# /api/v1/workload/notes/notes from this block plus the SQL under
# migrations/. The backing table follows the <module>_<resource>
# convention (notes_notes); id, tenant_id, created_at and updated_at are
# managed by the runtime and must not be declared as fields.
resources:
  - name: notes
    fields:
      - name: title
        type: string
        required: true
      - name: body
        type: string
`

const saasNotesMigration = `-- Module migrations are plain SQL applied forward-only by the runtime's
-- module runner (NOT goose — a Down section here would execute during
-- apply). Roll back by shipping a new forward migration.
--
-- Tenant-owned tables MUST carry tenant_id and be guarded by FORCE row
-- level security, so isolation holds even for the migration/owner role.
-- The policy reads app.tenant_id, bound per-connection by the runtime.
-- Table name follows the <module>_<resource> convention: notes_notes.
create table if not exists notes_notes (
  id         uuid        primary key default gen_random_uuid(),
  tenant_id  uuid        not null,
  title      text        not null,
  body       text        not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists notes_notes_tenant_idx
  on notes_notes (tenant_id, created_at desc);

alter table notes_notes enable row level security;
alter table notes_notes force row level security;

create policy tenant_isolation on notes_notes
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );
`

const saasNotesModuleReadme = `# notes module

Per-tenant notes for the customer app.

- ` + "`backai.module.yaml`" + ` — declarative manifest; the runtime generates
  tenant-scoped CRUD at ` + "`/api/v1/workload/notes/notes`" + ` from it.
- ` + "`migrations/00001_init.sql`" + ` — the ` + "`notes_notes`" + ` table with
  ` + "`tenant_id`" + ` + FORCE row level security (plain SQL, forward-only —
  module migrations are not goose files).

Validate it offline: ` + "`af-stack module validate notes`" + `.
`

const saasAgentMain = `"""notes-assistant — summarize + tag agent for the notes module."""

from __future__ import annotations

import os
from typing import Any

from agentfield import Agent, AIConfig
from pydantic import BaseModel


def select_model() -> str | None:
    if os.getenv("OPENROUTER_API_KEY"):
        return "openrouter/qwen/qwen-2.5-72b-instruct"
    if os.getenv("ANTHROPIC_API_KEY"):
        return "anthropic/claude-haiku-4-5-20251001"
    if os.getenv("OPENAI_API_KEY"):
        return "openai/gpt-4o-mini"
    return None


MODEL = select_model()

app = Agent(
    node_id=os.getenv("NODE_ID", "notes-assistant"),
    version=os.getenv("AGENT_VERSION", "0.1.0"),
    ai_config=AIConfig(model=MODEL) if MODEL else None,
)


@app.reasoner(tags=["echo"])
async def echo(payload: dict[str, Any]) -> dict[str, Any]:
    return {"echoed": payload}


class Summary(BaseModel):
    tldr: str
    tags: list[str]


if MODEL is not None:

    @app.reasoner(tags=["text"])
    async def summarize(payload: dict[str, Any]) -> dict[str, Any]:
        text = payload.get("body") or payload.get("text") or ""
        if not text:
            return {"error": "missing body"}
        result = await app.ai(
            system="Summarize the note in one line and suggest up to five tags.",
            user=text,
            schema=Summary,
        )
        return result.model_dump()


if __name__ == "__main__":
    app.run()
`

const saasAgentDockerfile = `FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app
COPY requirements.txt ./
RUN pip install -r requirements.txt
COPY main.py ./

ENV NODE_ID=notes-assistant \
    PORT=8090

EXPOSE 8090
CMD ["python", "main.py"]
`

const saasComposeReference = `# Compose REFERENCE for wiring this app's agent + module into a BackAI stack.
#
# This app consumes a BackAI backend over /api/v1 — it does not run one.
# Boot the backend from your BackAI checkout with ` + "`af-stack dev`" + `. To
# vendor the notes agent + module INTO that checkout, copy:
#   agents/notes-assistant  -> apps/backend/agents/notes-assistant
#   modules/notes           -> workload-modules/notes
# and add the service below to the checkout's docker-compose.yml.
services:
  notes-assistant:
    build: ./apps/backend/agents/notes-assistant
    environment:
      NODE_ID: notes-assistant
      # Model keys are optional — the echo reasoner needs none.
      OPENROUTER_API_KEY: ${OPENROUTER_API_KEY:-}
    networks: [default]
`

func saasCapabilitiesJSON(displayName, slug string) string {
	return `{
  "$comment": "Machine-readable capabilities, constraints, and URLs for coding agents. Generated by af-stack init --template saas.",
  "platform": "BackAI",
  "cli": "af-stack",
  "app": { "name": "` + displayName + `", "slug": "` + slug + `", "kind": "saas", "consumes_backend": true },
  "env": {
    "base_url": "AF_STACK_URL",
    "api_key": "AF_STACK_API_KEY",
    "frontend_base_url": "VITE_AF_STACK_URL",
    "mode": "AF_STACK_MODE"
  },
  "api_prefix": "/api/v1",
  "urls": {
    "customer_app": "http://localhost:34000",
    "dashboard": "http://localhost:33000",
    "api": "http://localhost:8080/api/v1",
    "health": "http://localhost:8080/health",
    "ready": "http://localhost:8080/ready"
  },
  "capabilities": {
    "agents": { "invoke": "POST /api/v1/agents/{node}.{reasoner}", "list": "GET /api/v1/agents" },
    "llm": { "openai_compatible": "/api/v1/llm/chat/completions", "note": "every model call MUST go through the gateway" },
    "auth": { "whoami": "GET /api/v1/auth/whoami" },
    "storage": "/api/v1/storage/*",
    "jobs": "/api/v1/jobs",
    "billing": { "entitlements": "GET /api/v1/billing/entitlements", "meter": "POST /api/v1/billing/meter", "checkout": "POST /api/v1/billing/checkout" },
    "secrets": "/api/v1/secrets"
  },
  "constraints": [
    "Every model call goes through the gateway at /api/v1/llm/* — never call a provider directly.",
    "Tenant identity comes from the API key or session, enforced by Postgres RLS. Never trust a client-supplied tenant id.",
    "Tenant-owned tables MUST have a tenant_id column and FORCE row level security with a policy on app.tenant_id.",
    "Do not reinvent auth, tenancy, billing, storage, jobs, secrets, or webhooks — call the platform primitives.",
    "Keep real credentials in .env (gitignored) or the secrets vault; never hardcode secrets."
  ],
  "modules": [
    { "id": "notes", "manifest": "modules/notes/backai.module.yaml", "routes_prefix": "/workload/notes" }
  ],
  "agents": [
    { "node_id": "notes-assistant", "reasoners": ["echo", "summarize"], "path": "agents/notes-assistant" }
  ],
  "validate": { "module": "af-stack module validate notes", "agent": "af-stack agent validate notes-assistant", "all": "af-stack test" }
}
`
}

func saasReadme(displayName, slug string) string {
	f := fence
	return "# " + displayName + `

A SaaS app on the **BackAI** backend: a lean Vite + React + TypeScript
customer app, one domain module (` + "`notes`" + `), and one agent
(` + "`notes-assistant`" + `). One backend, one base URL (` + "`/api/v1`" + `),
with auth, tenancy, and billing owned by the platform.

## Quickstart

1. Boot a backend from your BackAI checkout: ` + "`af-stack dev`" + `
2. Configure this app: ` + "`cp .env.example .env`" + ` and set
   ` + "`VITE_AF_STACK_URL`" + `.
3. Run it:

` + f + `sh
npm install
npm run dev        # customer app on http://localhost:34000
npm test           # vitest: the API-client contract
npm run typecheck  # tsc --noEmit
` + f + `

## Layout

- ` + "`src/`" + ` — the customer app (` + "`api/client.ts`" + ` is the auth-aware,
  zero-dependency API client; ` + "`pages/`" + ` has Login + the Notes page).
- ` + "`modules/notes/`" + ` — the notes workload module (manifest + tenant-isolated migration).
- ` + "`agents/notes-assistant/`" + ` — an AgentField agent (echo + summarize).
- ` + "`capabilities.json`" + ` — machine-readable capabilities/constraints/URLs for coding agents.

## Validate

` + f + `sh
af-stack test                          # all gates (manifests, migrations, typecheck, sdk smoke)
af-stack module validate notes         # just the notes module
af-stack agent validate notes-assistant
` + f + `

See ` + "`AGENTS.md`" + ` / ` + "`CLAUDE.md`" + ` for how to build on this scaffold.
Slug: ` + "`" + slug + "`" + `.
`
}

func saasAgentsMd(displayName string) string {
	return "# AGENTS.md — " + displayName + ` (BackAI saas scaffold)

This project was scaffolded by ` + "`af-stack init --template saas`" + `. It
**consumes** a BackAI backend over HTTP; it is not a fork of the platform.
` + "`capabilities.json`" + ` is the machine-readable index of URLs, capabilities,
and constraints — read it first.

## Where things go
- Customer app UI/logic: ` + "`src/`" + ` (React + TS). The API client is
  ` + "`src/api/client.ts`" + ` — auth-aware, everything under ` + "`/api/v1`" + `.
- Domain backend: ` + "`modules/notes/`" + ` (a workload module — manifest +
  migrations). Add tables with ` + "`tenant_id`" + ` + FORCE RLS.
- Agents: ` + "`agents/`" + ` (AgentField Python agents).

## Invariants (do not break)
- Every model call goes through the gateway at ` + "`/api/v1/llm/*`" + `.
- Tenant identity comes from the API key/session — never a query param.
- Tenant-owned tables MUST have ` + "`tenant_id`" + ` + FORCE row level security.
- Don't reinvent auth, tenancy, billing, storage, jobs, secrets, webhooks.

## Before you ship
- ` + "`npm run typecheck && npm test`" + ` in the app.
- ` + "`af-stack test`" + ` to run the fork gates (manifests, migrations incl.
  RLS, scaffold typecheck, sdk smoke).
`
}

func saasClaudeMd(displayName string) string {
	return "# " + displayName + ` — an app on the BackAI backend

This project was scaffolded by ` + "`af-stack init --template saas`" + `. It
**CONSUMES** a BackAI backend over ` + "`/api/v1`" + ` — it is not a fork of the
stack itself. Start with ` + "`capabilities.json`" + ` (machine-readable) and
` + "`AGENTS.md`" + `.

## How to talk to the backend
- One base URL, everything under ` + "`/api/v1`" + `, bearer auth. The app's
  client is ` + "`src/api/client.ts`" + `.
- Useful calls: ` + "`GET /api/v1/auth/whoami`" + `, ` + "`GET /api/v1/agents`" + `,
  ` + "`GET/POST /api/v1/workload/notes/notes`" + `.

## Ground rules
- The backend owns auth, tenancy, billing, and secrets — call it, don't
  reimplement it here.
- Tenant-owned tables need ` + "`tenant_id`" + ` + FORCE RLS (see
  ` + "`modules/notes/migrations`" + `).
- Keep real credentials in ` + "`.env`" + ` (gitignored), never in code.
`
}
