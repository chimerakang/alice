package engine

import "testing"

// Bug report: when a Hermes plan has fewer than ReviewMinSubTasks (or no
// review phase configured), runReview returns (ReviewResult{}, nil) — empty
// verdict + score=0 — to signal "review skipped". The task-retry decision
// path was then interpreting score 0 < threshold 60 as "fail, re-plan",
// wiping all completed sub-task progress and re-running the whole task
// from scratch.
//
// User-visible symptom in Telegram:
//   "⚠️ 重審不通過（verdict=, 0/100）— 自動 re-plan 重新執行（第 2/2 輪）"
//
// Empty verdict must NOT trigger retry.
func TestShouldRetryTask_EmptyVerdictDoesNotTrigger(t *testing.T) {
	cfg := TaskRetryConfig{Enabled: true, ScoreThreshold: 60, MaxTaskRetries: 1}
	review := ReviewResult{Verdict: "", OverallScore: 0}
	if shouldRetryTask(review, cfg, 0) {
		t.Fatalf("empty verdict / score 0 must NOT trigger retry (it means review was skipped)")
	}
}

func TestShouldRetryTask_FailVerdictTriggers(t *testing.T) {
	cfg := TaskRetryConfig{Enabled: true, ScoreThreshold: 60, MaxTaskRetries: 1}
	review := ReviewResult{Verdict: VerdictFail, OverallScore: 80}
	if !shouldRetryTask(review, cfg, 0) {
		t.Fatalf("verdict=fail must trigger retry regardless of score")
	}
}

func TestShouldRetryTask_LowScorePassTriggers(t *testing.T) {
	cfg := TaskRetryConfig{Enabled: true, ScoreThreshold: 60, MaxTaskRetries: 1}
	review := ReviewResult{Verdict: VerdictPass, OverallScore: 50}
	// Note: 50 < 70 would actually fail Validate(), so this state shouldn't
	// reach shouldRetryTask in practice. But the pure function should still
	// say "yes, retry" — the validate guard upstream is what prevents the
	// inconsistency from being acted on.
	if !shouldRetryTask(review, cfg, 0) {
		t.Fatalf("score below threshold must trigger retry when verdict is non-empty")
	}
}

func TestShouldRetryTask_DisabledNeverTriggers(t *testing.T) {
	cfg := TaskRetryConfig{Enabled: false, ScoreThreshold: 60, MaxTaskRetries: 5}
	review := ReviewResult{Verdict: VerdictFail, OverallScore: 0}
	if shouldRetryTask(review, cfg, 0) {
		t.Fatalf("disabled config must never trigger retry")
	}
}

func TestShouldRetryTask_MaxRetriesReached(t *testing.T) {
	cfg := TaskRetryConfig{Enabled: true, ScoreThreshold: 60, MaxTaskRetries: 1}
	review := ReviewResult{Verdict: VerdictFail, OverallScore: 0}
	if shouldRetryTask(review, cfg, 1) {
		t.Fatalf("attempt at max must not trigger another retry")
	}
}

func TestShouldRetryTask_HighScorePassesQuietly(t *testing.T) {
	cfg := TaskRetryConfig{Enabled: true, ScoreThreshold: 60, MaxTaskRetries: 1}
	review := ReviewResult{Verdict: VerdictPass, OverallScore: 88}
	if shouldRetryTask(review, cfg, 0) {
		t.Fatalf("high-score pass must not trigger retry")
	}
}
