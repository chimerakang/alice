package hermes

import "testing"

func TestClassifyGoal(t *testing.T) {
	cases := []struct {
		name string
		goal string
		want GoalComplexity
	}{
		{"empty", "", GoalSimple},
		{"short status", "請問 #225 進度如何", GoalSimple},
		{"git command", "commit and push", GoalSimple},
		{"factual question", "目前在哪個分支", GoalSimple},

		{"github issue inlined", "[GitHub #239] Fix login bug\n\nFull issue description: ...", GoalNeedsPlanner},
		{"refactor verb", "請重構 i18n 系統", GoalNeedsPlanner},
		{"english implement", "implement the new auth middleware", GoalNeedsPlanner},
		{"long goal", repeatHermesGoal("細節", 110), GoalNeedsPlanner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyGoal(tc.goal)
			if got != tc.want {
				t.Fatalf("goal=%q got=%v want=%v", tc.goal, got, tc.want)
			}
		})
	}
}

func repeatHermesGoal(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
