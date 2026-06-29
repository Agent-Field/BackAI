// SPDX-License-Identifier: Apache-2.0

//go:build e2e_tempo

package tempo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

func TestE2E_TempoTraceQLSearchAndGet(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("BACKAI_E2E_TEMPO_URL"), "/")
	if baseURL == "" {
		t.Skip("BACKAI_E2E_TEMPO_URL is not set")
	}
	otlpURL := strings.TrimRight(os.Getenv("BACKAI_E2E_TEMPO_OTLP_URL"), "/")
	if otlpURL == "" {
		otlpURL = deriveOTLPURL(baseURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	store, err := New(ctx, Config{BaseURL: baseURL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !store.Capabilities().SupportsTraceQL {
		t.Fatalf("Tempo version must be >= 2.0 and support TraceQL, caps=%+v", store.Capabilities())
	}
	traceID := hexID(16)
	spanID := hexID(8)
	service := "backai-tempo-e2e"
	operation := "block4-tempo-e2e-" + traceID[:8]
	start := time.Now().Add(-10 * time.Second).UTC()
	end := start.Add(25 * time.Millisecond)
	if err := pushOTLPSpan(ctx, otlpURL, traceID, spanID, service, operation, start, end); err != nil {
		t.Fatalf("push OTLP span: %v", err)
	}

	var result traces.SearchResult
	for i := 0; i < 30; i++ {
		result, err = store.Search(ctx, traces.SearchFilter{
			Service:   service,
			Operation: operation,
			From:      start.Add(-time.Minute),
			To:        time.Now().Add(time.Minute),
			Limit:     10,
		})
		if err == nil {
			for _, item := range result.Traces {
				if item.TraceID == traceID {
					goto foundSearch
				}
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("trace %s not found via TraceQL search, last result=%+v err=%v", traceID, result, err)

foundSearch:
	trace, err := store.Get(ctx, traceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if trace.TraceID != traceID {
		t.Fatalf("trace id=%q want %q", trace.TraceID, traceID)
	}
	if len(trace.Spans) == 0 {
		t.Fatalf("expected at least one span")
	}
	if trace.Spans[0].Operation == "" {
		t.Fatalf("decoded span missing operation: %+v", trace.Spans[0])
	}
}

func deriveOTLPURL(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	host := u.Hostname()
	port := u.Port()
	if port == "3200" || port == "" {
		u.Host = host + ":4318"
	}
	return strings.TrimRight(u.String(), "/")
}

func pushOTLPSpan(ctx context.Context, otlpURL, traceID, spanID, service, operation string, start, end time.Time) error {
	payload := map[string]any{
		"resourceSpans": []map[string]any{{
			"resource": map[string]any{
				"attributes": []map[string]any{{
					"key": "service.name",
					"value": map[string]any{
						"stringValue": service,
					},
				}},
			},
			"scopeSpans": []map[string]any{{
				"scope": map[string]any{"name": "backai-tempo-e2e"},
				"spans": []map[string]any{{
					"traceId":           traceID,
					"spanId":            spanID,
					"name":              operation,
					"kind":              "SPAN_KIND_SERVER",
					"startTimeUnixNano": fmt.Sprintf("%d", start.UnixNano()),
					"endTimeUnixNano":   fmt.Sprintf("%d", end.UnixNano()),
					"attributes": []map[string]any{{
						"key": "backai.block",
						"value": map[string]any{
							"stringValue": "4",
						},
					}},
					"status": map[string]any{"code": "STATUS_CODE_OK"},
				}},
			}},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, otlpURL+"/v1/traces", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("OTLP HTTP %d", resp.StatusCode)
	}
	return nil
}

func hexID(bytesLen int) string {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}
