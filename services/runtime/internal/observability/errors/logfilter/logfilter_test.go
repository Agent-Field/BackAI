// SPDX-License-Identifier: Apache-2.0

package logfilter

import (
	"context"
	"testing"
	"time"

	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

type stubLogStore struct {
	entries []logs.Entry
}

func (s stubLogStore) Query(context.Context, logs.Filter) (logs.Page, error) {
	return logs.Page{Entries: s.entries}, nil
}
func (s stubLogStore) Tail(context.Context, logs.Filter) (<-chan logs.Entry, error) {
	ch := make(chan logs.Entry)
	close(ch)
	return ch, nil
}
func (s stubLogStore) Capabilities() logs.Capabilities { return logs.Capabilities{} }

func TestLogfilterGroupsByNormalizedMessage(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	store := New(stubLogStore{entries: []logs.Entry{
		{TS: now, Level: "error", Service: "runtime", Msg: "provider request 123 failed for key sk-test"},
		{TS: now.Add(time.Second), Level: "error", Service: "runtime", Msg: "provider request 456 failed for key sk-test"},
		{TS: now, Level: "warn", Service: "runtime", Msg: "not an error"},
	}})
	page, err := store.ListGroups(context.Background(), obserrors.ListFilter{Status: obserrors.StatusOpen})
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(page.Groups) != 1 {
		t.Fatalf("groups=%d want 1: %+v", len(page.Groups), page.Groups)
	}
	if page.Groups[0].Count != 2 {
		t.Fatalf("count=%d want 2", page.Groups[0].Count)
	}
}

func TestLogfilterVolatileStatusOverrides(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	store := New(stubLogStore{entries: []logs.Entry{{TS: now, Level: "error", Service: "runtime", Msg: "boom"}}})
	page, err := store.ListGroups(context.Background(), obserrors.ListFilter{Status: obserrors.StatusOpen})
	if err != nil || len(page.Groups) != 1 {
		t.Fatalf("ListGroups: groups=%+v err=%v", page.Groups, err)
	}
	group, err := store.UpdateGroup(context.Background(), page.Groups[0].ID, obserrors.Update{Status: obserrors.StatusMuted})
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if group.Status != obserrors.StatusMuted {
		t.Fatalf("status=%q want muted", group.Status)
	}
	muted, err := store.ListGroups(context.Background(), obserrors.ListFilter{Status: obserrors.StatusMuted})
	if err != nil {
		t.Fatalf("ListGroups muted: %v", err)
	}
	if len(muted.Groups) != 1 || muted.Groups[0].ID != group.ID {
		t.Fatalf("muted groups=%+v want %s", muted.Groups, group.ID)
	}
	if store.Capabilities().Persistence != obserrors.PersistenceVolatile {
		t.Fatalf("persistence=%q want volatile", store.Capabilities().Persistence)
	}
}

func TestLogfilterGetGroupSearchesBeyondDefaultPage(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	entries := make([]logs.Entry, 0, defaultLimit+10)
	for i := 0; i < defaultLimit+10; i++ {
		suffix := string(rune('a'+i/26)) + string(rune('a'+i%26))
		entries = append(entries, logs.Entry{
			TS:      now.Add(time.Duration(i) * time.Second),
			Level:   "error",
			Service: "runtime",
			Msg:     "unique error shard " + suffix,
		})
	}
	store := New(stubLogStore{entries: entries})
	all, err := store.ListGroups(context.Background(), obserrors.ListFilter{Limit: maxGroupsPerPage})
	if err != nil {
		t.Fatalf("ListGroups all: %v", err)
	}
	if len(all.Groups) <= defaultLimit {
		t.Fatalf("groups=%d want more than default limit %d", len(all.Groups), defaultLimit)
	}
	target := all.Groups[len(all.Groups)-1]
	got, err := store.GetGroup(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("GetGroup beyond first page: %v", err)
	}
	if got.ID != target.ID {
		t.Fatalf("group id=%q want %q", got.ID, target.ID)
	}
}
