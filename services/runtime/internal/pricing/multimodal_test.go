// SPDX-License-Identifier: Apache-2.0

package pricing

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestEstimateMultimodalCostUSDTTS(t *testing.T) {
	// openai/tts-1 = $15/1M chars; 1000 chars = $0.015
	cost, ok := EstimateMultimodalCostUSD("openai/tts-1", "tts", 1000)
	if !ok {
		t.Fatal("expected hit on openai/tts-1")
	}
	if !almostEqual(cost, 0.015) {
		t.Errorf("expected 0.015, got %v", cost)
	}
}

func TestEstimateMultimodalCostUSDSTT(t *testing.T) {
	// openai/whisper-1 = $0.006/min; 60s = $0.006
	cost, ok := EstimateMultimodalCostUSD("openai/whisper-1", "stt", 60)
	if !ok {
		t.Fatal("expected hit on openai/whisper-1")
	}
	if !almostEqual(cost, 0.006) {
		t.Errorf("expected 0.006, got %v", cost)
	}
}

func TestEstimateMultimodalCostUSDImage(t *testing.T) {
	// openai/dall-e-3 = $0.04/image; 2 images = $0.08
	cost, ok := EstimateMultimodalCostUSD("openai/dall-e-3", "image", 2)
	if !ok {
		t.Fatal("expected hit on openai/dall-e-3")
	}
	if !almostEqual(cost, 0.08) {
		t.Errorf("expected 0.08, got %v", cost)
	}
}

func TestEstimateMultimodalCostUSDVerbMismatch(t *testing.T) {
	// openai/tts-1 is a TTS model — verb=stt should miss.
	if _, ok := EstimateMultimodalCostUSD("openai/tts-1", "stt", 60); ok {
		t.Errorf("expected miss when verb mismatches model entry")
	}
}

func TestEstimateMultimodalCostUSDUnknownModel(t *testing.T) {
	if _, ok := EstimateMultimodalCostUSD("openai/unknown-model", "tts", 1000); ok {
		t.Errorf("expected miss for unknown model")
	}
}
