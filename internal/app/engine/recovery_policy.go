package engine

import (
	"log"
	"strings"
	"time"

	"claude-tg-agent/internal/app/engine/errorclass"
)

type RecoveryAction string

const (
	RecoveryActionNone     RecoveryAction = "none"
	RecoveryActionRetry    RecoveryAction = "retry"
	RecoveryActionFallback RecoveryAction = "fallback"
	RecoveryActionCancel   RecoveryAction = "cancel"
	RecoveryActionFail     RecoveryAction = "fail"
)

type RecoveryRequest struct {
	Mode        string
	Attempt     int
	MaxAttempts int
	ErrorText   string
	Fallback    string
	Review      ReviewResult
	Strict      StrictReviewDecision
	TaskRetry   TaskRetryConfig
}

type RecoveryDecision struct {
	Action      RecoveryAction
	RetryAfter  time.Duration
	Retryable   bool
	Terminal    bool
	Reason      string
	NextAttempt int
}

type RecoveryTracePayload struct {
	Mode        string         `json:"mode"`
	Action      RecoveryAction `json:"action"`
	Reason      string         `json:"reason"`
	Attempt     int            `json:"attempt"`
	MaxAttempts int            `json:"max_attempts"`
	NextAttempt int            `json:"next_attempt,omitempty"`
	RetryAfter  time.Duration  `json:"retry_after,omitempty"`
	Retryable   bool           `json:"retryable"`
	Terminal    bool           `json:"terminal"`
	Fallback    string         `json:"fallback,omitempty"`
}

func DecideRecovery(req RecoveryRequest) RecoveryDecision {
	if strings.TrimSpace(req.ErrorText) == "" {
		switch req.Mode {
		case "task_review":
			return decideTaskReviewRecovery(req)
		case "sub_task_retry":
			return decideSubTaskRetryRecovery(req)
		case "strict_review":
			return decideStrictReviewRecovery(req)
		case "watchdog_cancel":
			return decideWatchdogCancelRecovery(req)
		case "planner_retry":
			return decidePlannerRetryRecovery(req)
		}
		return RecoveryDecision{Action: RecoveryActionNone, Reason: "no_error"}
	}
	if req.MaxAttempts < 0 {
		req.MaxAttempts = 0
	}
	if isTransientRecoveryError(req.ErrorText) && req.Attempt < req.MaxAttempts {
		nextAttempt := req.Attempt + 1
		return RecoveryDecision{
			Action:      RecoveryActionRetry,
			RetryAfter:  time.Duration(nextAttempt*15) * time.Second,
			Retryable:   true,
			Reason:      "transient_error",
			NextAttempt: nextAttempt,
		}
	}
	if strings.TrimSpace(req.Fallback) != "" {
		return RecoveryDecision{
			Action:   RecoveryActionFallback,
			Terminal: false,
			Reason:   "fallback_available",
		}
	}
	return RecoveryDecision{
		Action:   RecoveryActionFail,
		Terminal: true,
		Reason:   "terminal_error",
	}
}

func RecoveryTraceEvent(req RecoveryRequest, decision RecoveryDecision, at time.Time) Event {
	if at.IsZero() {
		at = time.Now()
	}
	return Event{
		Type:      "RecoveryDecision",
		Timestamp: at,
		Payload: RecoveryTracePayload{
			Mode:        strings.TrimSpace(req.Mode),
			Action:      decision.Action,
			Reason:      decision.Reason,
			Attempt:     req.Attempt,
			MaxAttempts: req.MaxAttempts,
			NextAttempt: decision.NextAttempt,
			RetryAfter:  decision.RetryAfter,
			Retryable:   decision.Retryable,
			Terminal:    decision.Terminal,
			Fallback:    strings.TrimSpace(req.Fallback),
		},
	}
}

func LogRecoveryDecision(req RecoveryRequest, decision RecoveryDecision) {
	event := RecoveryTraceEvent(req, decision, time.Now())
	payload, _ := event.Payload.(RecoveryTracePayload)
	log.Printf(
		"[recovery] event=%s mode=%s action=%s reason=%s attempt=%d max_attempts=%d next_attempt=%d retry_after=%s retryable=%t terminal=%t fallback=%q",
		event.Type,
		payload.Mode,
		payload.Action,
		payload.Reason,
		payload.Attempt,
		payload.MaxAttempts,
		payload.NextAttempt,
		payload.RetryAfter,
		payload.Retryable,
		payload.Terminal,
		payload.Fallback,
	)
}

