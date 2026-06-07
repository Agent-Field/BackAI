// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"testing"
)

func TestOpenRejectsEmptyURL(t *testing.T) {
	_, err := Open(context.Background(), Config{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestHealthOnNilDB(t *testing.T) {
	var d *DB
	if err := d.Health(context.Background()); err == nil {
		t.Error("expected error for nil DB")
	}
}

func TestStatsOnNilDB(t *testing.T) {
	var d *DB
	s := d.Stats()
	if s.TotalConns != 0 {
		t.Errorf("expected zero stats, got %+v", s)
	}
}