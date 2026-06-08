// SPDX-License-Identifier: Apache-2.0

package searxng

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search"
)

func TestAdapter_Unconfigured(t *testing.T) {
	a := New("")
	if a.Configured() {
		t.Errorf("Configured() = true, want false")
	}
	if _, err := a.Search(context.Background(), "x", 5); !errors.Is(err, search.ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestAdapter_ID(t *testing.T) {
	a := New("http://example.com")
	if a.ID() != "searxng" {
		t.Errorf("ID = %q, want searxng", a.ID())
	}
}
