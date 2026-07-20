// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// CreateParams collects everything needed to persist a new connection. The
// credential is sealed with the shared cipher before it touches the DB.
type CreateParams struct {
	Provider        string
	Kind            string
	Name            string
	Cred            credential
	RequestedScopes []string
	GrantedScopes   []string
	TokenExpiry     *time.Time
	WebhookSecret   string
	CreatedBy       string
}

// Loaded is a fully-hydrated connection: public metadata plus the decrypted
// credential + webhook secret. It never leaves the package — Service uses it
// to inject auth and verify webhooks; only Loaded.Conn (credential-free) is
// ever returned to callers.
type Loaded struct {
	Conn          Connection
	cred          credential
	webhookSecret string
}

// Store is the persistence boundary. The DB-backed implementation is
// dbStore; tests inject an in-memory fake so Service logic (refresh
// single-flight, auth injection, revoked handling) is exercised without a
// database.
type Store interface {
	Create(ctx context.Context, tenantID string, p CreateParams) (Connection, error)
	List(ctx context.Context, tenantID string) ([]Connection, error)
	Load(ctx context.Context, tenantID, id string) (Loaded, error)
	SaveTokens(ctx context.Context, tenantID, id string, cred credential, expiry *time.Time, grantedScopes []string) error
	UpdateStatus(ctx context.Context, tenantID, id, status string) error
	InsertEvent(ctx context.Context, tenantID, connID, eventType string, metadata map[string]any) error
}

// dbStore is the Postgres-backed Store. It seals credentials with the shared
// secrets cipher and stores webhook signing secrets in the vault (only a ref
// lands in suite_connections.webhook_secret_ref), matching the oauth_tokens
// defense-in-depth pattern.
type dbStore struct {
	pool   *pgxpool.Pool
	cipher Cipher
	vault  *secrets.Vault
	log    *slog.Logger
}

// NewStore constructs the DB-backed Store. pool and cipher are required;
// vault may be nil (webhook secrets then cannot be stored/loaded).
func NewStore(pool *pgxpool.Pool, cipher Cipher, vault *secrets.Vault, log *slog.Logger) (Store, error) {
	if pool == nil {
		return nil, errors.New("connections: store requires a database pool")
	}
	if cipher == nil {
		return nil, errors.New("connections: store requires a cipher")
	}
	if log == nil {
		log = slog.Default()
	}
	return &dbStore{pool: pool, cipher: cipher, vault: vault, log: log}, nil
}

func webhookSecretKey(id string) string { return "connection/" + id + "/webhook" }

func (s *dbStore) Create(ctx context.Context, tenantID string, p CreateParams) (Connection, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")

	sealed, err := sealCredential(s.cipher, p.Cred)
	if err != nil {
		return Connection{}, err
	}
	// A credential with no material seals to a non-empty envelope; store
	// NULL instead so an unconnected oauth row is honestly empty.
	var credCol []byte
	if p.Cred != (credential{}) {
		credCol = sealed
	}

	req := p.RequestedScopes
	if req == nil {
		req = []string{}
	}
	granted := p.GrantedScopes
	if granted == nil {
		granted = []string{}
	}

	var id string
	var createdAt, updatedAt time.Time
	err = s.pool.QueryRow(ctx, `
		insert into suite_connections
			(tenant_id, provider, kind, name, encrypted_credentials, kms_key_id,
			 requested_scopes, granted_scopes, status, token_expiry, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, 'active', $9, $10)
		returning id, created_at, updated_at
	`, tenantID, p.Provider, p.Kind, p.Name, credCol, s.cipher.KeyID(),
		req, granted, p.TokenExpiry, nullString(p.CreatedBy),
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		return Connection{}, fmt.Errorf("connections: insert: %w", err)
	}

	if p.WebhookSecret != "" && s.vault != nil {
		key := webhookSecretKey(id)
		if _, err := s.vault.Put(ctx, tenantID, key, secrets.PutInput{
			Value:       p.WebhookSecret,
			Description: "Webhook signing secret for connection " + id,
		}); err != nil {
			return Connection{}, fmt.Errorf("connections: store webhook secret: %w", err)
		}
		if _, err := s.pool.Exec(ctx, `
			update suite_connections set webhook_secret_ref = $3, updated_at = now()
			where tenant_id = $1 and id = $2
		`, tenantID, id, key); err != nil {
			return Connection{}, fmt.Errorf("connections: set webhook ref: %w", err)
		}
	}

	return Connection{
		ID:               id,
		Provider:         p.Provider,
		Kind:             p.Kind,
		Name:             p.Name,
		GrantedScopes:    granted,
		RequestedScopes:  req,
		Status:           StatusActive,
		TokenExpiry:      p.TokenExpiry,
		HasWebhookSecret: p.WebhookSecret != "",
		CreatedBy:        p.CreatedBy,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}, nil
}

