// SPDX-License-Identifier: Apache-2.0

//go:build e2e_loki

package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

func TestE2E_LokiQueryAndTail(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("BACKAI_E2E_LOKI_URL"), "/")
	if baseURL == "" {
		t.Skip("set BACKAI_E2E_LOKI_URL=http://localhost:3100")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	waitReady(t, ctx, baseURL)
	store, err := New(ctx, Config{BaseURL: baseURL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	queryMsg := "backai-query-" + time.Now().Format("150405.000000000")
	if err := pushLine(ctx, baseURL, queryMsg); err != nil {
		t.Fatalf("push query line: %v", err)
	}
	var page logs.Page
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		page, err = store.Query(ctx, logs.Filter{Services: []string{"runtime"}, Search: queryMsg, Limit: 10, From: time.Now().Add(-5 * time.Minute)})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(page.Entries) > 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(page.Entries) == 0 {
		t.Fatalf("query did not return pushed line %q", queryMsg)
	}

	tailCtx, tailCancel := context.WithCancel(ctx)
	defer tailCancel()
	ch, err := store.Tail(tailCtx, logs.Filter{Services: []string{"runtime"}, Search: "backai-tail-", From: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	tailMsg := "backai-tail-" + time.Now().Format("150405.000000000")
	if err := pushLine(ctx, baseURL, tailMsg); err != nil {
		t.Fatalf("push tail line: %v", err)
	}
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				t.Fatal("tail channel closed before receiving line")
			}
			if strings.Contains(entry.Msg, tailMsg) {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("tail did not receive pushed line %q", tailMsg)
		}
	}
}

func waitReady(t *testing.T, ctx context.Context, baseURL string) {
	t.Helper()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/ready", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("loki not ready: %v", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func pushLine(ctx context.Context, baseURL, msg string) error {
	now := strconvFormatUnixNano(time.Now())
	body := map[string]any{
		"streams": []any{
			map[string]any{
				"stream": map[string]string{"service": "runtime"},
				"values": [][]string{{now, fmt.Sprintf("level=info service=runtime msg=%q", msg)}},
			},
		},
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/loki/api/v1/push", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("push HTTP %d", resp.StatusCode)
	}
	return nil
}

func strconvFormatUnixNano(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixNano())
}
