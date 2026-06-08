// SPDX-License-Identifier: Apache-2.0

// Package tooladapters implements AF Stack's built-in tool adapter surface.
//
// It owns only tenant enablement/config and stateless calls to backend
// primitives. AgentField remains the authority for agent runs, tool-call
// spans, memory, and traces.
package tooladapters

import (
	"errors"
	"regexp"
)

type AdapterID string

const (
	AdapterBrowserUse AdapterID = "browser-use"
	AdapterSearXNG    AdapterID = "searxng"
	AdapterFS         AdapterID = "fs"
	AdapterExec       AdapterID = "exec"
	AdapterHTTP       AdapterID = "http"
	AdapterSQL        AdapterID = "sql"
)

var (
	ErrNotConfigured  = errors.New("tooladapters: not configured")
	ErrTenantRequired = errors.New("tooladapters: tenant required")
	ErrInvalidInput   = errors.New("tooladapters: invalid input")
	ErrNotFound       = errors.New("tooladapters: adapter not found")
	ErrDisabled       = errors.New("tooladapters: adapter disabled")
	ErrUnavailable    = errors.New("tooladapters: adapter unavailable")
)

var adapterPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type AdapterDefinition struct {
	ID             AdapterID        `json:"id"`
	Label          string           `json:"label"`
	Description    string           `json:"description"`
	Configured     bool             `json:"configured"`
	DefaultEnabled bool             `json:"default_enabled"`
	Tools          []ToolDefinition `json:"tools"`
}

type Adapter struct {
	ID             AdapterID        `json:"id"`
	Label          string           `json:"label"`
	Description    string           `json:"description"`
	Enabled        bool             `json:"enabled"`
	Configured     bool             `json:"configured"`
	DefaultEnabled bool             `json:"default_enabled"`
	Config         map[string]any   `json:"config"`
	UpdatedAt      *string          `json:"updated_at"`
	Tools          []ToolDefinition `json:"tools"`
}

type List struct {
	Adapters []Adapter `json:"adapters"`
}

type SetEnabledInput struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config,omitempty"`
}

type CallInput struct {
	Adapter   AdapterID      `json:"adapter"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type CallResult struct {
	Adapter    AdapterID `json:"adapter"`
	Tool       string    `json:"tool"`
	Status     string    `json:"status"`
	Result     any       `json:"result,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

type CallRecord struct {
	TenantID   string
	Adapter    AdapterID
	Tool       string
	Status     string
	DurationMS int64
	Error      string
}
