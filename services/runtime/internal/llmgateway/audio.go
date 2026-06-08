// SPDX-License-Identifier: Apache-2.0

package llmgateway

// AudioSpeechRequest is the OpenAI-compatible /audio/speech request.
//
// Body is the original JSON payload so new provider-specific fields can
// pass through without the runtime keeping a local schema in lockstep.
type AudioSpeechRequest struct {
	Model string
	Input string
	Body  []byte
}

// AudioTranscriptionRequest is the OpenAI-compatible
// /audio/transcriptions request.
//
// Body is the original multipart/form-data payload, including the file
// part. ContentType must include the multipart boundary.
type AudioTranscriptionRequest struct {
	Model       string
	ContentType string
	Body        []byte
}

// RawResponse is a non-JSON upstream response from the LLM gateway.
type RawResponse struct {
	Body        []byte
	ContentType string
}
