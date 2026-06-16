// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// mockSidecar is a sandbox-v1 sidecar implementation good enough to
// exercise every shim code-path. Tests configure it via fields.
type mockSidecar struct {
	t              *testing.T
	caps           sandboxCapsResp
	runResponse    runResultWire
	streamEvents   []string // raw SSE payloads (each ends with \n\n added by handler)
	terminationEv  string
	stopShouldFail bool
	receivedSpec   runSpecWire
	receivedStop   string
	requireToken   string
}

type sandboxCapsResp struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	Slot            string                 `json:"slot"`
	ProtocolVersion string                 `json:"protocol_version"`
	Vendor          string                 `json:"vendor"`
	Capabilities    map[string]interface{} `json:"capabilities"`
}

func (m *mockSidecar) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if m.requireToken != "" && r.Header.Get("Authorization") != "Bearer "+m.requireToken {
			http.Error(w, `{"code":"unauthorized","status":401}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(m.caps)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&m.receivedSpec)
		_ = json.NewEncoder(w).Encode(m.runResponse)
	})
	mux.HandleFunc("/v1/runs/stream", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&m.receivedSpec)
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, ev := range m.streamEvents {
			_, _ = io.WriteString(w, "data: "+ev+"\n\n")
			fl.Flush()
			time.Sleep(5 * time.Millisecond)
		}
		if m.terminationEv != "" {
			_, _ = io.WriteString(w, "data: "+m.terminationEv+"\n\n")
			fl.Flush()
		}
	})
	mux.HandleFunc("/v1/runs/", func(w http.ResponseWriter, r *http.Request) {
		// DELETE /v1/runs/{id}
		if r.Method != http.MethodDelete {
			http.Error(w, `{"code":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		m.receivedStop = strings.TrimPrefix(r.URL.Path, "/v1/runs/")
		if m.stopShouldFail {
			http.Error(w, `{"code":"adapter_unavailable","status":503}`, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/pool", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(poolWire{
			Adapter:        "mock",
			Warm:           2,
			Active:         1,
			Queued:         0,
			TotalRunsToday: 5,
		})
	})
	return mux
}

func newMock(t *testing.T) (*mockSidecar, *httptest.Server) {
	m := &mockSidecar{
		t: t,
		caps: sandboxCapsResp{
			Name:            "mock",
			Version:         "1.0.0",
			Slot:            "sandbox",
			ProtocolVersion: "v1",
			Vendor:          "BackAI",
			Capabilities: map[string]interface{}{
				"max_timeout_s":     3600,
				"supports_gpu":      false,
				"supports_network":  true,
				"supports_mounts":   true,
				"cold_start_ms":     100,
				"supports_streaming": true,
			},
		},
		runResponse: runResultWire{
			Status:    "done",
			ExitCode:  0,
			DurationS: 1,
			StartedAt: "2026-06-15T10:00:00Z",
			EndedAt:   "2026-06-15T10:00:01Z",
		},
	}
	srv := httptest.NewServer(m.handler())
	t.Cleanup(srv.Close)
	return m, srv
}

func TestAdapter_NewFetchesCapabilities(t *testing.T) {
	_, srv := newMock(t)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	caps := a.Capabilities()
	if caps.Adapter != "mock" {
		t.Errorf("adapter name: %q", caps.Adapter)
	}
	if caps.MaxTimeoutS != 3600 {
		t.Errorf("max_timeout_s: %d", caps.MaxTimeoutS)
	}
	if !caps.SupportsNetwork {
		t.Error("supports_network should be true")
	}
}

func TestAdapter_NewRejectsWrongSlot(t *testing.T) {
	m, srv := newMock(t)
	m.caps.Slot = "billing"
	_, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "slot") {
		t.Errorf("expected slot mismatch error, got: %v", err)
	}
}

