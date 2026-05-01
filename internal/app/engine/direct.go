package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

// DirectRunner is the legacy Agent.Run shape wrapped by DirectEngine.
type DirectRunner interface {
	Run(userMessage string, onUpdate func(string, bool)) (string, error)
}

// DirectEngine delegates execution to the existing direct Agent.Run path.
type DirectEngine struct {
	runner      DirectRunner
	reviewPhase ReviewPhase
}

type DirectOption func(*DirectEngine)

func WithReviewPhase(phase ReviewPhase) DirectOption {
	return func(e *DirectEngine) {
		e.reviewPhase = phase
	}
}

func NewDirectEngine(runner DirectRunner, opts ...DirectOption) *DirectEngine {
	engine := &DirectEngine{runner: runner}
	for _, opt := range opts {
		if opt != nil {
			opt(engine)
		}
	}
	return engine
}

func (e *DirectEngine) Name() string {
	return "direct"
}

// BindSubTask forwards the current sub-task to the underlying runner if it
// implements SubTaskBindable. Used by PlanExecuteEngine to let runners switch
// executor models (e.g. Haiku → Sonnet) per sub-task. Safe no-op when the
// runner does not implement the interface.
func (e *DirectEngine) BindSubTask(subTask hermes.SubTask) {
	if binder, ok := e.runner.(SubTaskBindable); ok {
		binder.BindSubTask(subTask)
	}
}

// SetWalkingEnabled forwards walking-mode toggle to the underlying runner if
// it implements WalkingToggleable. Issue #149.
func (e *DirectEngine) SetWalkingEnabled(enabled bool) {
	if w, ok := e.runner.(WalkingToggleable); ok {
		w.SetWalkingEnabled(enabled)
	}
}

// ForceFreshSession asks the runner to clear the target model session before
// the next Run call. This is used by walking-agent mode when the engine needs a
// cold prompt without disabling walking for the rest of the task.
func (e *DirectEngine) ForceFreshSession() {
	if w, ok := e.runner.(WalkingSessionResettable); ok {
		w.ForceFreshSession()
	}
}

func (e *DirectEngine) Run(ctx context.Context, goal string, cc *ChatContext, prog ProgressSink) (Result, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return Result{Duration: time.Since(start)}, err
	}

	executionGoal, reviewRequested := stripDirectReviewTrigger(goal)
	if prog != nil {
		prog.OnSubTaskStart(1, 1, executionGoal)
	}

	text, err := e.runner.Run(executionGoal, func(update string, silent bool) {
		if prog == nil || update == "" {
			return
		}
		if silent {
			prog.OnToolUse(update, nil)
			return
		}
		prog.OnContent("status", update)
	})

	result := Result{
		Text: text,
	}
	if metrics, ok := e.runner.(DirectRunnerMetrics); ok {
		result.Model, result.InputTokens, result.OutputTokens, result.Cost = metrics.LastCallMetrics()
	}
	if cacheMetrics, ok := e.runner.(DirectRunnerCacheMetrics); ok {
		result.CacheReadInputTokens, result.CacheCreationInputTokens = cacheMetrics.LastCacheMetrics()
	}
	if err != nil {
		result.Duration = time.Since(start)
		return result, err
	}

	if reviewRequested && e.reviewPhase != nil {
		review, reviewErr := e.reviewPhase.Review(ctx, ReviewRequest{
			Goal:        executionGoal,
			Accumulated: text,
			SubTaskResults: []ReviewSubTaskInput{{
				ID:          "direct",
				Index:       0,
				Description: executionGoal,
				Status:      "done",
				Result:      text,
			}},
		})
		if reviewErr == nil {
			if validateErr := review.Validate(); validateErr == nil {
				result.Review = &review
			}
		}
	}

	if prog != nil {
		prog.OnSubTaskDone(1, text)
		prog.OnComplete(text)
	}
	result.Duration = time.Since(start)
	return result, nil
}

func stripDirectReviewTrigger(goal string) (string, bool) {
	trimmed := strings.TrimSpace(goal)
	if trimmed == "" {
		return goal, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return goal, false
	}
	cmd := strings.ToLower(strings.Split(fields[0], "@")[0])
	switch cmd {
	case "/review", "/checkup":
		remainder := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		return remainder, true
	default:
		return goal, false
	}
}

func formatDirectReviewSummary(review ReviewResult) string {
	tags := make([]string, 0, len(review.IssueTags))
	for _, tag := range review.IssueTags {
		tags = append(tags, string(tag))
	}
	summary := fmt.Sprintf("🔎 Review: %s (%d/100)", review.Verdict, review.OverallScore)
	if len(tags) > 0 {
		summary += "\nIssue tags: " + strings.Join(tags, ", ")
	}
	if text := strings.TrimSpace(review.Feedback); text != "" {
		summary += "\n" + text
	}
	return summary
}
