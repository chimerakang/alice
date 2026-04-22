package app

import "testing"

func TestClassifyComplexity(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   Complexity
	}{
		{"empty", "", ComplexityTrivial},
		{"short git commit", "commit & push to github", ComplexityTrivial},
		{"short git tag", "幫忙打上版本號", ComplexityModerate},
		{"short git status", "git status", ComplexityTrivial},
		{"refactor", "請重構這個 i18n 系統", ComplexityComplex},
		{"implement", "implement the new hermes coordinator", ComplexityComplex},
		{"long prompt", "我想做一個很長很長的任務，" + repeat("細節", 100), ComplexityComplex},
		{"moderate default", "how does the telegram handler work", ComplexityModerate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyComplexity(tc.prompt)
			if got.Complexity != tc.want {
				t.Fatalf("prompt=%q got=%s (%s), want=%s", tc.prompt, got.Complexity, got.MatchedRule, tc.want)
			}
		})
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
