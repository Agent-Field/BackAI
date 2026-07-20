// SPDX-License-Identifier: Apache-2.0

// Package memory implements the AF Stack suite memory primitives.
//
// Memory is a per-scope key/value store with optional pgvector-backed
// similarity search. The store sits on the same Postgres instance as
// the rest of the runtime; the vector(1536) column is sized for
// OpenAI's text-embedding-3-small, the default embedding model.
//
// Scopes
//
//	global   — visible to everything; scope_id MUST be empty
//	tenant   — partitioned by tenant uuid; resolved from context
//	agent    — partitioned by agent name; caller-supplied
//	session  — partitioned by session id; caller-supplied
//	run      — partitioned by execution id; caller-supplied
//
// Plaintext value and metadata travel as JSONB. Embeddings travel as
// the pgvector text encoding (`'[1.2,3.4,...]'`) because we don't pull
// in the pgvector-go driver — the text encoding is round-trip safe and
// keeps the dependency tree small.
package memory

import "errors"

// ErrNotFound is returned when the requested (scope, scope_id, key)
// triple has no row in suite_memory.
var ErrNotFound = errors.New("memory: not found")

// ErrInvalidScope is returned when the caller supplies a scope outside
// the canonical enum, or when scope_id is missing for an enum value
// that requires one (agent / session / run).
var ErrInvalidScope = errors.New("memory: invalid scope")

// ErrInvalidKey is returned for empty / oversized keys.
var ErrInvalidKey = errors.New("memory: invalid key")

// ErrTenantRequired is returned by write paths when no tenant is bound
// to the request context. Every entry is partitioned by tenant_id (FORCE
// RLS on suite_memory); without a resolved tenant a write can't be
// attributed and would breach isolation. The handler maps this to 401.
var ErrTenantRequired = errors.New("memory: tenant context required")

// ErrEmbeddingDimMismatch is returned when an embedder returns a vector
// whose dimensionality does not match the schema (1536 for
// text-embedding-3-small).
var ErrEmbeddingDimMismatch = errors.New("memory: embedding dim mismatch")
