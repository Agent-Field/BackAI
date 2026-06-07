// SPDX-License-Identifier: Apache-2.0

// Package llmcache implements the exact-match LLM response cache used
// by the Phase 7.1 gateway to suppress duplicate upstream calls.
//
// Cache key construction:
//
//	cache_key = "<tenant_id>:<model>:<request_hash>"
//
// where request_hash is a stable SHA-256 of the canonical-JSON
// representation of the request body with volatile fields stripped.
// The hash function is intentionally model-agnostic; the model id is
// already part of the cache key so two providers can share a request
// shape without colliding.
package llmcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// volatileRequestFields lists keys that vary between otherwise-identical
// requests and must NOT affect the hash. Adding to this list invalidates
// every existing cache entry but never produces a wrong hit (the missed
// entry is just stored again on the next call).
//
// Why these:
//   - "stream":  controls transport, not the response content
//   - "user":    upstream attribution field; differs per-end-user but the
//     response is identical, so we collapse to a single entry
//     across users in the same tenant
var volatileRequestFields = map[string]struct{}{
	"stream": {},
	"user":   {},
}

// RequestHash returns the canonical SHA-256 of a request body, used as
// the request-identity component of the cache key.
//
// Stability guarantees:
//
//  1. Field order in the input does NOT affect the hash. We re-marshal
//     with sorted keys before hashing.
//  2. The "stream" and "user" fields are stripped before hashing so a
//     streaming + non-streaming pair of calls with the same prompt land
//     on the same entry. Same for the upstream-attribution "user" field.
//  3. The model is part of the cache key, not the hash — callers must
//     pass it separately. This means the same hash can appear under two
//     different models if the request body happens to match, but the
//     cache key keeps them distinct.
//
// The body may be any JSON value (object, array, primitive). Non-JSON
// input falls back to a raw SHA-256 of the bytes so callers can cache
// opaque payloads (e.g. binary embeddings) without a separate code path.
func RequestHash(model string, body []byte) string {
	// Best-effort canonicalisation: try to parse, strip volatile fields,
	// re-marshal with sorted keys. If parsing fails, fall back to the raw
	// bytes — a non-JSON body still has a stable hash.
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return rawSHA256(body)
	}
	cleaned := stripVolatile(v)
	canonical, err := marshalCanonical(cleaned)
	if err != nil {
		// Should not happen — Unmarshal succeeded so Marshal of the same
		// structure should too. But defend just in case.
		return rawSHA256(body)
	}
	// Prefix with the (lowercased, trimmed) model so callers that
	// forget to include it in their key still get a model-aware hash.
	// The cache key in cache.go re-includes the model separately for
	// explicit indexing; this prefix is a defence-in-depth.
	prefix := strings.ToLower(strings.TrimSpace(model))
	sum := sha256.Sum256(append([]byte(prefix+"\x00"), canonical...))
	return hex.EncodeToString(sum[:])
}

// rawSHA256 hashes a buffer with no canonicalisation. Used as the
// fallback when a request body isn't valid JSON.
func rawSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// stripVolatile walks a JSON value and removes any object field whose
// key appears in volatileRequestFields. Arrays are recursed into so
// nested objects (e.g. tool messages) are also cleaned. Maps are
// returned as new map values — the original input is never mutated.
func stripVolatile(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if _, drop := volatileRequestFields[k]; drop {
				continue
			}
			out[k] = stripVolatile(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = stripVolatile(val)
		}
		return out
	default:
		return v
	}
}

// marshalCanonical encodes a value with deterministic key ordering. The
// stdlib encoder sorts string-keyed maps by key, so we recursively
// rebuild the value into map[string]any (the encoder's canonical form)
// and trust json.Marshal for the actual ordering.
//
// Numbers come back from Unmarshal as float64 by default, which would
// lose precision on large integers. We don't try to round-trip those —
// callers should not be putting raw int64s past 2^53 in the cache key.
// LLM request bodies in practice never have such fields.
func marshalCanonical(v any) ([]byte, error) {
	switch x := v.(type) {
	case map[string]any:
		// Iterate keys in sorted order and marshal each piece manually
		// so we control ordering at every nesting level.
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf strings.Builder
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			child, err := marshalCanonical(x[k])
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte('}')
		return []byte(buf.String()), nil
	case []any:
		var buf strings.Builder
		buf.WriteByte('[')
		for i, el := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			child, err := marshalCanonical(el)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte(']')
		return []byte(buf.String()), nil
	default:
		// Primitives (string, bool, float64, nil) — stdlib marshal is
		// deterministic for these.
		return json.Marshal(x)
	}
}

// CacheKey returns the full storage key for (tenantID, model, hash).
// Exported because callers occasionally need to compute a key without
// going through Get/Put (e.g. to invalidate).
//
// Format: "<tenant_id>:<model>:<request_hash>". Empty tenant uses the
// literal "_" so the key still has the expected three-field shape.
func CacheKey(tenantID, model, requestHash string) string {
	t := tenantID
	if t == "" {
		t = "_"
	}
	return fmt.Sprintf("%s:%s:%s", t, strings.ToLower(strings.TrimSpace(model)), requestHash)
}