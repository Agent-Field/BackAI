// SPDX-License-Identifier: Apache-2.0

// Package validate holds the static, offline validators shared by
// `af-stack test`, `af-stack module validate`, and `af-stack agent
// validate`. Everything here works from files on disk with no runtime or
// database, so the gates run in CI and on a plane.
//
// The module manifest contract mirrors docs/workload-modules.md. The
// canonical filename is backai.module.yaml; the legacy manifest.yaml
// emitted by older `af-stack module new` is still accepted so existing
// forks validate.
package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Finding is one validation result. Level is "error" (fails the gate),
// "warn" (advisory), or "ok" (an affirmative pass worth reporting).
type Finding struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Result aggregates findings for one validated target.
type Result struct {
	Target   string    `json:"target"`
	OK       bool      `json:"ok"`
	Findings []Finding `json:"findings"`
}

func (r *Result) add(level, format string, args ...any) {
	r.Findings = append(r.Findings, Finding{Level: level, Message: fmt.Sprintf(format, args...)})
	if level == "error" {
		r.OK = false
	}
}

// ManifestFileNames is the ordered set of accepted manifest filenames; the
// first one present wins.
var ManifestFileNames = []string{"backai.module.yaml", "manifest.yaml"}

// knownFeatures is the set of platform features a module may declare in
// requires:. An unknown feature is a warning, not an error, so forks can
// name features this CLI version predates.
var knownFeatures = map[string]bool{
	"multi-tenancy": true, "llm-gateway": true, "memory": true,
	"storage": true, "billing": true, "jobs": true, "crons": true,
	"webhooks": true, "secrets": true, "auth": true, "sandbox": true,
	"vector": true, "search": true,
}

var (
	slugRE    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	versionRE = regexp.MustCompile(`^\d+\.\d+\.\d+`)
	methodSet = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
)

type manifestRoute struct {
	Method  string `yaml:"method"`
	Path    string `yaml:"path"`
	Handler string `yaml:"handler"`
}

type manifestMeter struct {
	Name string `yaml:"name"`
	Unit string `yaml:"unit"`
}

type moduleManifest struct {
	ID          string          `yaml:"id"`
	Name        string          `yaml:"name"`
	Version     string          `yaml:"version"`
	Description string          `yaml:"description"`
	Requires    []string        `yaml:"requires"`
	Routes      []manifestRoute `yaml:"routes"`
	Meters      []manifestMeter `yaml:"meters"`
}

// ModuleDir validates one workload-module directory end to end: the
// manifest shape and the migration RLS lint. dir is the module root. It is
// the composition of Manifest(dir) and Migrations(dir/migrations), and backs
// `af-stack module validate`.
func ModuleDir(dir string) *Result {
	res := Manifest(dir)
	res.Target = filepath.ToSlash(dir)
	migDir := filepath.Join(dir, "migrations")
	if info, err := os.Stat(migDir); err == nil && info.IsDir() {
		mig := Migrations(migDir)
		res.Findings = append(res.Findings, mig.Findings...)
		if !mig.OK {
			res.OK = false
		}
	}
	return res
}

// Manifest validates only the module manifest file in dir (no migrations).
// It backs the `module-manifest` gate of `af-stack test`.
func Manifest(dir string) *Result {
	res := &Result{Target: filepath.ToSlash(dir), OK: true}

	manifestPath := ""
	for _, name := range ManifestFileNames {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			manifestPath = p
			break
		}
	}
	if manifestPath == "" {
		res.add("error", "no module manifest found (expected one of: %s)",
			strings.Join(ManifestFileNames, ", "))
		return res
	}

	raw, err := os.ReadFile(manifestPath) // #nosec G304 -- operator-supplied module path
	if err != nil {
		res.add("error", "read %s: %v", filepath.Base(manifestPath), err)
		return res
	}
	var m moduleManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		res.add("error", "%s is not valid YAML: %v", filepath.Base(manifestPath), err)
		return res
	}
	validateManifest(&m, res)
	return res
}

// Migrations runs the RLS lint on a migrations directory and backs the
// `migration-rls` gate of `af-stack test`.
func Migrations(migDir string) *Result {
	res := &Result{Target: filepath.ToSlash(migDir), OK: true}
	lintMigrations(migDir, res)
	return res
}

