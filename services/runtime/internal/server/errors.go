// SPDX-License-Identifier: Apache-2.0

// errors.go — canonical error envelope helpers shared across handlers.
//
// Every non-2xx response from the AF Stack runtime MUST use this shape:
//
//	{
//	  "error": {
//	    "code":     "<MACHINE_READABLE_CODE>",
//	    "message":  "<human-readable summary>",
//	    "details":  { ... }   // optional, free-form per-code
//	  }
//	}
//
// The dashboard, SDKs, and external API consumers all branch on
// `error.code` for control flow, so codes are versioned: once a code
// ships, it stays. New codes are additive.
//
// Use writeError() for any new error path. errEnvelope() is kept as a
// thin alias for legacy call sites that build the body manually before
// passing it to writeJSON().
package server

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// errEnvelope returns the standard AF Stack error envelope without
// details. Kept for backwards compatibility with handlers that build a
// body before calling writeJSON.
func errEnvelope(code, message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
}

// errEnvelopeWith returns the standard error envelope with a structured
// detail payload. The detail can be any JSON-serialisable value
// (map[string]any is most common; the dashboard expects an object).
func errEnvelopeWith(code, message string, details any) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"details": details,
		},
	}
}

// validUUIDParam validates that a path/query id is a well-formed UUID
// before it reaches Postgres. A non-UUID string bound to a uuid column
// returns SQLSTATE 22P02, which would otherwise surface as a 500 leaking
// the raw driver error. On failure it writes a 400 VALIDATION_FAILED and
// returns ok=false; callers must return immediately when ok is false.
func validUUIDParam(w http.ResponseWriter, raw string) (string, bool) {
	id := strings.TrimSpace(raw)
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"id must be a valid UUID", nil)
		return "", false
	}
	return id, true
}

// writeError is the canonical helper for any error response.
//
// Always sets Content-Type: application/json and writes the envelope.
// Pass details as nil to omit the field; the dashboard treats the
// absence and explicit JSON null identically, so prefer nil for
// brevity.
//
// Handlers that need to set extra response headers (e.g. Retry-After
// on a 429) should set them BEFORE calling writeError — once
// WriteHeader is called from inside writeJSON, header mutations are
// ignored.
func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	body := errEnvelope(code, message)
	if details != nil {
		body = errEnvelopeWith(code, message, details)
	}
	// Stamp the per-request correlation id (same value as the
	// X-Request-ID response header) into error.request_id so every
	// failure is traceable, per the AGENTS.md contract. The id rides on
	// the statusWriter installed by withLogging; if a handler is invoked
	// outside that middleware (e.g. a bare unit test) the field is simply
	// omitted.
	if sw, ok := w.(*statusWriter); ok && sw.requestID != "" {
		if e, ok := body["error"].(map[string]any); ok {
			e["request_id"] = sw.requestID
		}
	}
	writeJSON(w, status, body)
}
