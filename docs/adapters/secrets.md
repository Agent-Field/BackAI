# Secrets Adapters

Two orthogonal selectors govern secrets:

- **`AF_STACK_SECRETS_ADAPTER`** — WHERE secret values are stored (the
  store backend). Covered directly below.
- **`AF_STACK_KMS_PROVIDER`** — HOW the encryption data key is wrapped
  (env value vs cloud KMS). Covered under [KMS provider](#provider-selector).

## Store adapter (`AF_STACK_SECRETS_ADAPTER`)

```bash
AF_STACK_SECRETS_ADAPTER=vault # vault | remote
```

| Adapter | Use |
|---|---|
| `vault` | (default) built-in Postgres-backed AES-256-GCM vault |
| `remote` | An out-of-process secrets backend speaking the [remote protocol](PROTOCOL.md) |

The selector is validated at boot — an unsupported value fails fast rather
than being silently ignored (which is what it used to do).

```bash
AF_STACK_SECRETS_ADAPTER=remote
AF_STACK_SECRETS_REMOTE_URL=https://secrets-adapter.example.com
AF_STACK_SECRETS_REMOTE_TOKEN=<bearer-token>
```

Remote secrets credentials are **env-only** — the vault can't be used to
configure its own backend (chicken-and-egg), so these are read from env at
boot, not from the Integrations UI.

> **Known limitation (roadmap).** The `remote` secrets adapter is
> selectable, validated, and capability-probed, but the server currently
> binds the concrete vault type, so a remote backend cannot yet fully back
> `/api/v1/secrets` end-to-end. Generalizing the server's secrets
> dependency to the `Store` interface is a follow-up. Until then, treat
> `remote` secrets as partial: selectable and health-checked, but not a
> finished replacement for the built-in vault.

# KMS provider

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