func (s *dbStore) List(ctx context.Context, tenantID string) ([]Connection, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	rows, err := s.pool.Query(ctx, `
		select id, provider, kind, name, granted_scopes, requested_scopes,
		       status, token_expiry, webhook_secret_ref, created_by,
		       created_at, updated_at
		from suite_connections
		where tenant_id = $1
		order by created_at desc
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("connections: list: %w", err)
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connections: list rows: %w", err)
	}
	return out, nil
}

func (s *dbStore) Load(ctx context.Context, tenantID, id string) (Loaded, error) {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	var (
		conn       Connection
		envelope   []byte
		webhookRef *string
		createdBy  *string
	)
	err := s.pool.QueryRow(ctx, `
		select id, provider, kind, name, granted_scopes, requested_scopes,
		       status, token_expiry, encrypted_credentials, webhook_secret_ref,
		       created_by, created_at, updated_at
		from suite_connections
		where tenant_id = $1 and id = $2
	`, tenantID, id).Scan(
		&conn.ID, &conn.Provider, &conn.Kind, &conn.Name, &conn.GrantedScopes,
		&conn.RequestedScopes, &conn.Status, &conn.TokenExpiry, &envelope,
		&webhookRef, &createdBy, &conn.CreatedAt, &conn.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Loaded{}, ErrNotFound
		}
		return Loaded{}, fmt.Errorf("connections: load: %w", err)
	}
	if createdBy != nil {
		conn.CreatedBy = *createdBy
	}
	conn.HasWebhookSecret = webhookRef != nil && *webhookRef != ""

	cred, err := openCredential(s.cipher, envelope)
	if err != nil {
		return Loaded{}, err
	}

	var webhookSecret string
	if conn.HasWebhookSecret && s.vault != nil {
		raw, err := s.vault.Get(ctx, tenantID, *webhookRef)
		if err != nil && !errors.Is(err, secrets.ErrSecretNotFound) {
			return Loaded{}, fmt.Errorf("connections: load webhook secret: %w", err)
		}
		webhookSecret = string(raw)
	}

	return Loaded{Conn: conn, cred: cred, webhookSecret: webhookSecret}, nil
}

func (s *dbStore) SaveTokens(ctx context.Context, tenantID, id string, cred credential, expiry *time.Time, grantedScopes []string) error {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	sealed, err := sealCredential(s.cipher, cred)
	if err != nil {
		return err
	}
	granted := grantedScopes
	if granted == nil {
		granted = []string{}
	}
	tag, err := s.pool.Exec(ctx, `
		update suite_connections
		set encrypted_credentials = $3,
		    kms_key_id            = $4,
		    token_expiry          = $5,
		    granted_scopes        = $6,
		    status                = 'active',
		    updated_at            = now()
		where tenant_id = $1 and id = $2
	`, tenantID, id, sealed, s.cipher.KeyID(), expiry, granted)
	if err != nil {
		return fmt.Errorf("connections: save tokens: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *dbStore) UpdateStatus(ctx context.Context, tenantID, id, status string) error {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	tag, err := s.pool.Exec(ctx, `
		update suite_connections set status = $3, updated_at = now()
		where tenant_id = $1 and id = $2
	`, tenantID, id, status)
	if err != nil {
		return fmt.Errorf("connections: update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *dbStore) InsertEvent(ctx context.Context, tenantID, connID, eventType string, metadata map[string]any) error {
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	if metadata == nil {
		metadata = map[string]any{}
	}
	metaBytes, _ := json.Marshal(metadata)
	_, err := s.pool.Exec(ctx, `
		insert into suite_connection_events (tenant_id, connection_id, event_type, metadata)
		values ($1, $2, $3, $4)
	`, tenantID, connID, eventType, metaBytes)
	if err != nil {
		return fmt.Errorf("connections: insert event: %w", err)
	}
	return nil
}

// scanConnection reads a metadata-only row (no credentials) into a
// Connection. Shared by List.
func scanConnection(rows pgx.Rows) (Connection, error) {
	var (
		c          Connection
		webhookRef *string
		createdBy  *string
	)
	if err := rows.Scan(
		&c.ID, &c.Provider, &c.Kind, &c.Name, &c.GrantedScopes, &c.RequestedScopes,
		&c.Status, &c.TokenExpiry, &webhookRef, &createdBy,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return Connection{}, fmt.Errorf("connections: scan: %w", err)
	}
	if createdBy != nil {
		c.CreatedBy = *createdBy
	}
	c.HasWebhookSecret = webhookRef != nil && *webhookRef != ""
	return c, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
