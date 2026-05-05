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

func TestDecideRecoveryRetriesBlockedStrictReview(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "strict_review",
		Attempt:     0,
		MaxAttempts: 2,
		Strict: StrictReviewDecision{
			Verdict:   VerdictBlock,
			Retryable: true,
		},
	})
	if decision.Action != RecoveryActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, RecoveryActionRetry)
	}
	if decision.NextAttempt != 1 {
		t.Fatalf("NextAttempt = %d, want 1", decision.NextAttempt)
	}
	if decision.Reason != "strict_review_blocked" {
		t.Fatalf("Reason = %q, want strict_review_blocked", decision.Reason)
	}
}

func TestDecideRecoveryStopsBlockedStrictReviewAtMax(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "strict_review",
		Attempt:     2,
		MaxAttempts: 2,
		Strict: StrictReviewDecision{
			Verdict:   VerdictBlock,
			Retryable: true,
		},
	})
	if decision.Action != RecoveryActionNone || !decision.Terminal {
		t.Fatalf("decision = %+v, want terminal none", decision)
	}
	if decision.Reason != "max_strict_review_retries_reached" {
		t.Fatalf("Reason = %q, want max_strict_review_retries_reached", decision.Reason)
	}
}

func TestDecideRecoveryAllowsPassingStrictReview(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "strict_review",
		Attempt:     0,
		MaxAttempts: 2,
		Strict: StrictReviewDecision{
			Verdict: VerdictAllow,
		},
	})
	if decision.Action != RecoveryActionNone || decision.Terminal {
		t.Fatalf("decision = %+v, want non-terminal none", decision)
	}
	if decision.Reason != "strict_review_allowed" {
		t.Fatalf("Reason = %q, want strict_review_allowed", decision.Reason)
	}
}

func TestDecideRecoveryCancelsOnWatchdogContextDone(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{Mode: "watchdog_cancel"})
	if decision.Action != RecoveryActionCancel || !decision.Terminal {
		t.Fatalf("decision = %+v, want terminal cancel", decision)
	}
	if decision.Reason != "watchdog_context_done" {
		t.Fatalf("Reason = %q, want watchdog_context_done", decision.Reason)
	}
}

func TestDecideRecoveryRetriesPlannerBelowMax(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "planner_retry",
		Attempt:     1,
		MaxAttempts: 3,
	})
	if decision.Action != RecoveryActionRetry || !decision.Retryable {
		t.Fatalf("decision = %+v, want retry", decision)
	}
	if decision.NextAttempt != 2 {
		t.Fatalf("NextAttempt = %d, want 2", decision.NextAttempt)
	}
	if decision.Reason != "planner_retry" {
		t.Fatalf("Reason = %q, want planner_retry", decision.Reason)
	}
}

func TestDecideRecoveryStopsPlannerAtMax(t *testing.T) {
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "planner_retry",
		Attempt:     3,
		MaxAttempts: 3,
	})
	if decision.Action != RecoveryActionFail || !decision.Terminal {
		t.Fatalf("decision = %+v, want terminal fail", decision)
	}
	if decision.Reason != "max_planner_retries_reached" {
		t.Fatalf("Reason = %q, want max_planner_retries_reached", decision.Reason)
	}
}

func TestRecoveryTraceEvent(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	req := RecoveryRequest{
		Mode:        "direct_stream",
		Attempt:     1,
		MaxAttempts: 2,
		Fallback:    "direct",
	}
	decision := RecoveryDecision{
		Action:      RecoveryActionRetry,
		Reason:      "transient_error",
		RetryAfter:  30 * time.Second,
		Retryable:   true,
		NextAttempt: 2,
	}
	event := RecoveryTraceEvent(req, decision, at)
	if event.Type != "RecoveryDecision" || !event.Timestamp.Equal(at) {
		t.Fatalf("event metadata = %+v", event)
	}
	payload, ok := event.Payload.(RecoveryTracePayload)
	if !ok {
		t.Fatalf("payload type = %T, want RecoveryTracePayload", event.Payload)
	}
	if payload.Mode != "direct_stream" || payload.Action != RecoveryActionRetry || payload.Reason != "transient_error" {
		t.Fatalf("payload core fields = %+v", payload)
	}
	if payload.Attempt != 1 || payload.MaxAttempts != 2 || payload.NextAttempt != 2 {
		t.Fatalf("payload attempts = %+v", payload)
	}
	if payload.RetryAfter != 30*time.Second || !payload.Retryable || payload.Terminal {
		t.Fatalf("payload flags = %+v", payload)
	}
	if payload.Fallback != "direct" {
		t.Fatalf("Fallback = %q, want direct", payload.Fallback)
	}
}
