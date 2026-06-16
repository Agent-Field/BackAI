// SPDX-License-Identifier: Apache-2.0

package empty

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

func TestStore_SearchEmpty(t *testing.T) {
	store := New()
	got, err := store.Search(context.Background(), traces.SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Traces) != 0 || got.HasMore {
		t.Fatalf("unexpected result: %+v", got)
	}
	if store.Capabilities() != (traces.Capabilities{}) {
		t.Fatalf("empty store should declare no capabilities")
	}
}

func TestStore_GetNoBackend(t *testing.T) {
	store := New()
	_, err := store.Get(context.Background(), "abc")
	if !errors.Is(err, traces.ErrNoBackend) {
		t.Fatalf("expected ErrNoBackend, got %v", err)
	}
	_, err = store.Get(context.Background(), "")
	if !errors.Is(err, traces.ErrInvalidTraceID) {
		t.Fatalf("expected ErrInvalidTraceID, got %v", err)
	}
}
