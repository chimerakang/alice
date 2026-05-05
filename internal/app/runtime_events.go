package app

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

type RuntimeEventRecord struct {
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"`
	ChatID    int64                  `json:"chat_id,omitempty"`
	ThreadID  int                    `json:"thread_id,omitempty"`
	TaskID    string                 `json:"task_id,omitempty"`
	Issue     int                    `json:"issue,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

func runtimeEventFromEngineEvent(event appengine.Event) RuntimeEventRecord {
	timestamp := event.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	payload := map[string]interface{}{}
	if event.Payload != nil {
		if raw, err := json.Marshal(event.Payload); err == nil {
			_ = json.Unmarshal(raw, &payload)
		}
	}
	return RuntimeEventRecord{
		Timestamp: timestamp,
		Type:      strings.TrimSpace(event.Type),
		ChatID:    event.ChatID,
		ThreadID:  event.ThreadID,
		TaskID:    strings.TrimSpace(event.TaskID),
		Issue:     event.Issue,
		Payload:   payload,
	}
}

func recordRuntimeEvent(ctx context.Context, event appengine.Event) {
	if globalStorage == nil {
		return
	}
	record := runtimeEventFromEngineEvent(event)
	if record.Type == "" {
		return
	}
	if err := globalStorage.InsertRuntimeEvent(record); err != nil {
		log.Printf("[runtime-event] insert failed type=%s task=%s chat=%d: %v", record.Type, record.TaskID, record.ChatID, err)
	}
}

func recordRecoveryDecision(ctx context.Context, req appengine.RecoveryRequest, decision appengine.RecoveryDecision, key chatKey, taskID string, issue int) {
	event := appengine.RecoveryTraceEvent(req, decision, time.Now())
	event.ChatID = key.chatID
	event.ThreadID = key.threadID
	event.TaskID = strings.TrimSpace(taskID)
	event.Issue = issue
	recordRuntimeEvent(ctx, event)
}