func TestAdapter_Run(t *testing.T) {
	m, srv := newMock(t)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	spec := sandbox.RunSpec{
		ID:       "run-1",
		Image:    "python:3.12-slim",
		Command:  []string{"python", "-c", "print(1)"},
		TimeoutS: 30,
		CPU:      2,
		MemoryGB: 4,
		Network:  sandbox.NetworkRestricted,
	}
	res, err := a.Run(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != sandbox.StatusDone {
		t.Errorf("status: %v", res.Status)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code: %d", res.ExitCode)
	}
	if m.receivedSpec.ID != "run-1" || m.receivedSpec.Image != "python:3.12-slim" {
		t.Errorf("server saw wrong spec: %+v", m.receivedSpec)
	}
	if m.receivedSpec.Network != "restricted" {
		t.Errorf("network mode wire form: %q", m.receivedSpec.Network)
	}
}

func TestAdapter_Stream(t *testing.T) {
	m, srv := newMock(t)
	m.streamEvents = []string{
		`{"ts":"2026-06-15T10:00:00.1Z","stream":"stdout","text":"hello\n"}`,
		`{"ts":"2026-06-15T10:00:00.2Z","stream":"stdout","text":"world\n"}`,
	}
	m.terminationEv = `{"event":"terminated","status":"done","exit_code":0,"duration_s":1,"started_at":"2026-06-15T10:00:00Z","ended_at":"2026-06-15T10:00:01Z"}`

	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	spec := sandbox.RunSpec{ID: "run-stream-1", Image: "x", Command: []string{"x"}}
	lines, results, err := a.Stream(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for line := range lines {
		got = append(got, line.Text)
	}
	if len(got) != 2 || got[0] != "hello\n" || got[1] != "world\n" {
		t.Errorf("log lines: %v", got)
	}
	res := <-results
	if res == nil || res.Status != sandbox.StatusDone {
		t.Errorf("terminal result: %+v", res)
	}
}

func TestAdapter_StreamCancellable(t *testing.T) {
	m, srv := newMock(t)
	// Many events so cancellation can interrupt mid-stream.
	for i := 0; i < 50; i++ {
		m.streamEvents = append(m.streamEvents,
			fmt.Sprintf(`{"ts":"2026-06-15T10:00:00Z","stream":"stdout","text":"%d\n"}`, i))
	}
	m.terminationEv = `{"event":"terminated","status":"done"}`

	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines, results, err := a.Stream(ctx, sandbox.RunSpec{ID: "r", Image: "x", Command: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for range lines {
		count++
		if count == 3 {
			cancel()
		}
	}
	// Drain results without blocking the test if no result emitted.
	select {
	case <-results:
	case <-time.After(500 * time.Millisecond):
	}
	if count < 3 {
		t.Errorf("expected at least 3 events, got %d", count)
	}
}

func TestAdapter_Stop(t *testing.T) {
	m, srv := newMock(t)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(context.Background(), "abc123"); err != nil {
		t.Fatal(err)
	}
	if m.receivedStop != "abc123" {
		t.Errorf("server saw stop=%q", m.receivedStop)
	}
}

func TestAdapter_StopMissingRunIsOK(t *testing.T) {
	m, srv := newMock(t)
	m.stopShouldFail = false
	// Override handler to 404.
	srv.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(m.caps)
	})
	mux.HandleFunc("/v1/runs/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"run_not_found","status":404}`))
	})
	srv2 := httptest.NewServer(mux)
	defer srv2.Close()

	a, err := New(context.Background(), Config{BaseURL: srv2.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(context.Background(), "missing"); err != nil {
		t.Errorf("404 on stop should be nil error, got %v", err)
	}
}

func TestAdapter_Pool(t *testing.T) {
	_, srv := newMock(t)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := a.Pool(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pool.Adapter != "mock" || pool.Warm != 2 || pool.Active != 1 {
		t.Errorf("pool: %+v", pool)
	}
	if !pool.Capabilities.SupportsNetwork {
		t.Errorf("caps merged into pool wrong: %+v", pool.Capabilities)
	}
}

func TestAdapter_Probe(t *testing.T) {
	_, srv := newMock(t)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st != "healthy" {
		t.Errorf("status: %s", st)
	}
}

func TestAdapter_StreamWithoutTerminationReportsFailure(t *testing.T) {
	m, srv := newMock(t)
	m.streamEvents = []string{`{"ts":"2026-06-15T10:00:00Z","stream":"stdout","text":"orphan\n"}`}
	m.terminationEv = "" // intentionally not sent
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	lines, results, err := a.Stream(context.Background(), sandbox.RunSpec{ID: "r", Image: "x", Command: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	for range lines {
	}
	res := <-results
	if res == nil || res.Status != sandbox.StatusFailed {
		t.Errorf("expected failed result when stream ends without terminated event, got %+v", res)
	}
}

func TestAdapter_AuthRequired(t *testing.T) {
	m, srv := newMock(t)
	m.requireToken = "secret"
	_, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err == nil {
		t.Error("expected auth failure without token")
	}
	a, err := New(context.Background(), Config{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Capabilities().Adapter != "mock" {
		t.Errorf("auth+caps did not work")
	}
}
