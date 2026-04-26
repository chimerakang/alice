package app

import (
	"testing"

	appengine "claude-tg-agent/internal/app/engine"
)

func TestBroadcastReviewEventEmitsCompactPayload(t *testing.T) {
	oldHub := globalWebSocketHub
	globalWebSocketHub = NewWebSocketHub()
	defer func() { globalWebSocketHub = oldHub }()

	notification := appengine.ReviewNotification{
		TaskID:          "task-9",
		ReviewerModel:   "gpt-5.5",
		Verdict:         appengine.VerdictPartial,
		OverallScore:    77,
		IssueTags:       []appengine.ReviewTag{appengine.ReviewTagMissingContext},
		AdvisoryRetry:   true,
		FailingSubTasks: 2,
		RetryNote:       "建議人工評估後重跑 2 個失敗/低分子任務",
	}

	BroadcastReviewEvent(notification)

	if got := len(globalWebSocketHub.eventBuffer); got != 1 {
		t.Fatalf("event buffer length = %d, want 1", got)
	}
	event := globalWebSocketHub.eventBuffer[0]
	if event.Type != "review_complete" {
		t.Fatalf("event type = %q, want review_complete", event.Type)
	}
	payload, ok := event.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("payload type = %T, want map[string]interface{}", event.Data)
	}
	if payload["task_id"] != "task-9" || payload["reviewer_model"] != "gpt-5.5" {
		t.Fatalf("payload missing key fields: %+v", payload)
	}
	if payload["advisory_retry"] != true || payload["failing_subtasks"] != 2 {
		t.Fatalf("payload missing retry fields: %+v", payload)
	}
	tags, ok := payload["issue_tags"].([]appengine.ReviewTag)
	if !ok || len(tags) != 1 || tags[0] != appengine.ReviewTagMissingContext {
		t.Fatalf("issue_tags = %#v", payload["issue_tags"])
	}
}
