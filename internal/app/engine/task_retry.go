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
	decision := DecideRecovery(RecoveryRequest{
		Mode:      "task_review",
		Attempt:   attempt,
		Review:    review,
		TaskRetry: cfg,
	})
	return decision.Action == RecoveryActionRetry
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

type partialRetryPlan struct {
	Preserved   []hermes.SubTask
	Failed      []hermes.SubTask
	Accumulated string
}

func buildPartialRetryPlan(prevReview ReviewResult, prevPlan []hermes.SubTask, scoreThreshold int) partialRetryPlan {
	if scoreThreshold <= 0 {
		scoreThreshold = 60
	}
	if len(prevReview.SubTaskResults) == 0 || len(prevPlan) == 0 {
		return partialRetryPlan{}
	}
	scores := make(map[string]ReviewSubTaskResult, len(prevReview.SubTaskResults))
	for _, result := range prevReview.SubTaskResults {
		id := strings.TrimSpace(result.SubTaskID)
		if id != "" {
			scores[id] = result
		}
	}
	if len(scores) == 0 {
		return partialRetryPlan{}
	}

	var out partialRetryPlan
	completed := 0
	for _, st := range prevPlan {
		result, ok := scores[st.ID]
		if ok && result.Score >= scoreThreshold && st.Status == hermes.SubTaskDone {
			out.Preserved = append(out.Preserved, st)
			completed++
			out.Accumulated, _ = hermes.AppendResult(out.Accumulated, st.Result, completed)
			continue
		}
		out.Failed = append(out.Failed, st)
	}
	if len(out.Preserved) == 0 || len(out.Failed) == 0 {
		return partialRetryPlan{}
	}
	return out
}

func buildPartialReplanGoal(originalGoal string, prevReview ReviewResult, partial partialRetryPlan) string {
	base := buildReplanGoal(originalGoal, prevReview, partial.Failed)
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n=== PARTIAL RETRY PRESERVED WORK ===\n")
	b.WriteString("The following previous sub-tasks were reviewed as good enough and MUST NOT be repeated:\n")
	for i, st := range partial.Preserved {
		fmt.Fprintf(&b, "  %d. %s — %s\n", i+1, st.ID, st.Description)
	}
	b.WriteString("\nRe-plan ONLY the failed or low-score scope listed in the rejected plan above. Do not include preserved sub-tasks in the new plan.")
	return b.String()
}

func mergePartialRetryPlan(preserved, replanned []hermes.SubTask, attempt int) []hermes.SubTask {
	if len(preserved) == 0 {
		return replanned
	}
	merged := make([]hermes.SubTask, 0, len(preserved)+len(replanned))
	seen := make(map[string]bool, len(preserved)+len(replanned))
	for _, st := range preserved {
		merged = append(merged, st)
		seen[st.ID] = true
	}
	for i, st := range replanned {
		if strings.TrimSpace(st.ID) == "" || seen[st.ID] {
			st.ID = fmt.Sprintf("retry%d-s%d", attempt, i+1)
		}
		seen[st.ID] = true
		if st.Status == "" {
			st.Status = hermes.SubTaskPending
		}
		merged = append(merged, st)
	}
	return merged
}
