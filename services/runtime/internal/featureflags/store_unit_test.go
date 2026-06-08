// SPDX-License-Identifier: Apache-2.0

package featureflags

import "testing"

func TestDefaultsAreCloned(t *testing.T) {
	flags := Defaults()
	flags[0].Metadata["mutated"] = true
	again := Defaults()
	if _, ok := again[0].Metadata["mutated"]; ok {
		t.Fatal("Defaults returned shared metadata map")
	}
}

func TestBuiltinByKey(t *testing.T) {
	flag, ok := builtinByKey("command-palette-recents")
	if !ok {
		t.Fatal("expected builtin flag")
	}
	if !flag.Enabled {
		t.Fatal("command-palette-recents should default enabled")
	}
}

func TestKeyPattern(t *testing.T) {
	for _, key := range []string{"alpha", "alpha.beta", "alpha-beta", "alpha_2026"} {
		if !keyPattern.MatchString(key) {
			t.Fatalf("expected %q to be valid", key)
		}
	}
	for _, key := range []string{"", "Alpha", "1alpha", "alpha beta"} {
		if keyPattern.MatchString(key) {
			t.Fatalf("expected %q to be invalid", key)
		}
	}
}
