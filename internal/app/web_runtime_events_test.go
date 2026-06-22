package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleRuntimeEventsReturnsPersistedEvents(t *testing.T) {
	prev := globalStorage
	s := newTestSQLiteStorage(t)
	globalStorage = s
	t.Cleanup(func() { globalStorage = prev })

	if err := s.InsertRuntimeEvent(RuntimeEventRecord{
		Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Type:      "RecoveryDecision",
		ChatID:    42,
		ThreadID:  7,
		TaskID:    "task-1",
		Issue:     156,
		Payload: map[string]interface{}{
			"mode":   "planner_retry",
			"action": "retry",
		},
	}); err != nil {
		t.Fatalf("InsertRuntimeEvent: %v", err)
	}
	if err := s.InsertRuntimeEvent(RuntimeEventRecord{
		Timestamp: time.Date(2026, 5, 5, 12, 1, 0, 0, time.UTC),
		Type:      "OtherEvent",
		Payload:   map[string]interface{}{"ok": true},
	}); err != nil {
		t.Fatalf("InsertRuntimeEvent other: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/events?type=RecoveryDecision&limit=10", nil)
	w := httptest.NewRecorder()
	(&WebInterface{}).handleRuntimeEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Events []RuntimeEventRecord `json:"events"`
		Total  int                  `json:"total"`
		Type   string               `json:"type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 1 || resp.Type != "RecoveryDecision" || len(resp.Events) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	event := resp.Events[0]
	if event.Type != "RecoveryDecision" || event.ChatID != 42 || event.ThreadID != 7 || event.TaskID != "task-1" || event.Issue != 156 {
		t.Fatalf("event = %+v", event)
	}
	if event.Payload["mode"] != "planner_retry" || event.Payload["action"] != "retry" {
		t.Fatalf("payload = %+v", event.Payload)
	}
}

func TestHandleRuntimeEventsFiltersByTaskIDAndType(t *testing.T) {
	prev := globalStorage
	s := newTestSQLiteStorage(t)
	globalStorage = s
	t.Cleanup(func() { globalStorage = prev })

	for _, event := range []RuntimeEventRecord{
		{
			Timestamp: time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC),
			Type:      "GraphNodeEvent",
			TaskID:    "task-graph-1",
			Payload:   map[string]interface{}{"node": "planner", "status": "started"},
		},
		{
			Timestamp: time.Date(2026, 5, 12, 9, 1, 0, 0, time.UTC),
			Type:      "GraphNodeEvent",
			TaskID:    "task-graph-2",
			Payload:   map[string]interface{}{"node": "planner", "status": "started"},
		},
		{
			Timestamp: time.Date(2026, 5, 12, 9, 2, 0, 0, time.UTC),
			Type:      "IssueFSMTransition",
			TaskID:    "task-graph-1",
			Payload:   map[string]interface{}{"to": "checklist_synced"},
		},
	} {
		if err := s.InsertRuntimeEvent(event); err != nil {
			t.Fatalf("InsertRuntimeEvent %s/%s: %v", event.TaskID, event.Type, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/events?task_id=task-graph-1&type=GraphNodeEvent&limit=10", nil)
	w := httptest.NewRecorder()
	(&WebInterface{}).handleRuntimeEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Events []RuntimeEventRecord `json:"events"`
		Total  int                  `json:"total"`
		Type   string               `json:"type"`
		TaskID string               `json:"task_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 1 || resp.Type != "GraphNodeEvent" || resp.TaskID != "task-graph-1" {
		t.Fatalf("response = %+v", resp)
	}
	if got := resp.Events[0]; got.TaskID != "task-graph-1" || got.Type != "GraphNodeEvent" {
		t.Fatalf("event = %+v", got)
	}
}

func TestHandleRuntimeEventsRequiresStorage(t *testing.T) {
	prev := globalStorage
	globalStorage = nil
	t.Cleanup(func() { globalStorage = prev })

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/events", nil)
	w := httptest.NewRecorder()
	(&WebInterface{}).handleRuntimeEvents(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d want %d body=%s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}
