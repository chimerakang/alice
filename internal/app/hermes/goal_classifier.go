package hermes

import "strings"

// GoalComplexity tells the Coordinator whether to invoke the Planner or to
// skip straight to a single Executor call. The classifier is intentionally
// conservative: the Planner is expensive (Opus call, ~300 tokens, ~10s
// latency) so simple goals should bypass it entirely.
type GoalComplexity int

const (
	// GoalSimple goals fit one Executor turn — short, no implementation
	// imperatives, no GitHub issue inlining. Coordinator skips Plan and
	// synthesises a single "Execute the goal directly" sub-task.
	GoalSimple GoalComplexity = iota
	// GoalNeedsPlanner goals warrant decomposition: long descriptions,
	// implementation verbs, or pre-fetched GitHub issue bodies (which carry
	// checklist items the Planner should map to sub-tasks).
	GoalNeedsPlanner
)

// implementationVerbs are the work signals that justify invoking the Planner.
// Kept narrow so casual queries still bypass it.
var implementationVerbs = []string{
	"重構", "重寫", "重新設計", "整合", "整體", "整個", "所有檔案",
	"實作", "實現", "建立新", "新增模組", "遷移",
	"refactor", "implement", "rewrite", "redesign", "migrate",
	"build a new", "create a new module",
}

// opsVerbs are operational action verbs. When a message contains two or more
// distinct ops verbs it is an implicit sequential workflow (e.g. commit → push
// → deploy) and must go through the Planner to be correctly decomposed.
var opsVerbs = []string{
	"commit", "push", "推版", "部署", "deploy", "ssh", "restart", "重啟",
	"reload", "重載", "merge", "tag", "release", "rollback", "回滾",
}

// ClassifyGoal returns whether a Hermes goal should engage the Planner.
//
// Rules:
//   - Goal already starts with "[GitHub #N] ..." → NeedsPlanner. The auto-route
//     fetcher inlined the issue body precisely so the Planner can decompose
//     against the checklist; bypassing here would defeat that.
//   - Goal length > 200 runes → NeedsPlanner. Long requests usually carry
//     multi-step intent.
//   - Goal contains an implementation verb → NeedsPlanner.
//   - Goal contains ≥ 2 distinct ops verbs → NeedsPlanner. Multiple operational
//     verbs (commit + push + deploy) imply a sequential workflow that the
//     Executor cannot safely collapse into a single call.
//   - Otherwise → Simple. Short status checks, single git commands, factual
//     questions all skip the Planner and run as one Executor call.
func ClassifyGoal(goal string) GoalComplexity {
	trimmed := strings.TrimSpace(goal)
	if strings.HasPrefix(trimmed, "[GitHub #") {
		return GoalNeedsPlanner
	}
	if len([]rune(trimmed)) > 200 {
		return GoalNeedsPlanner
	}
	lower := strings.ToLower(trimmed)
	for _, kw := range implementationVerbs {
		if strings.Contains(lower, kw) {
			return GoalNeedsPlanner
		}
	}
	if countDistinctOpsVerbs(lower) >= 2 {
		return GoalNeedsPlanner
	}
	return GoalSimple
}

// countDistinctOpsVerbs returns the number of distinct ops verbs found in s.
func countDistinctOpsVerbs(s string) int {
	count := 0
	for _, v := range opsVerbs {
		if strings.Contains(s, v) {
			count++
		}
	}
	return count
}
