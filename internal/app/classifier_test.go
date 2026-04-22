package app

import "testing"

func TestClassifyComplexity(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   Complexity
	}{
		// existing baseline
		{"empty", "", ComplexityTrivial},
		{"short git commit", "commit & push to github", ComplexityTrivial},
		{"short git status", "git status", ComplexityTrivial},
		{"refactor", "請重構這個 i18n 系統", ComplexityComplex},
		{"implement", "implement the new hermes coordinator", ComplexityComplex},
		{"long prompt", "我想做一個很長很長的任務，" + repeat("細節", 100), ComplexityComplex},
		{"moderate default", "how does the telegram handler work", ComplexityModerate},

		// Chinese work verbs
		{"cn investigate", "請幫我研究一下 bot 的執行錯誤", ComplexityModerate},
		{"cn fix", "請幫我修正 timestamp 顯示問題", ComplexityModerate},
		{"cn check", "幫忙檢查一下 log", ComplexityModerate},
		{"cn adjust", "調整一下 classifier 的關鍵字", ComplexityModerate},
		{"cn refactor beats short", "重構", ComplexityComplex},
		{"cn integration", "整合 Excel 資料進系統", ComplexityComplex},
		{"cn whole codebase", "整個專案檢查一次", ComplexityComplex},

		// Short shell-like Chinese
		{"cn short check", "查一下狀態", ComplexityTrivial},
		{"cn short restart", "重啟 bot", ComplexityTrivial},

		// Issue reference
		{"bare issue halfwidth", "#61", ComplexityModerate},
		{"bare issue fullwidth", "＃６１", ComplexityModerate},
		{"issue + short context", "開始處理 #61 的工作", ComplexityModerate},

		// IDE noise stripping
		{"ide opened only stays short", "<ide_opened_file>/path/to/foo.ts</ide_opened_file>git status", ComplexityTrivial},
		{"system-reminder does not inflate", "<system-reminder>" + repeat("x", 500) + "</system-reminder>git status", ComplexityTrivial},
		{"ide wrapping long real prompt", "<ide_opened_file>/path</ide_opened_file>請重構整個模組", ComplexityComplex},
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

func TestStripIDENoise(t *testing.T) {
	in := "prefix <ide_opened_file>x</ide_opened_file> middle <system-reminder>y</system-reminder> suffix"
	got := StripIDENoise(in)
	if got != "prefix  middle  suffix" {
		t.Fatalf("got=%q", got)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
