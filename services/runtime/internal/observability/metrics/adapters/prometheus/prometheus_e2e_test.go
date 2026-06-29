// SPDX-License-Identifier: Apache-2.0

//go:build e2e_prom

package prometheus

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPrometheusE2EQueryAndRange(t *testing.T) {
	baseURL := os.Getenv("BACKAI_E2E_PROM_URL")
	if baseURL == "" {
		t.Skip("BACKAI_E2E_PROM_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := New(ctx, Config{BaseURL: baseURL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var samples []any
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Query(ctx, "up{}", time.Now())
		if err == nil && len(got) > 0 {
			for _, sample := range got {
				samples = append(samples, sample)
			}
			break
		}
		time.Sleep(time.Second)
	}
	if len(samples) == 0 {
		t.Fatal("expected at least one up{} sample from real Prometheus")
	}
	to := time.Now().UTC()
	from := to.Add(-2 * time.Minute)
	series, err := store.QueryRange(ctx, "up{}", from, to, 15*time.Second)
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(series) == 0 {
		t.Fatal("expected at least one up{} range series from real Prometheus")
	}
}
