// Package secrets implements the AF Stack suite secrets vault.
//
// The vault stores per-tenant key/value pairs in Postgres (table
// `suite_secrets`) with envelope-encrypted values using AES-256-GCM. The
// master key (KEK) is loaded from the AF_STACK_KMS_KEY environment
// variable.
//
// Plaintext secret values are NEVER logged, attached to spans, or
// included in error messages. Tests in vault_test.go enforce this.
//
// v1 keeps the envelope simple: the KEK directly encrypts secret values
// and `kms_key_id` records the KEK version (default "v1"). Phase 6 will
// introduce a per-tenant data encryption key (DEK) wrapped by the KEK
// for true envelope encryption.
package secrets

import "errors"

// ErrSecretNotFound is returned when the requested (tenant, key) pair
// has no row in suite_secrets.
var ErrSecretNotFound = errors.New("secrets: not found")

// ErrInvalidKey is returned when a caller passes an empty key or a key
// containing characters outside the allowed set (a-z, A-Z, 0-9, '_',
// '-', '.', '/'). The character set is intentionally narrow so keys
// can travel safely in URL paths.
var ErrInvalidKey = errors.New("secrets: invalid key")

// ErrInvalidCiphertext is returned when stored ciphertext fails the
// version-byte check or is too short to contain a nonce. It signals
// either corruption or a KEK mismatch.
var ErrInvalidCiphertext = errors.New("secrets: invalid ciphertext")
