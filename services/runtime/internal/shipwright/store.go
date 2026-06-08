// SPDX-License-Identifier: Apache-2.0

package shipwright

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

type dbPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	pool dbPool
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("shipwright: pool required")
	}
	return NewWithPool(pool), nil
}

func NewWithPool(pool dbPool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreateTask(ctx context.Context, in CreateTaskInput) (Task, error) {
	if s == nil || s.pool == nil {
		return Task{}, errors.New("shipwright: store not configured")
	}
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Task{}, ErrTenantRequired
	}
	title := strings.TrimSpace(in.Title)
	description := strings.TrimSpace(in.Description)
	repoURL := strings.TrimSpace(in.RepoURL)
	if title == "" {
		return Task{}, fmt.Errorf("%w: title is required", ErrValidation)
	}
	if description == "" {
		return Task{}, fmt.Errorf("%w: description is required", ErrValidation)
	}
	if err := validateRepoURL(repoURL); err != nil {
		return Task{}, err
	}
	var userArg any
	if strings.TrimSpace(in.UserID) != "" {
		userArg = strings.TrimSpace(in.UserID)
	}
	row := s.pool.QueryRow(ctx, `
		insert into suite_shipwright_tasks
			(tenant_id, user_id, title, description, repo_url, status, created_at, updated_at)
		values
			($1, $2, $3, $4, $5, $6, now(), now())
		returning id::text, tenant_id::text, user_id::text, title, description,
		          repo_url, status, run_id, created_at, updated_at
	`, tenantID, userArg, title, description, repoURL, StatusQueued)
	return scanTask(row)
}

func (s *Store) StartTask(ctx context.Context, taskID, runID string) (Task, error) {
	return s.updateTaskRun(ctx, taskID, runID, StatusRunning)
}

func (s *Store) MarkTaskFailed(ctx context.Context, taskID string) (Task, error) {
	return s.updateTaskStatus(ctx, taskID, StatusFailed)
}

func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	if s == nil || s.pool == nil {
		return Task{}, errors.New("shipwright: store not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Task{}, fmt.Errorf("%w: task id is required", ErrValidation)
	}
	row := s.pool.QueryRow(ctx, `
		select id::text, tenant_id::text, user_id::text, title, description,
		       repo_url, status, run_id, created_at, updated_at
		from suite_shipwright_tasks
		where id = $1
	`, id)
	return scanTask(row)
}

