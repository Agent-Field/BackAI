// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/backai/services/cli/internal/buildinfo"
	"github.com/Agent-Field/backai/services/cli/internal/output"
	"github.com/Agent-Field/backai/services/cli/internal/starter"
)

// runScaffold implements the npm-like `af-stack init <name>`: it creates a
// NEW directory containing a starter project that *consumes* the AF Stack
// backend. Run dispatches here whenever init is given a positional project
// name; without one, init keeps its in-checkout behavior (rebrand and the
// --template scaffolds inside a fork).
//
// The default template has zero runtime dependencies — it talks to the
// backend with Node's built-in fetch — so `npm install && npm start` works
// immediately, before anything is published to a registry.
func runScaffold(args []string, stdout, stderr io.Writer) error {
	// Go's flag package stops at the first positional, so the caller pulls
	// the leading project name off before we parse flags.
	name := strings.TrimSpace(args[0])
	args = args[1:]

	fs := flag.NewFlagSet("af-stack init <name>", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".", "parent directory to create the project in")
	template := fs.String("template", "node", "starter template: node | saas (the coding-agent template is checkout-only: run af-stack init --template coding-agent inside a BackAI checkout)")
	force := fs.Bool("force", false, "scaffold into an existing non-empty directory")
	asJSON := fs.Bool("json", false, "emit the created file list as JSON")
	if err := fs.Parse(args); err != nil {
		return output.Usage("init: %v", err)
	}
	if fs.NArg() > 0 {
		return output.Usage("init: unexpected argument %q", fs.Arg(0))
	}
	if name == "" {
		return output.Usage("init: a project name is required (af-stack init <name>)")
	}

	tmpl := strings.TrimSpace(strings.ToLower(*template))
	var files map[string]string
	switch tmpl {
	case "", "node":
		tmpl = "node"
		files = nodeTemplate(name, slugify(name))
	case "saas":
		files = SaaSTemplateFiles(name, slugify(name))
	default:
		return output.Usage("init: unknown template %q for a new project (available: node, saas); to scaffold the coding-agent hero template inside an AF Stack checkout, run `af-stack init --template coding-agent` without a project name", *template)
	}

	// Every scaffold carries its own backend: the compose stack that boots
	// BackAI from the release images matching this CLI. `af-stack dev` (and
	// `npm start`, through its prestart hook) runs it in place.
	for rel, contents := range starter.BackendFiles(buildinfo.Version) {
		files[rel] = contents
	}

	projectSlug := slugify(name)
	target := filepath.Join(*dir, projectSlug)

	if err := ensureTargetDir(target, *force); err != nil {
		return err
	}

	written := make([]string, 0, len(files))
	for rel, contents := range files {
		path := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return output.Fail("init: create %s: %v", rel, err)
		}
		// #nosec G306 -- scaffolded project source files, not secrets.
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return output.Fail("init: write %s: %v", rel, err)
		}
		written = append(written, rel)
	}
	sort.Strings(written)

	machine := map[string]any{
		"project":  projectSlug,
		"template": tmpl,
		"target":   target,
		"files":    written,
	}
	return output.Result(stdout, *asJSON, machine, func(w io.Writer) error {
		fmt.Fprintf(w, "Created %s — a new %s app with its own BackAI backend (%d files).\n\n", target, tmpl, len(written))
		fmt.Fprintln(w, "Next steps (Docker must be running):")
		fmt.Fprintf(w, "  cd %s\n", target)
		if tmpl == "saas" {
			fmt.Fprintln(w, "  npm install && npm run dev    # boots the bundled backend, then the app on :34000")
			fmt.Fprintln(w, "  af-stack test                 # run the fork gates")
		} else {
			fmt.Fprintln(w, "  npm install && npm start      # boots the bundled backend, then runs src/index.mjs")
		}
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "The backend is docker-compose.yml in that directory: `af-stack dev` starts it")
		fmt.Fprintln(w, "(that is what the npm hook runs), `docker compose down` stops it. Ports are")
		fmt.Fprintln(w, "allocated into .env when the defaults (8080, 8081, 5432, ...) are busy.")
		return nil
	})
}

