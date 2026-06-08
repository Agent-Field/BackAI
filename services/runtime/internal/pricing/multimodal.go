// SPDX-License-Identifier: Apache-2.0

// multimodal.go — pricing for audio (TTS, STT) + image-gen models.
//
// Cost shape is per-unit:
//   - tts: USD per million characters (CostPerMillionChars)
//   - stt: USD per minute of input audio (CostPerMinute)
//   - image: USD per generated image (CostPerImage)
//
// EstimateMultimodalCostUSD picks the right field by verb.
//
// Sources: each provider's public pricing page, snapshot 2026-06-07.
// Numbers refresh when an operator notices a drift — keep this file
// the single source of truth for the dashboard "cost by modality" view.

package pricing

import "strings"

// MultimodalPrice describes pricing for one audio or image model.
type MultimodalPrice struct {
	// ID is the canonical gateway model id (e.g. "openai/tts-1",
	// "elevenlabs/multilingual", "fal/fast-sdxl").
	ID string
	// DisplayName for the dashboard.
	DisplayName string
	// Provider id (openai, elevenlabs, cartesia, fal, flux, replicate).
	Provider string
	// Verb is "tts" | "stt" | "image".
	Verb string
	// CostPerMillionChars is USD per 1M characters of input text (TTS).
	CostPerMillionChars float64
	// CostPerMinute is USD per minute of input audio (STT).
	CostPerMinute float64
	// CostPerImage is USD per generated image (image).
	CostPerImage float64
}

// MultimodalCatalog holds the known TTS/STT/image models with their rates.
var MultimodalCatalog = []MultimodalPrice{
	// ─── OpenAI — TTS ─────────────────────────────────────────────
	{
		ID:                  "openai/tts-1",
		DisplayName:         "OpenAI TTS-1",
		Provider:            "openai",
		Verb:                "tts",
		CostPerMillionChars: 15.00,
	},
	{
		ID:                  "openai/tts-1-hd",
		DisplayName:         "OpenAI TTS-1 HD",
		Provider:            "openai",
		Verb:                "tts",
		CostPerMillionChars: 30.00,
	},

	// ─── OpenAI — STT ─────────────────────────────────────────────
	{
		ID:            "openai/whisper-1",
		DisplayName:   "OpenAI Whisper",
		Provider:      "openai",
		Verb:          "stt",
		CostPerMinute: 0.006,
	},

	// ─── OpenAI — Image ───────────────────────────────────────────
	{
		ID:           "openai/dall-e-2",
		DisplayName:  "OpenAI DALL·E 2",
		Provider:     "openai",
		Verb:         "image",
		CostPerImage: 0.020,
	},
	{
		ID:           "openai/dall-e-3",
		DisplayName:  "OpenAI DALL·E 3 (standard)",
		Provider:     "openai",
		Verb:         "image",
		CostPerImage: 0.040,
	},
	{
		ID:           "openai/gpt-image-1",
		DisplayName:  "OpenAI gpt-image-1",
		Provider:     "openai",
		Verb:         "image",
		CostPerImage: 0.020,
	},

	// ─── ElevenLabs — TTS (Creator plan reference rates) ─────────
	{
		ID:                  "elevenlabs/multilingual",
		DisplayName:         "ElevenLabs Multilingual v2",
		Provider:            "elevenlabs",
		Verb:                "tts",
		CostPerMillionChars: 180.00,
	},
	{
		ID:                  "elevenlabs/turbo",
		DisplayName:         "ElevenLabs Turbo v2.5",
		Provider:            "elevenlabs",
		Verb:                "tts",
		CostPerMillionChars: 90.00,
	},
	{
		ID:                  "elevenlabs/flash",
		DisplayName:         "ElevenLabs Flash v2.5",
		Provider:            "elevenlabs",
		Verb:                "tts",
		CostPerMillionChars: 50.00,
	},

	// ─── Cartesia — TTS ───────────────────────────────────────────
	{
		ID:                  "cartesia/sonic",
		DisplayName:         "Cartesia Sonic English",
		Provider:            "cartesia",
		Verb:                "tts",
		CostPerMillionChars: 65.00,
	},
	{
		ID:                  "cartesia/sonic-multilingual",
		DisplayName:         "Cartesia Sonic Multilingual",
		Provider:            "cartesia",
		Verb:                "tts",
		CostPerMillionChars: 65.00,
	},

	// ─── Flux — image ─────────────────────────────────────────────
	{
		ID:           "flux/schnell",
		DisplayName:  "Flux Schnell",
		Provider:     "flux",
		Verb:         "image",
		CostPerImage: 0.003,
	},
	{
		ID:           "flux/dev",
		DisplayName:  "Flux Dev",
		Provider:     "flux",
		Verb:         "image",
		CostPerImage: 0.025,
	},
	{
		ID:           "flux/pro",
		DisplayName:  "Flux 1.1 Pro",
		Provider:     "flux",
		Verb:         "image",
		CostPerImage: 0.040,
	},
	{
		ID:           "flux/pro-ultra",
		DisplayName:  "Flux 1.1 Pro Ultra",
		Provider:     "flux",
		Verb:         "image",
		CostPerImage: 0.060,
	},

	// ─── fal.ai — image (representative rates; many models exist) ─
	{
		ID:           "fal/fast-sdxl",
		DisplayName:  "fal.ai Fast SDXL",
		Provider:     "fal",
		Verb:         "image",
		CostPerImage: 0.005,
	},
}

// LookupMultimodal finds the pricing entry for a multimodal model.
// Case-insensitive on ID. Returns (entry, true) on hit; (zero, false) on miss.
func LookupMultimodal(modelID string) (MultimodalPrice, bool) {
	target := strings.ToLower(strings.TrimSpace(modelID))
	for _, p := range MultimodalCatalog {
		if strings.ToLower(p.ID) == target {
			return p, true
		}
	}
	return MultimodalPrice{}, false
}

// EstimateMultimodalCostUSD returns the USD cost for a multimodal call.
//
//	verb = "tts"   → units = characters
//	verb = "stt"   → units = seconds
//	verb = "image" → units = images
//
// Returns (cost, true) on hit; (0, false) when the model isn't in the
// catalog or the verb doesn't match the entry's pricing shape.
func EstimateMultimodalCostUSD(modelID, verb string, units float64) (float64, bool) {
	price, ok := LookupMultimodal(modelID)
	if !ok {
		return 0, false
	}
	if price.Verb != verb {
		return 0, false
	}
	switch verb {
	case "tts":
		return (units / 1_000_000.0) * price.CostPerMillionChars, true
	case "stt":
		return (units / 60.0) * price.CostPerMinute, true
	case "image":
		return units * price.CostPerImage, true
	}
	return 0, false
}
