package app

import (
	"context"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/ghissue"
)

func TestDecideIssueQualityGateSkipsClosedIssue(t *testing.T) {
	issue := &ghissue.IssueContext{
		Number: 148,
		Title:  "Already closed",
		State:  "CLOSED",
		Body:   "Done",
	}

	decision := decideIssueQualityGate(issue, false)

	if decision.Action != issueQualityGateSkip || decision.Reason != "issue_closed" {
		t.Fatalf("decision = %+v, want closed skip", decision)
	}
}

func TestDecideIssueQualityGateSkipsRecentCompletion(t *testing.T) {
	completedAt := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	issue := &ghissue.IssueContext{
		Number: 148,
		Title:  "Recently completed",
		State:  "OPEN",
		Body:   "Run Hermes",
		Comments: []ghissue.IssueComment{
			{
				Author:    "alice",
				CreatedAt: completedAt,
				Body:      "**Hermes 完成** 3/3 SubTasks\n\nAll checks passed.",
			},
		},
	}

	decision := decideIssueQualityGate(issue, false)

	if decision.Action != issueQualityGateSkip || decision.Reason != "recent_hermes_completion" {
		t.Fatalf("decision = %+v, want recent completion skip", decision)
	}
	if decision.Signal == nil || decision.Signal.Done != 3 || decision.Signal.Total != 3 {
		t.Fatalf("signal = %+v, want 3/3", decision.Signal)
	}
}

func TestDecideIssueQualityGateNeedsClarificationForEmptyIssue(t *testing.T) {
	issue := &ghissue.IssueContext{Number: 148, Title: "Empty", State: "OPEN"}

	decision := decideIssueQualityGate(issue, false)

	if decision.Action != issueQualityGateNeedsClarification || decision.Reason != "missing_acceptance_context" {
		t.Fatalf("decision = %+v, want needs clarification", decision)
	}
}

func TestDecideIssueQualityGateNeedsClarificationForThinIssue(t *testing.T) {
	issue := &ghissue.IssueContext{
		Number: 148,
		Title:  "Thin",
		State:  "OPEN",
		Body:   "fix it",
	}

	decision := decideIssueQualityGate(issue, false)

	if decision.Action != issueQualityGateNeedsClarification || decision.Reason != "missing_acceptance_context" {
		t.Fatalf("decision = %+v, want needs clarification", decision)
	}
}

func TestDecideIssueQualityGateAllowsThinIssueWithChecklist(t *testing.T) {
	issue := &ghissue.IssueContext{
		Number:    148,
		Title:     "Thin but structured",
		State:     "OPEN",
		Body:      "fix it",
		Checklist: []ghissue.ChecklistItem{{Text: "Acceptance item", Checked: false}},
	}

	decision := decideIssueQualityGate(issue, false)

	if decision.Action != issueQualityGateAllow || decision.Reason != "gate_passed" {
		t.Fatalf("decision = %+v, want allow", decision)
	}
}

func TestRecordIssueQualityGateDecisionPersistsRuntimeEvent(t *testing.T) {
	prev := globalStorage
	s := newTestSQLiteStorage(t)
	globalStorage = s
	t.Cleanup(func() { globalStorage = prev })

	issue := &ghissue.IssueContext{
		Number: 148,
		State:  "OPEN",
		Checklist: []ghissue.ChecklistItem{
			{Text: "done", Checked: true},
			{Text: "todo", Checked: false},
		},
		Comments: []ghissue.IssueComment{{Body: "**Hermes 完成** 2/2 SubTasks"}},
	}
	decision := decideIssueQualityGate(issue, false)
	recordIssueQualityGateDecision(context.Background(), chatKey{chatID: 42, threadID: 7}, issue, decision, false)

	events, err := s.GetRuntimeEventsByType("IssueQualityGate", 10)
	if err != nil {
		t.Fatalf("GetRuntimeEventsByType: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.Type != "IssueQualityGate" || got.ChatID != 42 || got.ThreadID != 7 || got.Issue != 148 {
		t.Fatalf("event metadata = %+v", got)
	}
	if got.Payload["action"] != "skip" || got.Payload["reason"] != "recent_hermes_completion" {
		t.Fatalf("payload action/reason = %+v", got.Payload)
	}
	if got.Payload["unchecked_count"] != float64(1) || got.Payload["checked_count"] != float64(1) {
		t.Fatalf("payload checklist counts = %+v", got.Payload)
	}
}
