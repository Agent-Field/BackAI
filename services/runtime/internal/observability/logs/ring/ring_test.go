// SPDX-License-Identifier: Apache-2.0

package ring

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

func TestRingStoreQueryFilters(t *testing.T) {
	r := logger.NewRing(16, "runtime")
	r.Append(logger.Line{Level: logger.LevelInfo, Service: "runtime", Msg: "ready", TenantID: "a"})
	r.Append(logger.Line{Level: logger.LevelError, Service: "worker", Msg: "timeout", TenantID: "b"})
	store := New(r)
	page, err := store.Query(context.Background(), logs.Filter{
		Services: []string{"worker"},
		Levels:   []string{"error"},
		Search:   "time",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("entries=%d want 1", len(page.Entries))
	}
	if page.Entries[0].Msg != "timeout" {
		t.Fatalf("msg=%q", page.Entries[0].Msg)
	}
}

func TestRingStoreQueryPaginates(t *testing.T) {
	r := logger.NewRing(16, "runtime")
	for i := 0; i < 3; i++ {
		r.Append(logger.Line{Level: logger.LevelInfo, Msg: "line"})
	}
	store := New(r)
	page, err := store.Query(context.Background(), logs.Filter{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(page.Entries) != 2 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func TestRingStoreTail(t *testing.T) {
	r := logger.NewRing(16, "runtime")
	store := New(r)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := store.Tail(ctx, logs.Filter{Levels: []string{"warn"}})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	r.Append(logger.Line{Level: logger.LevelInfo, Msg: "skip"})
	r.Append(logger.Line{Level: logger.LevelWarn, Msg: "keep"})
	select {
	case entry := <-ch:
		if entry.Msg != "keep" {
			t.Fatalf("msg=%q", entry.Msg)
		}
	case <-time.After(time.Second):
		t.Fatal("tail did not emit matching line")
	}
}
