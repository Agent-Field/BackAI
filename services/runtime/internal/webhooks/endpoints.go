// SPDX-License-Identifier: Apache-2.0

// endpoints.go — persistence for suite_webhook_endpoints.
//
// The dashboard's CRUD surface (POST/GET/DELETE /api/v1/webhooks/endpoints)
// is a thin wrapper around this store. Slugs are globally unique because
// the inbound URL /webhooks/in/<slug> is routed before any tenant
// context exists.
package webhooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EndpointStore reads + writes suite_webhook_endpoints rows.
type EndpointStore struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewEndpointStore constructs an EndpointStore. pool may be nil; log
// defaults to slog.Default().
func NewEndpointStore(pool *pgxpool.Pool, log *slog.Logger) *EndpointStore {
	if log == nil {
		log = slog.Default()
	}
	return &EndpointStore{pool: pool, log: log}
}

// HasPool reports whether the store can talk to a database.
func (s *EndpointStore) HasPool() bool {
	return s != nil && s.pool != nil
}

// Defaults applied at Create when the input leaves a field unset.
const (
	defaultDedupWindowS    = 86400
	defaultProvider        = "custom"
	defaultSignatureAlgo   = "sha256"
	defaultSignatureHeader = "X-Signature"
	maxSlugLen             = 128
	maxForwardLen          = 1024
)

// Create inserts a new endpoint row. The slug must be unique; a
// uniqueness violation maps to ErrSlugConflict so the REST layer can
// return 409.
func (s *EndpointStore) Create(ctx context.Context, in CreateEndpointInput) (*Endpoint, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if err := validateCreateInput(&in); err != nil {
		return nil, err
	}

	var tenantPtr *string
	if in.TenantID != "" {
		t := in.TenantID
		tenantPtr = &t
	}
	var algoPtr, hdrPtr *string
	if in.SignatureAlgorithm != "" {
		v := in.SignatureAlgorithm
		algoPtr = &v
	}
	if in.SignatureHeader != "" {
		v := in.SignatureHeader
		hdrPtr = &v
	}

	const q = `
		insert into suite_webhook_endpoints
			(slug, tenant_id, provider, secret_key_ref, signature_algorithm,
			 signature_header, forward_to, dedup_window_s, is_active)
		values ($1, $2, $3, $4, $5, $6, $7, $8, true)
		returning id, created_at
	`
	var (
		id        string
		createdAt time.Time
	)
	err := s.pool.QueryRow(ctx, q,
		in.Slug,
		tenantPtr,
		in.Provider,
		in.SecretKeyRef,
		algoPtr,
		hdrPtr,
		in.ForwardTo,
		in.DedupWindowS,
	).Scan(&id, &createdAt)
	if err != nil {
		// pgx returns the underlying SQLSTATE in the wrapped pgconn error;
		// 23505 = unique_violation on the slug index.
		if isUniqueViolation(err) {
			return nil, ErrSlugConflict
		}
		return nil, fmt.Errorf("webhooks: insert endpoint: %w", err)
	}

	ep := &Endpoint{
		ID:           id,
		Slug:         in.Slug,
		TenantID:     tenantPtr,
		Provider:     in.Provider,
		SecretKeyRef: in.SecretKeyRef,
		ForwardTo:    in.ForwardTo,
		DedupWindowS: in.DedupWindowS,
		IsActive:     true,
		CreatedAt:    createdAt.UTC().Format(time.RFC3339Nano),
	}
	ep.SignatureAlgorithm = algoPtr
	ep.SignatureHeader = hdrPtr
	return ep, nil
}

