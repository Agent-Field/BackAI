// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SSEWriter writes OpenAI-format streaming chunks to an HTTP response.
//
// Output shape matches what openai-python expects:
//
//	data: {"id":"...","choices":[{"delta":{"content":"hi"},...}]}\n\n
//	data: {"id":"...","choices":[{"delta":{"content":" there"},...}]}\n\n
//	data: [DONE]\n\n
//
// Construct with NewSSEWriter. Call WriteChunk N times then End().
// Errors writing to the underlying connection are returned so the
// handler can log them; once an error is observed no further writes
// are attempted.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	failed  error
}

// NewSSEWriter prepares w for an SSE stream and writes the headers.
// Returns an error if the ResponseWriter doesn't implement Flusher
// (every stdlib + httptest implementation does).
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disables proxy buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteChunk serialises chunk as JSON and emits one `data: ...\n\n`
// SSE event. Returns the first write error encountered.
func (s *SSEWriter) WriteChunk(chunk ChatStreamChunk) error {
	if s == nil {
		return fmt.Errorf("nil SSEWriter")
	}
	if s.failed != nil {
		return s.failed
	}
	payload, err := json.Marshal(chunk)
	if err != nil {
		s.failed = err
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", payload); err != nil {
		s.failed = err
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteRaw emits an already-serialised data payload as one SSE event.
// Used to forward an arbitrary upstream chunk without re-marshalling.
func (s *SSEWriter) WriteRaw(payload []byte) error {
	if s == nil {
		return fmt.Errorf("nil SSEWriter")
	}
	if s.failed != nil {
		return s.failed
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", payload); err != nil {
		s.failed = err
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteError emits an OpenAI-style error event followed by [DONE].
// Used when the upstream fails mid-stream — the caller already sent
// a 200, so the only way to surface the error is in-band.
func (s *SSEWriter) WriteError(code, message string) {
	if s == nil || s.failed != nil {
		return
	}
	body := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"type":    "api_error",
		},
	}
	payload, _ := json.Marshal(body)
	_, _ = fmt.Fprintf(s.w, "data: %s\n\n", payload)
	s.flusher.Flush()
	_ = s.End()
}

// End writes the terminal `data: [DONE]\n\n` event.
func (s *SSEWriter) End() error {
	if s == nil {
		return fmt.Errorf("nil SSEWriter")
	}
	if _, err := io.WriteString(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// Failed reports the first write error encountered, or nil.
func (s *SSEWriter) Failed() error {
	if s == nil {
		return nil
	}
	return s.failed
}