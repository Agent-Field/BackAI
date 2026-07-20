// SPDX-License-Identifier: Apache-2.0

// This file implements the filesystem-discovered, declarative workload
// module system (PRD R2). It is distinct from the in-code Module/Registry
// contract in modules.go (the "Go in-runtime module" future): here a
// module is data — a backai.module.yaml manifest plus versioned SQL
// migrations — and the runtime auto-generates tenant-scoped CRUD from it.
package modules

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// DefaultWorkloadRoot is the config default for WORKLOAD_MODULES_PATH.
const DefaultWorkloadRoot = "./workload-modules"

// MigrationState is the applied-migration status surfaced to operators.
type MigrationState string

const (
	// MigrationPending: module is active and valid but migrations have not
	// been applied yet (ApplyMigrations not run, e.g. no DB at boot).
	MigrationPending MigrationState = "pending"
	// MigrationApplied: all migrations applied successfully.
	MigrationApplied MigrationState = "applied"
	// MigrationError: load-time lint or an apply-time failure disabled the
	// module. It is not routed; the runtime keeps serving everything else.
	MigrationError MigrationState = "error"
	// MigrationSkipped: module discovered but not enabled, so nothing runs.
	MigrationSkipped MigrationState = "skipped"
)

// Loaded is one discovered module plus its resolved runtime state.
type Loaded struct {
	Manifest       *WorkloadManifest
	Dir            string
	Active         bool
	LoadErr        error
	Migration      MigrationState
	AppliedVersion int
	MigrationErr   error

	migFiles []migrationFile
}

func (l *Loaded) routable() bool {
	return l.Active && l.LoadErr == nil && l.Migration != MigrationError
}

// Manager owns the set of discovered workload modules and mounts their
// tenant-scoped CRUD routes.
type Manager struct {
	root    string
	log     *slog.Logger
	modules []*Loaded
	db      Querier
	resp    Responder
}

// Load discovers modules under root, parsing + validating each manifest
// and statically linting its migrations for tenant isolation. It never
// returns an error for a bad module — invalid modules are recorded with a
// LoadErr and logged, and the runtime serves everything else. `enabled`
// is the operator's opt-in list (Config.Modules.WorkloadModules); a module
// is active when it appears there OR when its manifest sets enabled: true.
func Load(root string, enabled []string, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(root) == "" {
		root = DefaultWorkloadRoot
	}
	m := &Manager{root: root, log: log}

	enabledSet := make(map[string]struct{}, len(enabled))
	for _, id := range enabled {
		enabledSet[strings.TrimSpace(id)] = struct{}{}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("workload modules: cannot read root; none loaded", "root", root, "error", err)
		}
		return m
	}

	seen := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		manifestPath := filepath.Join(dir, ManifestFilename)
		if _, statErr := os.Stat(manifestPath); statErr != nil {
			continue // directory without a manifest — not a module
		}
		loaded := m.loadOne(dir, manifestPath, enabledSet, seen)
		m.modules = append(m.modules, loaded)
	}
	sort.Slice(m.modules, func(i, j int) bool { return m.modules[i].Dir < m.modules[j].Dir })
	return m
}

func (m *Manager) loadOne(dir, manifestPath string, enabledSet map[string]struct{}, seen map[string]string) *Loaded {
	l := &Loaded{Dir: dir, Migration: MigrationSkipped}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		l.LoadErr = fmt.Errorf("read manifest: %w", err)
		l.Migration = MigrationError
		m.log.Warn("workload module skipped: unreadable manifest", "dir", dir, "error", err)
		return l
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		l.LoadErr = err
		l.Migration = MigrationError
		m.log.Warn("workload module skipped: invalid manifest", "dir", dir, "error", err)
		return l
	}
	l.Manifest = manifest

	if prev, dup := seen[manifest.ID]; dup {
		l.LoadErr = fmt.Errorf("duplicate module id %q (already declared in %s)", manifest.ID, prev)
		l.Migration = MigrationError
		m.log.Warn("workload module skipped: duplicate id", "dir", dir, "id", manifest.ID, "first", prev)
		return l
	}
	seen[manifest.ID] = dir

	_, active := enabledSet[manifest.ID]
	l.Active = active || manifest.Enabled

	// Read + lint migrations regardless of active state so the admin view
	// reports the truth; only active modules get routed / migrated.
	migDir := filepath.Join(dir, manifest.Migrations)
	files, err := readMigrationFiles(migDir)
	if err != nil {
		l.LoadErr = err
		l.Migration = MigrationError
		m.log.Warn("workload module skipped: bad migrations", "dir", dir, "id", manifest.ID, "error", err)
		return l
	}
	l.migFiles = files

	if lintErr := LintMigrationSQL(concatSQL(files)); lintErr != nil {
		l.LoadErr = fmt.Errorf("migration tenant-isolation lint: %w", lintErr)
		l.Migration = MigrationError
		m.log.Error("workload module disabled: migrations fail tenant-isolation lint",
			"dir", dir, "id", manifest.ID, "error", lintErr)
		return l
	}

	if l.Active {
		l.Migration = MigrationPending
	} else {
		l.Migration = MigrationSkipped
	}
	return l
}

