package engine

import (
	"strings"
	"time"
)

type RecoveryAction string

const (
	RecoveryActionNone     RecoveryAction = "none"
	RecoveryActionRetry    RecoveryAction = "retry"
	RecoveryActionFallback RecoveryAction = "fallback"
	RecoveryActionFail     RecoveryAction = "fail"
)

type RecoveryRequest struct {
	Mode        string
	Attempt     int
	MaxAttempts int
	ErrorText   string
	Fallback    string
	Review      ReviewResult
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

func DecideRecovery(req RecoveryRequest) RecoveryDecision {
	if strings.TrimSpace(req.ErrorText) == "" {
		if req.Mode == "task_review" {
			return decideTaskReviewRecovery(req)
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

func IsRetryableRecoveryError(err error) bool {
	if err == nil {
		return false
	}
	return isTransientRecoveryError(err.Error())
}

func isTransientRecoveryError(text string) bool {
	s := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(s, "rate limit") ||
		strings.Contains(s, "429") ||
		strings.Contains(s, "overloaded") ||
		strings.Contains(s, "529") ||
		strings.Contains(s, "temporary") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "eof")
}
