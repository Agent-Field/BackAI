// SPDX-License-Identifier: Apache-2.0

// Package jobcmd implements `af-stack job new` — a scaffolder for the
// pull-based background-worker pattern (PRD R3):
//
//	af-stack job new resize-image             scaffold jobs/resize-image.py
//	af-stack job new resize-image --lang ts   scaffold jobs/resize-image.ts
//
// The generated file registers ONE job kind (the slugified name), wires a
// handler that receives (payload, ctx), and blocks in a run loop that
// leases + executes work over the runtime's worker protocol. A worker
// authenticates with a tenant API key carrying the `jobs:work` scope — the
// next-steps output printed after scaffolding spells that out.
//
// The command is a pure local scaffold: it makes no network call, so it
// works offline and needs no runtime. --json emits {"created": [paths]}.
package jobcmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// Run dispatches `af-stack job <subcommand>`.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return output.Usage("job: subcommand required: new")
	}
	switch args[0] {
	case "new":
		wd, err := os.Getwd()
		if err != nil {
			return output.Fail("job new: %v", err)
		}
		return runNew(wd, args[1:], stdout, stderr)
	default:
		return output.Usage("job: unknown subcommand %q (want new)", args[0])
	}
}

// newResult is the stable --json schema for `af-stack job new`.
type newResult struct {
	Created []string `json:"created"`
}

// runNew scaffolds a worker file under baseDir/jobs. baseDir is injected so
// tests can target a temp dir without chdir.
func runNew(baseDir string, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack job new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lang := fs.String("lang", "py", "worker language: py | ts")
	asJSON := fs.Bool("json", false, "emit the created paths as JSON")
	positionals, err := output.ParseArgs(fs, args)
	if err != nil {
		return output.Usage("job new: %v", err)
	}
	if len(positionals) != 1 {
		return output.Usage("job new: exactly one <name> is required (af-stack job new <name> [--lang py|ts])")
	}
	name := positionals[0]
	if strings.HasPrefix(name, "-") {
		return output.Usage("job new: <name> must not start with '-' (got %q)", name)
	}
	slug := slugify(name)
	if slug == "" {
		return output.Invalid("job new: name %q contains no usable characters (want [a-z0-9-])", name)
	}

	ext, contents, err := render(*lang, slug)
	if err != nil {
		return err
	}

	rel := "jobs/" + slug + ext
	abs := filepath.Join(baseDir, "jobs", slug+ext)
	if _, statErr := os.Stat(abs); statErr == nil {
		return output.Invalid("job new: %s already exists (refusing to overwrite)", rel)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return output.Fail("job new: create jobs dir: %v", err)
	}
	// #nosec G306 -- scaffolded worker source, world-readable (not a secret).
	if err := os.WriteFile(abs, []byte(contents), 0o644); err != nil {
		return output.Fail("job new: write %s: %v", rel, err)
	}

	return output.Result(stdout, *asJSON, newResult{Created: []string{rel}}, func(w io.Writer) error {
		return writeNextSteps(w, *lang, slug, rel)
	})
}

// render returns the file extension and rendered contents for a language.
func render(lang, slug string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "py", "python":
		return ".py", fill(pyTemplate, slug), nil
	case "ts", "typescript":
		return ".ts", fill(tsTemplate, slug), nil
	default:
		return "", "", output.Usage("job new: --lang must be 'py' or 'ts' (got %q)", lang)
	}
}

// fill substitutes {{KIND}} in a template with the slug.
func fill(tmpl, slug string) string {
	return strings.ReplaceAll(tmpl, "{{KIND}}", slug)
}

// writeNextSteps prints the run instructions + the jobs:work requirement.
func writeNextSteps(w io.Writer, lang, slug, rel string) error {
	run := "python " + rel
	extra := ""
	if isTS(lang) {
		run = "npx tsx " + rel
		extra = "     (needs the worker runtime from @af-stack/sdk/server)\n"
	}
	fmt.Fprintf(w, `Created %s

Next steps:
  1. Implement the %q handler in %s.
  2. Give a tenant API key the jobs:work scope:
       af-stack keys issue --tenant <id> --name worker --scopes jobs:work
  3. Run the worker (reads AF_STACK_URL + AF_STACK_API_KEY):
       export AF_STACK_API_KEY=af_live_...   # a key with the jobs:work scope
       %s
%s`, rel, slug, rel, run, extra)
	return nil
}

func isTS(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return l == "ts" || l == "typescript"
}

// slugify lower-cases input and collapses every run of non-[a-z0-9] into a
// single dash, trimming leading/trailing dashes. Empty when nothing usable.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
