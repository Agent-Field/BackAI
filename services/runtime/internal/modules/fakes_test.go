// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// recorder is a minimal http.ResponseWriter capturing status + body,
// avoiding a dependency on net/http/httptest.
type recorder struct {
	status int
	body   bytes.Buffer
	header http.Header
}

func newRecorder() *recorder { return &recorder{header: http.Header{}} }

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}
func (r *recorder) WriteHeader(status int) { r.status = status }

func (r *recorder) decode() map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(r.body.Bytes(), &out)
	return out
}

// newReq builds a request carrying the given tenant on its context (as the
// resolver would). An empty tenant leaves the context unbound.
func newReq(method, target, body, tenant string) *http.Request {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, target, rdr)
	ctx := req.Context()
	if tenant != "" {
		ctx = tenantctx.WithTenant(ctx, tenant, "")
	}
	return req.WithContext(ctx)
}

// recordedQuery captures one Query/Exec invocation for assertions.
type recordedQuery struct {
	sql  string
	args []any
}

// fakeQuerier records every call and returns canned rows / command tags.
type fakeQuerier struct {
	calls    []recordedQuery
	rows     *fakeRows
	queryErr error
	tag      pgconn.CommandTag
	execErr  error
}

func (f *fakeQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.calls = append(f.calls, recordedQuery{sql: sql, args: args})
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.rows == nil {
		return &fakeRows{}, nil
	}
	// Return a fresh cursor over the same data each call.
	cp := *f.rows
	cp.idx = 0
	return &cp, nil
}

func (f *fakeQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.calls = append(f.calls, recordedQuery{sql: sql, args: args})
	return f.tag, f.execErr
}

// fakeRows implements pgx.Rows over an in-memory column/value grid.
type fakeRows struct {
	cols []string
	data [][]any
	idx  int
}

func (r *fakeRows) Close()                        {}
func (r *fakeRows) Err() error                    { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }
func (r *fakeRows) RawValues() [][]byte           { return nil }
func (r *fakeRows) Conn() *pgx.Conn               { return nil }
func (r *fakeRows) Next() bool {
	if r.idx < len(r.data) {
		r.idx++
		return true
	}
	return false
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	fds := make([]pgconn.FieldDescription, len(r.cols))
	for i, c := range r.cols {
		fds[i].Name = c
	}
	return fds
}

func (r *fakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.data) {
		return nil, nil
	}
	return r.data[r.idx-1], nil
}

// Scan supports the count query (*int) and simple *string destinations.
func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return nil
	}
	row := r.data[r.idx-1]
	for i := range dest {
		if i >= len(row) {
			break
		}
		switch d := dest[i].(type) {
		case *int:
			if v, ok := row[i].(int); ok {
				*d = v
			}
		case *string:
			if v, ok := row[i].(string); ok {
				*d = v
			}
		}
	}
	return nil
}
