package app

import (
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func TestResolveTargetCandidatesPrefersSameTopicInterruptedHermesTask(t *testing.T) {
	now := time.Now()
	candidates := resolveTargetCandidates(ResolveTargetRequest{
		ChatID:     42,
		ThreadID:   7,
		ProjectDir: "/repo",
		Intent:     "hermes_continue",
		Kinds:      []TargetKind{TargetHermesTask},
		Limit:      3,
	}, targetResolverSources{
		Now: now,
		Tasks: []hermes.TaskState{
			{ID: "other-topic", ChatID: 42, ThreadID: 9, ProjectDir: "/repo", Status: hermes.TaskStatusExecuting, UpdatedAt: now.Add(-time.Minute)},
			{ID: "same-topic", ChatID: 42, ThreadID: 7, ProjectDir: "/repo", Status: hermes.TaskStatusInterrupted, UpdatedAt: now.Add(-5 * time.Minute), Accumulated: "done part"},
			{ID: "done-old", ChatID: 42, ThreadID: 7, ProjectDir: "/repo", Status: hermes.TaskStatusDone, UpdatedAt: now.Add(-48 * time.Hour), Accumulated: "done"},
		},
	})
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d, want 1: %#v", len(candidates), candidates)
	}
	if candidates[0].ID != "same-topic" || candidates[0].Kind != TargetHermesTask {
		t.Fatalf("top candidate = %#v, want same-topic hermes task", candidates[0])
	}
	if candidates[0].Score < 0.75 {
		t.Fatalf("score = %.2f, want high confidence", candidates[0].Score)
	}
}

func TestResolveTargetCandidatesReviewRankingUsesRecencyAndFailures(t *testing.T) {
	now := time.Now()
	candidates := resolveTargetCandidates(ResolveTargetRequest{
		ChatID:   42,
		ThreadID: 7,
		Intent:   "retry_all_failed",
		Kinds:    []TargetKind{TargetReviewResult},
		Limit:    2,
	}, targetResolverSources{
		Now: now,
		RetryCandidates: []retryTaskCandidate{
			{ID: "older", Goal: "older retry", FailedCount: 3, LatestReviewAt: now.Add(-48 * time.Hour)},
			{ID: "latest", Goal: "latest retry", FailedCount: 1, LatestReviewAt: now.Add(-10 * time.Minute)},
		},
	})
	if len(candidates) != 2 {
		t.Fatalf("candidates len = %d, want 2", len(candidates))
	}
	if candidates[0].ID != "latest" {
		t.Fatalf("top candidate = %#v, want latest", candidates[0])
	}
	if candidates[0].Metadata["failed_count"] != "1" {
		t.Fatalf("failed_count metadata = %q", candidates[0].Metadata["failed_count"])
	}
}

func TestResolveTargetCandidatesFuzzyGitHubIssue(t *testing.T) {
	candidates := resolveTargetCandidates(ResolveTargetRequest{
		Query: "處理登入那個 issue",
		Kinds: []TargetKind{TargetGitHubIssue},
		Limit: 3,
	}, targetResolverSources{
		Issues: []tasksGitHubIssue{
			{Number: 139, Title: "Hermes should resume interrupted task", Labels: []string{"hermes"}},
			{Number: 141, Title: "Add Smart Target Resolver for task and issue selection", Labels: []string{"telegram"}},
			{Number: 150, Title: "修復登入流程錯誤", Labels: []string{"bug", "auth"}},
		},
	})
	if len(candidates) == 0 {
		t.Fatal("expected fuzzy issue candidates")
	}
	if candidates[0].ID != "150" || candidates[0].Kind != TargetGitHubIssue {
		t.Fatalf("top candidate = %#v, want issue #150", candidates[0])
	}
}

func TestResolveTargetCandidatesTaskIDPrefixHighConfidence(t *testing.T) {
	now := time.Now()
	candidates := resolveTargetCandidates(ResolveTargetRequest{
		Query:      "6b1960",
		ChatID:     42,
		ThreadID:   7,
		ProjectDir: "/repo",
		Kinds:      []TargetKind{TargetHermesTask},
		Limit:      1,
	}, targetResolverSources{
		Now: now,
		Tasks: []hermes.TaskState{{
			ID:         "6b1960ba-1111",
			ChatID:     42,
			ThreadID:   7,
			ProjectDir: "/repo",
			Status:     hermes.TaskStatusInterrupted,
			UpdatedAt:  now,
		}},
	})
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d, want 1", len(candidates))
	}
	if candidates[0].Score < 0.95 {
		t.Fatalf("score = %.2f, want id-prefix high confidence", candidates[0].Score)
	}
}

func TestParseHermesCallbackDataIssueCandidate(t *testing.T) {
	mode, issueID, ok := parseHermesCallbackData("hermes:issue:141:codex")
	if !ok || mode != "issue:codex" || issueID != "141" {
		t.Fatalf("parseHermesCallbackData issue = (%q, %q, %v), want issue:codex 141 true", mode, issueID, ok)
	}
}
