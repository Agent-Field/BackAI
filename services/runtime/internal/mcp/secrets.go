// SPDX-License-Identifier: Apache-2.0

// secrets.go — env resolver. Walks an env map and, for any value
// prefixed "secret:<key>", looks up the bare key in the secrets vault
// scoped to the same tenant as the MCP server row.
//
// Design:
//
//   - Raw values pass through unchanged.
//   - "secret:" prefix is consumed; the remainder is the lookup key.
//   - An empty key after the prefix is rejected as ErrInvalidInput so
//     the operator gets a clear error instead of silently spawning a
//     child with an empty credential.
//   - Lookup failures wrap ErrSecretLookupFailed; the original env key
//     is included in the error (the SECRET name is never logged, only
//     the env var that referenced it).
//
// The returned map is a fresh allocation so callers can safely mutate
// it (e.g. append PATH passthroughs) without disturbing the persisted
// row's env field.
package mcp

import (
	"context"
	"fmt"
	"strings"
)

// secretPrefix is the magic prefix every env value goes through. Kept
// as a const so a future flip to e.g. "vault://" is a single edit.
const secretPrefix = "secret:"

// ResolveEnv returns a fresh env map with every "secret:<key>" value
// replaced by the plaintext from the vault. Non-secret values pass
// through verbatim.
//
// tenantID is the row's tenant_id (empty string for shared rows). The
// vault treats "" as "no tenant scoping" — see secrets.Vault.Get.
//
// When vault is nil, the resolver returns ErrSecretLookupFailed for any
// "secret:<key>" reference (vs. silently returning the literal string,
// which would leak the prefix into the child's env). Non-secret values
// still pass through, so an env map with no refs resolves cleanly even
// with no vault wired.
func ResolveEnv(
	ctx context.Context,
	env map[string]string,
	vault SecretReader,
	tenantID string,
) (map[string]string, error) {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if !strings.HasPrefix(v, secretPrefix) {
			out[k] = v
			continue
		}
		key := strings.TrimPrefix(v, secretPrefix)
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf(
				"%w: env %q has empty secret reference", ErrInvalidInput, k,
			)
		}
		if vault == nil {
			return nil, fmt.Errorf(
				"%w: env %q references %q but no secrets vault is wired",
				ErrSecretLookupFailed, k, key,
			)
		}
		plaintext, err := vault.Get(ctx, tenantID, key)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: env %q -> %q: %v",
				ErrSecretLookupFailed, k, key, err,
			)
		}
		out[k] = string(plaintext)
	}
	return out, nil
}