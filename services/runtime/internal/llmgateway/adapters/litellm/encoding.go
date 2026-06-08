// SPDX-License-Identifier: Apache-2.0

package litellm

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
)

// buildSpeechBody serializes a SpeechRequest into the OpenAI /audio/speech
// JSON body. Used when the caller didn't pass through a pre-built body.
func buildSpeechBody(req adapters.SpeechRequest) []byte {
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}
	if req.Voice != "" {
		payload["voice"] = req.Voice
	}
	if req.ResponseFormat != "" {
		payload["response_format"] = req.ResponseFormat
	}
	if req.Speed > 0 {
		payload["speed"] = req.Speed
	}
	out, _ := json.Marshal(payload)
	return out
}

// buildImageBody serializes an ImageRequest into the OpenAI
// /images/generations JSON body for plain generate calls.
func buildImageBody(req adapters.ImageRequest) []byte {
	payload := map[string]any{
		"prompt": req.Prompt,
	}
	if req.Model != "" {
		payload["model"] = req.Model
	}
	if req.N > 0 {
		payload["n"] = req.N
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}
	if req.Quality != "" {
		payload["quality"] = req.Quality
	}
	if req.Style != "" {
		payload["style"] = req.Style
	}
	if req.ResponseFormat != "" {
		payload["response_format"] = req.ResponseFormat
	}
	out, _ := json.Marshal(payload)
	return out
}

// decodeImageResponse parses the OpenAI /images/* response body into the
// adapter shape. We re-emit Created if absent so the dashboard's
// timeline view has a stable timestamp.
func decodeImageResponse(body []byte) (adapters.ImageResponse, error) {
	var out adapters.ImageResponse
	if len(body) == 0 {
		return out, fmt.Errorf("litellm adapter: empty image response (status %d)", http.StatusBadGateway)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("litellm adapter: decode image response: %w", err)
	}
	return out, nil
}