func validateManifest(m *moduleManifest, res *Result) {
	if strings.TrimSpace(m.ID) == "" {
		res.add("error", "manifest: id is required")
	} else if !slugRE.MatchString(m.ID) {
		res.add("error", "manifest: id %q must match [a-z][a-z0-9-]* (a slug)", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		res.add("error", "manifest: name is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		res.add("error", "manifest: version is required")
	} else if !versionRE.MatchString(m.Version) {
		res.add("error", "manifest: version %q must be semver (MAJOR.MINOR.PATCH)", m.Version)
	}
	for _, feat := range m.Requires {
		if !knownFeatures[feat] {
			res.add("warn", "manifest: requires %q is not a platform feature this CLI knows", feat)
		}
	}
	seen := map[string]bool{}
	for i, r := range m.Routes {
		method := strings.ToUpper(strings.TrimSpace(r.Method))
		if !methodSet[method] {
			res.add("error", "manifest: route[%d] method %q is not a valid HTTP method", i, r.Method)
		}
		if !strings.HasPrefix(r.Path, "/") {
			res.add("error", "manifest: route[%d] path %q must start with /", i, r.Path)
		}
		if strings.TrimSpace(r.Handler) == "" {
			res.add("error", "manifest: route[%d] (%s %s) is missing a handler", i, method, r.Path)
		}
		key := method + " " + r.Path
		if seen[key] {
			res.add("error", "manifest: duplicate route %s", key)
		}
		seen[key] = true
	}
	for i, mt := range m.Meters {
		if strings.TrimSpace(mt.Name) == "" {
			res.add("error", "manifest: meter[%d] name is required", i)
		}
	}
	if res.OK {
		res.add("ok", "manifest %s v%s is valid", m.ID, m.Version)
	}
}

var (
	createTableRE = regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?"?([a-z0-9_]+)"?\s*\((.*?)\)\s*;`)
)

// lintMigrations enforces the multi-tenancy invariant on migration SQL: any
// table that carries a tenant_id column MUST also enable + FORCE row level
// security and define a tenant-isolation policy that reads app.tenant_id.
// The check reads every .sql file in the dir together so a table created in
// one file can be policed in another.
func lintMigrations(migDir string, res *Result) {
	entries, err := os.ReadDir(migDir)
	if err != nil {
		res.add("error", "read migrations dir: %v", err)
		return
	}
	var combined strings.Builder
	tenantTables := map[string]bool{}
	sawSQL := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			continue
		}
		sawSQL = true
		raw, err := os.ReadFile(filepath.Join(migDir, e.Name())) // #nosec G304 -- module migration file
		if err != nil {
			res.add("error", "read migration %s: %v", e.Name(), err)
			continue
		}
		text := string(raw)
		combined.WriteString(strings.ToLower(text))
		combined.WriteString("\n")
		for _, mm := range createTableRE.FindAllStringSubmatch(text, -1) {
			table := strings.ToLower(mm[1])
			body := strings.ToLower(mm[2])
			if regexp.MustCompile(`\btenant_id\b`).MatchString(body) {
				tenantTables[table] = true
			}
		}
	}
	if !sawSQL {
		return
	}
	all := combined.String()
	names := make([]string, 0, len(tenantTables))
	for t := range tenantTables {
		names = append(names, t)
	}
	sort.Strings(names)
	for _, table := range names {
		enable := strings.Contains(all, "alter table "+table+" enable row level security") ||
			regexp.MustCompile(`alter\s+table\s+`+regexp.QuoteMeta(table)+`\s+enable\s+row\s+level\s+security`).MatchString(all)
		force := regexp.MustCompile(`alter\s+table\s+` + regexp.QuoteMeta(table) + `\s+force\s+row\s+level\s+security`).MatchString(all)
		policy := regexp.MustCompile(`create\s+policy\s+\w+\s+on\s+` + regexp.QuoteMeta(table)).MatchString(all)
		guc := strings.Contains(all, "app.tenant_id")
		switch {
		case !enable:
			res.add("error", "table %q has tenant_id but never enables row level security (multi-tenancy invariant)", table)
		case !force:
			res.add("error", "table %q enables RLS but does not FORCE it (owner/migration role would bypass)", table)
		case !policy:
			res.add("error", "table %q enables RLS but defines no isolation policy", table)
		case !guc:
			res.add("error", "table %q RLS policy does not reference app.tenant_id", table)
		default:
			res.add("ok", "table %q is tenant-isolated (RLS + FORCE + policy on app.tenant_id)", table)
		}
	}
}

// AgentDir validates one agent scaffold directory: presence of the entry
// point, the agentfield dependency, and a resolvable node id.
func AgentDir(dir string) *Result {
	res := &Result{Target: filepath.ToSlash(dir), OK: true}

	mainPy := filepath.Join(dir, "main.py")
	raw, err := os.ReadFile(mainPy) // #nosec G304 -- operator-supplied agent path
	if err != nil {
		res.add("error", "no main.py entry point in %s", filepath.ToSlash(dir))
		return res
	}
	src := string(raw)
	if !strings.Contains(src, "agentfield") {
		res.add("error", "main.py does not import agentfield")
	}
	if !strings.Contains(src, "Agent(") {
		res.add("error", "main.py does not construct an Agent()")
	}
	if !strings.Contains(src, "node_id") {
		res.add("warn", "main.py does not set an explicit node_id")
	}
	if !strings.Contains(src, "@app.reasoner") && !strings.Contains(src, ".reasoner(") {
		res.add("warn", "main.py defines no @app.reasoner — the agent exposes nothing to call")
	}

	reqPath := filepath.Join(dir, "requirements.txt")
	if reqRaw, err := os.ReadFile(reqPath); err != nil { // #nosec G304 -- agent path
		res.add("warn", "no requirements.txt — pin agentfield so builds are reproducible")
	} else if !strings.Contains(string(reqRaw), "agentfield") {
		res.add("error", "requirements.txt does not pin agentfield")
	}

	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		res.add("warn", "no Dockerfile — agent will not build into the compose stack")
	}
	if res.OK {
		res.add("ok", "agent scaffold at %s is valid", filepath.ToSlash(dir))
	}
	return res
}

// FindModuleDirs returns the module directories under root's
// workload-modules/ (and the saas-scaffold modules/ dir), i.e. any
// immediate subdir containing an accepted manifest file.
func FindModuleDirs(root string) []string {
	var out []string
	for _, base := range []string{"workload-modules", "modules"} {
		baseDir := filepath.Join(root, base)
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(baseDir, e.Name())
			for _, name := range ManifestFileNames {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					out = append(out, dir)
					break
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
