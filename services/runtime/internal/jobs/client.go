// SPDX-License-Identifier: Apache-2.0

// client.go wraps the River client with the suite's name-based dispatch
// API and exposes the high-level operations the runtime needs: Enqueue,
// Get, List, Retry, Cancel, and Summary.
//
// The Manager owns:
//   - the Registry of job definitions (names, languages, cron)
//   - the River client (Postgres queue, scheduler, fetcher)
//   - cron tickers for definitions that have a `Cron` field and are NOT
//     handled by Go (those use River's PeriodicJobs natively, which
//     accepts robfig/cron schedules)
//
// Lifecycle: NewManager() -> Register*(...) -> Start(ctx) -> Stop(ctx).
//
// The dashboard layer reads jobs back via List/Get/Summary; the REST
// endpoints in services/runtime/internal/server/jobs.go marshal those into
// the JobSchema shape from apps/dashboard/src/lib/api.ts.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

// aliasesMu guards the package-level kindAliasesAll slice. Reads happen
// from dispatchArgs.KindAliases() which River calls at AddWorker time on
// the zero value (so we can't store aliases on the struct itself).
var (
	aliasesMu      sync.RWMutex
	kindAliasesAll []string
)

func setAliases(names []string) {
	aliasesMu.Lock()
	defer aliasesMu.Unlock()
	out := make([]string, len(names))
	copy(out, names)
	kindAliasesAll = out
}

func registeredAliases() []string {
	aliasesMu.RLock()
	defer aliasesMu.RUnlock()
	out := make([]string, len(kindAliasesAll))
	copy(out, kindAliasesAll)
	return out
}

// ErrRemoteJobsNotSupported was returned by Enqueue when a job's definition
// was a remote language (python/typescript) back when this runtime had no
// cross-language dispatcher. Remote jobs are now supported via the pull-based
// worker protocol (remote_store.go / remote_executor.go), so Enqueue no
// longer returns this. The sentinel is retained for backward compatibility
// with callers (e.g. the REST handler) that still branch on it.
var ErrRemoteJobsNotSupported = errors.New("jobs: remote-language jobs are not supported by this runtime")

// Manager is the public handle for the jobs module.
type Manager struct {
	log      *slog.Logger
	registry *Registry
	pool     *pgxpool.Pool
	client   *river.Client[pgx.Tx]
	driver   *riverpgxv5.Driver
	workers  *river.Workers
	schema   string

	// runCtx is the long-lived context handed to river.Client.Start. It's
	// derived from context.Background() so a short-lived startup ctx
	// doesn't cause River to shut down immediately after Start returns.
	// Cancelled by Manager.Stop.
	runCtx    context.Context
	runCancel context.CancelFunc

	// remoteCronTickers tracks goroutines for remote (non-Go) cron defs
	// that don't go through River's PeriodicJobs (since their handler is
	// not a Go function — we still need to enqueue the row).
	cronCancels []context.CancelFunc
	cronWG      sync.WaitGroup

	// remote is the DB-backed rendezvous store for remote (python/ts) jobs
	// executed by out-of-process pull workers. Nil disables remote jobs.
	remote RemoteStore
	// remoteParams tunes the executor's lease polling.
	remoteParams remoteParams
}

// Config configures a Manager.
type Config struct {
	// Pool is the pgx connection pool. Required.
	Pool *pgxpool.Pool

	// Logger is the structured logger. If nil, slog.Default() is used.
	Logger *slog.Logger

	// MaxWorkers caps concurrent workers in the default queue.
	// Defaults to 25.
	MaxWorkers int

	// RunWorkers controls whether this Manager actually processes jobs.
	// When false, the Manager can still Enqueue/List but won't pull rows
	// off the queue. Useful for test clients that only want to push jobs.
	RunWorkers bool

	// Schema is the Postgres schema River queries against. Defaults to
	// empty (resolved via search_path at runtime). Set this for tests
	// that share a database across runs but isolate via schema — River
	// uses the Schema field to scope its LISTEN/NOTIFY topics, so leaving
	// it empty can cause stale notifications across test schemas.
	Schema string
}

// NewManager constructs a Manager.
//
// Migrations must be applied before constructing the client; call
// MigrateUp(ctx, pool) first if you're not sure they've been run.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Pool == nil {
		return nil, errors.New("jobs.NewManager: nil pool")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	registry := NewRegistry(log)
	return &Manager{
		log:          log,
		registry:     registry,
		pool:         cfg.Pool,
		driver:       riverpgxv5.New(cfg.Pool),
		workers:      river.NewWorkers(),
		schema:       cfg.Schema,
		remote:       newPGRemoteStore(cfg.Pool),
		remoteParams: defaultRemoteParams(),
	}, nil
}

