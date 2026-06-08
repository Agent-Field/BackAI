// SPDX-License-Identifier: Apache-2.0

package flux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

func TestNewReturnsNilWithoutKeys(t *testing.T) {
	if New(Config{}) != nil {
		t.Errorf("expected nil adapter when both keys empty")
	}
}

func TestPicksFalWhenFalKeySet(t *testing.T) {
	a := New(Config{FalAPIKey: "k"})
	if a == nil {
		t.Fatal("expected non-nil adapter")
	}
	if !strings.Contains(a.Name(), "fal") {
		t.Errorf("expected fal backend, got %s", a.Name())
	}
}

func TestPicksReplicateWhenOnlyReplicateSet(t *testing.T) {
	a := New(Config{ReplicateAPIKey: "k"})
	if a == nil {
		t.Fatal("expected non-nil adapter")
	}
	if !strings.Contains(a.Name(), "replicate") {
		t.Errorf("expected replicate backend, got %s", a.Name())
	}
}

func TestHandlesModelMatchesFluxPrefix(t *testing.T) {
	a := New(Config{FalAPIKey: "k"})
	if !a.HandlesModel("flux/schnell") {
		t.Errorf("expected HandlesModel(flux/schnell) = true")
	}
	if a.HandlesModel("fal/fast-sdxl") {
		t.Errorf("expected HandlesModel(fal/fast-sdxl) = false")
	}
}

func TestImageFluxFalHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fal-ai/flux/schnell" {
			t.Errorf("expected /fal-ai/flux/schnell, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x.com/f.png"}]}`))
	}))
	defer srv.Close()

	a := New(Config{
		FalAPIKey:       "k",
		Backend:         BackendFal,
		BaseURLOverride: srv.URL,
	})
	resp, err := a.Image(context.Background(), adapters.ImageRequest{
		Model:  "flux/schnell",
		Prompt: "a cat",
	})
	if err != nil {
		t.Fatalf("Image failed: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL == "" {
		t.Errorf("expected 1 image, got %+v", resp)
	}
}

func TestImageFluxReplicateHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/predictions") {
			t.Errorf("expected /predictions path, got %q", r.URL.Path)
		}
		if r.Header.Get("Prefer") != "wait" {
			t.Errorf("expected Prefer: wait header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"succeeded","output":["https://r.com/x.png"]}`))
	}))
	defer srv.Close()

	a := New(Config{
		ReplicateAPIKey: "k",
		Backend:         BackendReplicate,
		BaseURLOverride: srv.URL,
	})
	resp, err := a.Image(context.Background(), adapters.ImageRequest{
		Model:  "flux/schnell",
		Prompt: "a cat",
	})
	if err != nil {
		t.Fatalf("Image failed: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL == "" {
		t.Errorf("expected 1 image, got %+v", resp)
	}
}

func TestSpeechAndTranscribeUnsupported(t *testing.T) {
	a := New(Config{FalAPIKey: "k"})
	if _, err := a.Speech(context.Background(), adapters.SpeechRequest{}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
	if _, err := a.Transcribe(context.Background(), adapters.TranscribeRequest{}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}
