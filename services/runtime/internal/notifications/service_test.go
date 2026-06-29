// SPDX-License-Identifier: Apache-2.0

package notifications

import "testing"

func TestMergeChannelsDBOverridesEnvByKind(t *testing.T) {
	env := []Channel{{
		ID:      "env-email",
		Kind:    KindEmail,
		Config:  map[string]any{"adapter": "log"},
		Enabled: true,
		Source:  "env",
	}}
	db := []Channel{{
		ID:      "db-email",
		Kind:    KindEmail,
		Config:  map[string]any{"adapter": "resend"},
		Enabled: false,
		Source:  "db",
	}}
	got := mergeChannels(env, db)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].ID != "db-email" || got[0].Source != "db" || got[0].Enabled {
		t.Fatalf("merged=%+v", got[0])
	}
	if got[0].Config["adapter"] != "resend" {
		t.Fatalf("config=%v", got[0].Config)
	}
}
