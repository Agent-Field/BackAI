// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSourceRegistry(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		wantKind SourceKind
		wantVend string
		wantName string
		wantVer  string
	}{
		{
			in:       "af-skill://acme/security-audit@1.2.0",
			wantKind: SourceRegistry,
			wantVend: "acme",
			wantName: "security-audit",
			wantVer:  "1.2.0",
		},
		{
			// Version defaults to "latest" when omitted.
			in:       "af-skill://acme/security-audit",
			wantKind: SourceRegistry,
			wantVend: "acme",
			wantName: "security-audit",
			wantVer:  "latest",
		},
	}
	for _, tc := range cases {
		src, err := ParseSource(tc.in)
		if err != nil {
			t.Fatalf("ParseSource(%q) error: %v", tc.in, err)
		}
		if src.Kind != tc.wantKind {
			t.Errorf("ParseSource(%q).Kind = %q, want %q", tc.in, src.Kind, tc.wantKind)
		}
		if src.Vendor != tc.wantVend {
			t.Errorf("ParseSource(%q).Vendor = %q, want %q", tc.in, src.Vendor, tc.wantVend)
		}
		if src.Name != tc.wantName {
			t.Errorf("ParseSource(%q).Name = %q, want %q", tc.in, src.Name, tc.wantName)
		}
		if src.Version != tc.wantVer {
			t.Errorf("ParseSource(%q).Version = %q, want %q", tc.in, src.Version, tc.wantVer)
		}
		if src.Raw != tc.in {
			t.Errorf("ParseSource(%q).Raw = %q, want %q", tc.in, src.Raw, tc.in)
		}
	}
}

func TestParseSourceEmbedded(t *testing.T) {
	t.Parallel()
	src, err := ParseSource("embedded:default")
	if err != nil {
		t.Fatalf("ParseSource error: %v", err)
	}
	if src.Kind != SourceEmbedded {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceEmbedded)
	}
	if src.EmbeddedName != "default" {
		t.Errorf("EmbeddedName = %q, want %q", src.EmbeddedName, "default")
	}
}

func TestParseSourceLocal(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/abs/path/to/skill",
		"./relative/skill",
		"../parent/skill",
	}
	for _, in := range cases {
		src, err := ParseSource(in)
		if err != nil {
			t.Fatalf("ParseSource(%q) error: %v", in, err)
		}
		if src.Kind != SourceLocal {
			t.Errorf("ParseSource(%q).Kind = %q, want %q", in, src.Kind, SourceLocal)
		}
		if src.Path != in {
			t.Errorf("ParseSource(%q).Path = %q, want %q", in, src.Path, in)
		}
	}
}

func TestParseSourceErrors(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"   ",
		"af-skill://",                 // missing vendor/name
		"af-skill://onlyvendor",       // vendor present, no name
		"af-skill://acme/name@",       // empty version after @
		"embedded:",                    // missing name
		"unknown-format-no-prefix",     // not classified
	}
	for _, in := range cases {
		_, err := ParseSource(in)
		if err == nil {
			t.Errorf("ParseSource(%q) expected error, got nil", in)
			continue
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ParseSource(%q) error = %v, want ErrInvalidInput", in, err)
		}
	}
}

func TestInstallerEmbeddedDefault(t *testing.T) {
	t.Parallel()
	inst := NewInstaller()
	sk, err := inst.Install("embedded:default", "")
	if err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if sk.ID != "embedded:default" {
		t.Errorf("ID = %q, want embedded:default", sk.ID)
	}
	if sk.Name == "" || sk.Version == "" {
		t.Errorf("expected non-empty name+version, got name=%q version=%q", sk.Name, sk.Version)
	}
	if sk.TenantID != nil {
		t.Errorf("TenantID = %v, want nil (shared)", sk.TenantID)
	}
	if len(sk.Harnesses) == 0 {
		t.Errorf("expected at least one harness, got empty slice")
	}
}

func TestInstallerEmbeddedTenant(t *testing.T) {
	t.Parallel()
	inst := NewInstaller()
	sk, err := inst.Install("embedded:default", "t-acme")
	if err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if sk.TenantID == nil || *sk.TenantID != "t-acme" {
		t.Errorf("TenantID = %v, want t-acme", sk.TenantID)
	}
}

func TestInstallerUnknownEmbedded(t *testing.T) {
	t.Parallel()
	inst := NewInstaller()
	_, err := inst.Install("embedded:no-such-bundle", "")
	if err == nil {
		t.Fatal("expected error for unknown embedded skill")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

func TestInstallerRegistryStub(t *testing.T) {
	t.Parallel()
	inst := NewInstaller()
	sk, err := inst.Install("af-skill://acme/security-audit@1.2.0", "")
	if err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if sk.Name != "security-audit" {
		t.Errorf("Name = %q, want security-audit", sk.Name)
	}
	if sk.Version != "1.2.0" {
		t.Errorf("Version = %q, want 1.2.0", sk.Version)
	}
	if !strings.HasPrefix(sk.Source, "af-skill://") {
		t.Errorf("Source = %q, want af-skill:// prefix", sk.Source)
	}
}

func TestStoreNilHasPool(t *testing.T) {
	t.Parallel()
	var s *Store
	if s.HasPool() {
		t.Errorf("nil Store.HasPool() = true, want false")
	}
	s2 := NewStore(nil, nil)
	if s2.HasPool() {
		t.Errorf("zero-pool Store.HasPool() = true, want false")
	}
}