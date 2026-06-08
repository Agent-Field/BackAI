// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/url"
	"testing"
)

func TestRealtimePayloadMatches(t *testing.T) {
	raw := []byte(`{"table":"public.messages","tenant_id":"t1","record":{"id":"m1","room_id":"r1","count":2}}`)

	if !realtimePayloadMatches(raw, "public.messages", "t1", true, map[string]any{"room_id": "r1"}) {
		t.Fatal("expected tenant and filter match")
	}
	if realtimePayloadMatches(raw, "public.messages", "t2", true, map[string]any{"room_id": "r1"}) {
		t.Fatal("cross-tenant payload matched")
	}
	if realtimePayloadMatches(raw, "public.messages", "t1", true, map[string]any{"room_id": "r2"}) {
		t.Fatal("wrong filter matched")
	}
	if realtimePayloadMatches(raw, "public.other", "t1", true, nil) {
		t.Fatal("wrong table matched")
	}
	if !realtimePayloadMatches(raw, "public.messages", "t1", true, map[string]any{"count": float64(2)}) {
		t.Fatal("expected numeric JSON equality")
	}
}

func TestRealtimePayloadRequiresTenantWhenMTEnabled(t *testing.T) {
	raw := []byte(`{"table":"events","record":{"id":"e1"}}`)
	if realtimePayloadMatches(raw, "events", "t1", true, nil) {
		t.Fatal("missing tenant_id should not match when multi-tenancy is enabled")
	}
	if !realtimePayloadMatches(raw, "events", "t1", false, nil) {
		t.Fatal("missing tenant_id should match when multi-tenancy is disabled")
	}
}

func TestParseRealtimeFilter(t *testing.T) {
	encoded := url.QueryEscape(`{"room_id":"r1","count":2}`)
	filter, err := parseRealtimeFilter(mustUnescape(t, encoded))
	if err != nil {
		t.Fatalf("parseRealtimeFilter: %v", err)
	}
	if filter["room_id"] != "r1" {
		t.Fatalf("room_id = %v", filter["room_id"])
	}
	if _, err := parseRealtimeFilter(`[]`); err == nil {
		t.Fatal("expected non-object filter to fail")
	}
}

func TestValidRealtimeTable(t *testing.T) {
	for _, table := range []string{"messages", "public.messages", "_events"} {
		if !validRealtimeTable(table) {
			t.Fatalf("expected valid table %q", table)
		}
	}
	for _, table := range []string{"", "public.messages;drop", "bad-name", "1bad"} {
		if validRealtimeTable(table) {
			t.Fatalf("expected invalid table %q", table)
		}
	}
}

func mustUnescape(t *testing.T, raw string) string {
	t.Helper()
	out, err := url.QueryUnescape(raw)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
