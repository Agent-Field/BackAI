# Secrets KMS Adapters

AF Stack stores secret values in `suite_secrets` with AES-256-GCM. The
runtime data key can come from a local env value or from a cloud KMS
provider.

## Provider selector

```bash
AF_STACK_KMS_PROVIDER=env # env | aws | gcp | azure
```

## `env`

Quickstart and local self-hosting:

```bash
AF_STACK_KMS_PROVIDER=env
AF_STACK_KMS_KEY=<64 hex chars from openssl rand -hex 32>
```

## Cloud BYOK

For AWS, GCP, and Azure, create a random 32-byte data key, encrypt or
wrap it with your cloud KMS key, then provide the encrypted blob as
base64:

```bash
AF_STACK_KMS_ENCRYPTED_DATA_KEY=<base64 encrypted data key>
# or
AF_STACK_KMS_ENCRYPTED_DATA_KEY_FILE=/run/secrets/af-stack-dek.b64
```

The runtime unwraps the data key once at boot. Secret reads/writes still
use the same local AES-GCM vault path, and rows record the provider key
ID in `suite_secrets.kms_key_id`.

### AWS KMS

```bash
AF_STACK_KMS_PROVIDER=aws
AF_STACK_AWS_KMS_KEY_ID=arn:aws:kms:us-east-1:123456789012:key/...
AWS_REGION=us-east-1
AF_STACK_KMS_ENCRYPTED_DATA_KEY=...
```

The runtime uses the AWS SDK default credential chain and calls
`kms:Decrypt`.

### GCP Cloud KMS

```bash
AF_STACK_KMS_PROVIDER=gcp
AF_STACK_GCP_KMS_KEY_NAME=projects/.../locations/.../keyRings/.../cryptoKeys/...
AF_STACK_KMS_ENCRYPTED_DATA_KEY=...
```

The runtime uses Application Default Credentials and calls Cloud KMS
`Decrypt`.

### Azure Key Vault

```bash
AF_STACK_KMS_PROVIDER=azure
AF_STACK_AZURE_KEY_VAULT_URL=https://example.vault.azure.net/
AF_STACK_AZURE_KMS_KEY_NAME=af-stack-secrets
AF_STACK_AZURE_KMS_KEY_VERSION=
AF_STACK_AZURE_KMS_ALGORITHM=RSA-OAEP-256
AF_STACK_KMS_ENCRYPTED_DATA_KEY=...
```

The runtime uses Azure DefaultAzureCredential and calls Key Vault
`UnwrapKey`.
