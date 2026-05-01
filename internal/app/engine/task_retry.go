package engine

import (
	"fmt"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// TaskRetryConfig controls task-level re-plan retry. When a task finishes
// review and the result indicates failure or a low score, the engine
// asks the Planner to re-decompose the goal using the reviewer's
// feedback, then re-executes from scratch.
//
// This is distinct from strict-mode sub-task retry (which keys off
// BlockTags and re-executes the same sub-task with feedback). Task
// retry replaces the entire plan and is intended for cases where the
// decomposition itself was the root cause — e.g. a sub-task that
// bundled too many requirements, or a plan that omitted validation.
type TaskRetryConfig struct {
	// Enabled gates the whole feature; default false.
	Enabled bool `json:"enabled"`

	// ScoreThreshold is the minimum overall_score that avoids retry.
	// Reviews scoring strictly less than this trigger a re-plan.
	// Default 60. Verdict=fail always triggers retry regardless.
	ScoreThreshold int `json:"score_threshold"`

	// MaxTaskRetries caps the number of additional attempts after the
	// first. Default 1. Set 0 to disable while keeping Enabled true
	// for diagnostics (no retries will run but the would-retry path is
	// still evaluated).
	MaxTaskRetries int `json:"max_task_retries"`
}

// WithDefaults returns a copy with sensible default values applied to
// any zero fields. ScoreThreshold defaults to 60, MaxTaskRetries to 1.
func (c TaskRetryConfig) WithDefaults() TaskRetryConfig {
	if c.ScoreThreshold == 0 {
		c.ScoreThreshold = 60
	}
	if c.MaxTaskRetries == 0 {
		c.MaxTaskRetries = 1
	}
	return c
}

// shouldRetryTask reports whether the engine should re-plan and re-run
// the task based on the review result. attempt is the zero-based index
// of the just-completed attempt (0 = first run).
func shouldRetryTask(review ReviewResult, cfg TaskRetryConfig, attempt int) bool {
	if !cfg.Enabled {
		return false
	}
	cfg = cfg.WithDefaults()
	if attempt >= cfg.MaxTaskRetries {
		return false
	}
	// An empty verdict means the reviewer didn't actually run / didn't
	// conclude — the most common cause is plan length < ReviewMinSubTasks,
	// in which case runReview returns ReviewResult{} with no error. That's
	// "review was skipped", not "review said fail". Triggering a re-plan
	// here would wipe all completed sub-tasks based on a non-result; the
	// user reported this as "重審不通過 verdict=, 0/100 — 自動 re-plan
	// 重新執行" with a 1-sub-task plan that didn't need review.
	if review.Verdict == "" {
		return false
	}
	if review.Verdict == VerdictFail {
		return true
	}
	if review.OverallScore < cfg.ScoreThreshold {
		return true
	}
	return false
}

// buildReplanGoal augments the original goal with a structured
// summary of the previous attempt's plan and review feedback so the
// Planner can decompose differently on the second pass. The Planner
// already has its rules in PlannerRules; this just stuffs the prior
// outcome into the user-side message.
func buildReplanGoal(originalGoal string, prevReview ReviewResult, prevPlan []hermes.SubTask) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(originalGoal))
	b.WriteString("\n\n")
	b.WriteString("=== PREVIOUS ATTEMPT REVIEW (re-plan to address) ===\n")
	if prevReview.Verdict != "" {
		fmt.Fprintf(&b, "verdict: %s\n", prevReview.Verdict)
	}
	fmt.Fprintf(&b, "overall_score: %d/100\n", prevReview.OverallScore)
	if len(prevReview.IssueTags) > 0 {
		tags := make([]string, 0, len(prevReview.IssueTags))
		for _, t := range prevReview.IssueTags {
			tags = append(tags, string(t))
		}
		fmt.Fprintf(&b, "issue_tags: %s\n", strings.Join(tags, ", "))
	}
	if fb := strings.TrimSpace(prevReview.Feedback); fb != "" {
		b.WriteString("feedback:\n")
		b.WriteString(fb)
		b.WriteString("\n")
	}
	if len(prevPlan) > 0 {
		b.WriteString("\nprevious plan (rejected):\n")
		for i, st := range prevPlan {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, st.Description)
		}
	}
	b.WriteString("\nAdjust the plan to specifically address the issues above. " +
		"Add explicit sub-tasks for any missed validation, scope coverage, or " +
		"runtime checks. Do not repeat the same decomposition.")
	return b.String()
}
