package app

import (
	"context"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

func TestRuntimeEventStorageRoundTrip(t *testing.T) {
	s := newTestSQLiteStorage(t)
	event := RuntimeEventRecord{
		Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Type:      "RecoveryDecision",
		ChatID:    42,
		ThreadID:  7,
		TaskID:    "task-1",
		Issue:     151,
		Payload: map[string]interface{}{
			"mode":   "direct_stream",
			"action": "retry",
			"reason": "transient_error",
		},
	}
	if err := s.InsertRuntimeEvent(event); err != nil {
		t.Fatalf("InsertRuntimeEvent: %v", err)
	}
	events, err := s.GetRuntimeEventsByType("RecoveryDecision", 10)
	if err != nil {
		t.Fatalf("GetRuntimeEventsByType: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.Type != event.Type || got.ChatID != event.ChatID || got.ThreadID != event.ThreadID || got.TaskID != event.TaskID || got.Issue != event.Issue {
		t.Fatalf("event metadata = %+v", got)
	}
	if got.Payload["mode"] != "direct_stream" || got.Payload["action"] != "retry" || got.Payload["reason"] != "transient_error" {
		t.Fatalf("payload = %+v", got.Payload)
	}
}

func TestRecordRecoveryDecisionPersistsRuntimeEvent(t *testing.T) {
	prev := globalStorage
	s := newTestSQLiteStorage(t)
	globalStorage = s
	t.Cleanup(func() { globalStorage = prev })

	req := appengine.RecoveryRequest{
		Mode:        "planner_retry",
		Attempt:     1,
		MaxAttempts: 3,
	}
	decision := appengine.RecoveryDecision{
		Action:      appengine.RecoveryActionRetry,
		Reason:      "planner_retry",
		Retryable:   true,
		NextAttempt: 2,
	}
	recordRecoveryDecision(context.Background(), req, decision, chatKey{chatID: 42, threadID: 7}, "task-1", 156)

	events, err := s.GetRuntimeEvents(10, 0)
	if err != nil {
		t.Fatalf("GetRuntimeEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.Type != "RecoveryDecision" || got.ChatID != 42 || got.ThreadID != 7 || got.TaskID != "task-1" || got.Issue != 156 {
		t.Fatalf("event metadata = %+v", got)
	}
	if got.Payload["mode"] != "planner_retry" || got.Payload["action"] != "retry" || got.Payload["reason"] != "planner_retry" {
		t.Fatalf("payload = %+v", got.Payload)
	}
}

func TestRecordHermesInteractionGatePersistsRuntimeEvent(t *testing.T) {
	prev := globalStorage
	s := newTestSQLiteStorage(t)
	globalStorage = s
	t.Cleanup(func() { globalStorage = prev })

	recordHermesInteractionGate(context.Background(), chatKey{chatID: 42, threadID: 7}, "block_until_choice", "failure_pause_active", "task-1", 2, 5, "修復部署設定")

	events, err := s.GetRuntimeEventsByType("HermesInteractionGate", 10)
	if err != nil {
		t.Fatalf("GetRuntimeEventsByType: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.Type != "HermesInteractionGate" || got.ChatID != 42 || got.ThreadID != 7 || got.TaskID != "task-1" {
		t.Fatalf("event metadata = %+v", got)
	}
	if got.Payload["action"] != "block_until_choice" || got.Payload["reason"] != "failure_pause_active" || got.Payload["subtask"] != "修復部署設定" {
		t.Fatalf("payload = %+v", got.Payload)
	}
	if got.Payload["subtask_idx"] != float64(2) || got.Payload["subtask_num"] != float64(3) || got.Payload["total"] != float64(5) {
		t.Fatalf("numeric payload = %+v", got.Payload)
	}
}
