// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"strings"
	"testing"
)

const notesManifestYAML = `
id: notes
name: Notes
version: 0.1.0
description: Reference workload module.
enabled: false
migrations: migrations
resources:
  - name: notes
    fields:
      - name: title
        type: string
        required: true
      - name: body
        type: string
      - name: done
        type: bool
        default: false
`

func TestParseManifest_ValidNotesShape(t *testing.T) {
	m, err := ParseManifest([]byte(notesManifestYAML))
	if err != nil {
		t.Fatalf("expected valid manifest, got error: %v", err)
	}
	if m.ID != "notes" || m.Name != "Notes" || m.Version != "0.1.0" {
		t.Fatalf("unexpected identity: %+v", m)
	}
	if m.Enabled {
		t.Fatalf("enabled should default/parse to false")
	}
	if m.Migrations != "migrations" {
		t.Fatalf("migrations dir = %q, want migrations", m.Migrations)
	}
	if len(m.Resources) != 1 || m.Resources[0].Name != "notes" {
		t.Fatalf("unexpected resources: %+v", m.Resources)
	}
	if got := m.Resources[0].FieldNames(); strings.Join(got, ",") != "title,body,done" {
		t.Fatalf("field order = %v, want [title body done]", got)
	}
}

func TestParseManifest_DefaultsMigrationsDir(t *testing.T) {
	y := `
id: mod
name: Mod
version: 1.0.0
resources:
  - name: things
    fields:
      - name: label
        type: string
`
	m, err := ParseManifest([]byte(y))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Migrations != DefaultMigrationsDir {
		t.Fatalf("migrations dir = %q, want %q", m.Migrations, DefaultMigrationsDir)
	}
	if m.Enabled {
		t.Fatalf("enabled should default to false")
	}
}

func TestParseManifest_Rejects(t *testing.T) {
	cases := map[string]string{
		"missing id": `
name: X
version: 1.0.0
resources:
  - name: a
    fields: [{name: f, type: string}]
`,
		"bad id chars": `
id: Not Valid
name: X
version: 1.0.0
resources:
  - name: a
    fields: [{name: f, type: string}]
`,
		"missing name": `
id: mod
version: 1.0.0
resources:
  - name: a
    fields: [{name: f, type: string}]
`,
		"missing version": `
id: mod
name: X
resources:
  - name: a
    fields: [{name: f, type: string}]
`,
		"bad version": `
id: mod
name: X
version: not-a-version
resources:
  - name: a
    fields: [{name: f, type: string}]
`,
		"no resources": `
id: mod
name: X
version: 1.0.0
`,
		"resource no fields": `
id: mod
name: X
version: 1.0.0
resources:
  - name: a
    fields: []
`,
		"reserved field tenant_id": `
id: mod
name: X
version: 1.0.0
resources:
  - name: a
    fields: [{name: tenant_id, type: string}]
`,
		"reserved field id": `
id: mod
name: X
version: 1.0.0
resources:
  - name: a
    fields: [{name: id, type: string}]
`,
		"invalid field type": `
id: mod
name: X
version: 1.0.0
resources:
  - name: a
    fields: [{name: f, type: decimal}]
`,
		"duplicate field": `
id: mod
name: X
version: 1.0.0
resources:
  - name: a
    fields:
      - {name: f, type: string}
      - {name: f, type: int}
`,
		"duplicate resource": `
id: mod
name: X
version: 1.0.0
resources:
  - {name: a, fields: [{name: f, type: string}]}
  - {name: a, fields: [{name: g, type: string}]}
`,
		"bad default type": `
id: mod
name: X
version: 1.0.0
resources:
  - name: a
    fields: [{name: f, type: bool, default: "yes"}]
`,
		// The legacy routes-based manifest shape must be rejected outright
		// (KnownFields strict) so an un-migrated module fails loudly.
		"legacy routes shape": `
id: mod
name: X
version: 1.0.0
routes:
  - {method: POST, path: /events, handler: mod.CreateEvent}
`,
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(y)); err == nil {
				t.Fatalf("expected error for %q, got nil", name)
			}
		})
	}
}
