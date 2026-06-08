// SPDX-License-Identifier: Apache-2.0

// multimodal_shim.go — small helpers bridging adapters.* DTOs and the
// llmgateway DTOs so server/llm.go can stay in llmgateway-land without
// importing adapters everywhere.

package llmgateway

import (
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
	"github.com/Agent-Field/backai/services/runtime/internal/pricing"
)

// ImagesResponseFromAdapter converts an adapters.ImageResponse into the
// llmgateway.ImagesResponse wire shape (OpenAI-compatible) the HTTP
// handler returns. Inverse of the unmarshaller in
// adapters/litellm/encoding.go.
func ImagesResponseFromAdapter(in adapters.ImageResponse) ImagesResponse {
	out := ImagesResponse{Created: in.Created}
	if out.Created == 0 {
		out.Created = Now()
	}
	for _, it := range in.Data {
		out.Data = append(out.Data, ImageItem{
			URL:           it.URL,
			B64JSON:       it.B64JSON,
			RevisedPrompt: it.RevisedPrompt,
		})
	}
	return out
}

// EstimateMultimodalCost returns the cost (USD) for a multimodal call.
// Verb is "tts" (units = characters), "stt" (units = seconds), or
// "image" (units = images). Returns (cost, true) on hit, (0, false) on miss.
func EstimateMultimodalCost(modelID, verb string, units float64) (float64, bool) {
	return pricing.EstimateMultimodalCostUSD(modelID, verb, units)
}
