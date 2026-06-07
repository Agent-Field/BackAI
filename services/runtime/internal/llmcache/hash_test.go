// SPDX-License-Identifier: Apache-2.0

package llmcache

import (
	"encoding/json"
	"strings"
	"testing"
)

// Same request body in different key orders must produce the same hash.
// This is the headline stability guarantee — without it a model client
// that re-marshals between calls would constantly miss the cache.
func TestRequestHashStableAcrossKeyOrder(t *testing.T) {
	a := []byte(`{"model":"claude-haiku-4-5","temperature":0.2,"messages":[{"role":"user","content":"hello"}]}`)
	b := []byte(`{"messages":[{"role":"user","content":"hello"}],"temperature":0.2,"model":"claude-haiku-4-5"}`)

	if RequestHash("anthropic/claude-haiku-4-5", a) != RequestHash("anthropic/claude-haiku-4-5", b) {
		t.Fatalf("hashes differ for same body in different key order")
	}
}

// Adding a "stream":true field must NOT change the hash. We promise
// streaming and non-streaming calls share a cache entry.
func TestRequestHashStripsStream(t *testing.T) {
	plain := []byte(`{"model":"m","messages":[{"role":"user","content":"x"}]}`)
	stream := []byte(`{"model":"m","stream":true,"messages":[{"role":"user","content":"x"}]}`)

	if RequestHash("m", plain) != RequestHash("m", stream) {
		t.Fatalf("stream field affected hash; must be stripped")
	}
}

// The "user" upstream-attribution field must also be ignored so two
// end-users hit the same cache row.
func TestRequestHashStripsUser(t *testing.T) {
	a := []byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"user":"u-1"}`)
	b := []byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"user":"u-2"}`)

	if RequestHash("m", a) != RequestHash("m", b) {
		t.Fatalf("user field affected hash; must be stripped")
	}
}

// Temperature, top_p, max_tokens MUST affect the hash — they change the
// upstream response.
func TestRequestHashRetainsSamplingParams(t *testing.T) {
	base := map[string]any{
		"model":    "m",
		"messages": []any{map[string]any{"role": "user", "content": "x"}},
	}
	for _, field := range []string{"temperature", "top_p", "max_tokens"} {
		modified := map[string]any{}
		for k, v := range base {
			modified[k] = v
		}
		modified[field] = 0.7
		baseBytes, _ := json.Marshal(base)
		modBytes, _ := json.Marshal(modified)
		if RequestHash("m", baseBytes) == RequestHash("m", modBytes) {
			t.Errorf("hash should differ when %q changes", field)
		}
	}
}

// Different models must hash differently for the same body so two
// providers can share a request shape without collision.
func TestRequestHashIncludesModelPrefix(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"x"}]}`)
	if RequestHash("a", body) == RequestHash("b", body) {
		t.Fatalf("model prefix not affecting hash")
	}
}

// Nested objects must also be canonicalised — strip "stream" inside
// arrays + nested maps.
func TestRequestHashRecursiveStrip(t *testing.T) {
	a := []byte(`{"model":"m","messages":[{"role":"user","content":"x","stream":true}]}`)
	b := []byte(`{"model":"m","messages":[{"role":"user","content":"x"}]}`)
	if RequestHash("m", a) != RequestHash("m", b) {
		t.Fatalf("nested stream not stripped")
	}
}

// Non-JSON bodies fall back to a raw hash — useful for opaque
// (binary embedding) payloads. Identical bytes must hash identically.
func TestRequestHashNonJSONFallback(t *testing.T) {
	a := []byte{0x00, 0x01, 0x02, 0xff}
	b := []byte{0x00, 0x01, 0x02, 0xff}
	if RequestHash("m", a) != RequestHash("m", b) {
		t.Fatalf("identical non-JSON bodies hashed differently")
	}
	c := []byte{0x00, 0x01, 0x02, 0xfe}
	if RequestHash("m", a) == RequestHash("m", c) {
		t.Fatalf("different non-JSON bodies hashed identically")
	}
}

// CacheKey shape must be predictable for downstream invalidators.
func TestCacheKeyShape(t *testing.T) {
	k := CacheKey("11111111-1111-1111-1111-111111111111", "m", "abcd")
	parts := strings.Split(k, ":")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %q", len(parts), k)
	}
	if parts[0] != "11111111-1111-1111-1111-111111111111" || parts[1] != "m" || parts[2] != "abcd" {
		t.Errorf("unexpected parts: %v", parts)
	}
}

// Empty tenant must still produce a 3-part key.
func TestCacheKeyAnonTenant(t *testing.T) {
	k := CacheKey("", "m", "abcd")
	if !strings.HasPrefix(k, "_:") {
		t.Errorf("anon tenant key prefix wrong: %q", k)
	}
}

// Model case + whitespace must not produce different keys.
func TestCacheKeyModelNormalised(t *testing.T) {
	a := CacheKey("t", " ModelA ", "h")
	b := CacheKey("t", "modela", "h")
	if a != b {
		t.Errorf("model normalisation failed: %q vs %q", a, b)
	}
}