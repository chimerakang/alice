package engine

import (
	"strings"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// Issue #151 Phase 2.5 partial retry: when a task fails review, sub-tasks that
// already scored above the threshold + were marked Done must be preserved into
// the next plan attempt. The user's #305 case had 3-4 Hermes runs costing ~6M
// tokens because the engine wiped accumulated state on every retry. These
// tests pin the building blocks so the partial-retry pipeline can never
// silently regress to that wipe-and-restart shape.
func TestBuildPartialRetryPlan_SeparatesHighScorePreservedFromFailed(t *testing.T) {
	prevPlan := []hermes.SubTask{
		{ID: "s1", Description: "read context", Status: hermes.SubTaskDone, Result: "context: ok"},
		{ID: "s2", Description: "edit code", Status: hermes.SubTaskDone, Result: "edit: applied"},
		{ID: "s3", Description: "run tests", Status: hermes.SubTaskFailed, Result: "tests: 2 fail"},
	}
	prevReview := ReviewResult{
		Verdict:      VerdictFail,
		OverallScore: 50,
		SubTaskResults: []ReviewSubTaskResult{
			{SubTaskID: "s1", Score: 90, Feedback: "good"},
			{SubTaskID: "s2", Score: 85, Feedback: "ok"},
			{SubTaskID: "s3", Score: 30, Feedback: "tests fail; need fix"},
		},
	}

	got := buildPartialRetryPlan(prevReview, prevPlan, 60)

	if len(got.Preserved) != 2 {
		t.Fatalf("preserved count = %d, want 2 (s1+s2)", len(got.Preserved))
	}
	preservedIDs := []string{got.Preserved[0].ID, got.Preserved[1].ID}
	if preservedIDs[0] != "s1" || preservedIDs[1] != "s2" {
		t.Fatalf("preserved IDs = %v, want [s1 s2]", preservedIDs)
	}
	if len(got.Failed) != 1 || got.Failed[0].ID != "s3" {
		t.Fatalf("failed = %#v, want [s3]", got.Failed)
	}
	if !strings.Contains(got.Accumulated, "context: ok") || !strings.Contains(got.Accumulated, "edit: applied") {
		t.Fatalf("accumulated should include preserved results: %q", got.Accumulated)
	}
	if strings.Contains(got.Accumulated, "tests: 2 fail") {
		t.Fatalf("accumulated must NOT include failed sub-task result: %q", got.Accumulated)
	}
}

// When NO sub-task is preserved (all below threshold or all failed), the
// partial-retry pipeline returns an empty struct so the caller falls back to
// the legacy full-replan path.
func TestBuildPartialRetryPlan_AllBelowThresholdReturnsEmpty(t *testing.T) {
	prevPlan := []hermes.SubTask{
		{ID: "s1", Description: "first", Status: hermes.SubTaskDone, Result: "x"},
		{ID: "s2", Description: "second", Status: hermes.SubTaskDone, Result: "y"},
	}
	prevReview := ReviewResult{
		Verdict:      VerdictFail,
		OverallScore: 30,
		SubTaskResults: []ReviewSubTaskResult{
			{SubTaskID: "s1", Score: 40},
			{SubTaskID: "s2", Score: 50},
		},
	}

	got := buildPartialRetryPlan(prevReview, prevPlan, 60)

	if len(got.Preserved) != 0 {
		t.Fatalf("expected zero preserved when all below threshold, got %d", len(got.Preserved))
	}
	if len(got.Failed) != 0 {
		t.Fatalf("expected zero failed when none preserved (early return); got %d", len(got.Failed))
	}
}

// When ALL sub-tasks scored high, partial-retry also returns empty: there's
// nothing to retry, so re-plan is a no-op situation. The caller should stop
// retrying entirely (handled upstream by shouldRetryTask).
func TestBuildPartialRetryPlan_AllAboveThresholdReturnsEmpty(t *testing.T) {
	prevPlan := []hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskDone, Result: "x"},
		{ID: "s2", Status: hermes.SubTaskDone, Result: "y"},
	}
	prevReview := ReviewResult{
		Verdict:      VerdictPass,
		OverallScore: 88,
		SubTaskResults: []ReviewSubTaskResult{
			{SubTaskID: "s1", Score: 95},
			{SubTaskID: "s2", Score: 90},
		},
	}
	got := buildPartialRetryPlan(prevReview, prevPlan, 60)
	if len(got.Failed) != 0 {
		t.Fatalf("expected zero failed when all above threshold, got %d", len(got.Failed))
	}
}

