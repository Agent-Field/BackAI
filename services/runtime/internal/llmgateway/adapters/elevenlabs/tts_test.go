// SPDX-License-Identifier: Apache-2.0

package elevenlabs

import (
	"context"
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

func TestAdapterHandlesElevenLabsPrefix(t *testing.T) {
	a := New(Config{APIKey: "k"})
	if !a.HandlesModel("elevenlabs/multilingual") {
		t.Errorf("expected HandlesModel(elevenlabs/multilingual) = true")
	}
	if a.HandlesModel("openai/tts-1") {
		t.Errorf("expected HandlesModel(openai/tts-1) = false")
	}
}

func TestSpeechResolvesVoiceAlias(t *testing.T) {
	var seenVoice string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path: /v1/text-to-speech/<voice_id>
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) >= 4 {
			seenVoice = parts[3]
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "hello") {
			t.Errorf("expected body to contain input, got %s", body)
		}
		if r.Header.Get("xi-api-key") != "k" {
			t.Errorf("expected xi-api-key header, got %q", r.Header.Get("xi-api-key"))
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio-bytes"))
	}))
	defer srv.Close()

	a := New(Config{APIKey: "k", BaseURL: srv.URL})
	resp, err := a.Speech(context.Background(), adapters.SpeechRequest{
		Model: "elevenlabs/multilingual",
		Input: "hello",
		Voice: "Rachel",
	})
	if err != nil {
		t.Fatalf("Speech failed: %v", err)
	}
	if string(resp.Body) != "audio-bytes" {
		t.Errorf("expected audio-bytes, got %s", resp.Body)
	}
	if seenVoice != "21m00Tcm4TlvDq8ikWAM" {
		t.Errorf("expected resolved Rachel voice id, got %q", seenVoice)
	}
	if resp.CharCount != 5 {
		t.Errorf("expected CharCount=5, got %d", resp.CharCount)
	}
}

func TestSpeechMissingVoice(t *testing.T) {
	a := New(Config{APIKey: "k"})
	_, err := a.Speech(context.Background(), adapters.SpeechRequest{
		Model: "elevenlabs/multilingual",
		Input: "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "voice is required") {
		t.Errorf("expected voice required error, got %v", err)
	}
}

func TestSpeechUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"bad key"}`))
	}))
	defer srv.Close()

	a := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := a.Speech(context.Background(), adapters.SpeechRequest{
		Model: "elevenlabs/multilingual",
		Input: "hi",
		Voice: "rachel",
	})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got %v", err)
	}
}

func TestTranscribeAndImageReturnUnsupported(t *testing.T) {
	a := New(Config{APIKey: "k"})
	if _, err := a.Transcribe(context.Background(), adapters.TranscribeRequest{}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported for Transcribe, got %v", err)
	}
	if _, err := a.Image(context.Background(), adapters.ImageRequest{}); err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported for Image, got %v", err)
	}
}
