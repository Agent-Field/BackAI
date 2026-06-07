// SPDX-License-Identifier: Apache-2.0

// Package skills is AF Stack's Phase 11.3 skills install/list/attach layer.
//
// A Skill is an AF skillkit bundle — a manifest declaring prompt
// overlays, optional tool bindings, target harnesses, and tags — that
// has been installed into the runtime. Install reads the manifest from
// its Source (registry URL, local path, or "embedded:<name>"); subsequent
// list/attach operations are dispatched through Store.
//
// Contract notes
//
//	The wire schemas live in apps/dashboard/src/lib/api.ts
//	(SkillSchema, SkillListSchema, InstallSkillInputSchema,
//	AttachSkillInputSchema). Field names, enum values, and nullable
//	semantics here MUST mirror those zod schemas — drift breaks the
//	dashboard's safeParse and the SDKs that share that contract.
package skills

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Skill mirrors SkillSchema in apps/dashboard/src/lib/api.ts. Pointer
// fields are used for the wire-nullable columns so json.Marshal emits
// JSON null rather than the zero value — the dashboard's zod schemas
// declare these as .nullable().
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Source      string   `json:"source"`
	Description *string  `json:"description"`
	Harnesses   []string `json:"harnesses"`
	Tags        []string `json:"tags"`
	TenantID    *string  `json:"tenant_id"`
	InstalledAt string   `json:"installed_at"`
}

// Attachment is one row in suite_skill_attachments. The dashboard
// doesn't currently surface attachments in a dedicated list endpoint
// (they're rolled into the Skill via downstream queries), but we keep
// the struct here so Store.ListAttachments can return a useful type.
type Attachment struct {
	SkillID    string `json:"skill_id"`
	Agent      string `json:"agent"`
	AttachedAt string `json:"attached_at"`
}

// SourceKind enumerates the three formats accepted by Install.
type SourceKind string

const (
	// SourceRegistry is "af-skill://<vendor>/<name>@<version>". The v1
	// installer extracts {name, version} from the URL and persists with
	// the raw source string; full registry integration (HTTP fetch +
	// signature verify) is a follow-up.
	SourceRegistry SourceKind = "registry"
	// SourceLocal is an absolute or relative filesystem path. The
	// installer reads skill.toml or skill.json at the root.
	SourceLocal SourceKind = "local"
	// SourceEmbedded is "embedded:<name>" — a known bundle hardcoded
	// in the runtime. Useful as a smoke-test default ("embedded:default").
	SourceEmbedded SourceKind = "embedded"
)

// Source is the parsed representation of an install source string. The
// installer dispatches on Kind; the original string is kept for the
// Skill.Source column so the dashboard can show provenance verbatim.
type Source struct {
	// Kind is which of the three formats the source matched.
	Kind SourceKind
	// Raw is the input string unchanged (used as the Skill.Source value).
	Raw string
	// Vendor / Name / Version are populated for SourceRegistry.
	Vendor  string
	Name    string
	Version string
	// Path is populated for SourceLocal.
	Path string
	// EmbeddedName is populated for SourceEmbedded.
	EmbeddedName string
}

// Sentinel errors. The REST layer maps each to a code + status.
var (
	// ErrInvalidInput covers malformed source strings, missing required
	// fields, validation failures.
	ErrInvalidInput = errors.New("skills: invalid input")
	// ErrNotConfigured means the Store doesn't have a database (no
	// pool wired at boot); the REST layer surfaces this as 503
	// SKILLS_NOT_CONFIGURED.
	ErrNotConfigured = errors.New("skills: not configured")
	// ErrNotFound means the skill id doesn't exist in suite_skills.
	ErrNotFound = errors.New("skills: not found")
	// ErrSourceUnreadable means we parsed the source successfully but
	// couldn't read the manifest (file missing, registry unreachable).
	ErrSourceUnreadable = errors.New("skills: source unreadable")
)

