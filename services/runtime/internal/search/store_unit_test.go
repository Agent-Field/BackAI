// SPDX-License-Identifier: Apache-2.0

package search

import (
	"strings"
	"testing"
)

func TestMergeHybridUsesBothFTSAndVectorRanks(t *testing.T) {
	ftsRank := 0.42
	similarity := 0.91
	ftsHits := []Hit{
		{Document: Document{TenantID: "t1", Namespace: "docs", Key: "a", UpdatedAt: "2026-06-07T12:00:00Z"}, FTSRank: &ftsRank},
		{Document: Document{TenantID: "t1", Namespace: "docs", Key: "b", UpdatedAt: "2026-06-07T11:00:00Z"}, FTSRank: &ftsRank},
	}
	vectorHits := []Hit{
		{Document: Document{TenantID: "t1", Namespace: "docs", Key: "b", UpdatedAt: "2026-06-07T11:00:00Z"}, Similarity: &similarity},
		{Document: Document{TenantID: "t1", Namespace: "docs", Key: "c", UpdatedAt: "2026-06-07T10:00:00Z"}, Similarity: &similarity},
	}

	got := mergeHybrid(ftsHits, vectorHits)
	if len(got) != 3 {
		t.Fatalf("expected 3 merged hits, got %d", len(got))
	}
	if got[0].Document.Key != "b" {
		t.Fatalf("expected hit present in both rankings to win, got %q", got[0].Document.Key)
	}
	if got[0].FTSRank == nil || got[0].Similarity == nil {
		t.Fatalf("expected merged hit to keep both scores: %#v", got[0])
	}
	if got[0].Score <= got[1].Score {
		t.Fatalf("expected combined reciprocal-rank score to outrank one-sided hit")
	}
}

func TestValidateNamespace(t *testing.T) {
	for _, namespace := range []string{"default", "docs.v1", "customer-notes", "crm_2026"} {
		if err := validateNamespace(namespace); err != nil {
			t.Fatalf("expected %q to be valid: %v", namespace, err)
		}
	}
	for _, namespace := range []string{"", strings.Repeat("a", 129), "customer notes", "docs/2026"} {
		if err := validateNamespace(namespace); err == nil {
			t.Fatalf("expected %q to be invalid", namespace)
		}
	}
}

func TestEncodeVector(t *testing.T) {
	got := encodeVector([]float32{0.25, -1, 3.5})
	if got != "[0.25,-1,3.5]" {
		t.Fatalf("unexpected vector encoding: %s", got)
	}
}
