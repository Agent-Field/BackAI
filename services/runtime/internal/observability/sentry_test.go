// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSetupSentryNoopWithoutDSN(t *testing.T) {
	integration, err := SetupSentry(SentryConfig{})
	if err != nil {
		t.Fatalf("SetupSentry: %v", err)
	}
	if integration.Enabled() {
		t.Fatal("empty DSN should not enable Sentry")
	}
	if !integration.Flush() {
		t.Fatal("disabled Sentry flush should succeed")
	}
}

func TestSentryScrubbing(t *testing.T) {
	message := scrubString("Authorization: Bearer secret-token and sk-test1234567890")
	if strings.Contains(message, "secret-token") || strings.Contains(message, "sk-test") {
		t.Fatalf("message not scrubbed: %s", message)
	}
	extra := map[string]any{"api_key": "abc", "msg": "Bearer token"}
	scrubbed := scrubAny(extra).(map[string]any)
	if scrubbed["api_key"] != "[Filtered]" {
		t.Fatalf("api_key not filtered: %+v", scrubbed)
	}
	if strings.Contains(scrubbed["msg"].(string), "token") {
		t.Fatalf("bearer token not scrubbed: %+v", scrubbed)
	}
}

func TestSentryHandlerOnlyEnablesErrors(t *testing.T) {
	handler := &sentrySlogHandler{}
	if handler.Enabled(nil, slog.LevelInfo) {
		t.Fatal("info should not be enabled")
	}
	if !handler.Enabled(nil, slog.LevelError) {
		t.Fatal("error should be enabled")
	}
}
