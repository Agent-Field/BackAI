// SPDX-License-Identifier: Apache-2.0

package toolstats

import (
	"context"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

func TestNormalizeEventDefaults(t *testing.T) {
	ctx := tenantctx.WithTenantAndUser(context.Background(),
		"00000000-0000-0000-0000-000000000001", "", "00000000-0000-0000-0000-000000000002")
	ev := normalizeEvent(ctx, Event{ToolName: "search"})
	if ev.TenantID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("tenant=%q", ev.TenantID)
	}
	if ev.AgentID != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("agent=%q", ev.AgentID)
	}
	if ev.Transport != TransportNative || ev.Status != StatusSuccess {
		t.Fatalf("transport/status=%q/%q", ev.Transport, ev.Status)
	}
	if ev.CalledAt.IsZero() {
		t.Fatal("called_at not defaulted")
	}
}

func TestRecordNoPoolDoesNotPanic(t *testing.T) {
	NewRecorder(nil, nil).Record(context.Background(), Event{ToolName: "search"})
}
