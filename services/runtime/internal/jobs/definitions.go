// SPDX-License-Identifier: Apache-2.0

// Package jobs implements the suite runtime's background-jobs module on top
// of River (https://riverqueue.com), a Postgres-backed durable queue.
//
// This file defines the job-definition registry. A *definition* is the
// metadata side of a job kind: its name, its language, where its source
// lives, and (optionally) a cron schedule. Definitions are how the
// dashboard's `/api/v1/jobs/definitions` endpoint enumerates what kinds of
// work this runtime knows about, regardless of whether any concrete instance
// has been enqueued yet.
//
// Two flavours of definition exist:
//
//   - **Go (native)**: registered live with a River worker. The runtime
//     itself executes the handler in-process.
//   - **Remote (python/typescript)**: the actual handler would live in
//     another service. The cross-language dispatcher does not exist yet, so
//     the runtime cannot execute these. A remote definition may still be
//     *registered* (so the dashboard can enumerate it), but Manager.Enqueue
//     rejects it up front with ErrRemoteJobsNotSupported rather than
//     persisting a row that would silently never run.
//     (TODO Phase 5+: implement webhook callback or river-jobs-listener
//     sidecar so SDK-py / SDK-ts can subscribe and process.)
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Language identifies which runtime executes a job's handler.
type Language string

const (
	LanguageGo         Language = "go"
	LanguagePython     Language = "python"
	LanguageTypeScript Language = "typescript"
)

// Definition is the static metadata about a job kind. Definitions are
// registered at boot time and never change at runtime — only their
// `recent_stats` counters are derived from the live River tables on read.
type Definition struct {
	// Name is the River "kind" string. It uniquely identifies the job
	// across the queue and is what callers pass to Enqueue.
	Name string

	// Language indicates which runtime executes the handler.
	Language Language

	// SourcePath is a human-readable hint at where the handler is defined.
	// Optional; surfaced in the dashboard.
	SourcePath string

	// Description is a short one-liner shown in the dashboard.
	Description string

	// Cron is an optional 5-field crontab string. When set, the jobs
	// package will schedule the kind to fire on that cadence.
	//
	// We rely on River's PeriodicJobs (which accepts any cron.Schedule
	// produced by robfig/cron/v3) when a Go handler is registered. For
	// remote definitions a goroutine ticker is used instead.
	Cron string

	// MaxAttempts is the default max attempts at insertion time. 0 means
	// "use River default (25)".
	MaxAttempts int

	// goHandler is set when this definition has a live Go worker registered
	// via RegisterGo. nil for remote (python/ts) definitions.
	goHandler func(ctx context.Context, args []byte) error
}

// HasLiveHandler reports whether this definition currently has an in-process
// worker. False for remote definitions (python/ts) until a cross-language
// dispatcher lands.
func (d *Definition) HasLiveHandler() bool {
	return d.goHandler != nil
}

// Registry holds the set of known job definitions for a runtime instance.
//
// It's safe for concurrent reads. Writes (RegisterGo / RegisterRemote) are
// expected at boot time only.
type Registry struct {
	mu          sync.RWMutex
	log         *slog.Logger
	definitions map[string]*Definition
}

// NewRegistry constructs an empty registry.
func NewRegistry(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		log:         log,
		definitions: make(map[string]*Definition),
	}
}

// Get returns the definition for the given name, or nil if not registered.
func (r *Registry) Get(name string) *Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.definitions[name]
	if !ok {
		return nil
	}
	return d
}

// All returns a snapshot of all registered definitions.
func (r *Registry) All() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Definition, 0, len(r.definitions))
	for _, d := range r.definitions {
		out = append(out, d)
	}
	return out
}

// RegisterGo binds a native Go handler to a job kind. The handler receives
// raw JSON args and must return nil for success (River marks the job
// completed) or an error for retryable failure.
//
// Returns an error if a definition with the same name already exists.
func (r *Registry) RegisterGo(def Definition, handler func(ctx context.Context, args []byte) error) error {
	if def.Name == "" {
		return fmt.Errorf("jobs: definition name required")
	}
	if handler == nil {
		return fmt.Errorf("jobs: nil handler for %q", def.Name)
	}
	def.Language = LanguageGo
	def.goHandler = handler

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.definitions[def.Name]; exists {
		return fmt.Errorf("jobs: %q already registered", def.Name)
	}
	r.definitions[def.Name] = &def
	r.log.Debug("registered go job", "name", def.Name, "cron", def.Cron)
	return nil
}

// RegisterRemote stores a definition that points at a non-Go handler,
// executed by an external worker over the pull protocol (lease/heartbeat/
// complete on /api/v1/jobs/worker/*). Deployments declare their remote
// kinds via AF_STACK_REMOTE_JOB_KINDS at boot.
func (r *Registry) RegisterRemote(def Definition) error {
	if def.Name == "" {
		return fmt.Errorf("jobs: definition name required")
	}
	switch def.Language {
	case LanguagePython, LanguageTypeScript:
		// ok
	default:
		return fmt.Errorf("jobs: remote language must be python/typescript, got %q", def.Language)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.definitions[def.Name]; exists {
		return fmt.Errorf("jobs: %q already registered", def.Name)
	}
	r.definitions[def.Name] = &def
	r.log.Debug("registered remote job", "name", def.Name, "language", def.Language)
	return nil
}

// RecentStats counts the number of jobs for a kind in the last 24 hours,
// bucketed by terminal state. Used to populate JobDefinition.recent in the
// dashboard schema.
type RecentStats struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Running   int `json:"running"`
}

// statsFromJobs is a helper that buckets a list of JobRows by state.
func statsFromJobs(jobs []*rivertype.JobRow) RecentStats {
	var s RecentStats
	for _, j := range jobs {
		switch j.State {
		case rivertype.JobStateCompleted:
			s.Succeeded++
		case rivertype.JobStateDiscarded, rivertype.JobStateCancelled:
			s.Failed++
		case rivertype.JobStateRunning:
			s.Running++
		}
	}
	return s
}

// recentWindow is the window over which definition stats are aggregated.
const recentWindow = 24 * time.Hour

// statsContext is the parameter bag passed to RecentStatsFor so the function
// is easy to unit-test without a live River client.
type statsContext interface {
	// JobList delegates to River. Returns the matching JobRows.
	JobList(ctx context.Context, params *river.JobListParams) (*river.JobListResult, error)
}

// RecentStatsFor reads recent job rows for a kind via the provided River
// client and returns the bucketed counts. Returns zeroes if River is nil.
func (r *Registry) RecentStatsFor(ctx context.Context, riverClient statsContext, name string) (RecentStats, error) {
	if riverClient == nil {
		return RecentStats{}, nil
	}
	params := river.NewJobListParams().
		First(500).
		Kinds(name).
		States(
			rivertype.JobStateCompleted,
			rivertype.JobStateDiscarded,
			rivertype.JobStateCancelled,
			rivertype.JobStateRunning,
		)
	res, err := riverClient.JobList(ctx, params)
	if err != nil {
		return RecentStats{}, err
	}
	// Filter to the recent window by creation time.
	cutoff := time.Now().Add(-recentWindow)
	filtered := make([]*rivertype.JobRow, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		if j.CreatedAt.After(cutoff) || j.State == rivertype.JobStateRunning {
			filtered = append(filtered, j)
		}
	}
	return statsFromJobs(filtered), nil
}
