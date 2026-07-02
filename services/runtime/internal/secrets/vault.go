// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// RedactedMarker is the canonical placeholder string used wherever a
// secret value would otherwise appear in logs, spans, or errors.
const RedactedMarker = "[REDACTED]"

const tracerName = "af-stack/secrets"

// SecretMetadata is the public, non-secret view of a stored entry. The
// JSON tags match the SecretMetadataSchema in
// apps/dashboard/src/lib/api.ts exactly so handlers can marshal a
// SecretMetadata directly without an extra adapter layer.
type SecretMetadata struct {
	Key         string  `json:"key"`
	TenantID    *string `json:"tenant_id"`
	Description *string `json:"description"`
	RotateAfter *string `json:"rotate_after"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// Vault is the secrets store. Construct one per process and share it.
type Vault struct {
	pool   *pgxpool.Pool
	cipher *Cipher
	log    *slog.Logger
	tracer trace.Tracer
}

// New constructs a Vault. Both pool and cipher must be non-nil.
func New(pool *pgxpool.Pool, cipher *Cipher, log *slog.Logger) (*Vault, error) {
	if pool == nil {
		return nil, errors.New("secrets: pool required")
	}
	if cipher == nil {
		return nil, errors.New("secrets: cipher required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Vault{
		pool:   pool,
		cipher: cipher,
		log:    log,
		tracer: otel.Tracer(tracerName),
	}, nil
}

// PutInput collects the fields for a Put or Rotate.
type PutInput struct {
	Value       string
	Description string
	RotateAfter *time.Time
}

// validateKey rejects empty keys, oversized keys, and any character
// outside [a-z A-Z 0-9 _ - . /]. Keys travel as URL path segments and
// JSON object keys so we keep the charset narrow.
func validateKey(key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	if len(key) > 256 {
		return ErrInvalidKey
	}
	for _, ch := range key {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '_' || ch == '-' || ch == '.' || ch == '/':
		default:
			return ErrInvalidKey
		}
	}
	return nil
}

// Get returns the decrypted plaintext for (tenantID, key). The returned
// byte slice is the only place the plaintext exists outside the GCM
// open call; callers must NOT log it.
func (v *Vault) Get(ctx context.Context, tenantID, key string) ([]byte, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	ctx, span := v.tracer.Start(ctx, "secrets.get",
		trace.WithAttributes(
			attribute.String("secret.key", key),
			attribute.String("secret.tenant_id", tenantID),
		),
	)
	defer span.End()

	if err := validateKey(key); err != nil {
		span.RecordError(err)
		return nil, err
	}

	var envelope []byte
	err := v.pool.QueryRow(ctx, `
		select encrypted_value
		from suite_secrets
		where tenant_id = $1 and key = $2
	`, tenantID, key).Scan(&envelope)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSecretNotFound
		}
		// Never include secret material in error logs; the error path
		// only sees row metadata.
		v.log.Error("secrets: get failed",
			"key", key,
			"tenant_id", tenantID,
			"error", err,
		)
		span.RecordError(err)
		return nil, fmt.Errorf("secrets: get: %w", err)
	}

	plaintext, err := v.cipher.Decrypt(envelope)
	if err != nil {
		v.log.Error("secrets: decrypt failed",
			"key", key,
			"tenant_id", tenantID,
			"value", RedactedMarker,
			"error", err,
		)
		span.RecordError(err)
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	// Span attributes never see the plaintext.
	span.SetAttributes(attribute.Int("secret.plaintext_bytes", len(plaintext)))
	return plaintext, nil
}

// Put inserts or updates a secret. The ON CONFLICT path overwrites the
// ciphertext and bumps updated_at; created_at is preserved.
func (v *Vault) Put(ctx context.Context, tenantID, key string, in PutInput) (SecretMetadata, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	ctx, span := v.tracer.Start(ctx, "secrets.put",
		trace.WithAttributes(
			attribute.String("secret.key", key),
			attribute.String("secret.tenant_id", tenantID),
			attribute.Int("secret.plaintext_bytes", len(in.Value)),
		),
	)
	defer span.End()

	if err := validateKey(key); err != nil {
		span.RecordError(err)
		return SecretMetadata{}, err
	}

	envelope, err := v.cipher.Encrypt([]byte(in.Value))
	if err != nil {
		v.log.Error("secrets: encrypt failed",
			"key", key,
			"tenant_id", tenantID,
			"value", RedactedMarker,
			"error", err,
		)
		span.RecordError(err)
		return SecretMetadata{}, fmt.Errorf("secrets: encrypt: %w", err)
	}

	metaJSON := encodeMeta(in.Description)

	var (
		createdAt time.Time
		updatedAt time.Time
	)
	err = v.pool.QueryRow(ctx, `
		insert into suite_secrets
			(tenant_id, key, encrypted_value, kms_key_id, metadata, rotate_after, created_at, updated_at)
		values
			($1, $2, $3, $4, $5, $6, now(), now())
		on conflict (tenant_id, key) do update set
			encrypted_value = excluded.encrypted_value,
			kms_key_id      = excluded.kms_key_id,
			metadata        = excluded.metadata,
			rotate_after    = excluded.rotate_after,
			updated_at      = now()
		returning created_at, updated_at
	`,
		tenantID, key, envelope, v.cipher.KeyID(), metaJSON, in.RotateAfter,
	).Scan(&createdAt, &updatedAt)
	if err != nil {
		v.log.Error("secrets: put failed",
			"key", key,
			"tenant_id", tenantID,
			"value", RedactedMarker,
			"error", err,
		)
		span.RecordError(err)
		return SecretMetadata{}, fmt.Errorf("secrets: put: %w", err)
	}

	v.log.Info("secrets: put",
		"key", key,
		"tenant_id", tenantID,
		"value", RedactedMarker,
		"bytes", len(in.Value),
	)

	var descPtr *string
	if strings.TrimSpace(in.Description) != "" {
		d := in.Description
		descPtr = &d
	}
	return buildMetadata(key, tenantID, descPtr, in.RotateAfter, createdAt, updatedAt), nil
}

// Delete removes a secret. Returns ErrSecretNotFound if no row matched.
func (v *Vault) Delete(ctx context.Context, tenantID, key string) error {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	ctx, span := v.tracer.Start(ctx, "secrets.delete",
		trace.WithAttributes(
			attribute.String("secret.key", key),
			attribute.String("secret.tenant_id", tenantID),
		),
	)
	defer span.End()

	if err := validateKey(key); err != nil {
		span.RecordError(err)
		return err
	}

	tag, err := v.pool.Exec(ctx, `
		delete from suite_secrets
		where tenant_id = $1 and key = $2
	`, tenantID, key)
	if err != nil {
		v.log.Error("secrets: delete failed",
			"key", key,
			"tenant_id", tenantID,
			"error", err,
		)
		span.RecordError(err)
		return fmt.Errorf("secrets: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSecretNotFound
	}
	v.log.Info("secrets: deleted",
		"key", key,
		"tenant_id", tenantID,
	)
	return nil
}

// List returns metadata for every secret stored under tenantID. Values
// are NEVER materialised by this call.
func (v *Vault) List(ctx context.Context, tenantID string) ([]SecretMetadata, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	ctx, span := v.tracer.Start(ctx, "secrets.list",
		trace.WithAttributes(attribute.String("secret.tenant_id", tenantID)),
	)
	defer span.End()

	rows, err := v.pool.Query(ctx, `
		select key, metadata, rotate_after, created_at, updated_at
		from suite_secrets
		where tenant_id = $1
		order by key asc
	`, tenantID)
	if err != nil {
		v.log.Error("secrets: list failed",
			"tenant_id", tenantID,
			"error", err,
		)
		span.RecordError(err)
		return nil, fmt.Errorf("secrets: list: %w", err)
	}
	defer rows.Close()

	out := []SecretMetadata{}
	for rows.Next() {
		var (
			key         string
			metaBytes   []byte
			rotateAfter *time.Time
			createdAt   time.Time
			updatedAt   time.Time
		)
		if err := rows.Scan(&key, &metaBytes, &rotateAfter, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("secrets: list scan: %w", err)
		}
		desc := decodeMetaDescription(metaBytes)
		out = append(out, buildMetadata(key, tenantID, desc, rotateAfter, createdAt, updatedAt))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("secrets: list rows: %w", err)
	}
	span.SetAttributes(attribute.Int("secret.count", len(out)))
	return out, nil
}

// GetMetadata returns metadata for a single secret without decrypting
// the value. Returns ErrSecretNotFound if the row is missing.
func (v *Vault) GetMetadata(ctx context.Context, tenantID, key string) (SecretMetadata, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	ctx, span := v.tracer.Start(ctx, "secrets.get_metadata",
		trace.WithAttributes(
			attribute.String("secret.key", key),
			attribute.String("secret.tenant_id", tenantID),
		),
	)
	defer span.End()

	if err := validateKey(key); err != nil {
		span.RecordError(err)
		return SecretMetadata{}, err
	}

	var (
		metaBytes   []byte
		rotateAfter *time.Time
		createdAt   time.Time
		updatedAt   time.Time
	)
	err := v.pool.QueryRow(ctx, `
		select metadata, rotate_after, created_at, updated_at
		from suite_secrets
		where tenant_id = $1 and key = $2
	`, tenantID, key).Scan(&metaBytes, &rotateAfter, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SecretMetadata{}, ErrSecretNotFound
		}
		v.log.Error("secrets: get metadata failed",
			"key", key,
			"tenant_id", tenantID,
			"error", err,
		)
		span.RecordError(err)
		return SecretMetadata{}, fmt.Errorf("secrets: get metadata: %w", err)
	}
	desc := decodeMetaDescription(metaBytes)
	return buildMetadata(key, tenantID, desc, rotateAfter, createdAt, updatedAt), nil
}

// Rotate replaces the encrypted value for an existing secret. The
// secret must already exist (returns ErrSecretNotFound otherwise) so
// rotation can't accidentally create new entries.
func (v *Vault) Rotate(ctx context.Context, tenantID, key, newValue string) (SecretMetadata, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	ctx, span := v.tracer.Start(ctx, "secrets.rotate",
		trace.WithAttributes(
			attribute.String("secret.key", key),
			attribute.String("secret.tenant_id", tenantID),
			attribute.Int("secret.plaintext_bytes", len(newValue)),
		),
	)
	defer span.End()

	if err := validateKey(key); err != nil {
		span.RecordError(err)
		return SecretMetadata{}, err
	}

	envelope, err := v.cipher.Encrypt([]byte(newValue))
	if err != nil {
		v.log.Error("secrets: encrypt failed",
			"key", key,
			"tenant_id", tenantID,
			"value", RedactedMarker,
			"error", err,
		)
		span.RecordError(err)
		return SecretMetadata{}, fmt.Errorf("secrets: encrypt: %w", err)
	}

	var (
		metaBytes   []byte
		rotateAfter *time.Time
		createdAt   time.Time
		updatedAt   time.Time
	)
	err = v.pool.QueryRow(ctx, `
		update suite_secrets
		set encrypted_value = $3,
		    kms_key_id      = $4,
		    updated_at      = now()
		where tenant_id = $1 and key = $2
		returning metadata, rotate_after, created_at, updated_at
	`,
		tenantID, key, envelope, v.cipher.KeyID(),
	).Scan(&metaBytes, &rotateAfter, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SecretMetadata{}, ErrSecretNotFound
		}
		v.log.Error("secrets: rotate failed",
			"key", key,
			"tenant_id", tenantID,
			"value", RedactedMarker,
			"error", err,
		)
		span.RecordError(err)
		return SecretMetadata{}, fmt.Errorf("secrets: rotate: %w", err)
	}

	v.log.Info("secrets: rotated",
		"key", key,
		"tenant_id", tenantID,
		"value", RedactedMarker,
	)
	desc := decodeMetaDescription(metaBytes)
	return buildMetadata(key, tenantID, desc, rotateAfter, createdAt, updatedAt), nil
}

// encodeMeta builds the JSONB blob persisted in the metadata column.
// Description is the only non-secret field for now; future fields land
// here too rather than as new columns.
func encodeMeta(description string) []byte {
	m := map[string]any{}
	if strings.TrimSpace(description) != "" {
		m["description"] = description
	}
	b, _ := json.Marshal(m)
	return b
}

// decodeMetaDescription pulls "description" out of the stored JSONB,
// returning nil when absent or empty. The empty-vs-null distinction
// matters because the dashboard's SecretMetadataSchema marks the field
// as nullable string.
func decodeMetaDescription(raw []byte) *string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	v, ok := m["description"].(string)
	if !ok {
		return nil
	}
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

// buildMetadata centralises the SecretMetadata shape so list/get/put
// emit identical JSON.
func buildMetadata(
	key, tenantID string,
	description *string,
	rotateAfter *time.Time,
	createdAt, updatedAt time.Time,
) SecretMetadata {
	var tenantPtr *string
	if tenantID != "" {
		t := tenantID
		tenantPtr = &t
	}
	var rotatePtr *string
	if rotateAfter != nil {
		s := rotateAfter.UTC().Format(time.RFC3339Nano)
		rotatePtr = &s
	}
	return SecretMetadata{
		Key:         key,
		TenantID:    tenantPtr,
		Description: description,
		RotateAfter: rotatePtr,
		CreatedAt:   createdAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   updatedAt.UTC().Format(time.RFC3339Nano),
	}
}