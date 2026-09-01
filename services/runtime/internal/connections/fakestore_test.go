// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// fakeStore is an in-memory Store used to exercise Service logic (refresh
// single-flight, auth injection, revoked handling, audit emission) without a
// database. It holds decrypted credentials directly — the crypto envelope is
// tested separately in credential_test.go.
type fakeStore struct {
	mu        sync.Mutex
	byID      map[string]*fakeRow
	events    []fakeEvent
	saveCalls int
	seq       int
}

type fakeRow struct {
	conn          Connection
	cred          credential
	webhookSecret string
}

type fakeEvent struct {
	connID    string
	eventType string
	metadata  map[string]any
}

func newFakeStore() *fakeStore {
	return &fakeStore{byID: map[string]*fakeRow{}}
}

// seed inserts a fully-formed row directly (bypassing Create) so tests can
// set an arbitrary status / expiry / credential.
func (f *fakeStore) seed(_ string, conn Connection, cred credential, webhookSecret string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if conn.ID == "" {
		f.seq++
		conn.ID = fmt.Sprintf("conn-%d", f.seq)
	}
	f.byID[conn.ID] = &fakeRow{conn: conn, cred: cred, webhookSecret: webhookSecret}
}

func (f *fakeStore) Create(_ context.Context, _ string, p CreateParams) (Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := fmt.Sprintf("conn-%d", f.seq)
	now := time.Now()
	granted := p.GrantedScopes
	if granted == nil {
		granted = []string{}
	}
	req := p.RequestedScopes
	if req == nil {
		req = []string{}
	}
	conn := Connection{
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
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	f.byID[id] = &fakeRow{conn: conn, cred: p.Cred, webhookSecret: p.WebhookSecret}
	return conn, nil
}

func (f *fakeStore) List(_ context.Context, _ string) ([]Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []Connection{}
	for _, r := range f.byID {
		out = append(out, r.conn)
	}
	return out, nil
}

func (f *fakeStore) Load(_ context.Context, _, id string) (Loaded, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return Loaded{}, ErrNotFound
	}
	return Loaded{Conn: r.conn, cred: r.cred, webhookSecret: r.webhookSecret}, nil
}

func (f *fakeStore) SaveTokens(_ context.Context, _, id string, cred credential, expiry *time.Time, grantedScopes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return ErrNotFound
	}
	f.saveCalls++
	r.cred = cred
	r.conn.TokenExpiry = expiry
	r.conn.Status = StatusActive
	if grantedScopes != nil {
		r.conn.GrantedScopes = grantedScopes
	}
	return nil
}

func (f *fakeStore) UpdateStatus(_ context.Context, _, id, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return ErrNotFound
	}
	r.conn.Status = status
	return nil
}

func (f *fakeStore) InsertEvent(_ context.Context, _, connID, eventType string, metadata map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fakeEvent{connID: connID, eventType: eventType, metadata: metadata})
	return nil
}

func (f *fakeStore) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.eventType)
	}
	return out
}

func (f *fakeStore) countSaves() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveCalls
}
