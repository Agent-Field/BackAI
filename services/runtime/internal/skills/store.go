// SPDX-License-Identifier: Apache-2.0

// store.go — persistence layer for suite_skills + suite_skill_attachments.
//
// Store wraps a pgxpool and exposes the operations the REST + SDK
// surfaces consume. When pool is nil every method returns
// ErrNotConfigured so the runtime can still boot without a DB.
//
// Both tables have RLS enabled (see migration 00013_skills.sql). The
// runtime sets app.tenant_id per request via the tenant resolver; admin
// listings that span tenants opt into app.bypass_rls=on transactionally
// elsewhere.
package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the suite_skills + suite_skill_attachments accessor.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewStore constructs a Store. pool may be nil — every method then
// returns ErrNotConfigured. log defaults to slog.Default().
func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, log: log}
}

// HasPool reports whether the Store can talk to a database. Used by
// the REST layer for the 503 SKILLS_NOT_CONFIGURED branch.
func (s *Store) HasPool() bool {
	return s != nil && s.pool != nil
}

// List returns the skills visible to the caller, optionally filtered by
// tenant + harness. Results are ordered installed_at desc so the
// dashboard renders newest-first without client work.
//
// TenantID semantics:
//   - "" (default): match shared bundles (tenant_id IS NULL) and the
//     resolved tenant from the GUC. We don't filter on tenant_id
//     directly here because the RLS policy already does the scoping —
//     we just trust the policy.
//   - "shared": only the tenant_id IS NULL rows.
//   - <uuid>:   rows whose tenant_id equals the UUID (requires
//     app.bypass_rls=on or matching app.tenant_id GUC).
//
// Harness filters by membership in the skill's harnesses JSONB array.
// The "any" sentinel matches every skill (special-cased so dashboards
// can offer a "show all" toggle without omitting the param).
func (s *Store) List(ctx context.Context, f ListFilters) ([]Skill, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}

	conds := []string{}
	args := []any{}

	tenant := strings.TrimSpace(f.TenantID)
	switch tenant {
	case "":
		// no extra predicate — RLS handles it.
	case "shared":
		conds = append(conds, "tenant_id is null")
	default:
		args = append(args, tenant)
		conds = append(conds, fmt.Sprintf("(tenant_id = $%d or tenant_id is null)", len(args)))
	}

	harness := strings.TrimSpace(f.Harness)
	if harness != "" && strings.ToLower(harness) != "any" {
		args = append(args, harness)
		conds = append(conds, fmt.Sprintf("(harnesses @> to_jsonb(array[$%d::text]) or harnesses @> '[\"any\"]'::jsonb)", len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}

	q := `
		select id, name, version, source, description,
		       harnesses, tags, tenant_id, installed_at
		  from suite_skills` + where + `
		 order by installed_at desc, id asc`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("skills: list query: %w", err)
	}
	defer rows.Close()

	out := []Skill{}
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches a single skill by id. Returns ErrNotFound when no row
// matches (or when RLS hides it from the caller).
func (s *Store) Get(ctx context.Context, id string) (*Skill, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	rows, err := s.pool.Query(ctx, `
		select id, name, version, source, description,
		       harnesses, tags, tenant_id, installed_at
		  from suite_skills
		 where id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("skills: get: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return scanSkill(rows)
}

// Install persists a Skill row. Idempotent: re-installing the same id
// updates the existing row's metadata (name/version/source/description/
// harnesses/tags) and bumps installed_at to now(). tenant_id is taken
// verbatim from the input — "" means a shared bundle (NULL column).
//
// Returns the persisted Skill (with the canonical installed_at timestamp
// the DB stamped).
func (s *Store) Install(ctx context.Context, sk Skill) (*Skill, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if sk.ID == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	if sk.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if sk.Version == "" {
		return nil, fmt.Errorf("%w: version is required", ErrInvalidInput)
	}
	if sk.Source == "" {
		return nil, fmt.Errorf("%w: source is required", ErrInvalidInput)
	}
	if sk.Harnesses == nil {
		sk.Harnesses = []string{}
	}
	if sk.Tags == nil {
		sk.Tags = []string{}
	}

	harnessesJSON, err := json.Marshal(sk.Harnesses)
	if err != nil {
		return nil, fmt.Errorf("skills: encode harnesses: %w", err)
	}
	tagsJSON, err := json.Marshal(sk.Tags)
	if err != nil {
		return nil, fmt.Errorf("skills: encode tags: %w", err)
	}

	var tenantPtr *string
	if sk.TenantID != nil && *sk.TenantID != "" {
		t := *sk.TenantID
		tenantPtr = &t
	}

	const q = `
		insert into suite_skills
		    (id, name, version, source, description, harnesses, tags, tenant_id)
		values
		    ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8)
		on conflict (id) do update set
		    name         = excluded.name,
		    version      = excluded.version,
		    source       = excluded.source,
		    description  = excluded.description,
		    harnesses    = excluded.harnesses,
		    tags         = excluded.tags,
		    tenant_id    = excluded.tenant_id,
		    installed_at = now()
		returning installed_at`

	var installedAt time.Time
	if err := s.pool.QueryRow(ctx, q,
		sk.ID, sk.Name, sk.Version, sk.Source, sk.Description,
		harnessesJSON, tagsJSON, tenantPtr,
	).Scan(&installedAt); err != nil {
		return nil, fmt.Errorf("skills: install: %w", err)
	}

	out := sk
	out.TenantID = tenantPtr
	out.InstalledAt = installedAt.UTC().Format(time.RFC3339Nano)
	return &out, nil
}

// Uninstall removes a skill by id. The ON DELETE CASCADE on
// suite_skill_attachments cleans up associated attachments. Returns
// ErrNotFound if no row was deleted.
func (s *Store) Uninstall(ctx context.Context, id string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	tag, err := s.pool.Exec(ctx, `delete from suite_skills where id = $1`, id)
	if err != nil {
		return fmt.Errorf("skills: uninstall: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Attach binds a skill onto an agent. Idempotent: re-attaching the
// same (skill_id, agent) pair is a no-op (ON CONFLICT DO NOTHING).
// Returns ErrNotFound if the skill doesn't exist.
func (s *Store) Attach(ctx context.Context, skillID, agent string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	if skillID == "" {
		return fmt.Errorf("%w: skill_id is required", ErrInvalidInput)
	}
	if agent == "" {
		return fmt.Errorf("%w: agent is required", ErrInvalidInput)
	}
	tag, err := s.pool.Exec(ctx, `
		insert into suite_skill_attachments (skill_id, agent)
		values ($1, $2)
		on conflict (skill_id, agent) do nothing
	`, skillID, agent)
	if err != nil {
		// pgx surfaces foreign-key violations as PgError 23503; we
		// translate that to ErrNotFound so the REST layer can return
		// a sensible 404 rather than 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrNotFound
		}
		return fmt.Errorf("skills: attach: %w", err)
	}
	_ = tag
	return nil
}

// Detach removes one (skill_id, agent) row. Returns ErrNotFound if no
// row was deleted (so callers can distinguish "wasn't attached" from
// success).
func (s *Store) Detach(ctx context.Context, skillID, agent string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	if skillID == "" {
		return fmt.Errorf("%w: skill_id is required", ErrInvalidInput)
	}
	if agent == "" {
		return fmt.Errorf("%w: agent is required", ErrInvalidInput)
	}
	tag, err := s.pool.Exec(ctx, `
		delete from suite_skill_attachments
		 where skill_id = $1 and agent = $2
	`, skillID, agent)
	if err != nil {
		return fmt.Errorf("skills: detach: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAttachments returns every (skill_id, agent) pair for the given
// skill, ordered attached_at desc. An empty result is not an error.
func (s *Store) ListAttachments(ctx context.Context, skillID string) ([]Attachment, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if skillID == "" {
		return nil, fmt.Errorf("%w: skill_id is required", ErrInvalidInput)
	}
	rows, err := s.pool.Query(ctx, `
		select skill_id, agent, attached_at
		  from suite_skill_attachments
		 where skill_id = $1
		 order by attached_at desc, agent asc
	`, skillID)
	if err != nil {
		return nil, fmt.Errorf("skills: list attachments: %w", err)
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var (
			sid, agent string
			attachedAt time.Time
		)
		if err := rows.Scan(&sid, &agent, &attachedAt); err != nil {
			return nil, fmt.Errorf("skills: scan attachment: %w", err)
		}
		out = append(out, Attachment{
			SkillID:    sid,
			Agent:      agent,
			AttachedAt: attachedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanSkill reads one suite_skills row into a Skill. Used by both List
// and Get so the column order stays in one place.
func scanSkill(rows pgx.Rows) (*Skill, error) {
	var (
		id, name, version, source string
		description               *string
		harnessesRaw, tagsRaw     []byte
		tenantID                  *string
		installedAt               time.Time
	)
	if err := rows.Scan(&id, &name, &version, &source, &description,
		&harnessesRaw, &tagsRaw, &tenantID, &installedAt); err != nil {
		return nil, fmt.Errorf("skills: scan: %w", err)
	}
	harnesses := []string{}
	if len(harnessesRaw) > 0 {
		if err := json.Unmarshal(harnessesRaw, &harnesses); err != nil {
			return nil, fmt.Errorf("skills: decode harnesses: %w", err)
		}
	}
	tags := []string{}
	if len(tagsRaw) > 0 {
		if err := json.Unmarshal(tagsRaw, &tags); err != nil {
			return nil, fmt.Errorf("skills: decode tags: %w", err)
		}
	}
	if harnesses == nil {
		harnesses = []string{}
	}
	if tags == nil {
		tags = []string{}
	}
	return &Skill{
		ID:          id,
		Name:        name,
		Version:     version,
		Source:      source,
		Description: description,
		Harnesses:   harnesses,
		Tags:        tags,
		TenantID:    tenantID,
		InstalledAt: installedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}