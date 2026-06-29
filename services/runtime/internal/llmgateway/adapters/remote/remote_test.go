// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

func TestNewFailsWithoutBaseURL(t *testing.T) {
	_, err := New(context.Background(), Config{})
	if err == nil || !strings.Contains(err.Error(), "BaseURL required") {
		t.Errorf("expected BaseURL required error, got %v", err)
	}
}

func TestNewProbesCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/capabilities" {
			t.Errorf("expected /v1/capabilities, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":              "test-adapter",
			"version":           "1.0.0",
			"slot":              "multimodal",
			"protocol_version":  "v1",
			"vendor":            "TestVendor",
			"capabilities": json.RawMessage(`{
				"supports_tts": true,
				"supports_stt": false,
				"supports_image_generation": true,
				"supports_image_edit": false,
				"supports_image_variation": false,
				"model_prefixes": ["test"]
			}`),
		})
	}))
	defer srv.Close()

	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if a.Name() != "test-adapter" {
		t.Errorf("expected name=test-adapter, got %s", a.Name())
	}
	if !a.SupportsTTS() {
		t.Errorf("expected SupportsTTS=true")
	}
	if a.SupportsSTT() {
		t.Errorf("expected SupportsSTT=false")
	}
}

func TestNewFailsWithWrongSlot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":              "wrong-adapter",
			"version":           "1.0.0",
			"slot":              "sandbox",
			"protocol_version":  "v1",
			"vendor":           "TestVendor",
			"capabilities":      json.RawMessage(`{}`),
		})
	}))
	defer srv.Close()

	_, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "expected multimodal") {
		t.Errorf("expected slot mismatch error, got %v", err)
	}
}

func TestHandlesModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":             "test-adapter",
			"version":          "1.0.0",
			"slot":             "multimodal",
			"protocol_version": "v1",
			"vendor":           "TestVendor",
			"capabilities": json.RawMessage(`{
				"supports_tts": true,
				"model_prefixes": ["test", "audio"]
			}`),
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})

	tests := []struct {
		model    string
		expected bool
	}{
		{"test/tts-1", true},
		{"audio/whisper", true},
		{"openai/tts-1", false},
		{"test", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := a.HandlesModel(tc.model); got != tc.expected {
			t.Errorf("HandlesModel(%q) = %v, expected %v", tc.model, got, tc.expected)
		}
	}
}

func TestSpeechReturnsAudioBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-adapter",
				"version":          "1.0.0",
				"slot":             "multimodal",
				"protocol_version": "v1",
				"vendor":           "TestVendor",
				"capabilities": json.RawMessage(`{
					"supports_tts": true,
					"model_prefixes": ["test"]
				}`),
			})
			return
		}

		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("expected /v1/audio/speech, got %s", r.URL.Path)
		}

		// Verify request body structure.
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "test/tts-1" {
			t.Errorf("expected model=test/tts-1, got %v", body["model"])
		}
		if body["input"] != "hello world" {
			t.Errorf("expected input=hello world, got %v", body["input"])
		}
		if body["voice"] != "alloy" {
			t.Errorf("expected voice=alloy, got %v", body["voice"])
		}

		// Return audio bytes with char count header.
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("X-Backai-Char-Count", "11")
		w.Write([]byte("audio-data-bytes"))
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	resp, err := a.Speech(context.Background(), adapters.SpeechRequest{
		Model: "test/tts-1",
		Input: "hello world",
		Voice: "alloy",
	})
	if err != nil {
		t.Fatalf("Speech failed: %v", err)
	}
	if string(resp.Body) != "audio-data-bytes" {
		t.Errorf("expected audio-data-bytes, got %s", resp.Body)
	}
	if resp.ContentType != "audio/mpeg" {
		t.Errorf("expected audio/mpeg, got %s", resp.ContentType)
	}
	if resp.CharCount != 11 {
		t.Errorf("expected CharCount=11, got %d", resp.CharCount)
	}
}

func TestSpeechUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":             "test-adapter",
			"version":          "1.0.0",
			"slot":             "multimodal",
			"protocol_version": "v1",
			"vendor":           "TestVendor",
			"capabilities": json.RawMessage(`{
				"supports_tts": false,
				"model_prefixes": ["test"]
			}`),
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	_, err := a.Speech(context.Background(), adapters.SpeechRequest{
		Model: "test/something",
		Input: "test",
	})
	if err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

func TestTranscribeReturnsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-adapter",
				"version":          "1.0.0",
				"slot":             "multimodal",
				"protocol_version": "v1",
				"vendor":           "TestVendor",
				"capabilities": json.RawMessage(`{
					"supports_stt": true,
					"model_prefixes": ["test"]
				}`),
			})
			return
		}

		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("expected /v1/audio/transcriptions, got %s", r.URL.Path)
		}

		// Verify multipart forwarding.
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart") {
			t.Errorf("expected multipart content-type, got %s", r.Header.Get("Content-Type"))
		}

		// Return JSON transcription with duration header.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backai-Duration-Seconds", "3.5")
		json.NewEncoder(w).Encode(map[string]any{
			"text":     "hello world",
			"language": "en",
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})

	// Simulate multipart body.
	multipartBody := `--boundary\r\nContent-Disposition: form-data; name="file"\r\n\r\naudio-data\r\n--boundary--\r\n`

	resp, err := a.Transcribe(context.Background(), adapters.TranscribeRequest{
		Model:       "test/whisper",
		ContentType: "multipart/form-data; boundary=boundary",
		Body:        []byte(multipartBody),
		Translate:   false,
	})
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}
	if resp.ContentType != "application/json" {
		t.Errorf("expected application/json, got %s", resp.ContentType)
	}
	if resp.DurationSeconds != 3.5 {
		t.Errorf("expected DurationSeconds=3.5, got %v", resp.DurationSeconds)
	}

	var body map[string]any
	json.Unmarshal(resp.Body, &body)
	if body["text"] != "hello world" {
		t.Errorf("expected text=hello world, got %v", body["text"])
	}
}

func TestTranscribeTranslate(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-adapter",
				"version":          "1.0.0",
				"slot":             "multimodal",
				"protocol_version": "v1",
				"vendor":           "TestVendor",
				"capabilities": json.RawMessage(`{
					"supports_stt": true,
					"model_prefixes": ["test"]
				}`),
			})
			return
		}

		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backai-Duration-Seconds", "2.0")
		json.NewEncoder(w).Encode(map[string]any{
			"text": "translated to english",
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	resp, err := a.Transcribe(context.Background(), adapters.TranscribeRequest{
		Model:       "test/whisper",
		ContentType: "multipart/form-data; boundary=b",
		Body:        []byte("--b\r\n\r\n--b--\r\n"),
		Translate:   true,
	})
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}
	if seenPath != "/v1/audio/translations" {
		t.Errorf("expected /v1/audio/translations, got %s", seenPath)
	}
	if resp.DurationSeconds != 2.0 {
		t.Errorf("expected DurationSeconds=2.0, got %v", resp.DurationSeconds)
	}
}

func TestTranscribeUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":             "test-adapter",
			"version":          "1.0.0",
			"slot":             "multimodal",
			"protocol_version": "v1",
			"vendor":           "TestVendor",
			"capabilities": json.RawMessage(`{
				"supports_stt": false,
				"model_prefixes": ["test"]
			}`),
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	_, err := a.Transcribe(context.Background(), adapters.TranscribeRequest{
		Model:       "test/whisper",
		ContentType: "multipart/form-data; boundary=b",
		Body:        []byte("data"),
	})
	if err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

