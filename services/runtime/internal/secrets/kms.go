// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	cloudkms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
)

const (
	KMSProviderEnv   = "env"
	KMSProviderAWS   = "aws"
	KMSProviderGCP   = "gcp"
	KMSProviderAzure = "azure"
)

// KMSConfigured reports whether the operator has explicitly opted into
// real (non-dev) key management. When true, a LoadCipher/Preflight
// failure must be fatal at boot rather than silently disabling the
// vault — the operator signalled production intent, so a broken key is
// a misconfiguration to surface, not a soft fallback.
//
// True when AF_STACK_KMS_PROVIDER names anything other than the local
// env provider (including a typo, which we'd rather fail loudly on), or
// when AF_STACK_KMS_KEY holds a non-empty value that isn't the dev
// sentinel. False for the zero-config dev path (unset / sentinel key,
// env provider), which boots a dev KEK.
func KMSConfigured() bool {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_KMS_PROVIDER")))
	if provider != "" && provider != KMSProviderEnv && provider != "local" {
		return true
	}
	key := strings.TrimSpace(os.Getenv("AF_STACK_KMS_KEY"))
	if key != "" && !strings.EqualFold(key, devKEKSentinel) {
		return true
	}
	return false
}

// LoadCipher builds the vault cipher from the configured KMS provider.
//
// AF_STACK_KMS_PROVIDER=env preserves the legacy AF_STACK_KMS_KEY path.
// Cloud providers unwrap AF_STACK_KMS_ENCRYPTED_DATA_KEY at boot and use
// the resulting 32-byte data key for the existing AES-GCM vault.
func LoadCipher(ctx context.Context, log *slog.Logger) (*Cipher, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_KMS_PROVIDER")))
	if provider == "" {
		provider = KMSProviderEnv
	}
	switch provider {
	case KMSProviderEnv, "local":
		return LoadKEK(log)
	case KMSProviderAWS, "aws-kms":
		return loadAWSKMSCipher(ctx)
	case KMSProviderGCP, "gcp-kms":
		return loadGCPKMSCipher(ctx)
	case KMSProviderAzure, "azure-key-vault", "azure-kms":
		return loadAzureKMSCipher(ctx)
	default:
		return nil, fmt.Errorf("secrets: unsupported AF_STACK_KMS_PROVIDER %q", provider)
	}
}

func loadAWSKMSCipher(ctx context.Context) (*Cipher, error) {
	encrypted, err := encryptedDataKeyFromEnv()
	if err != nil {
		return nil, err
	}
	keyID := strings.TrimSpace(os.Getenv("AF_STACK_AWS_KMS_KEY_ID"))
	region := strings.TrimSpace(os.Getenv("AWS_REGION"))
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("secrets: aws kms config: %w", err)
	}
	in := &awskms.DecryptInput{CiphertextBlob: encrypted}
	if keyID != "" {
		in.KeyId = aws.String(keyID)
	}
	out, err := awskms.NewFromConfig(cfg).Decrypt(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("secrets: aws kms decrypt data key: %w", err)
	}
	if keyID == "" && out.KeyId != nil {
		keyID = *out.KeyId
	}
	if keyID == "" {
		keyID = "aws-kms"
	}
	return cipherFromUnwrappedDataKey(out.Plaintext, "aws:"+keyID)
}

func loadGCPKMSCipher(ctx context.Context) (*Cipher, error) {
	encrypted, err := encryptedDataKeyFromEnv()
	if err != nil {
		return nil, err
	}
	keyName := strings.TrimSpace(os.Getenv("AF_STACK_GCP_KMS_KEY_NAME"))
	if keyName == "" {
		return nil, errors.New("secrets: AF_STACK_GCP_KMS_KEY_NAME is required for gcp KMS")
	}
	client, err := cloudkms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcp kms client: %w", err)
	}
	defer client.Close()
	out, err := client.Decrypt(ctx, &kmspb.DecryptRequest{
		Name:       keyName,
		Ciphertext: encrypted,
	})
	if err != nil {
		return nil, fmt.Errorf("secrets: gcp kms decrypt data key: %w", err)
	}
	return cipherFromUnwrappedDataKey(out.Plaintext, "gcp:"+keyName)
}

func loadAzureKMSCipher(ctx context.Context) (*Cipher, error) {
	encrypted, err := encryptedDataKeyFromEnv()
	if err != nil {
		return nil, err
	}
	vaultURL := strings.TrimSpace(os.Getenv("AF_STACK_AZURE_KEY_VAULT_URL"))
	keyName := strings.TrimSpace(os.Getenv("AF_STACK_AZURE_KMS_KEY_NAME"))
	keyVersion := strings.TrimSpace(os.Getenv("AF_STACK_AZURE_KMS_KEY_VERSION"))
	algValue := strings.TrimSpace(os.Getenv("AF_STACK_AZURE_KMS_ALGORITHM"))
	if algValue == "" {
		algValue = string(azkeys.EncryptionAlgorithmRSAOAEP256)
	}
	if vaultURL == "" || keyName == "" {
		return nil, errors.New("secrets: AF_STACK_AZURE_KEY_VAULT_URL and AF_STACK_AZURE_KMS_KEY_NAME are required for azure KMS")
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: azure credential: %w", err)
	}
	client, err := azkeys.NewClient(vaultURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: azure key client: %w", err)
	}
	alg := azkeys.EncryptionAlgorithm(algValue)
	out, err := client.UnwrapKey(ctx, keyName, keyVersion, azkeys.KeyOperationParameters{
		Algorithm: &alg,
		Value:     encrypted,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: azure kms unwrap data key: %w", err)
	}
	keyID := "azure:" + strings.TrimRight(vaultURL, "/") + "/keys/" + keyName
	if keyVersion != "" {
		keyID += "/" + keyVersion
	}
	return cipherFromUnwrappedDataKey(out.Result, keyID)
}

func encryptedDataKeyFromEnv() ([]byte, error) {
	if file := strings.TrimSpace(os.Getenv("AF_STACK_KMS_ENCRYPTED_DATA_KEY_FILE")); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("secrets: read AF_STACK_KMS_ENCRYPTED_DATA_KEY_FILE: %w", err)
		}
		return decodeEncryptedDataKey(string(raw))
	}
	return decodeEncryptedDataKey(os.Getenv("AF_STACK_KMS_ENCRYPTED_DATA_KEY"))
}

func decodeEncryptedDataKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("secrets: AF_STACK_KMS_ENCRYPTED_DATA_KEY is required for cloud KMS providers")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: AF_STACK_KMS_ENCRYPTED_DATA_KEY must be base64: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("secrets: AF_STACK_KMS_ENCRYPTED_DATA_KEY decoded to empty data")
	}
	return data, nil
}

func cipherFromUnwrappedDataKey(dataKey []byte, keyID string) (*Cipher, error) {
	if len(dataKey) != kekSize {
		return nil, fmt.Errorf("secrets: cloud KMS data key must decrypt to %d bytes (got %d)", kekSize, len(dataKey))
	}
	cipher, err := newCipherWithKeyID(dataKey, keyID, false)
	if err != nil {
		return nil, err
	}
	return cipher, nil
}
