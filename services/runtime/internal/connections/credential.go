// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"encoding/json"
	"fmt"
)

// Cipher is the minimal encryption surface connections needs. The vault's
// *secrets.Cipher (internal/secrets/crypto.go) satisfies it directly, so
// connection credentials ride the exact same AES-256-GCM envelope
// ([version_byte | nonce | ciphertext]) that backs suite_secrets. Tests use
// secrets.NewCipherFromKey to build a fake-key cipher.
type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(envelope []byte) ([]byte, error)
	KeyID() string
}

// credential is the plaintext secret material for a connection. Exactly one
// of the api_key path (APIKey) or the oauth path (AccessToken [+
// RefreshToken]) is populated. It exists only transiently, decrypted, inside
// the runtime — it is never returned to app code, logged, or placed on a
// Connection.
type credential struct {
	APIKey       string `json:"api_key,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// token returns the value injected as the outbound auth header: the API key
// for api_key connections, otherwise the OAuth access token.
func (c credential) token(kind string) string {
	if kind == KindAPIKey {
		return c.APIKey
	}
	return c.AccessToken
}

// sealCredential JSON-encodes then AES-256-GCM-encrypts a credential using
// the shared secrets cipher. The output is stored verbatim in
// suite_connections.encrypted_credentials.
func sealCredential(c Cipher, cred credential) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("connections: cipher not configured")
	}
	plaintext, err := json.Marshal(cred)
	if err != nil {
		return nil, fmt.Errorf("connections: marshal credential: %w", err)
	}
	sealed, err := c.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("connections: encrypt credential: %w", err)
	}
	return sealed, nil
}

// openCredential reverses sealCredential. A nil/empty envelope yields the
// zero credential (a connection may legitimately carry no stored secret,
// e.g. an OAuth connection created before its consent round-trip completes).
func openCredential(c Cipher, envelope []byte) (credential, error) {
	if len(envelope) == 0 {
		return credential{}, nil
	}
	if c == nil {
		return credential{}, fmt.Errorf("connections: cipher not configured")
	}
	plaintext, err := c.Decrypt(envelope)
	if err != nil {
		return credential{}, fmt.Errorf("connections: decrypt credential: %w", err)
	}
	var cred credential
	if err := json.Unmarshal(plaintext, &cred); err != nil {
		return credential{}, fmt.Errorf("connections: unmarshal credential: %w", err)
	}
	return cred, nil
}
