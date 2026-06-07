// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
)

// Embedder turns a chunk of text into a fixed-length embedding.
//
// Implementations MUST return a slice of length EmbeddingDim. Returning
// a slice of any other size causes Put / Search to fail with
// ErrEmbeddingDimMismatch.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// DefaultEmbeddingModel is the model id used by LLMGatewayEmbedder when
// the caller doesn't override. text-embedding-3-small returns 1536-dim
// vectors, which matches the schema's vector(1536) column.
const DefaultEmbeddingModel = "openai/text-embedding-3-small"

// LLMGatewayEmbedder wraps an *llmgateway.Gateway and turns text into
// embeddings by issuing a single-input Embeddings call.
//
// The gateway is the suite's policy boundary for every LLM-style call:
// cost tracking, retries, model routing all flow through it. Routing
// memory embeddings through the same code path means we get those
// benefits for free.
type LLMGatewayEmbedder struct {
	gw    *llmgateway.Gateway
	model string
}

// NewLLMGatewayEmbedder returns an Embedder backed by gw. Pass an empty
// model string to use DefaultEmbeddingModel.
func NewLLMGatewayEmbedder(gw *llmgateway.Gateway, model string) *LLMGatewayEmbedder {
	if model == "" {
		model = DefaultEmbeddingModel
	}
	return &LLMGatewayEmbedder{gw: gw, model: model}
}

// Embed issues a single-input embedding request through the gateway.
//
// The OpenAI-compat response shape allows two encodings for the
// embedding payload: a JSON array of numbers (default) or a base64-
// encoded little-endian float32 buffer (encoding_format=base64). We
// handle both because OpenAI silently switches representations on
// large batches and we want the embedder to be tolerant of that.
func (e *LLMGatewayEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if e == nil || e.gw == nil {
		return nil, fmt.Errorf("memory: llm gateway embedder not configured")
	}
	resp, err := e.gw.Embeddings(ctx, llmgateway.EmbeddingsRequest{
		Model: e.model,
		Input: text,
	})
	if err != nil {
		return nil, fmt.Errorf("memory: gateway embed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("memory: gateway returned no embedding data")
	}
	return decodeEmbedding(resp.Data[0].Embedding)
}

// decodeEmbedding converts the heterogeneous OpenAI embedding payload
// into []float32. The upstream returns either []any (of numbers) when
// encoding_format defaults to "float" or a base64 string when
// encoding_format="base64".
func decodeEmbedding(raw any) ([]float32, error) {
	switch v := raw.(type) {
	case []float32:
		return append([]float32(nil), v...), nil
	case []float64:
		out := make([]float32, len(v))
		for i, x := range v {
			out[i] = float32(x)
		}
		return out, nil
	case []any:
		out := make([]float32, 0, len(v))
		for i, item := range v {
			switch n := item.(type) {
			case float64:
				out = append(out, float32(n))
			case float32:
				out = append(out, n)
			case json.Number:
				f, err := n.Float64()
				if err != nil {
					return nil, fmt.Errorf("memory: embedding item %d not numeric: %w", i, err)
				}
				out = append(out, float32(f))
			default:
				return nil, fmt.Errorf("memory: embedding item %d unexpected type %T", i, item)
			}
		}
		return out, nil
	case string:
		// base64 encoded little-endian float32 buffer
		buf, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("memory: decode base64 embedding: %w", err)
		}
		if len(buf)%4 != 0 {
			return nil, fmt.Errorf("memory: base64 embedding length %d not multiple of 4", len(buf))
		}
		out := make([]float32, len(buf)/4)
		for i := range out {
			bits := binary.LittleEndian.Uint32(buf[i*4 : i*4+4])
			out[i] = math.Float32frombits(bits)
		}
		return out, nil
	}
	return nil, fmt.Errorf("memory: unexpected embedding payload type %T", raw)
}