// SPDX-License-Identifier: Apache-2.0

package none

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
)

func TestNoneStore(t *testing.T) {
	store := New()
	if _, err := store.Query(context.Background(), "up{}", time.Now()); !errors.Is(err, metrics.ErrNoBackend) {
		t.Fatalf("Query err=%v want ErrNoBackend", err)
	}
	if _, err := store.QueryRange(context.Background(), "up{}", time.Now().Add(-time.Minute), time.Now(), time.Second); !errors.Is(err, metrics.ErrNoBackend) {
		t.Fatalf("QueryRange err=%v want ErrNoBackend", err)
	}
	if caps := store.Capabilities(); caps.SupportsInstantQuery || caps.SupportsRangeQuery || caps.NativeQueryLang != "" {
		t.Fatalf("caps=%+v", caps)
	}
}
