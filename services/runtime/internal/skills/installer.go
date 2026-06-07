// installer.go — turns a source string into a Skill ready for Store.Install.
//
// The Installer handles the three source formats parsed by ParseSource:
//
//   - SourceRegistry — "af-skill://<vendor>/<name>@<version>". v1
//     extracts {name, version} from the URL and produces a stub Skill
//     with the source string verbatim. Full registry integration
//     (HTTP fetch + signature verify) is a follow-up.
//
//   - SourceLocal    — absolute or relative filesystem path. The
//     installer reads skill.toml or skill.json from the root and
//     populates {name, version, description, harnesses, tags} from
//     the manifest.
//
//   - SourceEmbedded — "embedded:<name>". Known bundles are hardcoded;
//     "embedded:default" is the smoke-test default.
//
// The Installer doesn't talk to the database — it returns a fully
// populated Skill that callers (the REST handlers) pass to Store.Install.
// Separating parse + manifest from persistence keeps unit testing easy.
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Installer constructs Skill records from source strings. It is
// stateless; the only reason it's a struct is to leave room for
// future fields (registry client, signing key, manifest cache).
type Installer struct{}

// NewInstaller returns a default Installer.
func NewInstaller() *Installer { return &Installer{} }

// manifest is the on-disk shape of skill.toml / skill.json.
//
// All fields are optional at decode time; the installer fills in
// sensible defaults when something is missing so a half-written
// manifest can still bootstrap (the operator can re-install with a
// fuller manifest later).
type manifest struct {
	Name        string   `json:"name" toml:"name"`
	Version     string   `json:"version" toml:"version"`
	Description string   `json:"description" toml:"description"`
	Harnesses   []string `json:"harnesses" toml:"harnesses"`
	Tags        []string `json:"tags" toml:"tags"`
}

// Install parses sourceStr, reads the manifest, and returns a Skill
// ready to hand to Store.Install. tenantID is the caller's tenant
// scope ("" → shared bundle).
func (i *Installer) Install(sourceStr string, tenantID string) (Skill, error) {
	src, err := ParseSource(sourceStr)
	if err != nil {
		return Skill{}, err
	}

	switch src.Kind {
	case SourceRegistry:
		return i.installRegistry(src, tenantID), nil
	case SourceLocal:
		return i.installLocal(src, tenantID)
	case SourceEmbedded:
		return i.installEmbedded(src, tenantID)
	default:
		return Skill{}, fmt.Errorf("%w: unknown source kind %q", ErrInvalidInput, src.Kind)
	}
}

// installRegistry is the v1 stub: we don't actually fetch anything
// over the wire yet. We persist the source string verbatim and use
// the parsed {vendor, name, version} so the dashboard has something
// meaningful to render. The id is the raw source string so re-installs
// of the same URL hit the existing row's ON CONFLICT DO UPDATE branch.
func (i *Installer) installRegistry(src Source, tenantID string) Skill {
	desc := fmt.Sprintf("Registry skill from %s/%s", src.Vendor, src.Name)
	return Skill{
		ID:          src.Raw,
		Name:        src.Name,
		Version:     src.Version,
		Source:      src.Raw,
		Description: &desc,
		Harnesses:   []string{"any"},
		Tags:        []string{"registry"},
		TenantID:    nilIfEmpty(tenantID),
	}
}

