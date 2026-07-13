// SPDX-License-Identifier: Apache-2.0

package e2b

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with empty key should return ErrAPIKeyMissing")
	}
	if _, err := New(Config{APIKey: "e2b_x"}); err != nil {
		t.Fatalf("New with key: %v", err)
	}
}

// connectFrame builds a Connect envelope: [flags][be-uint32 len][payload].
func connectFrame(flags byte, payload []byte) []byte {
	out := make([]byte, 5+len(payload))
	out[0] = flags
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out
}

func dataFrame(stream, text string) []byte {
	msg := map[string]any{"event": map[string]any{"data": map[string]any{
		stream: base64.StdEncoding.EncodeToString([]byte(text)),
	}}}
	b, _ := json.Marshal(msg)
	return connectFrame(0x00, b)
}

func endFrame(exitCode int) []byte {
	msg := map[string]any{"event": map[string]any{"end": map[string]any{
		"exitCode": exitCode, "exited": true,
	}}}
	b, _ := json.Marshal(msg)
	return connectFrame(0x00, b)
}

func eosFrame() []byte { return connectFrame(0x02, []byte(`{}`)) }

// e2bMock is a fake E2B: control plane (create/kill) + envd (files/exec).
type e2bMock struct {
	apiKey        string
	createBody    createSandboxRequest
	execProcess   processConfig
	killedID      string
	filesWritten  []string
	stdout        string
	stderr        string
	exitCode      int
	token         string
	gotAPIKeyOn   []string
	gotAccessTok  string
	sandboxIDHdr  string
	returnCreate5 bool
}

func newMockServer(t *testing.T, m *e2bMock) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /sandboxes", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == m.apiKey {
			m.gotAPIKeyOn = append(m.gotAPIKeyOn, "/sandboxes")
		}
		if m.returnCreate5 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"message":"Invalid auth"}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&m.createBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createSandboxResponse{
			SandboxID: "sbx-123", EnvdAccessToken: m.token, EnvdVersion: "0.5.9",
		})
	})

	mux.HandleFunc("DELETE /sandboxes/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.killedID = r.PathValue("id")
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /files", func(w http.ResponseWriter, r *http.Request) {
		m.gotAccessTok = r.Header.Get("X-Access-Token")
		m.filesWritten = append(m.filesWritten, r.URL.Query().Get("path"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})

	mux.HandleFunc("POST /process.Process/Start", func(w http.ResponseWriter, r *http.Request) {
		m.gotAccessTok = r.Header.Get("X-Access-Token")
		m.sandboxIDHdr = r.Header.Get("E2b-Sandbox-Id")
		hdr := make([]byte, 5)
		_, _ = io.ReadFull(r.Body, hdr)
		n := binary.BigEndian.Uint32(hdr[1:5])
		body := make([]byte, n)
		_, _ = io.ReadFull(r.Body, body)
		var sr startRequest
		_ = json.Unmarshal(body, &sr)
		m.execProcess = sr.Process

		w.Header().Set("Content-Type", "application/connect+json")
		w.WriteHeader(http.StatusOK)
		if m.stdout != "" {
			_, _ = w.Write(dataFrame("stdout", m.stdout))
		}
		if m.stderr != "" {
			_, _ = w.Write(dataFrame("stderr", m.stderr))
		}
		_, _ = w.Write(endFrame(m.exitCode))
		_, _ = w.Write(eosFrame())
	})

	return httptest.NewServer(mux)
}