// Sub-tasks marked Done but absent from review.SubTaskResults are NOT
// preserved — without per-sub-task scoring evidence we can't tell whether
// they were actually good. Treat as failed by exclusion. (This is a
// conservative choice: it prefers re-doing work over silently keeping
// possibly-bad results.)
func TestBuildPartialRetryPlan_DonewithoutScoreNotPreserved(t *testing.T) {
	prevPlan := []hermes.SubTask{
		{ID: "s1", Description: "read", Status: hermes.SubTaskDone, Result: "x"},
		{ID: "s2", Description: "edit", Status: hermes.SubTaskDone, Result: "y"},
	}
	prevReview := ReviewResult{
		Verdict:      VerdictFail,
		OverallScore: 50,
		SubTaskResults: []ReviewSubTaskResult{
			{SubTaskID: "s1", Score: 90},
			// s2 omitted — reviewer didn't score it
		},
	}
	got := buildPartialRetryPlan(prevReview, prevPlan, 60)
	// s1 preserved, s2 falls through to Failed → both lists non-empty → returned.
	if len(got.Preserved) != 1 || got.Preserved[0].ID != "s1" {
		t.Fatalf("preserved = %#v, want [s1]", got.Preserved)
	}
	if len(got.Failed) != 1 || got.Failed[0].ID != "s2" {
		t.Fatalf("failed = %#v, want [s2 — unscored treated as failed]", got.Failed)
	}
}

func TestBuildPartialReplanGoal_TellsPlannerNotToRepeatPreserved(t *testing.T) {
	partial := partialRetryPlan{
		Preserved: []hermes.SubTask{
			{ID: "s1", Description: "read context"},
			{ID: "s2", Description: "edit auth.go"},
		},
		Failed: []hermes.SubTask{
			{ID: "s3", Description: "run tests"},
		},
	}
	prevReview := ReviewResult{
		Verdict: VerdictFail, OverallScore: 50,
		Feedback: "tests fail",
	}

	got := buildPartialReplanGoal("implement auth", prevReview, partial)

	for _, want := range []string{
		"PARTIAL RETRY PRESERVED WORK",
		"s1",
		"s2",
		"MUST NOT be repeated",
		"Re-plan ONLY the failed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("partial-retry goal missing %q:\n%s", want, got)
		}
	}
	// Original goal preserved.
	if !strings.Contains(got, "implement auth") {
		t.Fatalf("partial-retry goal lost original: %s", got)
	}
}

func TestMergePartialRetryPlan_PreservedKeepStatusRetainedReplannedGetUniqueIDs(t *testing.T) {
	preserved := []hermes.SubTask{
		{ID: "s1", Description: "read", Status: hermes.SubTaskDone, Result: "ok"},
	}
	replanned := []hermes.SubTask{
		// Same ID as a preserved one — must be renamed to avoid collision.
		{ID: "s1", Description: "duplicate-id sub-task", Status: ""},
		{ID: "s2", Description: "fresh sub-task", Status: ""},
	}

	merged := mergePartialRetryPlan(preserved, replanned, 1)

	if len(merged) != 3 {
		t.Fatalf("merged length = %d, want 3 (1 preserved + 2 replanned)", len(merged))
	}
	if merged[0].ID != "s1" || merged[0].Status != hermes.SubTaskDone {
		t.Fatalf("preserved sub-task lost identity: %#v", merged[0])
	}
	if merged[1].ID == "s1" {
		t.Fatalf("replanned sub-task with duplicate ID was not renamed: %#v", merged[1])
	}
	for _, st := range merged[1:] {
		if st.Status != hermes.SubTaskPending {
			t.Fatalf("replanned sub-task missing pending status: %#v", st)
		}
	}
}

func TestMergePartialRetryPlan_NoPreservedReturnsReplannedAsIs(t *testing.T) {
	replanned := []hermes.SubTask{
		{ID: "s1", Description: "first", Status: hermes.SubTaskPending},
		{ID: "s2", Description: "second", Status: hermes.SubTaskPending},
	}
	merged := mergePartialRetryPlan(nil, replanned, 0)
	if len(merged) != 2 {
		t.Fatalf("merged length = %d, want 2", len(merged))
	}
	if merged[0].ID != "s1" || merged[1].ID != "s2" {
		t.Fatalf("replanned IDs mutated unexpectedly: %#v", merged)
	}
}
