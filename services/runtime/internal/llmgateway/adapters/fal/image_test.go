// SPDX-License-Identifier: Apache-2.0

package fal

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

func TestHandlesModel(t *testing.T) {
	a := New(Config{APIKey: "k"})
	if !a.HandlesModel("fal/fast-sdxl") {
		t.Errorf("expected HandlesModel(fal/fast-sdxl) = true")
	}
	if a.HandlesModel("flux/schnell") {
		t.Errorf("expected HandlesModel(flux/schnell) = false")
	}
}

func TestImageGenerationHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fal-ai/fast-sdxl" {
			t.Errorf("expected /fal-ai/fast-sdxl, got %q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Key ") {
			t.Errorf("expected Key auth, got %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		var p map[string]any
		_ = json.Unmarshal(body, &p)
		if p["prompt"] != "a cat" {
			t.Errorf("expected prompt=a cat, got %v", p["prompt"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x.com/1.png"}]}`))
	}))
	defer srv.Close()

	a := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := a.Image(context.Background(), adapters.ImageRequest{
		Model:  "fal/fast-sdxl",
		Prompt: "a cat",
		N:      1,
	})
	if err != nil {
		t.Fatalf("Image failed: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL == "" {
		t.Errorf("expected 1 image, got %+v", resp)
	}
}

func TestImageRejectsEditAndVariations(t *testing.T) {
	a := New(Config{APIKey: "k"})
	if _, err := a.Image(context.Background(), adapters.ImageRequest{Model: "fal/x", Prompt: "p", IsEdit: true}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported for edit, got %v", err)
	}
	if _, err := a.Image(context.Background(), adapters.ImageRequest{Model: "fal/x", IsVariations: true}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported for variations, got %v", err)
	}
}

func TestSpeechAndTranscribeUnsupported(t *testing.T) {
	a := New(Config{APIKey: "k"})
	if _, err := a.Speech(context.Background(), adapters.SpeechRequest{}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
	if _, err := a.Transcribe(context.Background(), adapters.TranscribeRequest{}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}
