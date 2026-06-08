// SPDX-License-Identifier: Apache-2.0

package cartesia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

func TestNewReturnsNilWithoutKey(t *testing.T) {
	if New(Config{}) != nil {
		t.Errorf("expected nil adapter when APIKey is empty")
	}
}

func TestHandlesModelMatchesCartesiaPrefix(t *testing.T) {
	a := New(Config{APIKey: "k"})
	if !a.HandlesModel("cartesia/sonic") {
		t.Errorf("expected HandlesModel(cartesia/sonic) = true")
	}
}

func TestSpeechSendsExpectedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			t.Errorf("missing api key, got %q", r.Header.Get("X-API-Key"))
		}
		if r.Header.Get("Cartesia-Version") == "" {
			t.Errorf("missing version header")
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("body not JSON: %v body=%s", err, body)
		}
		if payload["model_id"] != "sonic-english" {
			t.Errorf("expected model_id=sonic-english, got %v", payload["model_id"])
		}
		if payload["transcript"] != "hello" {
			t.Errorf("expected transcript=hello, got %v", payload["transcript"])
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("wav-bytes"))
	}))
	defer srv.Close()

	a := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := a.Speech(context.Background(), adapters.SpeechRequest{
		Model: "cartesia/sonic",
		Input: "hello",
		Voice: "voice-uuid",
	})
	if err != nil {
		t.Fatalf("Speech failed: %v", err)
	}
	if !strings.Contains(string(resp.Body), "wav-bytes") {
		t.Errorf("expected wav-bytes, got %s", resp.Body)
	}
}

func TestSpeechMissingInput(t *testing.T) {
	a := New(Config{APIKey: "k"})
	_, err := a.Speech(context.Background(), adapters.SpeechRequest{
		Model: "cartesia/sonic",
		Voice: "v",
	})
	if err == nil || !strings.Contains(err.Error(), "input is required") {
		t.Errorf("expected input required error, got %v", err)
	}
}
