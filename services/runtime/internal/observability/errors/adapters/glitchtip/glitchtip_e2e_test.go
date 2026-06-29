// SPDX-License-Identifier: Apache-2.0

//go:build e2e_glitchtip

package glitchtip

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
)

func TestGlitchTipE2EListAndMutate(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("BACKAI_E2E_GLITCHTIP_URL"))
	org := strings.TrimSpace(os.Getenv("BACKAI_E2E_GLITCHTIP_ORG"))
	token := strings.TrimSpace(os.Getenv("BACKAI_E2E_GLITCHTIP_TOKEN"))
	if url == "" || org == "" || token == "" {
		t.Skip("set BACKAI_E2E_GLITCHTIP_URL, BACKAI_E2E_GLITCHTIP_ORG, and BACKAI_E2E_GLITCHTIP_TOKEN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := New(ctx, Config{BaseURL: url, Org: org, Token: token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	page, err := store.ListGroups(ctx, obserrors.ListFilter{Status: obserrors.StatusOpen, Limit: 5})
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if store.Capabilities().Persistence != obserrors.PersistenceDurable {
		t.Fatalf("persistence=%q want durable", store.Capabilities().Persistence)
	}
	if len(page.Groups) == 0 {
		t.Skip("GlitchTip returned no open issues; trigger an SDK event before running mutation coverage")
	}
	groupID := page.Groups[0].ID
	group, err := store.GetGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("GetGroup(%s): %v", groupID, err)
	}
	if group.ID != groupID {
		t.Fatalf("group id=%q want %q", group.ID, groupID)
	}
	resolved, err := store.UpdateGroup(ctx, groupID, obserrors.Update{Status: obserrors.StatusResolved})
	if err != nil {
		t.Fatalf("resolve %s: %v", groupID, err)
	}
	if resolved.Status != obserrors.StatusResolved {
		t.Fatalf("resolved status=%q", resolved.Status)
	}
	reopened, err := store.UpdateGroup(ctx, groupID, obserrors.Update{Status: obserrors.StatusOpen})
	if err != nil {
		t.Fatalf("reopen %s: %v", groupID, err)
	}
	if reopened.Status != obserrors.StatusOpen {
		t.Fatalf("reopened status=%q", reopened.Status)
	}
}