func newTestAdapter(t *testing.T, srv *httptest.Server, key string) *Adapter {
	t.Helper()
	a, err := New(Config{
		APIKey:      key,
		BaseURL:     srv.URL,
		EnvdBaseURL: srv.URL,
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRunHappyPath(t *testing.T) {
	m := &e2bMock{apiKey: "e2b_key", token: "tok-abc", stdout: "hi\n", exitCode: 0}
	srv := newMockServer(t, m)
	defer srv.Close()
	a := newTestAdapter(t, srv, "e2b_key")

	res, err := a.Run(context.Background(), sandbox.RunSpec{
		Image: "base", Command: []string{"echo", "hi"}, TimeoutS: 60,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != sandbox.StatusDone || res.ExitCode != 0 {
		t.Errorf("status=%v exit=%d, want done/0", res.Status, res.ExitCode)
	}
	if len(m.gotAPIKeyOn) == 0 {
		t.Error("create did not send X-API-Key")
	}
	if m.createBody.TemplateID != "base" || m.createBody.Timeout != 60 {
		t.Errorf("create body = %+v, want templateID=base timeout=60", m.createBody)
	}
	if m.sandboxIDHdr != "sbx-123" || m.gotAccessTok != "tok-abc" {
		t.Errorf("envd headers id=%q tok=%q", m.sandboxIDHdr, m.gotAccessTok)
	}
	if m.execProcess.Cmd != "echo" || len(m.execProcess.Args) != 1 || m.execProcess.Args[0] != "hi" {
		t.Errorf("exec process = %+v, want cmd=echo args=[hi]", m.execProcess)
	}
	out, _ := a.fetchCachedLogs()
	if out != "hi\n" {
		t.Errorf("stdout = %q, want %q", out, "hi\n")
	}
	if m.killedID != "sbx-123" {
		t.Errorf("killed id = %q, want sbx-123", m.killedID)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	m := &e2bMock{apiKey: "e2b_key", token: "t", stderr: "boom\n", exitCode: 3}
	srv := newMockServer(t, m)
	defer srv.Close()
	a := newTestAdapter(t, srv, "e2b_key")

	res, err := a.Run(context.Background(), sandbox.RunSpec{Image: "base", Command: []string{"false"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != sandbox.StatusFailed || res.ExitCode != 3 {
		t.Errorf("status=%v exit=%d, want failed/3", res.Status, res.ExitCode)
	}
	_, se := a.fetchCachedLogs()
	if se != "boom\n" {
		t.Errorf("stderr = %q", se)
	}
}

func TestRunSeedsFiles(t *testing.T) {
	m := &e2bMock{apiKey: "e2b_key", token: "t", stdout: "ok", exitCode: 0}
	srv := newMockServer(t, m)
	defer srv.Close()
	a := newTestAdapter(t, srv, "e2b_key")

	_, err := a.Run(context.Background(), sandbox.RunSpec{
		Image: "base", Command: []string{"cat", "/home/user/in.txt"},
		Files: map[string]string{"/home/user/in.txt": "data"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(m.filesWritten) != 1 || m.filesWritten[0] != "/home/user/in.txt" {
		t.Errorf("files written = %v", m.filesWritten)
	}
}

func TestRunCreateAuthFailureSurfaces(t *testing.T) {
	m := &e2bMock{apiKey: "e2b_key", returnCreate5: true}
	srv := newMockServer(t, m)
	defer srv.Close()
	a := newTestAdapter(t, srv, "e2b_key")

	_, err := a.Run(context.Background(), sandbox.RunSpec{Image: "base", Command: []string{"echo", "hi"}})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected a surfaced 401 error, got %v", err)
	}
}

func TestRunEmptyCommand(t *testing.T) {
	a, _ := New(Config{APIKey: "e2b_key"})
	if _, err := a.Run(context.Background(), sandbox.RunSpec{Image: "base"}); err == nil {
		t.Fatal("empty command should error")
	}
}

func TestExecStreamMissingExitEvent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sandboxes", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createSandboxResponse{SandboxID: "s"})
	})
	mux.HandleFunc("DELETE /sandboxes/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /process.Process/Start", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(dataFrame("stdout", "partial"))
		_, _ = w.Write(eosFrame())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	a := newTestAdapter(t, srv, "e2b_key")

	_, err := a.Run(context.Background(), sandbox.RunSpec{Image: "base", Command: []string{"echo"}})
	if err == nil || !strings.Contains(err.Error(), "exit event") {
		t.Fatalf("expected missing-exit-event error, got %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	a, _ := New(Config{APIKey: "e2b_key"})
	if c := a.Capabilities(); c.Adapter != "e2b" || c.MaxTimeoutS != 3600 {
		t.Errorf("caps = %+v", c)
	}
}