// ParseSource classifies a source string into one of the three known
// forms. Returns ErrInvalidInput for anything that doesn't match.
//
// Examples:
//
//	"af-skill://acme/security-audit@1.2.0" -> registry
//	"/abs/path/to/skill"                   -> local
//	"./relative/skill"                     -> local
//	"embedded:default"                     -> embedded
//
// The parser is permissive: it does the minimum needed to dispatch the
// installer. Deep validation (manifest schema, signature, version
// sanity) happens in the installer.
func ParseSource(s string) (Source, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Source{}, fmt.Errorf("%w: source is required", ErrInvalidInput)
	}

	// af-skill:// — the canonical registry form.
	if strings.HasPrefix(s, "af-skill://") {
		return parseRegistrySource(s)
	}

	// embedded:<name> — hardcoded bundles shipped with the runtime.
	if strings.HasPrefix(s, "embedded:") {
		name := strings.TrimPrefix(s, "embedded:")
		name = strings.TrimSpace(name)
		if name == "" {
			return Source{}, fmt.Errorf("%w: embedded source missing name", ErrInvalidInput)
		}
		return Source{
			Kind:         SourceEmbedded,
			Raw:          s,
			EmbeddedName: name,
		}, nil
	}

	// Plain http/https registry URLs are accepted as registry sources;
	// the installer treats them as the v1 stub (no signature verify
	// yet) so we record provenance even when an operator points at a
	// non-canonical mirror.
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return Source{}, fmt.Errorf("%w: invalid http(s) url: %v", ErrInvalidInput, err)
		}
		// Best-effort: pull the last path segment as the name.
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		name := ""
		if len(segs) > 0 {
			name = segs[len(segs)-1]
		}
		return Source{
			Kind:    SourceRegistry,
			Raw:     s,
			Vendor:  u.Host,
			Name:    name,
			Version: "0.0.0",
		}, nil
	}

	// Local path: anything starting with "/" or "./" or "../".
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return Source{Kind: SourceLocal, Raw: s, Path: s}, nil
	}

	return Source{}, fmt.Errorf("%w: source %q is not a recognised format (want af-skill://, http(s)://, /abs/path, ./relative, or embedded:<name>)", ErrInvalidInput, s)
}

// parseRegistrySource handles "af-skill://<vendor>/<name>@<version>".
// Vendor and name are required; version defaults to "latest" when the
// "@<version>" suffix is omitted so dev workflows don't have to pin.
func parseRegistrySource(s string) (Source, error) {
	rest := strings.TrimPrefix(s, "af-skill://")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return Source{}, fmt.Errorf("%w: af-skill:// missing vendor/name", ErrInvalidInput)
	}

	// Split off the @version suffix first; it may or may not be present.
	version := "latest"
	if at := strings.LastIndex(rest, "@"); at != -1 {
		version = strings.TrimSpace(rest[at+1:])
		rest = strings.TrimSpace(rest[:at])
		if version == "" {
			return Source{}, fmt.Errorf("%w: af-skill:// version is empty", ErrInvalidInput)
		}
	}

	// vendor/name split. We accept multiple path segments as
	// "vendor/sub/.../name" by treating everything before the last
	// segment as the vendor — most v1 callers will pass single-segment
	// vendors though.
	segs := strings.Split(rest, "/")
	if len(segs) < 2 {
		return Source{}, fmt.Errorf("%w: af-skill:// must be <vendor>/<name>[@version]", ErrInvalidInput)
	}
	name := strings.TrimSpace(segs[len(segs)-1])
	vendor := strings.TrimSpace(strings.Join(segs[:len(segs)-1], "/"))
	if vendor == "" || name == "" {
		return Source{}, fmt.Errorf("%w: af-skill:// vendor or name is empty", ErrInvalidInput)
	}

	return Source{
		Kind:    SourceRegistry,
		Raw:     s,
		Vendor:  vendor,
		Name:    name,
		Version: version,
	}, nil
}

// ListFilters mirrors the query params on GET /api/v1/skills.
//
// Both fields are free-form: TenantID gates by the suite_skills.tenant_id
// column (and the NULL "shared" bundles), Harness filters by membership
// in the skill's harnesses JSONB column.
type ListFilters struct {
	TenantID string
	Harness  string
}