// ensureTargetDir creates target, refusing to clobber a non-empty directory
// unless force is set.
func ensureTargetDir(target string, force bool) error {
	info, err := os.Stat(target)
	switch {
	case err == nil && info.IsDir():
		if force {
			return nil
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return readErr
		}
		if len(entries) > 0 {
			return fmt.Errorf("init: %s already exists and is not empty (use --force)", target)
		}
		return nil
	case err == nil:
		return fmt.Errorf("init: %s already exists and is not a directory", target)
	case os.IsNotExist(err):
		return os.MkdirAll(target, 0o755)
	default:
		return err
	}
}

// fence is a triple-backtick, factored out so the README template can be a Go
// raw string literal without colliding on backticks.
const fence = "```"

// nodeTemplate returns the relative-path -> contents map for the default
// zero-dependency Node starter.
func nodeTemplate(displayName, slug string) map[string]string {
	pkg := `{
  "name": "` + slug + `",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "description": "An app built on the AF Stack backend.",
  "scripts": {
    "prestart": "af-stack dev",
    "start": "node src/index.mjs",
    "backend": "af-stack dev",
    "backend:stop": "docker compose down"
  }
}
`

	index := `// ` + displayName + ` — a starter app on the AF Stack backend.
//
// AF Stack exposes ONE backend behind ONE base URL, with Supabase-shaped
// namespaces (agents, llm, storage, billing, ...) and AI as a first-class
// primitive. This starter talks to it with Node's built-in fetch — zero
// dependencies. For a typed client, install @af-stack/sdk and swap the api()
// helper for ` + "`suite.agents.call(...)`" + `, ` + "`suite.llm.chat(...)`" + `, etc.

import { readFileSync } from "node:fs";

// Load ./.env (create it with ` + "`cp .env.example .env`" + `) without a dependency.
// Real environment variables win over the file.
try {
  for (const line of readFileSync(new URL("../.env", import.meta.url), "utf8").split(/\r?\n/)) {
    const m = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$/);
    if (m && !(m[1] in process.env)) process.env[m[1]] = m[2].replace(/^(['"])(.*)\1$/, "$2");
  }
} catch {
  // no .env yet — defaults below apply
}

// The runtime's base URL: what ` + "`af-stack dev`" + ` prints as "API runtime". A pasted
// ".../api/v1" suffix is tolerated.
const BASE_URL = (process.env.AF_STACK_URL ?? "http://localhost:8080")
  .replace(/\/+$/, "")
  .replace(/\/api\/v1$/, "");
const API_KEY = process.env.AF_STACK_API_KEY ?? "";

async function api(path, { method = "GET", body } = {}) {
  const res = await fetch(BASE_URL + "/api/v1" + path, {
    method,
    headers: {
      "content-type": "application/json",
      ...(API_KEY ? { authorization: "Bearer " + API_KEY } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const err = new Error(method + " " + path + " -> " + res.status + " " + (await res.text()));
    err.status = res.status;
    throw err;
  }
  return res.status === 204 ? null : res.json();
}

function fail(what, detail) {
  console.error("\n" + what);
  if (detail) console.error("Details: " + detail);
  console.error(` + "`" + `
This app carries its own backend (docker-compose.yml). Start it with
'af-stack dev' in this directory — 'npm start' does that first — and it
writes the runtime's URL into .env as AF_STACK_URL (another port than 8080
when 8080 is busy). Docker must be running.` + "`" + `);
  process.exit(1);
}

// Is there a BackAI runtime at BASE_URL? Its /health answers {"status":"alive"}.
// Anything else on that port (an AgentField control plane, another dev server)
// answers differently, and that is the usual failure when :8080 was busy.
async function checkRuntime() {
  let res;
  try {
    res = await fetch(BASE_URL + "/health");
  } catch (err) {
    const cause = err.cause;
    fail("Nothing is listening at " + BASE_URL + ".",
      cause?.code ?? cause?.errors?.[0]?.code ?? cause?.message ?? err.message);
  }
  const text = await res.text();
  let body = null;
  try { body = JSON.parse(text); } catch { /* not JSON */ }
  if (!res.ok || !body || (body.status !== "alive" && body.status !== "ready")) {
    fail("Something is listening at " + BASE_URL + ", but it is not a BackAI runtime.",
      "GET /health -> " + res.status + " " + text.slice(0, 160));
  }
}

async function main() {
  console.log("Talking to BackAI at " + BASE_URL);
  await checkRuntime();
  try {
    // The simplest call that proves the wiring: list the registered agents.
    const listing = await api("/agents");
    const agents = Array.isArray(listing) ? listing : listing?.agents ?? [];
    console.log("Registered agents:", agents.map((a) => a.node_id ?? a).join(", ") || "(none yet)");

    // Call the bundled demo agent's no-key reasoner: it echoes its input back
    // through the gateway -> AgentField -> agent round trip.
    if (agents.some((a) => (a.node_id ?? a) === "supportdesk")) {
      const reply = await api("/agents/supportdesk.echo", {
        method: "POST",
        body: { input: { payload: { message: "hello from " + BASE_URL } } },
      });
      const echoed = reply?.result?.echoed ?? reply?.output?.echoed ?? reply?.echoed ?? reply;
      console.log("Echo agent replied: " + JSON.stringify(echoed));
    } else {
      console.log("The supportdesk agent has not registered yet; run again in a few seconds.");
    }

    // Example: ask the OpenAI-compatible LLM gateway for a one-liner.
    // const reply = await api("/llm/chat/completions", { method: "POST", body: {
    //   model: "gpt-4o-mini",
    //   messages: [{ role: "user", content: "Say hi in five words." }],
    // }});
    // console.log(reply.choices?.[0]?.message?.content);
  } catch (err) {
    if (err.status === 401 || err.status === 403) {
      fail("The runtime has auth on and rejected this app's key.",
        err.message + "\nMint one with 'af-stack keys create' (operator key needed) and set AF_STACK_API_KEY in .env.");
    }
    fail("The runtime answered, but the call failed.", err.message);
  }
}

main();
`

	env := `# BackAI runtime base URL. ` + "`af-stack dev`" + ` (run by ` + "`npm start`" + `) boots the bundled
# backend and writes the real value here — another port than 8080 when 8080
# is busy — so you normally never edit this line.
AF_STACK_URL=http://localhost:8080
# Bearer token — required when the runtime has auth enabled
AF_STACK_API_KEY=

# The bundled backend (docker-compose.yml) reads this file too:
#   AF_STACK_VERSION=<tag>        run another BackAI release
#   OPENROUTER_API_KEY=...        (or OPENAI/ANTHROPIC/...) turns demo mode off
#   AF_STACK_MODE=personal        no login, no paywall
#   AF_STACK_PORT=..., AGENTFIELD_PORT=..., POSTGRES_PORT=...   host ports
`

	gitignore := `node_modules/
.env
`

	readme := "# " + displayName + `

An app built on the **AF Stack** backend — one backend, one base URL, with
Supabase-shaped namespaces (agents, llm, storage, billing, …) and AI as a
first-class primitive.

## Quickstart

Docker (with Compose) must be running. Then:

` + fence + `sh
npm install && npm start
` + fence + `

` + "`npm start`" + ` first runs ` + "`af-stack dev`" + `, which boots the bundled backend in
` + "`docker-compose.yml`" + ` — Postgres, MinIO, LiteLLM, the AgentField control plane,
the BackAI runtime, the operator dashboard, and the ` + "`supportdesk`" + ` demo agent —
waits for the runtime, and writes its URL into ` + "`.env`" + ` as ` + "`AF_STACK_URL`" + `.
Then ` + "`src/index.mjs`" + ` lists the registered agents and calls ` + "`supportdesk.echo`" + `.
The first run pulls the images; later runs are a no-op when the backend is up.

- Operator dashboard: http://localhost:33000 (` + "`operator@af-stack.local`" + ` / ` + "`changeme123`" + `)
- API: http://localhost:8080/api/v1 (` + "`af-stack dev`" + ` picks other ports when these are busy and records them in ` + "`.env`" + `)
- Stop: ` + "`npm run backend:stop`" + ` (` + "`docker compose down`" + `; add ` + "`-v`" + ` to drop the data)
- Live model calls: set ` + "`OPENROUTER_API_KEY`" + ` (or another provider key) in ` + "`.env`" + ` and restart

## What's here

- ` + "`src/index.mjs`" + ` — talks to the backend with the built-in ` + "`fetch`" + ` (zero deps).
- ` + "`docker-compose.yml`" + ` + ` + "`backend/`" + ` — the bundled backend, pinned to the BackAI release that scaffolded this app.
- ` + "`.env.example`" + ` — the app's env vars; the backend reads the same file.

## Upgrade to the typed SDK

Install ` + "`@af-stack/sdk`" + ` for a typed, autocompleted client:

` + fence + `ts
import { suite } from "@af-stack/sdk";

const agents = await suite.agents.list();
const reply = await suite.llm.chat({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Say hi in five words." }],
});
` + fence + `
`

	claudeMd := "# " + displayName + ` — an app on the AF Stack backend

This project was scaffolded by ` + "`af-stack init " + slug + "`" + `. It CONSUMES an
AF Stack backend over HTTP — it is not a fork of the stack itself. The backend
it consumes is bundled: ` + "`docker-compose.yml`" + ` boots BackAI from the release
images, ` + "`af-stack dev`" + ` (run by ` + "`npm start`" + `) starts it and writes its URL
into ` + "`.env`" + `, ` + "`docker compose down`" + ` stops it.

## How to talk to the backend
- One base URL (` + "`AF_STACK_URL`" + `), everything under ` + "`/api/v1`" + `, bearer auth
  via ` + "`AF_STACK_API_KEY`" + `. See ` + "`src/index.mjs`" + ` for the zero-dep pattern.
- Useful early calls: ` + "`GET /api/v1/auth/whoami`" + ` (who am I / which tenant),
  ` + "`GET /api/v1/agents`" + ` (what can I call), ` + "`POST /api/v1/billing/meter`" + `
  (record usage events).
- For a typed client, install ` + "`@af-stack/sdk`" + ` (TypeScript) or ` + "`af-stack`" + `
  (Python) and use the ` + "`suite.*`" + ` namespaces — ` + "`suite.agents`" + `,
  ` + "`suite.llm`" + `, ` + "`suite.storage`" + `, ` + "`suite.billing`" + `, ` + "`suite.auth`" + `.

## Billing: never build it yourself
The backend ships a turnkey pricing engine — plan catalog, hosted Stripe
checkout, entitlements, and hard budget enforcement (402s). Your job is
three calls, not a billing system:
- gate paid features with ` + "`GET /api/v1/billing/entitlements`" + ` (plan +
  entitlements + current usage in one read),
- record usage with ` + "`POST /api/v1/billing/meter`" + `,
- send upgrades to ` + "`POST /api/v1/billing/checkout`" + ` (returns the hosted
  checkout URL; in keyless dev mode the plan applies instantly).
Plans and Stripe keys are configured by the operator in the dashboard
(Platform → Billing) — do not hardcode prices or plan logic in app code.

## Ground rules
- The backend owns auth, tenancy, billing, and secrets — call it, don't
  reimplement it here.
- Backend not running? ` + "`af-stack dev`" + ` in this directory (needs Docker).
- Keep real credentials in ` + "`.env`" + ` (gitignored), never in code.
`

	return map[string]string{
		"package.json":  pkg,
		"src/index.mjs": index,
		".env.example":  env,
		".gitignore":    gitignore,
		"README.md":     readme,
		"CLAUDE.md":     claudeMd,
	}
}
