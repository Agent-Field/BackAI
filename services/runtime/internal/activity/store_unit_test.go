// SPDX-License-Identifier: Apache-2.0

package activity

import "testing"

func TestResolveActorType(t *testing.T) {
	cases := []struct {
		name     string
		in       ActorType
		userID   string
		apiKeyID string
		want     ActorType
	}{
		{name: "explicit anonymous", in: ActorAnonymous, want: ActorAnonymous},
		{name: "user wins default", userID: "u1", apiKeyID: "k1", want: ActorUser},
		{name: "api key default", apiKeyID: "k1", want: ActorAPIKey},
		{name: "system fallback", want: ActorSystem},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveActorType(tc.in, tc.userID, tc.apiKeyID)
			if err != nil {
				t.Fatalf("resolveActorType error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := resolveActorType("bot", "", ""); err == nil {
		t.Fatal("expected invalid actor type to fail")
	}
}

func TestValidateAction(t *testing.T) {
	if err := validateAction("document.uploaded"); err != nil {
		t.Fatalf("valid action failed: %v", err)
	}
	if err := validateAction(""); err == nil {
		t.Fatal("empty action should fail")
	}
}

func TestClampPaging(t *testing.T) {
	if got := clampLimit(0); got != 50 {
		t.Fatalf("default limit = %d, want 50", got)
	}
	if got := clampLimit(500); got != 200 {
		t.Fatalf("max limit = %d, want 200", got)
	}
	if got := clampOffset(-1); got != 0 {
		t.Fatalf("negative offset = %d, want 0", got)
	}
}
