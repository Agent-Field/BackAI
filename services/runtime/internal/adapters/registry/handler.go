// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"encoding/json"
	"net/http"
)

// Handler returns an http.Handler for GET /api/v1/admin/adapters.
// The handler is a thin wrapper around Registry.List + JSON encoding.
func Handler(r *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r == nil {
			http.Error(w, `{"error":"registry not configured"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Bound the probe context to the request lifetime so client
		// cancel propagates to in-flight health checks.
		_ = json.NewEncoder(w).Encode(r.List(req.Context()))
	})
}
