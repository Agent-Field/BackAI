// SPDX-License-Identifier: Apache-2.0

package idempotency

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const testTenant = "00000000-0000-0000-0000-000000000000"

func tctx() context.Context {
	return tenantctx.WithTenant(context.Background(), testTenant, "")
}

func TestFingerprintDeterministicAndDiscriminating(t *testing.T) {
	base := Fingerprint("POST", "/api/v1/x", "a=1", []byte(`{"k":1}`))
	if base == "" {
		t.Fatal("fingerprint is empty")
	}
	if again := Fingerprint("POST", "/api/v1/x", "a=1", []byte(`{"k":1}`)); again != base {
		t.Errorf("fingerprint not deterministic: %s != %s", again, base)
	}
	// Any dimension change must change the fingerprint.
	for name, fp := range map[string]string{
		"method": Fingerprint("PUT", "/api/v1/x", "a=1", []byte(`{"k":1}`)),
		"path":   Fingerprint("POST", "/api/v1/y", "a=1", []byte(`{"k":1}`)),
		"query":  Fingerprint("POST", "/api/v1/x", "a=2", []byte(`{"k":1}`)),
		"body":   Fingerprint("POST", "/api/v1/x", "a=1", []byte(`{"k":2}`)),
	} {
		if fp == base {
			t.Errorf("fingerprint did not change when %s changed", name)
		}
	}
}

func TestReserveRequiresTenant(t *testing.T) {
	st := NewWithPool(&fakePool{})
	if _, err := st.Reserve(context.Background(), "k", "fp"); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("Reserve without tenant err = %v, want ErrTenantRequired", err)
	}
}

func TestReserveAcquiredWhenInsertWins(t *testing.T) {
	// Insert returns a row id ⇒ we claimed the key.
	fp := &fakePool{insertRow: &scanRow{vals: []any{"11111111-1111-1111-1111-111111111111"}}}
	st := NewWithPool(fp)
	res, err := st.Reserve(tctx(), "k", "fp")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Outcome != OutcomeAcquired {
		t.Errorf("Outcome = %d, want OutcomeAcquired", res.Outcome)
	}
}

func TestReserveMismatchWhenFingerprintDiffers(t *testing.T) {
	// Insert conflicts (ErrNoRows); the existing row has a different fp.
	var status *int
	fp := &fakePool{
		insertRow: &scanRow{err: pgx.ErrNoRows},
		selectRow: &scanRow{vals: []any{"other-fp", status, []byte(nil), []byte(nil)}},
	}
	st := NewWithPool(fp)
	res, err := st.Reserve(tctx(), "k", "my-fp")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Outcome != OutcomeMismatch {
		t.Errorf("Outcome = %d, want OutcomeMismatch", res.Outcome)
	}
}

func TestReserveInFlightWhenPending(t *testing.T) {
	var status *int // NULL status_code ⇒ still reserved
	fp := &fakePool{
		insertRow: &scanRow{err: pgx.ErrNoRows},
		selectRow: &scanRow{vals: []any{"same-fp", status, []byte(nil), []byte(nil)}},
	}
	st := NewWithPool(fp)
	res, err := st.Reserve(tctx(), "k", "same-fp")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Outcome != OutcomeInFlight {
		t.Errorf("Outcome = %d, want OutcomeInFlight", res.Outcome)
	}
}

func TestReserveReplayWhenCompleted(t *testing.T) {
	code := 201
	fp := &fakePool{
		insertRow: &scanRow{err: pgx.ErrNoRows},
		selectRow: &scanRow{vals: []any{
			"same-fp", &code,
			[]byte(`{"Content-Type":"application/json"}`),
			[]byte(`{"ok":true}`),
		}},
	}
	st := NewWithPool(fp)
	res, err := st.Reserve(tctx(), "k", "same-fp")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Outcome != OutcomeReplay {
		t.Fatalf("Outcome = %d, want OutcomeReplay", res.Outcome)
	}
	if res.Response == nil || res.Response.Status != 201 {
		t.Fatalf("replay response = %+v, want status 201", res.Response)
	}
	if got := res.Response.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("replay Content-Type = %q, want application/json", got)
	}
	if string(res.Response.Body) != `{"ok":true}` {
		t.Errorf("replay body = %q", res.Response.Body)
	}
}

func TestReserveInFlightWhenRowVanished(t *testing.T) {
	// Insert conflicts but the follow-up select finds nothing (a concurrent
	// owner completed + purge removed it): treat as in-flight, not a crash.
	fp := &fakePool{
		insertRow: &scanRow{err: pgx.ErrNoRows},
		selectRow: &scanRow{err: pgx.ErrNoRows},
	}
	st := NewWithPool(fp)
	res, err := st.Reserve(tctx(), "k", "fp")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Outcome != OutcomeInFlight {
		t.Errorf("Outcome = %d, want OutcomeInFlight", res.Outcome)
	}
}

// --- fake pgx pool -------------------------------------------------------

type fakePool struct {
	insertRow *scanRow
	selectRow *scanRow
	queryRows int
	execCalls int
}

func (f *fakePool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	f.execCalls++
	return pgconn.CommandTag{}, nil
}

func (f *fakePool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	f.queryRows++
	// First QueryRow in Reserve is the INSERT ... RETURNING; the second is
	// the SELECT of the existing row.
	if f.queryRows == 1 {
		return f.insertRow
	}
	return f.selectRow
}

// scanRow is a minimal pgx.Row: Scan copies programmed values into dest, or
// returns the programmed error.
type scanRow struct {
	vals []any
	err  error
}

func (r *scanRow) Scan(dest ...any) error {
	if r == nil {
		return pgx.ErrNoRows
	}
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		if i >= len(r.vals) {
			break
		}
		assign(dest[i], r.vals[i])
	}
	return nil
}

// assign copies src into the pointer dst for the handful of types Reserve
// scans (string, *int, []byte).
func assign(dst, src any) {
	switch d := dst.(type) {
	case *string:
		if s, ok := src.(string); ok {
			*d = s
		}
	case **int:
		if p, ok := src.(*int); ok {
			*d = p
		}
	case *[]byte:
		if b, ok := src.([]byte); ok {
			*d = b
		}
	}
}
