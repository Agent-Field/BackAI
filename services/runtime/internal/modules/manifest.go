// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFilename is the fixed on-disk name of a workload module's
// declarative manifest, discovered at <root>/<id>/backai.module.yaml.
const ManifestFilename = "backai.module.yaml"

// DefaultMigrationsDir is the manifest.Migrations fallback: module
// migrations live under <module>/migrations unless overridden.
const DefaultMigrationsDir = "migrations"

// Field types a resource column may declare. Each maps to a Postgres
// column type the module author uses in their own migration (the runtime
// never generates DDL — it only validates the manifest + serves CRUD).
const (
	FieldTypeString    = "string"
	FieldTypeInt       = "int"
	FieldTypeBool      = "bool"
	FieldTypeTimestamp = "timestamp"
	FieldTypeJSON      = "json"
)

// reservedColumns are auto-managed by the runtime on every resource
// table. A manifest MUST NOT redeclare them as resource fields.
var reservedColumns = map[string]struct{}{
	"id":         {},
	"tenant_id":  {},
	"created_at": {},
	"updated_at": {},
}

var (
	idPattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)
	identPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	// versionPattern is a lenient semver-ish check: N(.N(.N))(-suffix)?.
	versionPattern = regexp.MustCompile(`^\d+(\.\d+){0,2}([-+][0-9A-Za-z.-]+)?$`)
)

// WorkloadManifest is the parsed backai.module.yaml. It is the declarative
// contract a workload module ships: identity, versioned migrations, and
// the resources whose tenant-scoped CRUD the runtime auto-generates.
type WorkloadManifest struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	// Enabled is the module's own default posture. A module is served when
	// this is true OR when its id appears in the operator's enabled list
	// (Config.Modules.WorkloadModules). Defaults to false so a discovered
	// module never auto-serves without an explicit opt-in.
	Enabled bool `yaml:"enabled"`
	// Migrations is the directory (relative to the module root) holding the
	// versioned .sql files. Defaults to DefaultMigrationsDir.
	Migrations string     `yaml:"migrations"`
	Resources  []Resource `yaml:"resources"`
}

// Resource declares one CRUD entity. Its backing table follows the
// TableName convention (<module>_<resource>) and is created by the
// module's own migrations.
type Resource struct {
	Name   string  `yaml:"name"`
	Fields []Field `yaml:"fields"`
}

// Field is one typed column on a resource.
type Field struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	// Default is the value applied when a create request omits the field.
	// Its concrete Go type must be compatible with Type; validated at parse.
	Default any `yaml:"default"`
}

// ParseManifest unmarshals and validates a backai.module.yaml payload.
// A returned error means the manifest is unusable; the caller logs and
// skips the module (the runtime keeps serving everything else).
func ParseManifest(data []byte) (*WorkloadManifest, error) {
	var m WorkloadManifest
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if strings.TrimSpace(m.Migrations) == "" {
		m.Migrations = DefaultMigrationsDir
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *WorkloadManifest) validate() error {
	m.ID = strings.TrimSpace(m.ID)
	if m.ID == "" {
		return fmt.Errorf("manifest: id is required")
	}
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("manifest: id %q must be lowercase alphanumeric with - or _ separators", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("manifest %q: name is required", m.ID)
	}
	m.Version = strings.TrimSpace(m.Version)
	if m.Version == "" {
		return fmt.Errorf("manifest %q: version is required", m.ID)
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("manifest %q: version %q is not a valid semver", m.ID, m.Version)
	}
	if len(m.Resources) == 0 {
		return fmt.Errorf("manifest %q: at least one resource is required", m.ID)
	}
	seen := make(map[string]struct{}, len(m.Resources))
	for i := range m.Resources {
		if err := m.Resources[i].validate(m.ID); err != nil {
			return err
		}
		if _, dup := seen[m.Resources[i].Name]; dup {
			return fmt.Errorf("manifest %q: duplicate resource %q", m.ID, m.Resources[i].Name)
		}
		seen[m.Resources[i].Name] = struct{}{}
	}
	return nil
}

func (r *Resource) validate(moduleID string) error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return fmt.Errorf("manifest %q: resource name is required", moduleID)
	}
	if !identPattern.MatchString(r.Name) {
		return fmt.Errorf("manifest %q: resource name %q must be a lowercase identifier", moduleID, r.Name)
	}
	if len(r.Fields) == 0 {
		return fmt.Errorf("manifest %q: resource %q declares no fields", moduleID, r.Name)
	}
	seen := make(map[string]struct{}, len(r.Fields))
	for i := range r.Fields {
		f := &r.Fields[i]
		f.Name = strings.TrimSpace(f.Name)
		f.Type = strings.TrimSpace(strings.ToLower(f.Type))
		if f.Name == "" {
			return fmt.Errorf("manifest %q: resource %q has an unnamed field", moduleID, r.Name)
		}
		if !identPattern.MatchString(f.Name) {
			return fmt.Errorf("manifest %q: field %q must be a lowercase identifier", moduleID, f.Name)
		}
		if _, bad := reservedColumns[f.Name]; bad {
			return fmt.Errorf("manifest %q: field %q is reserved (managed by the runtime)", moduleID, f.Name)
		}
		if _, dup := seen[f.Name]; dup {
			return fmt.Errorf("manifest %q: resource %q duplicate field %q", moduleID, r.Name, f.Name)
		}
		seen[f.Name] = struct{}{}
		if !validFieldType(f.Type) {
			return fmt.Errorf("manifest %q: field %q has invalid type %q (want string|int|bool|timestamp|json)", moduleID, f.Name, f.Type)
		}
		if f.Default != nil {
			if _, err := coerceValue(f.Type, f.Default); err != nil {
				return fmt.Errorf("manifest %q: field %q default: %w", moduleID, f.Name, err)
			}
		}
	}
	return nil
}

func validFieldType(t string) bool {
	switch t {
	case FieldTypeString, FieldTypeInt, FieldTypeBool, FieldTypeTimestamp, FieldTypeJSON:
		return true
	default:
		return false
	}
}

// FieldNames returns the declared field names in manifest order.
func (r Resource) FieldNames() []string {
	out := make([]string, len(r.Fields))
	for i, f := range r.Fields {
		out[i] = f.Name
	}
	return out
}

// ResourceNames returns the module's resource names sorted, for stable
// diagnostics and admin output.
func (m *WorkloadManifest) ResourceNames() []string {
	out := make([]string, 0, len(m.Resources))
	for _, r := range m.Resources {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}
