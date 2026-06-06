package pricing

import (
	"strings"
	"testing"
)

func TestLookupHit(t *testing.T) {
	p, ok := Lookup("openrouter/qwen/qwen-2.5-72b-instruct")
	if !ok {
		t.Fatal("expected qwen 2.5 to be in the catalog")
	}
	if p.Provider != "openrouter" {
		t.Errorf("expected provider openrouter, got %q", p.Provider)
	}
	if p.PromptUSDPer1M <= 0 || p.CompletionUSDPer1M <= 0 {
		t.Errorf("expected positive pricing, got %+v", p)
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	_, ok := Lookup("OPENROUTER/QWEN/QWEN-2.5-72B-INSTRUCT")
	if !ok {
		t.Error("lookup should be case-insensitive")
	}
}

func TestLookupMiss(t *testing.T) {
	p, ok := Lookup("does-not-exist")
	if ok {
		t.Error("expected miss")
	}
	if p.ID != Unknown.ID {
		t.Errorf("expected Unknown sentinel, got %q", p.ID)
	}
}

func TestEstimateCostUSD(t *testing.T) {
	// 1M prompt tokens at qwen rates should equal the prompt rate exactly
	cost, ok := EstimateCostUSD("openrouter/qwen/qwen-2.5-72b-instruct", 1_000_000, 0)
	if !ok {
		t.Fatal("expected hit")
	}
	if cost < 0.34 || cost > 0.36 {
		t.Errorf("expected ~$0.35 for 1M qwen prompt tokens, got $%.4f", cost)
	}
}

func TestEstimateCostUSDForKnownModelIsRealistic(t *testing.T) {
	// 1k prompt + 500 completion via gpt-4o-mini should be well under a penny.
	cost, ok := EstimateCostUSD("openai/gpt-4o-mini", 1_000, 500)
	if !ok {
		t.Fatal("expected hit")
	}
	if cost > 0.001 {
		t.Errorf("expected sub-cent cost, got $%.4f", cost)
	}
}

func TestCatalogHasNoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range Catalog {
		key := strings.ToLower(p.ID)
		if seen[key] {
			t.Errorf("duplicate model id in catalog: %q", p.ID)
		}
		seen[key] = true
	}
}
