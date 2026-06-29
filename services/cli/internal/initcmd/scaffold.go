// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// runScaffold implements the canonical, npm-like `af-stack init <name>`: it
// creates a NEW directory containing a starter project that *consumes* the AF
// Stack backend (as opposed to `--brand`, which re-themes an existing fork).
//
// The default template has zero runtime dependencies — it talks to the
// backend with Node's built-in fetch — so `npm install && npm start` works
// immediately, before anything is published to a registry.
func runScaffold(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	// Go's flag package stops at the first positional, so pull a leading
	// project-name argument out before parsing the flags. This lets both
	// `af-stack init my-app --template node` and `af-stack init --name my-app`
	// work.
	positional := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional = strings.TrimSpace(args[0])
		args = args[1:]
	}

	fs := flag.NewFlagSet("af-stack init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nameFlag := fs.String("name", "", "project name (also accepted as a positional argument)")
	dir := fs.String("dir", ".", "parent directory to create the project in")
	template := fs.String("template", "node", "starter template (node)")
	force := fs.Bool("force", false, "scaffold into an existing non-empty directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("init: unexpected argument %q", fs.Arg(0))
	}

	// Name comes from the positional arg, then --name, then an interactive prompt.
	name := positional
	if name == "" {
		name = strings.TrimSpace(*nameFlag)
	}
	if name == "" {
		prompted, err := prompt(stdin, stdout, "Project name")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(prompted)
	}
	if name == "" {
		return errors.New("init: a project name is required (af-stack init <name>)")
	}

	if *template != "node" {
		return fmt.Errorf("init: unknown template %q (available: node)", *template)
	}

	slug := slugify(name)
	target := filepath.Join(*dir, slug)

	if err := ensureTargetDir(target, *force); err != nil {
		return err
	}

	files := nodeTemplate(name, slug)
	for rel, contents := range files {
		path := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("init: create %s: %w", rel, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("init: write %s: %w", rel, err)
		}
	}

	fmt.Fprintf(stdout, "Created %s — a new app on the AF Stack backend.\n\n", target)
	fmt.Fprintln(stdout, "Next steps:")
	fmt.Fprintf(stdout, "  cd %s\n", target)
	fmt.Fprintln(stdout, "  cp .env.example .env    # set AF_STACK_URL / AF_STACK_API_KEY")
	fmt.Fprintln(stdout, "  npm install && npm start")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "No backend yet? Start one from your AF Stack checkout with: af-stack dev")
	return nil
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

	return map[string]string{
		"package.json":  pkg,
		"src/index.mjs": index,
		".env.example":  env,
		".gitignore":    gitignore,
		"README.md":     readme,
	}
}