// installLocal reads skill.toml or skill.json from the source root.
// At least one must exist; we prefer skill.toml when both are present.
// The id for local installs is "local:<absolute-path>" so two installs
// of the same path collapse to one row.
func (i *Installer) installLocal(src Source, tenantID string) (Skill, error) {
	root := src.Path
	abs, err := filepath.Abs(root)
	if err != nil {
		return Skill{}, fmt.Errorf("%w: cannot resolve %q: %v", ErrInvalidInput, root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Skill{}, fmt.Errorf("%w: %v", ErrSourceUnreadable, err)
	}

	var (
		mfst manifest
		read bool
	)
	if info.IsDir() {
		tomlPath := filepath.Join(abs, "skill.toml")
		jsonPath := filepath.Join(abs, "skill.json")
		if data, err := os.ReadFile(tomlPath); err == nil {
			if err := toml.Unmarshal(data, &mfst); err != nil {
				return Skill{}, fmt.Errorf("%w: skill.toml: %v", ErrSourceUnreadable, err)
			}
			read = true
		} else if data, err := os.ReadFile(jsonPath); err == nil {
			if err := json.Unmarshal(data, &mfst); err != nil {
				return Skill{}, fmt.Errorf("%w: skill.json: %v", ErrSourceUnreadable, err)
			}
			read = true
		}
	} else {
		// Caller pointed straight at a manifest file.
		data, err := os.ReadFile(abs)
		if err != nil {
			return Skill{}, fmt.Errorf("%w: %v", ErrSourceUnreadable, err)
		}
		switch strings.ToLower(filepath.Ext(abs)) {
		case ".toml":
			if err := toml.Unmarshal(data, &mfst); err != nil {
				return Skill{}, fmt.Errorf("%w: %v", ErrSourceUnreadable, err)
			}
		case ".json":
			if err := json.Unmarshal(data, &mfst); err != nil {
				return Skill{}, fmt.Errorf("%w: %v", ErrSourceUnreadable, err)
			}
		default:
			return Skill{}, fmt.Errorf("%w: %q is neither .toml nor .json", ErrSourceUnreadable, abs)
		}
		read = true
	}

	if !read {
		return Skill{}, fmt.Errorf("%w: no skill.toml or skill.json found at %s", ErrSourceUnreadable, abs)
	}

	if mfst.Name == "" {
		// Fall back to the directory name if the manifest omitted it.
		mfst.Name = filepath.Base(abs)
	}
	if mfst.Version == "" {
		mfst.Version = "0.0.1"
	}
	if mfst.Harnesses == nil {
		mfst.Harnesses = []string{"any"}
	}
	if mfst.Tags == nil {
		mfst.Tags = []string{}
	}

	var descPtr *string
	if mfst.Description != "" {
		d := mfst.Description
		descPtr = &d
	}

	return Skill{
		ID:          "local:" + abs,
		Name:        mfst.Name,
		Version:     mfst.Version,
		Source:      abs,
		Description: descPtr,
		Harnesses:   mfst.Harnesses,
		Tags:        mfst.Tags,
		TenantID:    nilIfEmpty(tenantID),
	}, nil
}

// embeddedBundles is the in-runtime catalog of named embedded skills.
// "default" exists so the smoke-test path can install at least one
// skill without any external deps. Add more entries here as the
// runtime ships built-ins (e.g. a security review overlay, a doc
// writer overlay).
var embeddedBundles = map[string]manifest{
	"default": {
		Name:        "AF Stack default",
		Version:     "0.0.1",
		Description: "Built-in starter skill — gentle prompt overlay used as a smoke-test default.",
		Harnesses:   []string{"any"},
		Tags:        []string{"starter", "embedded"},
	},
}

// installEmbedded looks the name up in embeddedBundles and emits a
// Skill record. Unknown names → ErrInvalidInput so the dashboard
// surfaces a 400 rather than a generic install failure.
func (i *Installer) installEmbedded(src Source, tenantID string) (Skill, error) {
	mfst, ok := embeddedBundles[src.EmbeddedName]
	if !ok {
		return Skill{}, fmt.Errorf("%w: unknown embedded skill %q", ErrInvalidInput, src.EmbeddedName)
	}
	var descPtr *string
	if mfst.Description != "" {
		d := mfst.Description
		descPtr = &d
	}
	return Skill{
		ID:          src.Raw,
		Name:        mfst.Name,
		Version:     mfst.Version,
		Source:      src.Raw,
		Description: descPtr,
		Harnesses:   appendDefaultIfNil(mfst.Harnesses, "any"),
		Tags:        appendDefaultIfNil(mfst.Tags),
		TenantID:    nilIfEmpty(tenantID),
	}, nil
}

// nilIfEmpty mirrors the conventional pattern: pointer fields that map
// to nullable wire columns stay nil when the caller passed "".
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// appendDefaultIfNil ensures the slice is non-nil. When values is nil
// and defaults is supplied, defaults wins so the wire emits an array
// with sensible content rather than an empty placeholder.
func appendDefaultIfNil(values []string, defaults ...string) []string {
	if values != nil {
		return values
	}
	if len(defaults) == 0 {
		return []string{}
	}
	out := make([]string, len(defaults))
	copy(out, defaults)
	return out
}
