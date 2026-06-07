// SPDX-License-Identifier: Apache-2.0

// Package probes carries the static, per-provider probe definitions used
// by the harnesses Service. Probes describe HOW to detect a harness
// (which binaries to try, which version args, which env vars must be
// set); the actual detection logic lives in harnesses/prober.go.
//
// Keeping this in its own package makes the probe table easy to grep
// when adding a new harness: search for `probes.Probe` and add an entry.
//
// This package intentionally does NOT import the parent harnesses
// package — provider identifiers are passed in as raw strings so the
// import graph stays acyclic (parent imports child, never the reverse).
// The string keys match HarnessProviderSchema in
// apps/dashboard/src/lib/api.ts.
package probes

// Probe is the static description of how to detect a single harness.
type Probe struct {
	// Binary lists candidate executable names to try via exec.LookPath
	// in order. Empty = the harness has no CLI binary (not currently
	// used; every supported harness ships at least one entry).
	Binary []string
	// VersionArgs is appended to the resolved binary to get a version
	// string (typically "--version"). Stdout is captured and trimmed.
	VersionArgs []string
	// RequiredEnv lists env vars that must be non-empty for the harness
	// to authenticate. Missing env vars demote a Ready harness to
	// NeedsAuth (the binary is fine; just no API key wired).
	RequiredEnv []string
	// InstallDocURL points operators at the install instructions when
	// the harness is missing. Surfaced as a link in the dashboard.
	InstallDocURL string
	// Description is a short human label rendered next to the provider
	// name in the dashboard's card grid.
	Description string
}

// Provider ID constants used as map keys. Mirror HarnessProviderSchema
// in apps/dashboard/src/lib/api.ts.
const (
	ProviderClaudecode = "claude-code"
	ProviderCodex      = "codex"
	ProviderGemini     = "gemini"
	ProviderOpencode   = "opencode"
)

// Registry returns the static map of provider -> probe. The map is
// rebuilt on every call so callers can mutate freely without affecting
// future lookups.
//
// The required-env decisions:
//   - claude-code, codex, gemini: each shells out to a hosted model
//     provider and needs an API key. Missing key -> needs_auth.
//   - opencode: ships its own model gateway and uses a settings file
//     for credentials, so we don't require any env at probe time.
func Registry() map[string]Probe {
	return map[string]Probe{
		ProviderClaudecode: {
			Binary:        []string{"claude", "claude-code"},
			VersionArgs:   []string{"--version"},
			RequiredEnv:   []string{"ANTHROPIC_API_KEY"},
			InstallDocURL: "https://docs.claude.com/claude-code",
			Description:   "Claude Code CLI from Anthropic.",
		},
		ProviderCodex: {
			Binary:        []string{"codex"},
			VersionArgs:   []string{"--version"},
			RequiredEnv:   []string{"OPENAI_API_KEY"},
			InstallDocURL: "https://github.com/openai/codex",
			Description:   "OpenAI Codex CLI.",
		},
		ProviderGemini: {
			Binary:        []string{"gemini"},
			VersionArgs:   []string{"--version"},
			RequiredEnv:   []string{"GEMINI_API_KEY"},
			InstallDocURL: "https://github.com/google-gemini/gemini-cli",
			Description:   "Google Gemini CLI.",
		},
		ProviderOpencode: {
			Binary:        []string{"opencode"},
			VersionArgs:   []string{"--version"},
			RequiredEnv:   []string{}, // bundled credentials; no env required
			InstallDocURL: "https://opencode.ai",
			Description:   "OpenCode terminal harness (bundled in the default agent image).",
		},
	}
}

// For returns the Probe for the given provider id or (zero, false).
func For(provider string) (Probe, bool) {
	r := Registry()
	pr, ok := r[provider]
	return pr, ok
}