func concatSQL(files []migrationFile) string {
	parts := make([]string, len(files))
	for i, f := range files {
		parts[i] = f.SQL
	}
	return strings.Join(parts, "\n;\n")
}

// SetDB wires the query surface the CRUD handlers use. Must be called
// before Mount. In production this is the runtime's serving *pgxpool.Pool
// (tenant GUC bound per acquire); tests inject a fake.
func (m *Manager) SetDB(db Querier) { m.db = db }

// SetResponder injects the server's canonical JSON/error writers so module
// responses carry request_id like every other endpoint.
func (m *Manager) SetResponder(resp Responder) { m.resp = resp }

// ApplyMigrations applies every active module's pending migrations,
// recording state per module. A module whose migrations fail is marked
// MigrationError and excluded from routing; the returned error aggregates
// failures for logging but the caller should NOT abort boot — the runtime
// keeps serving. DB-bound: skipped in unit tests, verified live downstream.
func (m *Manager) ApplyMigrations(ctx context.Context, db MigrationDB) error {
	var failures []string
	for _, l := range m.modules {
		if !l.Active || l.LoadErr != nil {
			continue
		}
		version, err := applyMigrations(ctx, db, l.Manifest.ID, l.migFiles)
		if err != nil {
			l.Migration = MigrationError
			l.MigrationErr = err
			m.log.Error("workload module migration failed; module disabled",
				"module", l.Manifest.ID, "error", err)
			failures = append(failures, err.Error())
			continue
		}
		l.Migration = MigrationApplied
		l.AppliedVersion = version
		m.log.Info("workload module migrations applied",
			"module", l.Manifest.ID, "version", version, "resources", l.Manifest.ResourceNames())
	}
	if len(failures) > 0 {
		return fmt.Errorf("workload module migrations: %s", strings.Join(failures, "; "))
	}
	return nil
}

// Mount registers tenant-scoped CRUD routes for every routable module onto
// mux and records them in the OpenAPI spec. Inactive, invalid, or
// migration-errored modules contribute no routes. Returns the list of
// mounted route patterns (for diagnostics + tests).
func (m *Manager) Mount(mux *http.ServeMux, ob *openapi.Builder) []string {
	var mounted []string
	for _, l := range m.modules {
		if !l.routable() {
			continue
		}
		for _, res := range l.Manifest.Resources {
			h := newResourceHandler(l.Manifest.ID, res, m.db, m.resp)
			base := "/api/v1/workload/" + l.Manifest.ID + "/" + res.Name
			routes := []struct {
				method  string
				pattern string
				h       http.HandlerFunc
				summary string
			}{
				{"GET", base, h.list, "List " + res.Name},
				{"POST", base, h.create, "Create " + res.Name},
				{"GET", base + "/{id}", h.get, "Get " + res.Name + " by id"},
				{"PATCH", base + "/{id}", h.update, "Update " + res.Name},
				{"DELETE", base + "/{id}", h.delete, "Delete " + res.Name},
			}
			for _, rt := range routes {
				mux.HandleFunc(rt.method+" "+rt.pattern, rt.h)
				mounted = append(mounted, rt.method+" "+rt.pattern)
				if ob != nil {
					meta := openapi.RouteMeta{Summary: rt.summary, Tags: []string{"workload"}}
					if strings.Contains(rt.pattern, "{id}") {
						meta.Parameters = []openapi.Parameter{{
							Name: "id", In: "path", Required: true,
							Schema: map[string]any{"type": "string"},
						}}
					}
					ob.Register(rt.method, rt.pattern, meta)
				}
			}
		}
	}
	return mounted
}

// AdminModuleView is one row of GET /api/v1/admin/modules.
type AdminModuleView struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Enabled        bool     `json:"enabled"`
	Health         string   `json:"health"`
	MigrationState string   `json:"migration_state"`
	AppliedVersion int      `json:"applied_version"`
	Resources      []string `json:"resources"`
	Dir            string   `json:"dir,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// AdminList returns the operator-facing inventory of discovered modules.
func (m *Manager) AdminList() []AdminModuleView {
	out := make([]AdminModuleView, 0, len(m.modules))
	for _, l := range m.modules {
		view := AdminModuleView{
			Enabled:        l.Active,
			MigrationState: string(l.Migration),
			AppliedVersion: l.AppliedVersion,
			Dir:            l.Dir,
			Resources:      []string{},
		}
		if l.Manifest != nil {
			view.ID = l.Manifest.ID
			view.Name = l.Manifest.Name
			view.Version = l.Manifest.Version
			view.Resources = l.Manifest.ResourceNames()
		}
		switch {
		case l.LoadErr != nil || l.Migration == MigrationError:
			view.Health = "error"
		case !l.Active:
			view.Health = "disabled"
		default:
			view.Health = "ok"
		}
		if l.LoadErr != nil {
			view.Error = l.LoadErr.Error()
		} else if l.MigrationErr != nil {
			view.Error = l.MigrationErr.Error()
		}
		out = append(out, view)
	}
	return out
}

// Modules exposes the loaded set (read-only) for diagnostics + tests.
func (m *Manager) Modules() []*Loaded { return m.modules }
