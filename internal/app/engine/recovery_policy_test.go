package engine

import (
	"errors"
	"testing"
	"time"
)

func TestDecideRecoveryRetriesTransientErrors(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "direct_stream",
		Attempt:     0,
		MaxAttempts: 2,
		ErrorText:   "rate limit 429",
	})
	if decision.Action != RecoveryActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, RecoveryActionRetry)
	}
	if !decision.Retryable || decision.Terminal {
		t.Fatalf("unexpected flags: %+v", decision)
	}
	if decision.NextAttempt != 1 {
		t.Fatalf("NextAttempt = %d, want 1", decision.NextAttempt)
	}
	if decision.RetryAfter != 15*time.Second {
		t.Fatalf("RetryAfter = %v, want 15s", decision.RetryAfter)
	}
}

func TestDecideRecoveryStopsAtMaxAttempts(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "direct_stream",
		Attempt:     2,
		MaxAttempts: 2,
		ErrorText:   "temporary overload",
	})
	if decision.Action != RecoveryActionFail || !decision.Terminal {
		t.Fatalf("decision = %+v, want terminal fail", decision)
	}
}

func TestDecideRecoveryFailsTerminalErrors(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "direct_stream",
		Attempt:     0,
		MaxAttempts: 2,
		ErrorText:   "permission denied",
	})
	if decision.Action != RecoveryActionFail || decision.Retryable {
		t.Fatalf("decision = %+v, want non-retryable fail", decision)
	}
}

func TestDecideRecoveryFallsBackWhenTargetAvailable(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "plan_execute",
		ErrorText:   "planner failed",
		Fallback:    "direct",
		MaxAttempts: 0,
	})
	if decision.Action != RecoveryActionFallback {
		t.Fatalf("Action = %q, want %q", decision.Action, RecoveryActionFallback)
	}
	if decision.Terminal {
		t.Fatalf("Terminal = true, want false: %+v", decision)
	}
}

func TestIsRetryableRecoveryError(t *testing.T) {
	if !IsRetryableRecoveryError(errors.New("connection reset by peer")) {
		t.Fatal("connection reset should be retryable")
	}
	if IsRetryableRecoveryError(errors.New("syntax error")) {
		t.Fatal("syntax error should not be retryable")
	}
}

func TestDecideRecoveryRetriesFailedTaskReview(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:    "task_review",
		Attempt: 0,
		Review: ReviewResult{
			Verdict:      VerdictFail,
			OverallScore: 90,
		},
		TaskRetry: TaskRetryConfig{Enabled: true, ScoreThreshold: 60, MaxTaskRetries: 1},
	})
	if decision.Action != RecoveryActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, RecoveryActionRetry)
	}
	if decision.Reason != "review_failed" {
		t.Fatalf("Reason = %q, want review_failed", decision.Reason)
	}
}

func TestDecideRecoverySkipsEmptyTaskReviewVerdict(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:      "task_review",
		Attempt:   0,
		Review:    ReviewResult{},
		TaskRetry: TaskRetryConfig{Enabled: true, ScoreThreshold: 60, MaxTaskRetries: 1},
	})
	if decision.Action != RecoveryActionNone {
		t.Fatalf("Action = %q, want %q", decision.Action, RecoveryActionNone)
	}
	if decision.Reason != "review_skipped" {
		t.Fatalf("Reason = %q, want review_skipped", decision.Reason)
	}
}

func TestDecideRecoveryAllowsSubTaskRetryBelowMax(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "sub_task_retry",
		Attempt:     2,
		MaxAttempts: 3,
	})
	if decision.Action != RecoveryActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, RecoveryActionRetry)
	}
	if decision.NextAttempt != 3 {
		t.Fatalf("NextAttempt = %d, want 3", decision.NextAttempt)
	}
	if decision.Reason != "sub_task_retry_allowed" {
		t.Fatalf("Reason = %q, want sub_task_retry_allowed", decision.Reason)
	}
}

func TestDecideRecoveryStopsSubTaskRetryAtMax(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "sub_task_retry",
		Attempt:     3,
		MaxAttempts: 3,
	})
	if decision.Action != RecoveryActionNone || !decision.Terminal {
		t.Fatalf("decision = %+v, want terminal none", decision)
	}
	if decision.Reason != "max_sub_task_retries_reached" {
		t.Fatalf("Reason = %q, want max_sub_task_retries_reached", decision.Reason)
	}
}
