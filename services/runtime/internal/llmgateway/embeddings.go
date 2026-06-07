// SPDX-License-Identifier: Apache-2.0

package llmgateway

// EmbeddingsRequest is the inbound /embeddings body shape.
//
// Matches OpenAI's `openai.embeddings.create()` request: input is a
// string OR a list of strings OR a list of token id arrays. We accept
// `any` for input and forward verbatim to the upstream which already
// handles the union.
type EmbeddingsRequest struct {
	Model          string `json:"model"`
	Input          any    `json:"input"`
	EncodingFormat string `json:"encoding_format,omitempty"`
	Dimensions     *int   `json:"dimensions,omitempty"`
	User           string `json:"user,omitempty"`
}

// EmbeddingsResponse is the /embeddings response.
type EmbeddingsResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  *Usage          `json:"usage,omitempty"`
}

// EmbeddingItem is one entry in data[].
//
// Embedding is `any` because OpenAI returns []float32 by default but
// switches to a base64 string when encoding_format=base64.
type EmbeddingItem struct {
	Object    string `json:"object"`
	Index     int    `json:"index"`
	Embedding any    `json:"embedding"`
}