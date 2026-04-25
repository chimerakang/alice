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

// ClassifyGoal returns whether a Hermes goal should engage the Planner.
//
// Rules:
//   - Goal already starts with "[GitHub #N] ..." → NeedsPlanner. The auto-route
//     fetcher inlined the issue body precisely so the Planner can decompose
//     against the checklist; bypassing here would defeat that.
//   - Goal length > 200 runes → NeedsPlanner. Long requests usually carry
//     multi-step intent.
//   - Goal contains an implementation verb → NeedsPlanner.
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
	return GoalSimple
}