func decideTaskReviewRecovery(req RecoveryRequest) RecoveryDecision {
	if !req.TaskRetry.Enabled {
		return RecoveryDecision{Action: RecoveryActionNone, Reason: "task_retry_disabled"}
	}
	cfg := req.TaskRetry.WithDefaults()
	if req.Attempt >= cfg.MaxTaskRetries {
		return RecoveryDecision{Action: RecoveryActionNone, Reason: "max_task_retries_reached"}
	}
	if req.Review.Verdict == "" {
		return RecoveryDecision{Action: RecoveryActionNone, Reason: "review_skipped"}
	}
	if req.Review.Verdict == VerdictFail {
		return RecoveryDecision{
			Action:      RecoveryActionRetry,
			Retryable:   true,
			Reason:      "review_failed",
			NextAttempt: req.Attempt + 1,
		}
	}
	if req.Review.OverallScore < cfg.ScoreThreshold {
		return RecoveryDecision{
			Action:      RecoveryActionRetry,
			Retryable:   true,
			Reason:      "review_score_below_threshold",
			NextAttempt: req.Attempt + 1,
		}
	}
	return RecoveryDecision{Action: RecoveryActionNone, Reason: "review_passed"}
}

func decideSubTaskRetryRecovery(req RecoveryRequest) RecoveryDecision {
	if req.MaxAttempts <= 0 {
		return RecoveryDecision{
			Action:   RecoveryActionNone,
			Terminal: true,
			Reason:   "sub_task_retry_disabled",
		}
	}
	if req.Attempt >= req.MaxAttempts {
		return RecoveryDecision{
			Action:   RecoveryActionNone,
			Terminal: true,
			Reason:   "max_sub_task_retries_reached",
		}
	}
	return RecoveryDecision{
		Action:      RecoveryActionRetry,
		Retryable:   true,
		Reason:      "sub_task_retry_allowed",
		NextAttempt: req.Attempt + 1,
	}
}

func decideStrictReviewRecovery(req RecoveryRequest) RecoveryDecision {
	if req.Strict.Verdict != VerdictBlock {
		return RecoveryDecision{Action: RecoveryActionNone, Reason: "strict_review_allowed"}
	}
	if req.Attempt < req.MaxAttempts {
		return RecoveryDecision{
			Action:      RecoveryActionRetry,
			Retryable:   true,
			Reason:      "strict_review_blocked",
			NextAttempt: req.Attempt + 1,
		}
	}
	return RecoveryDecision{
		Action:   RecoveryActionNone,
		Terminal: true,
		Reason:   "max_strict_review_retries_reached",
	}
}

func decideWatchdogCancelRecovery(req RecoveryRequest) RecoveryDecision {
	return RecoveryDecision{
		Action:   RecoveryActionCancel,
		Terminal: true,
		Reason:   "watchdog_context_done",
	}
}

func decidePlannerRetryRecovery(req RecoveryRequest) RecoveryDecision {
	if req.MaxAttempts <= 0 {
		return RecoveryDecision{
			Action:   RecoveryActionFail,
			Terminal: true,
			Reason:   "planner_retry_disabled",
		}
	}
	if req.Attempt < req.MaxAttempts {
		return RecoveryDecision{
			Action:      RecoveryActionRetry,
			Retryable:   true,
			Reason:      "planner_retry",
			NextAttempt: req.Attempt + 1,
		}
	}
	return RecoveryDecision{
		Action:   RecoveryActionFail,
		Terminal: true,
		Reason:   "max_planner_retries_reached",
	}
}

func IsRetryableRecoveryError(err error) bool {
	if err == nil {
		return false
	}
	return isTransientRecoveryError(err.Error())
}

// isTransientRecoveryError keeps the original boolean signature but delegates
// classification to the canonical errorclass package. Pattern coverage is a
// strict superset of the previous local list (verified by
// errorclass.TestRegression_IsTransientRecoveryErrorPatterns).
func isTransientRecoveryError(text string) bool {
	return errorclass.ClassifyText(text).IsTransient()
}
