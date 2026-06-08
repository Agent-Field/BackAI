// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCipherDefaultsToEnvProvider(t *testing.T) {
	t.Setenv("AF_STACK_KMS_PROVIDER", "")
	t.Setenv("AF_STACK_KMS_KEY", strings.Repeat("cd", 32))

	c, err := LoadCipher(context.Background(), nil)
	if err != nil {
		t.Fatalf("LoadCipher: %v", err)
	}
	if c.DevMode() {
		t.Fatal("explicit AF_STACK_KMS_KEY should not produce dev-mode cipher")
	}
	if c.KeyID() != CurrentKeyID {
		t.Fatalf("KeyID = %q, want %q", c.KeyID(), CurrentKeyID)
	}
}

func TestLoadCipherRejectsUnsupportedProvider(t *testing.T) {
	t.Setenv("AF_STACK_KMS_PROVIDER", "not-a-provider")
	if _, err := LoadCipher(context.Background(), nil); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestCloudProvidersRequireEncryptedDataKey(t *testing.T) {
	for _, provider := range []string{KMSProviderAWS, KMSProviderGCP, KMSProviderAzure} {
		t.Run(provider, func(t *testing.T) {
			t.Setenv("AF_STACK_KMS_PROVIDER", provider)
			t.Setenv("AF_STACK_KMS_ENCRYPTED_DATA_KEY", "")
			if _, err := LoadCipher(context.Background(), nil); err == nil {
				t.Fatal("expected missing encrypted data key error")
			}
		})
	}
}

func TestEncryptedDataKeyFromFile(t *testing.T) {
	want := []byte("ciphertext")
	dir := t.TempDir()
	path := filepath.Join(dir, "dek.b64")
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(want)), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("AF_STACK_KMS_ENCRYPTED_DATA_KEY", "")
	t.Setenv("AF_STACK_KMS_ENCRYPTED_DATA_KEY_FILE", path)
	got, err := encryptedDataKeyFromEnv()
	if err != nil {
		t.Fatalf("encryptedDataKeyFromEnv: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("decoded data key = %q, want %q", got, want)
	}
}

func TestCipherFromUnwrappedDataKeyUsesProviderKeyID(t *testing.T) {
	key := []byte(strings.Repeat("a", kekSize))
	c, err := cipherFromUnwrappedDataKey(key, "aws:arn:example")
	if err != nil {
		t.Fatalf("cipherFromUnwrappedDataKey: %v", err)
	}
	if c.KeyID() != "aws:arn:example" {
		t.Fatalf("KeyID = %q", c.KeyID())
	}
	if c.DevMode() {
		t.Fatal("cloud KMS data key should not be marked dev-mode")
	}
}

func TestCipherFromUnwrappedDataKeyRejectsWrongSize(t *testing.T) {
	if _, err := cipherFromUnwrappedDataKey([]byte("short"), "aws:k"); err == nil {
		t.Fatal("expected short data key error")
	}
}
