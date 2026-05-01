package app

import "testing"

func TestComputeCacheBreakdownSplitsProviderModelAndProject(t *testing.T) {
	metrics := []PerformanceMetrics{
		{
			Model:            "sonnet",
			ProjectPath:      "/repo/a",
			TokensUsed:       1000,
			InputTokens:      100,
			CacheReadTokens:  700,
			CacheWriteTokens: 100,
			OutputTokens:     100,
			EstimatedCost:    0.1,
		},
		{
			Model:           "gpt-5.5",
			ProjectPath:     "/repo/b",
			TokensUsed:      2000,
			InputTokens:     1500,
			CacheReadTokens: 300,
			OutputTokens:    200,
			EstimatedCost:   0.2,
		},
		{
			Model:            "opus",
			ProjectPath:      "/repo/a",
			TokensUsed:       500,
			InputTokens:      50,
			CacheReadTokens:  300,
			CacheWriteTokens: 100,
			OutputTokens:     50,
			EstimatedCost:    0.3,
		},
	}

	breakdown := computeCacheBreakdown(metrics)
	byProvider := breakdown["by_provider"]
	if len(byProvider) != 2 {
		t.Fatalf("provider rows = %d, want 2: %#v", len(byProvider), byProvider)
	}
	if byProvider[0].Group != "codex" || byProvider[0].Tokens != 2000 {
		t.Fatalf("first provider row = %#v, want codex sorted by tokens", byProvider[0])
	}
	if byProvider[1].Group != "claude" || byProvider[1].CacheReadTokens != 1000 {
		t.Fatalf("second provider row = %#v, want claude cache_read=1000", byProvider[1])
	}
	if byProvider[1].CacheReadInputPercent < 74.0 || byProvider[1].CacheReadInputPercent > 74.1 {
		t.Fatalf("claude cache input pct = %.2f, want about 74.07", byProvider[1].CacheReadInputPercent)
	}

	byProject := breakdown["by_project"]
	var repoA cacheBreakdownRow
	for _, row := range byProject {
		if row.Group == "/repo/a" {
			repoA = row
			break
		}
	}
	if repoA.Group != "/repo/a" || repoA.Calls != 2 {
		t.Fatalf("project rows = %#v, want /repo/a with two calls", byProject)
	}
}

func TestCacheProviderForModel(t *testing.T) {
	tests := map[string]string{
		"claude-sonnet-4-6": "claude",
		"opus":              "claude",
		"gpt-5.5":           "codex",
		"gpt-5.4":           "codex",
		"gpt-4o-mini":       "codex",
		"unknown-model":     "unknown",
	}
	for model, want := range tests {
		if got := cacheProviderForModel(model); got != want {
			t.Fatalf("cacheProviderForModel(%q) = %q, want %q", model, got, want)
		}
	}
}
