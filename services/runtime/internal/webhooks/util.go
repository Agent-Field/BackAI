// SPDX-License-Identifier: Apache-2.0

// util.go — small helpers shared by inbound + outbound.
//
// Kept in its own file so Phase 10.2 (inbound) and Phase 10.3 (outbound)
// don't fight over the same file when they both need the same helpers.
package webhooks

import (
	"encoding/json"
	"unicode/utf8"
)

// marshalHeaders renders a string map as a JSON object suitable for
// the suite_webhook_deliveries.headers jsonb column.
//
// nil / empty map encodes as `{}` so the column never holds NULL when
// the row was written by a caller that just didn't supply headers.
func marshalHeaders(h map[string]string) ([]byte, error) {
	if h == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(h)
}

// unmarshalHeaders is the inverse. A null / empty payload returns a
// nil map rather than an empty one so callers can distinguish "no
// custom headers" from "the request had an explicit empty map".
func unmarshalHeaders(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if string(raw) == "null" || string(raw) == "{}" {
		return nil, nil
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// truncatePreview returns the first `maxBytes` of body as a UTF-8 safe
// string for the dashboard's preview column. Always returns a valid
// UTF-8 string (replacement char for invalid sequences).
func truncatePreview(body []byte, maxBytes int) string {
	if len(body) == 0 {
		return ""
	}
	if maxBytes <= 0 {
		return ""
	}
	if len(body) <= maxBytes {
		if utf8.Valid(body) {
			return string(body)
		}
		return string([]rune(string(body)))
	}
	// Walk back to a rune boundary so we don't slice a multi-byte
	// codepoint in half.
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	prefix := body[:cut]
	if utf8.Valid(prefix) {
		return string(prefix) + "…"
	}
	return string([]rune(string(prefix))) + "…"
}