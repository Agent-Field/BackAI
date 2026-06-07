// SPDX-License-Identifier: Apache-2.0

// Package crons is AF Stack's Phase 12.2 cron scheduling layer.
//
// A Cron is a single row in suite_crons describing "every <schedule>,
// enqueue job <job_name> with <args>". The scheduler in this package
// owns a single goroutine that ticks once a minute, claims due rows via
// the Store's ClaimDue helper, fires JobEnqueuer for each, then writes
// last_run_at + next_run_at back. The dashboard mutates rows directly
// through the REST handlers; the scheduler picks changes up on the
// next tick.
//
// Contract notes:
//
//	The wire schemas live in apps/dashboard/src/lib/api.ts (CronSchema,
//	CronListSchema, CreateCronInputSchema). Field names, enum values,
//	and nullable semantics here MUST mirror those zod schemas — drift
//	breaks the dashboard's safeParse and the SDKs that share that
//	contract.
//
// Schedule parsing uses robfig/cron/v3 in standard 5-field mode, which
// also accepts the @hourly / @daily / @weekly / @monthly / @yearly
// macros. We do NOT enable the seconds-resolution dialect — operators
// would shoot themselves in the foot with @every 1s.
package crons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Cron mirrors CronSchema in apps/dashboard/src/lib/api.ts. Pointer
// fields are used for nullable JSON columns (LastRunAt, TenantID) so
// json.Marshal emits a JSON null rather than a zero-value string.
type Cron struct {
	ID         string                 `json:"id"`
	TenantID   *string                `json:"tenant_id"`
	Name       string                 `json:"name"`
	JobName    string                 `json:"job_name"`
	Schedule   string                 `json:"schedule"`
	Args       map[string]any         `json:"args"`
	IsActive   bool                   `json:"is_active"`
	LastRunAt  *string                `json:"last_run_at"`
	NextRunAt  string                 `json:"next_run_at"`
	CreatedAt  string                 `json:"created_at"`
}

// CreateInput mirrors CreateCronInputSchema. TenantID lives outside the
// dashboard wire shape (the dashboard always speaks the operator's own
// tenant, which the resolver attaches via the request context); we
// thread it through here so internal callers can pre-populate shared
// crons.
type CreateInput struct {
	Name     string         `json:"name"`
	JobName  string         `json:"job_name"`
	Schedule string         `json:"schedule"`
	Args     map[string]any `json:"args,omitempty"`
	IsActive *bool          `json:"is_active,omitempty"`
	TenantID string         `json:"-"`
}

// JobEnqueuer is the slim interface the scheduler uses to fire a job
// when a cron is due. Defined here (instead of importing the full jobs
// package) so the crons package stays focused on scheduling — wiring
// happens in main.go where both packages are already available.
type JobEnqueuer interface {
	// Enqueue creates one job row of the given name with the given
	// args. The scheduler invokes this once per due row; failures are
	// logged but do not stop the loop.
	Enqueue(ctx context.Context, name string, args json.RawMessage, tenantID string) error
}

// Sentinel errors. The REST layer maps each to a status + code.
var (
	// ErrInvalidInput covers malformed schedules, empty names, etc.
	ErrInvalidInput = errors.New("crons: invalid input")
	// ErrNotConfigured means the Store has no pool. The REST layer
	// surfaces this as 503 CRONS_NOT_CONFIGURED.
	ErrNotConfigured = errors.New("crons: not configured")
	// ErrNotFound means the id doesn't exist in suite_crons.
	ErrNotFound = errors.New("crons: not found")
)

// ListFilters mirrors the query params on GET /api/v1/crons.
type ListFilters struct {
	TenantID string
}

// scheduleParser is the package-wide cron parser. Standard 5-field
// crontab plus the @hourly / @daily / @weekly / @monthly / @yearly
// macros. Constructed once because cron.ParseStandard allocates.
var scheduleParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// ParseSchedule validates and parses a cron expression. Returns
// ErrInvalidInput on anything robfig/cron refuses.
//
// The returned cron.Schedule has a .Next(time.Time) method the
// scheduler uses to compute the next firing.
func ParseSchedule(expr string) (cron.Schedule, error) {
	if expr == "" {
		return nil, fmt.Errorf("%w: schedule is required", ErrInvalidInput)
	}
	sched, err := scheduleParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return sched, nil
}

// NextRun returns the next firing time for the given expression after
// now. Helper used by the REST create handler when stamping the
// initial next_run_at column.
func NextRun(expr string, now time.Time) (time.Time, error) {
	sched, err := ParseSchedule(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(now), nil
}