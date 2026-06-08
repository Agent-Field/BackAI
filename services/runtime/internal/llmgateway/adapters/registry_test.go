// SPDX-License-Identifier: Apache-2.0

package adapters

import (
	"context"
	"testing"
)

// fakeAdapter is the minimal MultimodalAdapter implementation used by
// the registry tests.
type fakeAdapter struct {
	name     string
	prefix   string
	tts, stt bool
	image    bool
}

func (f *fakeAdapter) Name() string                  { return f.name }
func (f *fakeAdapter) HandlesModel(model string) bool { return HasPrefix(model, f.prefix) }
func (f *fakeAdapter) SupportsTTS() bool              { return f.tts }
func (f *fakeAdapter) SupportsSTT() bool              { return f.stt }
func (f *fakeAdapter) SupportsImage() bool            { return f.image }
func (f *fakeAdapter) Speech(_ context.Context, _ SpeechRequest) (SpeechResponse, error) {
	if !f.tts {
		return SpeechResponse{}, ErrUnsupported
	}
	return SpeechResponse{Body: []byte("fake-audio")}, nil
}
func (f *fakeAdapter) Transcribe(_ context.Context, _ TranscribeRequest) (TranscribeResponse, error) {
	if !f.stt {
		return TranscribeResponse{}, ErrUnsupported
	}
	return TranscribeResponse{Body: []byte(`{"text":"fake"}`)}, nil
}
func (f *fakeAdapter) Image(_ context.Context, _ ImageRequest) (ImageResponse, error) {
	if !f.image {
		return ImageResponse{}, ErrUnsupported
	}
	return ImageResponse{Data: []ImageItem{{URL: "https://fake"}}}, nil
}

func TestRegistryPickReturnsFirstHandler(t *testing.T) {
	a := &fakeAdapter{name: "elevenlabs", prefix: "elevenlabs", tts: true}
	b := &fakeAdapter{name: "cartesia", prefix: "cartesia", tts: true}
	r := NewRegistry(a, b)

	got := r.Pick("elevenlabs/multilingual")
	if got == nil || got.Name() != "elevenlabs" {
		t.Errorf("expected elevenlabs, got %v", got)
	}
	got = r.Pick("cartesia/sonic")
	if got == nil || got.Name() != "cartesia" {
		t.Errorf("expected cartesia, got %v", got)
	}
	if r.Pick("openai/tts-1") != nil {
		t.Errorf("expected nil for openai/tts-1 (no fallback registered)")
	}
}

func TestRegistryHandlesNilEntries(t *testing.T) {
	a := &fakeAdapter{name: "elevenlabs", prefix: "elevenlabs", tts: true}
	r := NewRegistry(nil, a, nil)
	if len(r.Names()) != 1 || r.Names()[0] != "elevenlabs" {
		t.Errorf("expected only elevenlabs in registry, got %v", r.Names())
	}
}

func TestHasPrefix(t *testing.T) {
	cases := []struct {
		model  string
		prefix string
		want   bool
	}{
		{"elevenlabs/multilingual", "elevenlabs", true},
		{"elevenlabs", "elevenlabs", false}, // no slash → not handled
		{"elevenlabsmodel", "elevenlabs", false},
		{"cartesia/sonic-english", "cartesia", true},
		{"openai/tts-1", "elevenlabs", false},
	}
	for _, c := range cases {
		if got := HasPrefix(c.model, c.prefix); got != c.want {
			t.Errorf("HasPrefix(%q, %q) = %v, want %v", c.model, c.prefix, got, c.want)
		}
	}
}
