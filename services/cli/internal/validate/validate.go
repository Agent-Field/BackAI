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

// The manifest schema below mirrors the runtime's declarative contract
// (services/runtime/internal/modules/manifest.go) field for field. The
// runtime parses with strict field checking, so a manifest carrying keys
// outside this shape (e.g. the pre-PRD imperative routes:/meters: form)
// will not load — the validator errors on those with a pointer to the
// declarative shape rather than silently passing them.
var (
	// slugRE matches the runtime's module-id rule: lowercase alphanumeric
	// segments joined by - or _.
	slugRE = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)
	// identRE matches resource + field names (lowercase identifiers).
	identRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	// versionRE is the runtime's lenient semver-ish check.
	versionRE = regexp.MustCompile(`^\d+(\.\d+){0,2}([-+][0-9A-Za-z.-]+)?$`)
)

// reservedColumns are auto-managed by the runtime on every resource table
// and must not be redeclared as fields.
var reservedColumns = map[string]bool{
	"id": true, "tenant_id": true, "created_at": true, "updated_at": true,
}

// fieldTypes is the set of declarable resource-field types.
var fieldTypes = map[string]bool{
	"string": true, "int": true, "bool": true, "timestamp": true, "json": true,
}

type manifestField struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  any    `yaml:"default"`
}

type manifestResource struct {
	Name   string          `yaml:"name"`
	Fields []manifestField `yaml:"fields"`
}

type moduleManifest struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Version     string             `yaml:"version"`
	Description string             `yaml:"description"`
	Enabled     bool               `yaml:"enabled"`
	Migrations  string             `yaml:"migrations"`
	Resources   []manifestResource `yaml:"resources"`
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
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		// The runtime refuses unknown keys the same way; call out the
		// legacy imperative shape specifically so the fix is obvious.
		if strings.Contains(err.Error(), "not found in type") &&
			(strings.Contains(string(raw), "routes:") || strings.Contains(string(raw), "meters:")) {
			res.add("error", "%s uses the legacy imperative manifest shape (routes:/meters:); "+
				"the runtime loads declarative manifests — declare resources: with typed fields "+
				"(see workload-modules/notes/backai.module.yaml)", filepath.Base(manifestPath))
			return res
		}
		res.add("error", "%s does not parse as a module manifest: %v", filepath.Base(manifestPath), err)
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
		res.add("error", "manifest: id %q must be lowercase alphanumeric with - or _ separators", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		res.add("error", "manifest: name is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		res.add("error", "manifest: version is required")
	} else if !versionRE.MatchString(strings.TrimSpace(m.Version)) {
		res.add("error", "manifest: version %q is not a valid semver", m.Version)
	}
	if len(m.Resources) == 0 {
		res.add("error", "manifest: at least one resource is required (declarative modules serve resources)")
	}
	seenRes := map[string]bool{}
	for _, r := range m.Resources {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			res.add("error", "manifest: resource name is required")
			continue
		}
		if !identRE.MatchString(name) {
			res.add("error", "manifest: resource name %q must be a lowercase identifier", name)
		}
		if seenRes[name] {
			res.add("error", "manifest: duplicate resource %q", name)
		}
		seenRes[name] = true
		if len(r.Fields) == 0 {
			res.add("error", "manifest: resource %q declares no fields", name)
		}
		seenField := map[string]bool{}
		for _, f := range r.Fields {
			fname := strings.TrimSpace(f.Name)
			ftype := strings.ToLower(strings.TrimSpace(f.Type))
			if fname == "" {
				res.add("error", "manifest: resource %q has an unnamed field", name)
				continue
			}
			if !identRE.MatchString(fname) {
				res.add("error", "manifest: field %q must be a lowercase identifier", fname)
			}
			if reservedColumns[fname] {
				res.add("error", "manifest: field %q is reserved (managed by the runtime)", fname)
			}
			if seenField[fname] {
				res.add("error", "manifest: resource %q duplicate field %q", name, fname)
			}
			seenField[fname] = true
			if !fieldTypes[ftype] {
				res.add("error", "manifest: field %q has invalid type %q (want string|int|bool|timestamp|json)", fname, f.Type)
			}
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
