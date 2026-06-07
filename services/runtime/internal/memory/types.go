// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"encoding/json"
)

// EmbeddingDim is the expected number of float32 components in any
// embedding written to suite_memory. Tied to the migration's
// vector(1536) column.
const EmbeddingDim = 1536

// Scope is the enum used to partition memory entries. Field-level
// validation lives in Validate(); the type itself is just a string so
// JSON round-trips don't need a custom unmarshaller.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeTenant  Scope = "tenant"
	ScopeAgent   Scope = "agent"
	ScopeSession Scope = "session"
	ScopeRun     Scope = "run"
)

// Valid reports whether s is one of the enum values.
func (s Scope) Valid() bool {
	switch s {
	case ScopeGlobal, ScopeTenant, ScopeAgent, ScopeSession, ScopeRun:
		return true
	}
	return false
}

// RequiresScopeID reports whether the enum value MUST be paired with a
// non-empty scope_id. global is implicitly empty; tenant is filled
// from request context when missing; agent / session / run must be
// supplied by the caller.
func (s Scope) RequiresScopeID() bool {
	switch s {
	case ScopeAgent, ScopeSession, ScopeRun:
		return true
	}
	return false
}

// Entry is the wire shape returned by Get / Put / List / Search.
//
// JSON tags MUST match MemoryEntrySchema in
// apps/dashboard/src/lib/api.ts exactly (snake_case, scope_id is
// nullable, has_embedding is required).
type Entry struct {
	Scope        Scope           `json:"scope"`
	ScopeID      *string         `json:"scope_id"`
	Key          string          `json:"key"`
	Value        json.RawMessage `json:"value"`
	Metadata     map[string]any  `json:"metadata"`
	HasEmbedding bool            `json:"has_embedding"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// PutInput mirrors MemoryPutInputSchema. ScopeID and Metadata are
// optional; Embed defaults to false.
type PutInput struct {
	Scope    Scope           `json:"scope"`
	ScopeID  string          `json:"scope_id,omitempty"`
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
	Metadata map[string]any  `json:"metadata,omitempty"`
	Embed    bool            `json:"embed,omitempty"`
}

// ListOpts narrows a List() query.
type ListOpts struct {
	Scope   Scope
	ScopeID string
	Prefix  string
	Limit   int
	Offset  int
}

// List mirrors MemoryListSchema.
type List struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	HasMore bool    `json:"has_more"`
}

// SearchRequest mirrors MemorySearchRequestSchema.
type SearchRequest struct {
	Query     string   `json:"query"`
	Scope     Scope    `json:"scope,omitempty"`
	ScopeID   string   `json:"scope_id,omitempty"`
	TopK      int      `json:"top_k,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
}

// SearchHit mirrors MemorySearchHitSchema.
type SearchHit struct {
	Entry      Entry   `json:"entry"`
	Similarity float64 `json:"similarity"`
	Distance   float64 `json:"distance"`
}

// SearchResult mirrors MemorySearchResultSchema.
type SearchResult struct {
	Hits       []SearchHit `json:"hits"`
	DurationMS int64       `json:"duration_ms"`
}