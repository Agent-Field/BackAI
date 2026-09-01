// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"net/http"
)

// Responder lets the server inject its canonical JSON + error-envelope
// writers (which stamp the per-request request_id and Content-Type) so a
// workload module's auto-generated CRUD responses are indistinguishable
// from every other runtime endpoint. When a field is nil the package
// default is used, which keeps the handlers self-contained for unit tests
// that exercise them without the server middleware.
type Responder struct {
	JSON  func(w http.ResponseWriter, status int, v any)
	Error func(w http.ResponseWriter, status int, code, message string, details any)
}

func (r Responder) writeJSON(w http.ResponseWriter, status int, v any) {
	if r.JSON != nil {
		r.JSON(w, status, v)
		return
	}
	defaultWriteJSON(w, status, v)
}

func (r Responder) writeError(w http.ResponseWriter, status int, code, message string, details any) {
	if r.Error != nil {
		r.Error(w, status, code, message, details)
		return
	}
	body := map[string]any{"error": map[string]any{"code": code, "message": message}}
	if details != nil {
		body["error"].(map[string]any)["details"] = details
	}
	defaultWriteJSON(w, status, body)
}

func defaultWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
