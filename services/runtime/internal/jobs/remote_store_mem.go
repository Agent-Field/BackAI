// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// memRemoteStore is an in-memory RemoteStore used by unit tests. It is a
// faithful implementation of the lease state machine — the contract tests
// exercise real transitions, not a mock. A production runtime uses
// pgRemoteStore instead.
//
// The clock is injectable so TTL-expiry and unleased-grace paths are
// testable without wall-clock sleeps.
type memRemoteStore struct {
	mu    sync.Mutex
	byKey map[attemptKey]*RemoteAttempt
	logs  map[attemptKey][]LogLine
	now   func() time.Time
}

type attemptKey struct {
	jobID   int64
	attempt int
}

func newMemRemoteStore(now func() time.Time) *memRemoteStore {
	if now == nil {
		now = time.Now
	}
	return &memRemoteStore{
		byKey: make(map[attemptKey]*RemoteAttempt),
		logs:  make(map[attemptKey][]LogLine),
		now:   now,
	}
}

func (m *memRemoteStore) EnqueueAttempt(_ context.Context, att RemoteAttempt) (*RemoteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	key := attemptKey{att.JobID, att.Attempt}
	if existing, ok := m.byKey[key]; ok {
		// Idempotent: re-registering the same attempt returns the current
		// row without clobbering an in-flight lease or result.
		return cloneAttempt(existing), nil
	}

	// Supersede older non-terminal attempts of the same job.
	for k, a := range m.byKey {
		if k.jobID == att.JobID && k.attempt < att.Attempt && !a.State.Terminal() {
			a.State = AttemptSuperseded
			a.UpdatedAt = now
		}
	}

	row := cloneAttempt(&att)
	if row.State == "" {
		row.State = AttemptReady
	}
	if len(row.Payload) == 0 {
		row.Payload = json.RawMessage("{}")
	}
	row.WorkerID = ""
	row.LeaseExpiresAt = nil
	row.Result = nil
	row.Error = ""
	row.Retryable = false
	row.Canceled = false
	row.CreatedAt = now
	row.UpdatedAt = now
	m.byKey[key] = row
	return cloneAttempt(row), nil
}

func (m *memRemoteStore) Lease(_ context.Context, req LeaseRequest) (*RemoteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	kinds := make(map[string]struct{}, len(req.Kinds))
	for _, k := range req.Kinds {
		kinds[k] = struct{}{}
	}

	// Oldest ready attempt for this tenant among the declared kinds.
	candidates := make([]*RemoteAttempt, 0, len(m.byKey))
	for _, a := range m.byKey {
		if a.TenantID != req.TenantID || a.State != AttemptReady {
			continue
		}
		if _, ok := kinds[a.Kind]; !ok {
			continue
		}
		candidates = append(candidates, a)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].JobID < candidates[j].JobID
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	claimed := candidates[0]
	claimed.State = AttemptLeased
	claimed.WorkerID = req.WorkerID
	exp := now.Add(req.TTL)
	claimed.LeaseExpiresAt = &exp
	claimed.UpdatedAt = now
	return cloneAttempt(claimed), nil
}

func (m *memRemoteStore) Heartbeat(_ context.Context, tenantID string, jobID int64, attempt int, workerID string, ttl time.Duration) (*RemoteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	a, err := m.leasedByWorker(tenantID, jobID, attempt, workerID)
	if err != nil {
		return nil, err
	}
	now := m.now()
	exp := now.Add(ttl)
	a.LeaseExpiresAt = &exp
	a.UpdatedAt = now
	return cloneAttempt(a), nil
}

func (m *memRemoteStore) Complete(_ context.Context, tenantID string, jobID int64, attempt int, workerID string, result json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	a, err := m.leasedByWorker(tenantID, jobID, attempt, workerID)
	if err != nil {
		return err
	}
	now := m.now()
	a.State = AttemptCompleted
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	a.Result = result
	a.LeaseExpiresAt = nil
	a.UpdatedAt = now
	return nil
}

func (m *memRemoteStore) Fail(_ context.Context, tenantID string, jobID int64, attempt int, workerID string, errMsg string, retryable bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	a, err := m.leasedByWorker(tenantID, jobID, attempt, workerID)
	if err != nil {
		return err
	}
	now := m.now()
	a.State = AttemptFailed
	a.Error = errMsg
	a.Retryable = retryable
	a.LeaseExpiresAt = nil
	a.UpdatedAt = now
	return nil
}

func (m *memRemoteStore) GetAttempt(_ context.Context, tenantID string, jobID int64, attempt int) (*RemoteAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	a, ok := m.byKey[attemptKey{jobID, attempt}]
	if !ok || a.TenantID != tenantID {
		return nil, ErrAttemptNotFound
	}
	return cloneAttempt(a), nil
}

func (m *memRemoteStore) MarkCanceled(_ context.Context, tenantID string, jobID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	found := false
	for k, a := range m.byKey {
		if k.jobID == jobID && a.TenantID == tenantID && !a.State.Terminal() {
			a.Canceled = true
			a.UpdatedAt = now
			found = true
		}
	}
	if !found {
		return ErrAttemptNotFound
	}
	return nil
}

func (m *memRemoteStore) AppendLogs(_ context.Context, tenantID string, jobID int64, attempt int, lines []LogLine) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	a, ok := m.byKey[attemptKey{jobID, attempt}]
	if !ok || a.TenantID != tenantID {
		return 0, ErrAttemptNotFound
	}
	key := attemptKey{jobID, attempt}
	m.logs[key] = append(m.logs[key], lines...)
	return len(lines), nil
}

// leasedByWorker returns the attempt iff it exists for the tenant, is in
// `leased`, and is held by workerID. Callers hold m.mu.
func (m *memRemoteStore) leasedByWorker(tenantID string, jobID int64, attempt int, workerID string) (*RemoteAttempt, error) {
	a, ok := m.byKey[attemptKey{jobID, attempt}]
	if !ok || a.TenantID != tenantID {
		return nil, ErrAttemptNotFound
	}
	if a.State.Terminal() {
		return nil, ErrAttemptResolved
	}
	if a.State != AttemptLeased || a.WorkerID != workerID {
		return nil, ErrAttemptNotLeased
	}
	return a, nil
}
