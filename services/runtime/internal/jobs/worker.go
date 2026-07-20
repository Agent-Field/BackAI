// SPDX-License-Identifier: Apache-2.0

// worker.go bridges River's typed-worker model to our dynamic, name-based
// dispatch model.
//
// River's natural API binds one Go type per kind, with a `Kind() string`
// method and a `Work` function for that type. We want a *registry* where
// definitions and handlers can be added without inventing new Go types per
// kind, so we use this trick:
//
//   - All jobs share one Args struct, `dispatchArgs`.
//   - At Insert time, dispatchArgs.kindName is set to the real kind name
//     and dispatchArgs.Kind() returns it — so River persists the correct
//     kind on the row.
//   - At Worker-register time, River reads the *zero-value's* Kind() (which
//     returns the sentinel `umbrellaKind`) plus KindAliases() (the list of
//     all real kind names from the registry). River routes any row with
//     any of those kinds to the single umbrella worker.
//   - The umbrella worker reads the row's actual Kind from the embedded
//     JobRow and dispatches inside the registry.
//
// This keeps registration & dispatch fully dynamic without invoking
// reflection or runtime type generation.
//
// All running jobs open an OTel span named "jobs.run.<kind>".
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// umbrellaKind is the sentinel Kind() value the *zero-value* dispatchArgs
// returns. It only exists so River has a primary kind to key the worker
// under — no real job ever has this kind on its row.
const umbrellaKind = "_af-stack.dispatch"

// jobsTracerName is the OTel tracer used by job handlers.
const jobsTracerName = "af-stack/jobs"

// dispatchArgs is the single Args struct used by every job kind we
// register. The kindName field is set at Insert time so the persisted row
// gets the right Kind; for the zero-value used during worker registration
// it returns the umbrella sentinel.
type dispatchArgs struct {
	// Payload is the user-supplied JSON args verbatim. Serialized to the
	// River row's args column so the dashboard can display it.
	Payload json.RawMessage `json:"payload"`
	// TenantID is the optional tenant the job belongs to. Empty string
	// means "no tenant scope".
	TenantID string `json:"tenant_id,omitempty"`

	// kindName is intentionally NOT JSON-serialized. It's set by the
	// Client at Insert time to drive Kind(); at worker-dispatch time it's
	// empty (River decodes from JSON) and the umbrella worker reads the
	// real kind from the embedded JobRow.Kind field instead.
	kindName string

	// aliases is also not serialized. It's set ONLY at worker-registration
	// time so KindAliases() can return the dynamically-discovered list.
	aliases []string
}

// MarshalJSON forces only Payload and TenantID into the encoded args. We
// implement this explicitly because the bare struct includes the unexported
// kindName field which `encoding/json` would skip, but being explicit keeps
// the format stable.
func (d dispatchArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Payload  json.RawMessage `json:"payload"`
		TenantID string          `json:"tenant_id,omitempty"`
	}{
		Payload:  d.Payload,
		TenantID: d.TenantID,
	})
}

// UnmarshalJSON populates Payload and TenantID from the persisted row.
func (d *dispatchArgs) UnmarshalJSON(data []byte) error {
	var aux struct {
		Payload  json.RawMessage `json:"payload"`
		TenantID string          `json:"tenant_id,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	d.Payload = aux.Payload
	d.TenantID = aux.TenantID
	return nil
}

// Kind implements river.JobArgs. For the zero value (used during
// AddWorker) it returns the umbrella sentinel; for instances created at
// Insert time it returns the caller-supplied real kind.
func (d dispatchArgs) Kind() string {
	if d.kindName == "" {
		return umbrellaKind
	}
	return d.kindName
}

// KindAliases implements river.JobArgsWithKindAliases. River calls this on
// the zero value at AddWorker time to find all extra kinds the worker
// should be registered under. We can't access the zero value's
// aliases field (it's unset), so KindAliases reads from a package-level
// variable populated by the Manager before AddWorker.
func (d dispatchArgs) KindAliases() []string {
	if len(d.aliases) > 0 {
		return d.aliases
	}
	return registeredAliases()
}

// dispatcherWorker is the single in-process worker that handles every Go
// job kind. It dispatches inside Work based on the row's real Kind field.
// For remote (python/ts) kinds it hands off to manager.runRemoteJob.
type dispatcherWorker struct {
	river.WorkerDefaults[dispatchArgs]
	registry *Registry
	manager  *Manager
}

// Work is the entry point River calls when any registered-kind row becomes
// available.
func (w *dispatcherWorker) Work(ctx context.Context, job *river.Job[dispatchArgs]) error {
	// Read the real kind from the persisted row (embedded JobRow). Args
	// don't carry kindName because UnmarshalJSON doesn't restore it.
	kind := job.Kind
	if kind == "" {
		return errors.New("jobs: empty kind on job row")
	}

	tracer := otel.Tracer(jobsTracerName)
	ctx, span := tracer.Start(ctx, "jobs.run."+kind, trace.WithAttributes(
		attribute.Int64("job.id", job.ID),
		attribute.String("job.kind", kind),
		attribute.Int("job.attempt", job.Attempt),
		attribute.String("job.tenant_id", job.Args.TenantID),
	))
	defer span.End()

	def := w.registry.Get(kind)
	if def == nil {
		err := fmt.Errorf("jobs: no definition for kind %q", kind)
		span.RecordError(err)
		return err
	}

	// Remote (python/typescript) jobs have no in-process Go handler. Hand
	// them to the pull-worker executor: it registers a DB-backed rendezvous
	// attempt and blocks until an out-of-process worker leases + resolves it
	// (or the lease TTL expires / the job is cancelled). River keeps the job
	// `running` throughout, so its durability + retry semantics are retained.
	if !def.HasLiveHandler() {
		if w.manager == nil {
			err := errors.New("jobs: remote job but manager not wired for pull workers")
			span.RecordError(err)
			return river.JobCancel(err)
		}
		span.SetAttributes(attribute.String("job.exec", "remote-pull"))
		var deadline *time.Time
		return w.manager.runRemoteJob(ctx, job.ID, job.Attempt, kind, job.Args.TenantID, job.Args.Payload, deadline)
	}

	if err := def.goHandler(ctx, job.Args.Payload); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}