// List returns every endpoint matching the filter. The dashboard fetches
// every row; pagination isn't needed in v1.
func (s *EndpointStore) List(ctx context.Context, f ListEndpointsFilter) ([]Endpoint, error) {
	if !s.HasPool() {
		return []Endpoint{}, nil
	}
	conds := []string{}
	args := []any{}
	if f.TenantID != "" {
		args = append(args, f.TenantID)
		conds = append(conds, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}
	q := endpointSelectColumns + " from suite_webhook_endpoints" + where +
		" order by created_at desc"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("webhooks: list endpoints: %w", err)
	}
	defer rows.Close()
	out := make([]Endpoint, 0, 16)
	for rows.Next() {
		ep, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches by id. Returns ErrEndpointNotFound when no row matches.
func (s *EndpointStore) Get(ctx context.Context, id string) (*Endpoint, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	rows, err := s.pool.Query(ctx, endpointSelectColumns+` from suite_webhook_endpoints where id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("webhooks: select endpoint: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrEndpointNotFound
	}
	return scanEndpoint(rows)
}

// GetBySlug is the lookup used by the inbound HTTP handler. Returns
// ErrEndpointNotFound when no row matches.
func (s *EndpointStore) GetBySlug(ctx context.Context, slug string) (*Endpoint, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	rows, err := s.pool.Query(ctx, endpointSelectColumns+` from suite_webhook_endpoints where slug = $1`, slug)
	if err != nil {
		return nil, fmt.Errorf("webhooks: select endpoint by slug: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrEndpointNotFound
	}
	return scanEndpoint(rows)
}

// Delete removes a single row by id. Returns ErrEndpointNotFound when
// the row doesn't exist (idempotent delete is a footgun for the
// dashboard — better to surface the miss).
func (s *EndpointStore) Delete(ctx context.Context, id string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	tag, err := s.pool.Exec(ctx, `delete from suite_webhook_endpoints where id = $1`, id)
	if err != nil {
		return fmt.Errorf("webhooks: delete endpoint: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEndpointNotFound
	}
	return nil
}

// ─── internals ────────────────────────────────────────────────────────────

const endpointSelectColumns = `
	select id, slug, tenant_id, provider, secret_key_ref,
	       signature_algorithm, signature_header, forward_to,
	       dedup_window_s, is_active, created_at
`

func scanEndpoint(rows pgx.Rows) (*Endpoint, error) {
	var (
		id           string
		slug         string
		tenantID     *string
		provider     string
		secretRef    *string
		sigAlgo      *string
		sigHdr       *string
		forwardTo    string
		dedupWindowS int
		isActive     bool
		createdAt    time.Time
	)
	if err := rows.Scan(
		&id, &slug, &tenantID, &provider, &secretRef,
		&sigAlgo, &sigHdr, &forwardTo, &dedupWindowS, &isActive, &createdAt,
	); err != nil {
		return nil, fmt.Errorf("webhooks: scan endpoint: %w", err)
	}
	return &Endpoint{
		ID:                 id,
		Slug:               slug,
		TenantID:           tenantID,
		Provider:           provider,
		SecretKeyRef:       secretRef,
		SignatureAlgorithm: sigAlgo,
		SignatureHeader:    sigHdr,
		ForwardTo:          forwardTo,
		DedupWindowS:       dedupWindowS,
		IsActive:           isActive,
		CreatedAt:          createdAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// validateCreateInput normalises the input in-place and rejects bad
// shapes. Defaults applied here so the row always carries a sane
// dedup window + provider regardless of how the caller spelled the
// request.
func validateCreateInput(in *CreateEndpointInput) error {
	if in == nil {
		return fmt.Errorf("%w: nil input", ErrInvalidInput)
	}
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Slug == "" {
		return fmt.Errorf("%w: slug required", ErrInvalidInput)
	}
	if len(in.Slug) > maxSlugLen {
		return fmt.Errorf("%w: slug exceeds %d chars", ErrInvalidInput, maxSlugLen)
	}
	if !validSlug(in.Slug) {
		return fmt.Errorf("%w: slug must be [a-z0-9._-]", ErrInvalidInput)
	}
	in.ForwardTo = strings.TrimSpace(in.ForwardTo)
	if in.ForwardTo == "" {
		return fmt.Errorf("%w: forward_to required", ErrInvalidInput)
	}
	if len(in.ForwardTo) > maxForwardLen {
		return fmt.Errorf("%w: forward_to exceeds %d chars", ErrInvalidInput, maxForwardLen)
	}
	if !strings.HasPrefix(in.ForwardTo, "http://") &&
		!strings.HasPrefix(in.ForwardTo, "https://") &&
		!strings.HasPrefix(in.ForwardTo, "af://agents/") {
		return fmt.Errorf("%w: forward_to must be http(s)://... or af://agents/<name>", ErrUnsupportedForward)
	}
	in.Provider = strings.TrimSpace(in.Provider)
	if in.Provider == "" {
		in.Provider = defaultProvider
	}
	in.SignatureAlgorithm = strings.TrimSpace(strings.ToLower(in.SignatureAlgorithm))
	in.SignatureHeader = strings.TrimSpace(in.SignatureHeader)
	if in.SecretKeyRef != nil {
		// Apply per-provider defaults for the signature config when the
		// caller supplied a secret but no algo/header.
		if in.SignatureAlgorithm == "" {
			in.SignatureAlgorithm = defaultSignatureAlgo
		}
		if in.SignatureHeader == "" {
			in.SignatureHeader = signatureHeaderForProvider(in.Provider)
		}
	}
	if in.DedupWindowS <= 0 {
		in.DedupWindowS = defaultDedupWindowS
	}
	return nil
}

func validSlug(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// signatureHeaderForProvider returns the conventional signature header
// for well-known providers. Falls through to a generic "X-Signature"
// when the provider is custom / unknown.
func signatureHeaderForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "github":
		return "X-Hub-Signature-256"
	case "stripe":
		return "Stripe-Signature"
	case "shopify":
		return "X-Shopify-Hmac-Sha256"
	default:
		return defaultSignatureHeader
	}
}

// isUniqueViolation reports whether err is a PG SQLSTATE 23505. The
// EndpointStore Create path uses this to translate slug collisions into
// ErrSlugConflict.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps the pgconn error; we match on the textual SQLSTATE
	// rather than importing pgconn just for the const, since the
	// surface is already wrapped with %w by the time it reaches us.
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "unique constraint")
}