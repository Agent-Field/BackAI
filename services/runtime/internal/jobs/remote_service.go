// SPDX-License-Identifier: Apache-2.0

// remote_service.go exposes the pull-worker protocol operations the REST
// handlers call. They are thin, tenant-scoped delegations to the underlying
// RemoteStore — the store stays unexported so the only entry points are
// these guarded methods.
//
// Every call is scoped to a tenant id the caller resolved from an
// authenticated worker key (scope jobs:work). The store additionally
// enforces isolation at the DB (FORCE RLS), so even a handler bug can't
// cross the tenant boundary.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrRemoteDisabled is returned when the manager has no rendezvous store
// (e.g. no DB at boot). Handlers surface it as 503.
var ErrRemoteDisabled = errors.New("jobs: remote worker protocol not available")

// RemoteEnabled reports whether pull-worker endpoints can serve requests.
func (m *Manager) RemoteEnabled() bool {
	return m != nil && m.remote != nil
}

// LeaseRemote claims the oldest ready attempt for the tenant among kinds.
// Returns (nil, nil) when nothing is available.
func (m *Manager) LeaseRemote(ctx context.Context, tenantID string, kinds []string, workerID string, ttl time.Duration) (*RemoteAttempt, error) {
	if !m.RemoteEnabled() {
		return nil, ErrRemoteDisabled
	}
	return m.remote.Lease(ctx, LeaseRequest{
		TenantID: tenantID,
		Kinds:    kinds,
		WorkerID: workerID,
		TTL:      ttl,
	})
}

// HeartbeatRemote extends a lease and returns the current attempt (so the
// caller can observe Canceled).
func (m *Manager) HeartbeatRemote(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, ttl time.Duration) (*RemoteAttempt, error) {
	if !m.RemoteEnabled() {
		return nil, ErrRemoteDisabled
	}
	return m.remote.Heartbeat(ctx, tenantID, jobID, attempt, workerID, ttl)
}

// CompleteRemote resolves an attempt successfully.
func (m *Manager) CompleteRemote(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, result json.RawMessage) error {
	if !m.RemoteEnabled() {
		return ErrRemoteDisabled
	}
	return m.remote.Complete(ctx, tenantID, jobID, attempt, workerID, result)
}

// FailRemote resolves an attempt as failed with a retryable flag.
func (m *Manager) FailRemote(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, errMsg string, retryable bool) error {
	if !m.RemoteEnabled() {
		return ErrRemoteDisabled
	}
	return m.remote.Fail(ctx, tenantID, jobID, attempt, workerID, errMsg, retryable)
}

// AppendRemoteLogs attaches structured log lines to a run.
func (m *Manager) AppendRemoteLogs(ctx context.Context, tenantID string, jobID int64, attempt int, lines []LogLine) (int, error) {
	if !m.RemoteEnabled() {
		return 0, ErrRemoteDisabled
	}
	return m.remote.AppendLogs(ctx, tenantID, jobID, attempt, lines)
}
