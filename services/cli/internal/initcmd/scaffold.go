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

	"github.com/Agent-Field/backai/services/cli/internal/output"
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
		fmt.Fprintf(w, "Created %s — a new %s app on the AF Stack backend (%d files).\n\n", target, tmpl, len(written))
		fmt.Fprintln(w, "Next steps:")
		fmt.Fprintf(w, "  cd %s\n", target)
		if tmpl == "saas" {
			fmt.Fprintln(w, "  cp .env.example .env    # set VITE_AF_STACK_URL")
			fmt.Fprintln(w, "  npm install && npm run dev")
			fmt.Fprintln(w, "  af-stack test           # run the fork gates")
		} else {
			fmt.Fprintln(w, "  cp .env.example .env    # set AF_STACK_URL / AF_STACK_API_KEY")
			fmt.Fprintln(w, "  npm install && npm start")
		}
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "No backend yet? Start one from your AF Stack checkout with: af-stack dev")
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
    "start": "node src/index.mjs"
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

const BASE_URL = process.env.AF_STACK_URL ?? "http://localhost:8080";
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
    throw new Error(method + " " + path + " -> " + res.status + " " + (await res.text()));
  }
  return res.status === 204 ? null : res.json();
}

async function main() {
  console.log("Talking to AF Stack at " + BASE_URL);
  try {
    // The simplest call that proves the wiring: list available agents.
    const agents = await api("/agents");
    console.log("Available agents:", agents);

    // Example: ask the OpenAI-compatible LLM gateway for a one-liner.
    // const reply = await api("/llm/chat/completions", { method: "POST", body: {
    //   model: "gpt-4o-mini",
    //   messages: [{ role: "user", content: "Say hi in five words." }],
    // }});
    // console.log(reply.choices?.[0]?.message?.content);
  } catch (err) {
    console.error("\nCould not reach the backend. Start it with 'af-stack dev',");
    console.error("then set AF_STACK_URL (and AF_STACK_API_KEY if auth is on).\n");
    console.error("Details:", err.message);
    process.exitCode = 1;
  }
}

main();
`

	env := `# AF Stack runtime base URL
AF_STACK_URL=http://localhost:8080
# Bearer token — required when the runtime has auth enabled
AF_STACK_API_KEY=
`

	gitignore := `node_modules/
.env
`

	readme := "# " + displayName + `

An app built on the **AF Stack** backend — one backend, one base URL, with
Supabase-shaped namespaces (agents, llm, storage, billing, …) and AI as a
first-class primitive.

## Quickstart

1. Start a backend (from your AF Stack checkout): ` + "`af-stack dev`" + `
2. Configure this app: ` + "`cp .env.example .env`" + ` and set ` + "`AF_STACK_URL`" + ` /
   ` + "`AF_STACK_API_KEY`" + `.
3. Run it: ` + "`npm install && npm start`" + `

## What's here

- ` + "`src/index.mjs`" + ` — talks to the backend with the built-in ` + "`fetch`" + ` (zero deps).
- ` + "`.env.example`" + ` — the two env vars the app needs.

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
AF Stack backend over HTTP — it is not a fork of the stack itself.

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
- No backend running? Start one from an AF Stack checkout with ` + "`af-stack dev`" + `.
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
