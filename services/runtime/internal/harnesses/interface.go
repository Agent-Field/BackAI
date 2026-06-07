// SPDX-License-Identifier: Apache-2.0

// Package harnesses probes the local environment for CLI agent harnesses
// (Claude Code, Codex, Gemini CLI, OpenCode) and surfaces their state to
// the dashboard + SDKs. This is Phase 11.4: probe-only — the runtime
// reports whether each harness is installed and reachable, but never
// installs them itself (that stays a CLI / docs concern).
//
// Contract notes
//
//	The wire schemas live in apps/dashboard/src/lib/api.ts
//	(HarnessProviderSchema, HarnessSchema, HarnessListSchema). Field
//	names, enum values, and nullable semantics here MUST mirror those
//	zod schemas — drift breaks the dashboard's safeParse.
package harnesses

import (
	"errors"
)

// Provider enumerates the supported CLI harnesses. Values match
// HarnessProviderSchema in apps/dashboard/src/lib/api.ts.
type Provider string

const (
	Claudecode Provider = "claude-code"
	Codex      Provider = "codex"
	Gemini     Provider = "gemini"
	Opencode   Provider = "opencode"
)

// AllProviders is the canonical ordering used by ListAll. Keep this in
// sync with the enum in api.ts so the dashboard's card grid renders in
// the same order across reloads.
var AllProviders = []Provider{
	Claudecode,
	Codex,
	Gemini,
	Opencode,
}

// IsValid reports whether p is one of the four supported providers.
func (p Provider) IsValid() bool {
	switch p {
	case Claudecode, Codex, Gemini, Opencode:
		return true
	}
	return false
}

// Status is the result of a probe. Mirrors HarnessSchema.status in api.ts.
type Status string

const (
	// Ready: binary resolved, version check succeeded, all required env
	// vars present. Safe to launch from app.harness().
	StatusReady Status = "ready"
	// NeedsAuth: binary present and version check succeeded, but one or
	// more required env vars (e.g. ANTHROPIC_API_KEY) are missing. The
	// harness is installed but won't authenticate to its model provider.
	StatusNeedsAuth Status = "needs_auth"
	// Missing: no binary resolved via exec.LookPath on any candidate.
	StatusMissing Status = "missing"
	// Errored: binary resolved, but the version check failed (non-zero
	// exit, timeout, panic). last_error carries the underlying message.
	StatusErrored Status = "errored"
)

// Harness mirrors HarnessSchema in apps/dashboard/src/lib/api.ts. Nullable
// wire fields use pointer types so json.Marshal emits null rather than
// the zero value.
type Harness struct {
	Provider      Provider `json:"provider"`
	IsInstalled   bool     `json:"is_installed"`
	BinaryPath    *string  `json:"binary_path"`
	Version       *string  `json:"version"`
	Status        Status   `json:"status"`
	LastError     *string  `json:"last_error"`
	RequiredEnv   []string `json:"required_env"`
	InstallDocURL *string  `json:"install_doc_url"`
}

// Sentinel errors. The REST layer maps each to a code + status.
var (
	ErrUnknownProvider = errors.New("harnesses: unknown provider")
)