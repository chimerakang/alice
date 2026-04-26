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
		{"git command", "commit and push", GoalNeedsPlanner},
		{"factual question", "目前在哪個分支", GoalSimple},

		{"github issue inlined", "[GitHub #239] Fix login bug\n\nFull issue description: ...", GoalNeedsPlanner},
		{"refactor verb", "請重構 i18n 系統", GoalNeedsPlanner},
		{"english implement", "implement the new auth middleware", GoalNeedsPlanner},
		{"long goal", repeatHermesGoal("細節", 110), GoalNeedsPlanner},

		// Multi-ops-verb cases — must route to Planner (#117)
		{"commit then push deploy zh", "請 commit 然後推版部署到 vps", GoalNeedsPlanner},
		{"commit push en", "commit and push the changes", GoalNeedsPlanner},
		{"commit push deploy en", "commit, push and deploy to production", GoalNeedsPlanner},
		{"push restart", "push to main then restart the service", GoalNeedsPlanner},
		{"ssh deploy", "ssh into the server and deploy", GoalNeedsPlanner},
		{"single commit only", "commit the changes", GoalSimple},
		{"single push only", "git push", GoalSimple},
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