func TestImageGenerations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-adapter",
				"version":          "1.0.0",
				"slot":             "multimodal",
				"protocol_version": "v1",
				"vendor":           "TestVendor",
				"capabilities": json.RawMessage(`{
					"supports_image_generation": true,
					"model_prefixes": ["test"]
				}`),
			})
			return
		}

		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("expected /v1/images/generations, got %s", r.URL.Path)
		}

		// Verify request body.
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["prompt"] != "a cat" {
			t.Errorf("expected prompt=a cat, got %v", body["prompt"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adapters.ImageResponse{
			Created: 1700000000,
			Data: []adapters.ImageItem{
				{
					URL:           "https://example.com/image.png",
					RevisedPrompt: "a fluffy cat",
				},
			},
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	resp, err := a.Image(context.Background(), adapters.ImageRequest{
		Model:  "test/dall-e-3",
		Prompt: "a cat",
		N:      1,
	})
	if err != nil {
		t.Fatalf("Image failed: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Errorf("expected 1 image, got %d", len(resp.Data))
	}
	if resp.Data[0].URL != "https://example.com/image.png" {
		t.Errorf("expected image URL, got %s", resp.Data[0].URL)
	}
}

func TestImageEdits(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-adapter",
				"version":          "1.0.0",
				"slot":             "multimodal",
				"protocol_version": "v1",
				"vendor":           "TestVendor",
				"capabilities": json.RawMessage(`{
					"supports_image_edit": true,
					"model_prefixes": ["test"]
				}`),
			})
			return
		}

		seenPath = r.URL.Path
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart") {
			t.Errorf("expected multipart content-type for edits")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adapters.ImageResponse{
			Created: 1700000001,
			Data: []adapters.ImageItem{
				{
					URL: "https://example.com/edited.png",
				},
			},
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	resp, err := a.Image(context.Background(), adapters.ImageRequest{
		Model:                "test/dall-e-3",
		Prompt:               "make it blue",
		IsEdit:               true,
		Body:                 []byte("--boundary\r\n\r\nimage-data\r\n--boundary--\r\n"),
		MultipartContentType: "multipart/form-data; boundary=boundary",
	})
	if err != nil {
		t.Fatalf("Image edits failed: %v", err)
	}
	if seenPath != "/v1/images/edits" {
		t.Errorf("expected /v1/images/edits, got %s", seenPath)
	}
	if resp.Data[0].URL != "https://example.com/edited.png" {
		t.Errorf("expected edited image URL, got %s", resp.Data[0].URL)
	}
}

func TestImageVariations(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-adapter",
				"version":          "1.0.0",
				"slot":             "multimodal",
				"protocol_version": "v1",
				"vendor":           "TestVendor",
				"capabilities": json.RawMessage(`{
					"supports_image_variation": true,
					"model_prefixes": ["test"]
				}`),
			})
			return
		}

		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adapters.ImageResponse{
			Created: 1700000002,
			Data: []adapters.ImageItem{
				{URL: "https://example.com/variation1.png"},
				{URL: "https://example.com/variation2.png"},
			},
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	resp, err := a.Image(context.Background(), adapters.ImageRequest{
		Model:                "test/dall-e-3",
		IsVariations:         true,
		N:                    2,
		Body:                 []byte("multipart-data"),
		MultipartContentType: "multipart/form-data; boundary=b",
	})
	if err != nil {
		t.Fatalf("Image variations failed: %v", err)
	}
	if seenPath != "/v1/images/variations" {
		t.Errorf("expected /v1/images/variations, got %s", seenPath)
	}
	if len(resp.Data) != 2 {
		t.Errorf("expected 2 variations, got %d", len(resp.Data))
	}
}

func TestImageUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":             "test-adapter",
			"version":          "1.0.0",
			"slot":             "multimodal",
			"protocol_version": "v1",
			"vendor":           "TestVendor",
			"capabilities": json.RawMessage(`{
				"supports_image_generation": false,
				"supports_image_edit": false,
				"supports_image_variation": false,
				"model_prefixes": ["test"]
			}`),
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	_, err := a.Image(context.Background(), adapters.ImageRequest{
		Model:  "test/flux",
		Prompt: "test",
	})
	if err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

func TestSupportsImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":             "test-adapter",
			"version":          "1.0.0",
			"slot":             "multimodal",
			"protocol_version": "v1",
			"vendor":           "TestVendor",
			"capabilities": json.RawMessage(`{
				"supports_image_generation": true,
				"model_prefixes": ["test"]
			}`),
		})
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{BaseURL: srv.URL})
	if !a.SupportsImage() {
		t.Errorf("expected SupportsImage=true when any image capability is enabled")
	}
}

func TestNilAdapterSafety(t *testing.T) {
	var a *Adapter

	if a.Name() != "" {
		t.Errorf("expected Name() to return empty string for nil adapter")
	}
	if a.HandlesModel("test/anything") {
		t.Errorf("expected HandlesModel() to return false for nil adapter")
	}
	if a.SupportsTTS() || a.SupportsSTT() || a.SupportsImage() {
		t.Errorf("expected all capabilities to return false for nil adapter")
	}

	_, err := a.Speech(context.Background(), adapters.SpeechRequest{})
	if err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported from nil adapter, got %v", err)
	}

	_, err = a.Transcribe(context.Background(), adapters.TranscribeRequest{})
	if err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported from nil adapter, got %v", err)
	}

	_, err = a.Image(context.Background(), adapters.ImageRequest{})
	if err != adapters.ErrUnsupported {
		t.Errorf("expected ErrUnsupported from nil adapter, got %v", err)
	}
}
