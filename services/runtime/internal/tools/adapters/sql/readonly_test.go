// SPDX-License-Identifier: Apache-2.0

package sql

import (
	"errors"
	"testing"
)

func TestValidateReadOnly_OK(t *testing.T) {
	cases := []string{
		"SELECT 1",
		"select id from foo",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"EXPLAIN SELECT 1",
	}
	for _, q := range cases {
		if _, err := validateReadOnly(q); err != nil {
			t.Errorf("validateReadOnly(%q) err = %v, want nil", q, err)
		}
	}
}

func TestValidateReadOnly_Reject(t *testing.T) {
	cases := []struct {
		sql  string
		want error
	}{
		{"INSERT INTO foo VALUES (1)", ErrNotReadOnly},
		{"DELETE FROM foo", ErrNotReadOnly},
		{"DROP TABLE foo", ErrNotReadOnly},
		{"SELECT 1; DROP TABLE foo", ErrMultiStatement},
		{"", ErrInvalidArgs},
		{"COPY foo TO STDOUT", ErrNotReadOnly},
		{"  SELECT 1 ; SELECT 2", ErrMultiStatement},
		{"UPDATE foo SET bar = 1", ErrNotReadOnly},
		{"SET ROLE admin", ErrNotReadOnly},
	}
	for _, tc := range cases {
		_, err := validateReadOnly(tc.sql)
		if !errors.Is(err, tc.want) {
			t.Errorf("validateReadOnly(%q) err = %v, want %v", tc.sql, err, tc.want)
		}
	}
}

func TestAdapter_Unconfigured(t *testing.T) {
	a := New(nil)
	if a.Configured() {
		t.Errorf("Configured() = true, want false")
	}
	_, err := a.Query(nil, "", "SELECT 1", nil, 10) //nolint:staticcheck // nil ctx ok in test
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Query unconfigured: err = %v, want ErrNotConfigured", err)
	}
}