// Registry exposes the underlying registry so callers can register
// Go/Remote job definitions before Start is called.
func (m *Manager) Registry() *Registry { return m.registry }

// RegisterSampleJob installs the built-in `sample-job` Go handler. Used by
// the smoke test and by integration tests to prove end-to-end works.
func (m *Manager) RegisterSampleJob() error {
	return m.registry.RegisterGo(Definition{
		Name:        "sample-job",
		Language:    LanguageGo,
		Description: "Built-in sample job: sleeps 500ms then logs its message.",
		SourcePath:  "services/runtime/internal/jobs/sample.go",
		MaxAttempts: 3,
	}, sampleJobHandler(m.log))
}

// sampleJobHandler returns the live Go handler for "sample-job".
func sampleJobHandler(log *slog.Logger) func(ctx context.Context, args []byte) error {
	return func(ctx context.Context, args []byte) error {
		var payload struct {
			Message string `json:"message"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &payload); err != nil {
				return fmt.Errorf("sample-job: bad args: %w", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		log.Info("sample-job ran", "message", payload.Message)
		return nil
	}
}

// Start wires River up and (when configured) begins consuming jobs.
//
// Must be called AFTER all Register* calls so the worker registers under
// all known kind aliases. Idempotent failures are not supported — callers
// should treat any error here as fatal at boot.
func (m *Manager) Start(ctx context.Context, runWorkers bool, maxWorkers int) error {
	// Snapshot all registered kinds — primary + aliases. River requires
	// the primary kind be distinct from aliases, so we use the umbrella
	// sentinel as primary and put EVERY real kind in aliases.
	defs := m.registry.All()
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	setAliases(names)

	// Register the umbrella worker. River will key it under umbrellaKind
	// (primary) plus every name in `names` (via KindAliases).
	river.AddWorker(m.workers, &dispatcherWorker{registry: m.registry, manager: m})

	if maxWorkers <= 0 {
		maxWorkers = 25
	}

	periodic, err := m.buildPeriodicJobs()
	if err != nil {
		return fmt.Errorf("jobs.Start: build periodic jobs: %w", err)
	}

	cfg := &river.Config{
		Logger:       m.log,
		Workers:      m.workers,
		PeriodicJobs: periodic,
		Schema:       m.schema,
	}
	if runWorkers {
		cfg.Queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: maxWorkers},
		}
	}

	client, err := river.NewClient(m.driver, cfg)
	if err != nil {
		return fmt.Errorf("jobs.Start: new river client: %w", err)
	}
	m.client = client

	if runWorkers {
		// River keeps running until the context passed to Start is
		// cancelled. We derive a fresh long-lived context from
		// Background here so the caller's short-lived startup context
		// (typically wrapped in a 10s timeout) can be cancelled without
		// killing River. Manager.Stop cancels m.runCancel.
		m.runCtx, m.runCancel = context.WithCancel(context.Background())
		if err := client.Start(m.runCtx); err != nil {
			m.runCancel()
			return fmt.Errorf("jobs.Start: river client start: %w", err)
		}
		m.log.Info("jobs manager started", "kinds", len(names), "max_workers", maxWorkers)
	} else {
		m.log.Info("jobs manager started in enqueue-only mode", "kinds", len(names))
	}

	// Remote cron tickers (non-Go definitions only — River's PeriodicJobs
	// works fine for Go definitions because we have a worker).
	for _, d := range defs {
		if d.Cron == "" || d.HasLiveHandler() {
			continue
		}
		if err := m.spawnRemoteCron(ctx, d); err != nil {
			m.log.Error("failed to spawn remote cron", "kind", d.Name, "error", err)
		}
	}

	return nil
}

// buildPeriodicJobs collects all definitions with Cron set AND a live Go
// handler. Remote-cron definitions go through manual goroutines (we don't
// have a worker, so PeriodicJobs would queue rows we couldn't run; we
// still want to record them, but for now we skip and the SDKs can poll).
func (m *Manager) buildPeriodicJobs() ([]*river.PeriodicJob, error) {
	var out []*river.PeriodicJob
	for _, d := range m.registry.All() {
		if d.Cron == "" || !d.HasLiveHandler() {
			continue
		}
		schedule, err := cron.ParseStandard(d.Cron)
		if err != nil {
			return nil, fmt.Errorf("definition %q invalid cron %q: %w", d.Name, d.Cron, err)
		}
		// Capture by value to avoid closure capture pitfalls.
		kindName := d.Name
		out = append(out, river.NewPeriodicJob(
			schedule,
			func() (river.JobArgs, *river.InsertOpts) {
				return dispatchArgs{
					Payload:  json.RawMessage("{}"),
					kindName: kindName,
				}, nil
			},
			nil,
		))
	}
	return out, nil
}

// spawnRemoteCron runs a goroutine that fires a tick at each cron firing
// and enqueues the named job. Used for remote (non-Go) definitions where
// we want the schedule but don't have an in-process handler.
func (m *Manager) spawnRemoteCron(ctx context.Context, def *Definition) error {
	schedule, err := cron.ParseStandard(def.Cron)
	if err != nil {
		return fmt.Errorf("invalid cron %q: %w", def.Cron, err)
	}
	tickerCtx, cancel := context.WithCancel(ctx)
	m.cronCancels = append(m.cronCancels, cancel)
	m.cronWG.Add(1)
	go func(name string) {
		defer m.cronWG.Done()
		for {
			now := time.Now()
			next := schedule.Next(now)
			d := time.Until(next)
			if d < 0 {
				d = time.Second
			}
			select {
			case <-tickerCtx.Done():
				return
			case <-time.After(d):
				if _, err := m.Enqueue(tickerCtx, name, json.RawMessage("{}"), EnqueueOpts{}); err != nil {
					m.log.Warn("remote cron enqueue failed",
						"kind", name, "error", err)
				}
			}
		}
	}(def.Name)
	return nil
}

// Stop drains and stops the River client and any cron tickers.
func (m *Manager) Stop(ctx context.Context) error {
	for _, cancel := range m.cronCancels {
		cancel()
	}
	m.cronWG.Wait()
	var stopErr error
	if m.client != nil {
		stopErr = m.client.Stop(ctx)
	}
	if m.runCancel != nil {
		m.runCancel()
	}
	return stopErr
}

// ─── Enqueue / Get / List / Retry / Cancel / Summary ──────────────────────

// EnqueueOpts mirrors the bits of EnqueueJobInput we actually pass through.
type EnqueueOpts struct {
	ScheduledAt time.Time
	MaxAttempts int
	TenantID    string
}

// Enqueue inserts a new job of the given name with the given args.
func (m *Manager) Enqueue(ctx context.Context, name string, args json.RawMessage, opts EnqueueOpts) (*rivertype.JobRow, error) {
	if name == "" {
		return nil, errors.New("jobs.Enqueue: name required")
	}
	def := m.registry.Get(name)
	if def == nil {
		return nil, fmt.Errorf("jobs.Enqueue: unknown kind %q", name)
	}
	// Both Go (in-process handler) and remote (python/typescript) definitions
	// are enqueued the same way: a River row. Go kinds run in-process via the
	// dispatcher worker; remote kinds block on the DB-backed rendezvous while
	// an out-of-process pull worker leases and executes them (see worker.go /
	// remote_executor.go). Remote execution requires a started manager with
	// running workers.
	if m.client == nil {
		return nil, errors.New("jobs.Enqueue: manager not started")
	}
	if len(args) == 0 {
		args = json.RawMessage("null")
	}
	insertOpts := &river.InsertOpts{}
	if !opts.ScheduledAt.IsZero() {
		insertOpts.ScheduledAt = opts.ScheduledAt
	}
	if opts.MaxAttempts > 0 {
		insertOpts.MaxAttempts = opts.MaxAttempts
	}
	res, err := m.client.Insert(ctx, dispatchArgs{
		Payload:  args,
		TenantID: opts.TenantID,
		kindName: name,
	}, insertOpts)
	if err != nil {
		return nil, fmt.Errorf("jobs.Enqueue: %w", err)
	}
	return res.Job, nil
}

// Get fetches a single job by id.
func (m *Manager) Get(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	if m.client == nil {
		return nil, errors.New("jobs.Get: manager not started")
	}
	return m.client.JobGet(ctx, id)
}

// ListFilters is a flat filter struct used by List.
type ListFilters struct {
	Name     string
	State    string
	TenantID string
	Limit    int
	Offset   int
}

// ListResult is the result of a paginated List.
type ListResult struct {
	Jobs    []*rivertype.JobRow
	Total   int
	HasMore bool
}

// List returns jobs filtered by the given filters.
//
// Pagination uses limit/offset by sorting on id DESC. River's native
// pagination is cursor-based; we run a fresh query per page so offset
// works straightforwardly for the dashboard.
func (m *Manager) List(ctx context.Context, f ListFilters) (ListResult, error) {
	if m.client == nil {
		return ListResult{Jobs: []*rivertype.JobRow{}}, errors.New("jobs.List: manager not started")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	// River's JobList doesn't support OFFSET directly; emulate via
	// First(offset+limit) and slicing.
	params := river.NewJobListParams().First(offset+limit).OrderBy(river.JobListOrderByID, river.SortOrderDesc)
	if f.Name != "" {
		params = params.Kinds(f.Name)
	}
	if f.State != "" {
		state := rivertype.JobState(f.State)
		params = params.States(state)
	}
	if f.TenantID != "" {
		// Tenant filter relies on the JSON args column, since River
		// doesn't have a first-class tenant column. The column is `args`
		// (jsonb) on river_job — NOT `encoded_args` (that was a wrong
		// reference that only surfaced once tenant-scoped jobs actually
		// carried a tenant_id and this filter began to fire).
		params = params.Where(
			"args->>'tenant_id' = @tenant_id",
			river.NamedArgs{"tenant_id": f.TenantID},
		)
	}

	res, err := m.client.JobList(ctx, params)
	if err != nil {
		return ListResult{Jobs: []*rivertype.JobRow{}}, fmt.Errorf("jobs.List: %w", err)
	}
	jobs := res.Jobs
	total := len(jobs)
	if offset >= total {
		return ListResult{Jobs: []*rivertype.JobRow{}, Total: total, HasMore: false}, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := jobs[offset:end]
	return ListResult{
		Jobs:    page,
		Total:   total,
		HasMore: end < total,
	}, nil
}

// Retry marks a job retryable so it runs again. Mirrors river.JobRetry.
func (m *Manager) Retry(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	if m.client == nil {
		return nil, errors.New("jobs.Retry: manager not started")
	}
	return m.client.JobRetry(ctx, id)
}

// Cancel cancels a job.
func (m *Manager) Cancel(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	if m.client == nil {
		return nil, errors.New("jobs.Cancel: manager not started")
	}
	return m.client.JobCancel(ctx, id)
}

// QueueSummary returns a snapshot of pending/running/failed/succeeded counts
// plus the recent N rows for the dashboard's /api/v1/queues/summary endpoint.
type QueueSummary struct {
	Pending        int
	Running        int
	Failed         int
	SucceededToday int
	Recent         []*rivertype.JobRow
}

// Summary builds a QueueSummary by aggregating River's job state counts.
func (m *Manager) Summary(ctx context.Context, recentLimit int) (QueueSummary, error) {
	out := QueueSummary{Recent: []*rivertype.JobRow{}}
	if m.client == nil {
		return out, nil
	}
	if recentLimit <= 0 {
		recentLimit = 50
	}

	// One pass per state — small, bounded, and cheap. River's JobList
	// returns up to First() rows; for counts we just take the length.
	pending, err := m.countByStates(ctx, []rivertype.JobState{
		rivertype.JobStateAvailable,
		rivertype.JobStateScheduled,
		rivertype.JobStateRetryable,
		rivertype.JobStatePending,
	})
	if err != nil {
		return out, err
	}
	out.Pending = pending

	running, err := m.countByStates(ctx, []rivertype.JobState{rivertype.JobStateRunning})
	if err != nil {
		return out, err
	}
	out.Running = running

	failed, err := m.countByStates(ctx, []rivertype.JobState{
		rivertype.JobStateDiscarded,
		rivertype.JobStateCancelled,
	})
	if err != nil {
		return out, err
	}
	out.Failed = failed

	// SucceededToday: completed rows finalized within the last 24h.
	completed, err := m.client.JobList(ctx, river.NewJobListParams().
		First(10_000).
		States(rivertype.JobStateCompleted))
	if err != nil {
		return out, err
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, j := range completed.Jobs {
		if j.FinalizedAt != nil && j.FinalizedAt.After(cutoff) {
			out.SucceededToday++
		}
	}

	// Recent: all states, most recent first.
	recent, err := m.client.JobList(ctx, river.NewJobListParams().
		First(recentLimit).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc))
	if err != nil {
		return out, err
	}
	out.Recent = recent.Jobs
	return out, nil
}

func (m *Manager) countByStates(ctx context.Context, states []rivertype.JobState) (int, error) {
	if m.client == nil {
		return 0, nil
	}
	res, err := m.client.JobList(ctx, river.NewJobListParams().
		First(10_000).
		States(states...))
	if err != nil {
		return 0, err
	}
	return len(res.Jobs), nil
}

// JobIDFromString parses a job-id string (used by REST handlers).
func JobIDFromString(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// JobIDToString renders a job id as a decimal string. River ids are int64
// and can exceed JS's safe-integer range, so they always cross the wire as
// strings (matching the dashboard's JobSchema.id).
func JobIDToString(id int64) string {
	return strconv.FormatInt(id, 10)
}

// JobList exposes the River client's JobList directly. Used by the
// registry's RecentStatsFor helper so it can produce per-kind stats
// without holding a River client reference itself.
//
// Returns an empty result if the manager isn't started.
func (m *Manager) JobList(ctx context.Context, params *river.JobListParams) (*river.JobListResult, error) {
	if m.client == nil {
		return &river.JobListResult{}, nil
	}
	return m.client.JobList(ctx, params)
}
