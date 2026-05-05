package app

import (
	"context"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func TestDecideIssueQualityGateSkipsClosedIssue(t *testing.T) {
	issue := &hermes.IssueContext{
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
	issue := &hermes.IssueContext{
		Number: 148,
		Title:  "Recently completed",
		State:  "OPEN",
		Body:   "Run Hermes",
		Comments: []hermes.IssueComment{
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
	issue := &hermes.IssueContext{Number: 148, Title: "Empty", State: "OPEN"}

	decision := decideIssueQualityGate(issue, false)

	if decision.Action != issueQualityGateNeedsClarification || decision.Reason != "missing_acceptance_context" {
		t.Fatalf("decision = %+v, want needs clarification", decision)
	}
}

func TestDecideIssueQualityGateNeedsClarificationForThinIssue(t *testing.T) {
	issue := &hermes.IssueContext{
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
	issue := &hermes.IssueContext{
		Number:    148,
		Title:     "Thin but structured",
		State:     "OPEN",
		Body:      "fix it",
		Checklist: []hermes.ChecklistItem{{Text: "Acceptance item", Checked: false}},
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

	issue := &hermes.IssueContext{
		Number: 148,
		State:  "OPEN",
		Checklist: []hermes.ChecklistItem{
			{Text: "done", Checked: true},
			{Text: "todo", Checked: false},
		},
		Comments: []hermes.IssueComment{{Body: "**Hermes 完成** 2/2 SubTasks"}},
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

func TestStartHermesFromIssueModeQualityGateSkipsClosedIssue(t *testing.T) {
	oldFetch := hermesFetchIssue
	prevStorage := globalStorage
	s := newTestSQLiteStorage(t)
	globalStorage = s
	t.Cleanup(func() {
		hermesFetchIssue = oldFetch
		globalStorage = prevStorage
	})

	hermesFetchIssue = func(ctx context.Context, projectDir string, issueNumber int) (*hermes.IssueContext, error) {
		if projectDir != "/repo" || issueNumber != 148 {
			t.Fatalf("unexpected fetch args: %q #%d", projectDir, issueNumber)
		}
		return &hermes.IssueContext{Number: 148, Title: "Closed", State: "CLOSED", Body: "done"}, nil
	}
	bot := &TelegramBot{
		config:       &Config{},
		messageQueue: make(chan *TelegramMessage, 2),
	}
	key := chatKey{chatID: 42, threadID: 7}

	bot.startHermesFromIssueMode(key, 148, "/repo", false)

	assertQueuedMessageContains(t, bot.messageQueue, "已經是 closed")
	events, err := s.GetRuntimeEventsByType("IssueQualityGate", 10)
	if err != nil {
		t.Fatalf("GetRuntimeEventsByType: %v", err)
	}
	if len(events) != 1 || events[0].Payload["reason"] != "issue_closed" {
		t.Fatalf("events = %+v, want issue_closed gate event", events)
	}
}
