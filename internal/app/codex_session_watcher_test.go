package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexSessionJSONLLine(t *testing.T) {
	line := []byte(`{"type":"item.completed","thread_id":"thread-1","item":{"id":"item-1","type":"command_execution","command":"go test ./...","status":"completed","exit_code":0},"usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":3}}`)

	payload, ok := parseCodexSessionJSONLLine("/tmp/rollout-test.jsonl", line)
	if !ok {
		t.Fatal("parseCodexSessionJSONLLine ok = false")
	}
	if payload.EventType != "item.completed" || payload.ThreadID != "thread-1" {
		t.Fatalf("payload event/thread = %q/%q", payload.EventType, payload.ThreadID)
	}
	if payload.ItemType != "command_execution" || payload.Command != "go test ./..." || payload.Status != "completed" {
		t.Fatalf("payload item = %+v", payload)
	}
	if payload.ExitCode == nil || *payload.ExitCode != 0 {
		t.Fatalf("payload exit code = %#v, want 0", payload.ExitCode)
	}
	if payload.TokensInput != 10 || payload.CachedInputTokens != 4 || payload.TokensOutput != 3 {
		t.Fatalf("payload tokens = %+v", payload)
	}
}

func TestRecordCodexSessionUpdatePersistsRuntimeEvent(t *testing.T) {
	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = s
	defer func() { globalStorage = oldStorage }()

	recordCodexSessionUpdate(context.Background(), CodexSessionUpdatePayload{
		SessionID:   "thread-abc",
		Event:       "session_update",
		Source:      "codex-vscode",
		SessionPath: "/tmp/rollout-thread-abc.jsonl",
		EventType:   "turn.completed",
		TokensInput: 12,
	})

	events, err := s.GetRuntimeEventsByType(codexSessionRuntimeEventType, 10)
	if err != nil {
		t.Fatalf("GetRuntimeEventsByType: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.Type != codexSessionRuntimeEventType {
		t.Fatalf("event type = %q", got.Type)
	}
	if got.Payload["session_id"] != "thread-abc" || got.Payload["event_type"] != "turn.completed" {
		t.Fatalf("payload = %#v", got.Payload)
	}
}

func TestHandleCodexSessionUpdate(t *testing.T) {
	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = s
	defer func() { globalStorage = oldStorage }()

	wi := &WebInterface{}
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/codex-session-update", strings.NewReader(`{
		"thread_id":"thread-hook",
		"event_type":"agent_message",
		"message":"hello",
		"session_path":"/tmp/rollout-thread-hook.jsonl"
	}`))
	rec := httptest.NewRecorder()

	wi.handleCodexSessionUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	events, err := s.GetRuntimeEventsByType(codexSessionRuntimeEventType, 10)
	if err != nil {
		t.Fatalf("GetRuntimeEventsByType: %v", err)
	}
	if len(events) != 1 || events[0].Payload["session_id"] != "thread-hook" {
		t.Fatalf("events = %#v", events)
	}
}

func TestCodexSessionWatcherProcessesOnlyNewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-test.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"thread.started","thread_id":"old-thread"}`+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w, err := newCodexSessionWatcher(dir)
	if err != nil {
		t.Fatalf("newCodexSessionWatcher: %v", err)
	}
	defer w.Close()
	w.markExistingFile(path)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString(`{"type":"item.completed","thread_id":"new-thread","item":{"id":"i1","type":"agent_message","text":"new text"}}` + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = f.Close()

	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = s
	defer func() { globalStorage = oldStorage }()

	if err := w.processFile(context.Background(), path); err != nil {
		t.Fatalf("processFile: %v", err)
	}
	events, err := s.GetRuntimeEventsByType(codexSessionRuntimeEventType, 10)
	if err != nil {
		t.Fatalf("GetRuntimeEventsByType: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want only appended line", len(events))
	}
	if events[0].Payload["thread_id"] != "new-thread" {
		t.Fatalf("payload = %#v", events[0].Payload)
	}
}
