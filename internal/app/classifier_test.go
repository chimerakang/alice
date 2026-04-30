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
		{"long prompt + issue ref", "我想規劃一個很長的任務 #142 " + repeat("細節", 100), ComplexityComplex},
		{"long prompt no issue stays moderate", "我跟你討論很久很久" + repeat("這個問題", 100), ComplexityModerate},
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

		// Action verb + issue ref (promoted to complex for Hermes auto-route)
		{"action verb + issue fullwidth", "請處理＃２３９", ComplexityComplex},
		{"action verb + issue halfwidth", "請處理 #239", ComplexityComplex},
		{"fix + issue", "fix #61 the token detector", ComplexityComplex},
		{"implement + issue", "請實作 #102 的功能", ComplexityComplex},
		{"start work on issue", "開始處理 #61 的工作", ComplexityComplex},

		// Issue ref with non-action verb stays moderate (not every issue reference means "do it")
		{"research + issue", "研究一下 #61 的現況", ComplexityModerate},
		{"check + issue", "檢查 #239 是什麼狀況", ComplexityModerate},

		// Status-query suppression now lives at the routing layer (see
		// isHermesIssueStatusQuery in telegram.go); the classifier itself just
		// reports the structural complexity. "處理 #225 完成了嗎" still contains
		// an action verb + issue ref, so it scores complex here — telegram
		// suppresses the auto-route via isHermesIssueStatusQuery.
		{"action+issue: status query reads as complex", "處理 #239 完成了嗎？", ComplexityComplex},

		// 修 / 做 / do alone no longer false-fires on common substrings
		{"not action: 修改", "請修改文字 #61", ComplexityModerate},
		{"not action: 做的", "我做的判斷對嗎 #225", ComplexityModerate},
		{"not action: do you", "do you know about #239", ComplexityModerate},

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
