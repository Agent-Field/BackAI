// SPDX-License-Identifier: Apache-2.0

package secrets

import "context"

// Store is the abstract secrets backend. The in-tree *Vault satisfies
// it; the remote-adapter shim in adapters/remote/ also satisfies it.
// Other callers should depend on this interface rather than *Vault
// directly so that swapping the backend (envelope-local, AWS Secrets
// Manager, HashiCorp Vault, etc.) doesn't require code changes in the
// rest of the runtime.
//
// All methods take a context and respect cancellation. Implementations
// must be safe for concurrent use from multiple goroutines.
//
// The tenantID parameter is the multi-tenancy scoping key. Implementations
// MUST isolate secrets across tenants — a Get for (tenantA, key=X) must
// never return the value stored under (tenantB, key=X).
type Store interface {
	// Get returns the decrypted plaintext for (tenantID, key). Callers
	// MUST NOT log the returned bytes. Returns ErrSecretNotFound if
	// no row exists; ErrInvalidKey for malformed keys.
	Get(ctx context.Context, tenantID, key string) ([]byte, error)

	// GetMetadata returns metadata for a single secret without
	// decrypting the value. Useful for "exists?" checks and dashboard
	// listings that should never materialise plaintext.
	GetMetadata(ctx context.Context, tenantID, key string) (SecretMetadata, error)

	// Put inserts or updates a secret. The returned SecretMetadata
	// carries created_at / updated_at the implementation chose.
	Put(ctx context.Context, tenantID, key string, in PutInput) (SecretMetadata, error)

	// Delete removes a secret. Returns ErrSecretNotFound if no row
	// matched.
	Delete(ctx context.Context, tenantID, key string) error

	// List returns metadata for every secret stored under tenantID.
	// Values are NEVER materialised by this call.
	List(ctx context.Context, tenantID string) ([]SecretMetadata, error)

	// Rotate replaces the value of an existing secret. The secret
	// must already exist (returns ErrSecretNotFound otherwise) so
	// rotation can't accidentally create new entries.
	Rotate(ctx context.Context, tenantID, key, newValue string) (SecretMetadata, error)
}

// Compile-time check that *Vault satisfies Store.
var _ Store = (*Vault)(nil)
