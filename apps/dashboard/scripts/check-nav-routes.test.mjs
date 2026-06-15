// SPDX-License-Identifier: Apache-2.0

// Route-existence guard for the operator dashboard.
//
// Validation contract: every `href` declared in src/lib/nav.ts (the single
// source of truth for the sidebar + ⌘K palette) MUST resolve to a real
// Next.js App Router page. A missing page renders a hard 404 — exactly the
// "dead link" class of bug this test exists to prevent from regressing.
//
// Intentionally dependency-free (node:test + node:fs only) so it runs under
// `pnpm -r test` with no extra devDependencies or lockfile churn. It reads
// the nav source as text rather than importing it, because nav.ts pulls in
// React/lucide modules that a plain Node runtime can't evaluate.

import assert from "node:assert/strict"
import { readdirSync, readFileSync, statSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import { test } from "node:test"

const here = dirname(fileURLToPath(import.meta.url))
const appDir = join(here, "..", "src", "app")
const navFile = join(here, "..", "src", "lib", "nav.ts")

// Extract every static `href: "/..."` literal from the nav source of truth.
// Plugin hrefs are injected at runtime via loadPlugins() and are out of scope
// for a static check.
function navHrefs() {
  const src = readFileSync(navFile, "utf8")
  const hrefs = new Set()
  for (const m of src.matchAll(/href:\s*"([^"]+)"/g)) {
    const href = m[1]
    if (href.startsWith("/")) hrefs.add(href)
  }
  return [...hrefs].sort()
}

// Walk src/app and compute the set of URL paths that have a page.{tsx,ts,jsx,js}.
// Route-group segments like (admin) are transparent in the URL; dynamic
// segments like [id] are kept verbatim (no nav href uses them today).
const PAGE_RE = /^page\.(tsx|ts|jsx|js)$/
function routePaths() {
  const routes = new Set()
  function walk(dir, urlSegments) {
    for (const entry of readdirSync(dir)) {
      const abs = join(dir, entry)
      if (statSync(abs).isDirectory()) {
        // Route groups (admin) and parallel/intercepting routes don't
        // contribute a URL segment.
        const isGroupOrSlot =
          entry.startsWith("(") || entry.startsWith("@") || entry.startsWith("_")
        walk(abs, isGroupOrSlot ? urlSegments : [...urlSegments, entry])
      } else if (PAGE_RE.test(entry)) {
        routes.add("/" + urlSegments.join("/"))
      }
    }
  }
  walk(appDir, [])
  // Root page lives at src/app/(group)/page.tsx -> "/" but the join above
  // yields "/" already for an empty segment list.
  return routes
}

test("every nav href resolves to an App Router page", () => {
  const hrefs = navHrefs()
  const routes = routePaths()

  assert.ok(hrefs.length > 0, "expected to parse at least one href from nav.ts")

  const missing = hrefs.filter((href) => !routes.has(href))
  assert.deepEqual(
    missing,
    [],
    `Nav links with no matching App Router page (dead links → 404):\n` +
      missing.map((h) => `  - ${h}`).join("\n") +
      `\n\nKnown routes:\n` +
      [...routes]
        .sort()
        .map((r) => `  - ${r}`)
        .join("\n"),
  )
})