func (s *Store) ListTasks(ctx context.Context, in ListTasksInput) (ListTasksResult, error) {
	if s == nil || s.pool == nil {
		return ListTasksResult{}, errors.New("shipwright: store not configured")
	}
	if tenantctx.TenantID(ctx) == "" {
		return ListTasksResult{Tasks: []Task{}}, ErrTenantRequired
	}
	status := strings.TrimSpace(in.Status)
	if status != "" && !validStatus(status) {
		return ListTasksResult{Tasks: []Task{}}, fmt.Errorf("%w: invalid status", ErrValidation)
	}
	limit := clamp(in.Limit, 1, 100, 25)
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	args := []any{limit, offset}
	where := "true"
	if status != "" {
		args = append(args, status)
		where = fmt.Sprintf("status = $%d", len(args))
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		with filtered as (
			select id::text, tenant_id::text, user_id::text, title, description,
			       repo_url, status, run_id, created_at, updated_at,
			       count(*) over() as total
			from suite_shipwright_tasks
			where %s
			order by created_at desc
			limit $1 offset $2
		)
		select * from filtered
	`, where), args...)
	if err != nil {
		return ListTasksResult{}, fmt.Errorf("shipwright: list tasks: %w", err)
	}
	defer rows.Close()
	out := ListTasksResult{Tasks: []Task{}}
	for rows.Next() {
		var t Task
		var total int
		if err := rows.Scan(&t.ID, &t.TenantID, &t.UserID, &t.Title, &t.Description,
			&t.RepoURL, &t.Status, &t.RunID, &t.CreatedAt, &t.UpdatedAt, &total); err != nil {
			return ListTasksResult{}, fmt.Errorf("shipwright: scan task: %w", err)
		}
		out.Tasks = append(out.Tasks, t)
		out.Total = total
	}
	if err := rows.Err(); err != nil {
		return ListTasksResult{}, fmt.Errorf("shipwright: list tasks rows: %w", err)
	}
	out.HasMore = offset+len(out.Tasks) < out.Total
	return out, nil
}

func (s *Store) CompleteTask(ctx context.Context, in CompleteTaskInput) (Task, error) {
	if s == nil || s.pool == nil {
		return Task{}, errors.New("shipwright: store not configured")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = StatusSucceeded
	}
	if status != StatusSucceeded && status != StatusFailed && status != StatusCancelled {
		return Task{}, fmt.Errorf("%w: completion status must be succeeded, failed, or cancelled", ErrValidation)
	}
	task, err := s.updateTaskStatus(ctx, in.TaskID, status)
	if err != nil {
		return Task{}, err
	}
	if status == StatusSucceeded && strings.TrimSpace(in.Ref) != "" {
		if _, err := s.UpsertPatch(ctx, Patch{
			TaskID:  task.ID,
			Ref:     strings.TrimSpace(in.Ref),
			Summary: strings.TrimSpace(in.Summary),
			DiffURL: nilIfEmpty(in.DiffURL),
		}); err != nil {
			return Task{}, err
		}
	}
	return task, nil
}

func (s *Store) ListPatches(ctx context.Context, taskID string) ([]Patch, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("shipwright: store not configured")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("%w: task id is required", ErrValidation)
	}
	rows, err := s.pool.Query(ctx, `
		select task_id::text, ref, summary, diff_url, created_at
		from suite_shipwright_patches
		where task_id = $1
		order by created_at desc
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("shipwright: list patches: %w", err)
	}
	defer rows.Close()
	patches := []Patch{}
	for rows.Next() {
		var p Patch
		if err := rows.Scan(&p.TaskID, &p.Ref, &p.Summary, &p.DiffURL, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("shipwright: scan patch: %w", err)
		}
		patches = append(patches, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("shipwright: list patches rows: %w", err)
	}
	return patches, nil
}

func (s *Store) UpsertPatch(ctx context.Context, p Patch) (Patch, error) {
	taskID := strings.TrimSpace(p.TaskID)
	ref := strings.TrimSpace(p.Ref)
	if taskID == "" || ref == "" {
		return Patch{}, fmt.Errorf("%w: task_id and ref are required", ErrValidation)
	}
	row := s.pool.QueryRow(ctx, `
		insert into suite_shipwright_patches (task_id, ref, summary, diff_url, created_at)
		values ($1, $2, $3, $4, now())
		on conflict (task_id, ref) do update set
			summary = excluded.summary,
			diff_url = excluded.diff_url,
			created_at = now()
		returning task_id::text, ref, summary, diff_url, created_at
	`, taskID, ref, strings.TrimSpace(p.Summary), p.DiffURL)
	return scanPatch(row)
}

func (s *Store) updateTaskRun(ctx context.Context, taskID, runID, status string) (Task, error) {
	taskID = strings.TrimSpace(taskID)
	runID = strings.TrimSpace(runID)
	if taskID == "" || runID == "" {
		return Task{}, fmt.Errorf("%w: task id and run id are required", ErrValidation)
	}
	row := s.pool.QueryRow(ctx, `
		update suite_shipwright_tasks
		set run_id = $2, status = $3, updated_at = now()
		where id = $1
		returning id::text, tenant_id::text, user_id::text, title, description,
		          repo_url, status, run_id, created_at, updated_at
	`, taskID, runID, status)
	return scanTask(row)
}

func (s *Store) updateTaskStatus(ctx context.Context, taskID, status string) (Task, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, fmt.Errorf("%w: task id is required", ErrValidation)
	}
	if !validStatus(status) {
		return Task{}, fmt.Errorf("%w: invalid status", ErrValidation)
	}
	row := s.pool.QueryRow(ctx, `
		update suite_shipwright_tasks
		set status = $2, updated_at = now()
		where id = $1
		returning id::text, tenant_id::text, user_id::text, title, description,
		          repo_url, status, run_id, created_at, updated_at
	`, taskID, status)
	return scanTask(row)
}

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	if err := row.Scan(&t.ID, &t.TenantID, &t.UserID, &t.Title, &t.Description,
		&t.RepoURL, &t.Status, &t.RunID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("shipwright: scan task: %w", err)
	}
	return t, nil
}

func scanPatch(row pgx.Row) (Patch, error) {
	var p Patch
	if err := row.Scan(&p.TaskID, &p.Ref, &p.Summary, &p.DiffURL, &p.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Patch{}, ErrNotFound
		}
		return Patch{}, fmt.Errorf("shipwright: scan patch: %w", err)
	}
	return p, nil
}

func validateRepoURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: repo_url is required", ErrValidation)
	}
	if strings.HasPrefix(raw, "git@") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: repo_url must be an https or git URL", ErrValidation)
	}
	switch u.Scheme {
	case "https", "http", "ssh", "git":
		return nil
	default:
		return fmt.Errorf("%w: repo_url must be an https or git URL", ErrValidation)
	}
}

func validStatus(status string) bool {
	switch status {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func clamp(v, min, max, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func nilIfEmpty(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
