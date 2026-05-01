package hermes

import (
	"context"
	"reflect"
	"testing"
)

func TestPlannerSessionResumesAcrossJSONRetries(t *testing.T) {
	var gotSessionIDs []string
	calls := 0
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		gotSessionIDs = append(gotSessionIDs, sessionID)
		calls++
		if calls == 1 {
			return CallPlanResult{Text: "not json", SessionID: "planner-session-1"}, nil
		}
		return CallPlanResult{
			Text:      "```json\n" + `[{"id":"s1","description":"Execute directly","tool_hints":["Read"]}]` + "\n```",
			SessionID: "planner-session-1",
		}, nil
	}

	planner := NewPlannerSession(planFn, 2, "")
	if _, _, _, _, err := planner.Plan(context.Background(), "implement feature", "/repo"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if want := []string{"", "planner-session-1"}; !reflect.DeepEqual(gotSessionIDs, want) {
		t.Fatalf("session ids = %#v, want %#v", gotSessionIDs, want)
	}
	if got := planner.SessionID(); got != "planner-session-1" {
		t.Fatalf("planner session = %q, want planner-session-1", got)
	}
}

func TestPlannerSessionUsesSeededSessionID(t *testing.T) {
	var gotSessionID string
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error) {
		gotSessionID = sessionID
		return CallPlanResult{
			Text:      "```json\n" + `[{"id":"s1","description":"Execute directly","tool_hints":["Read"]}]` + "\n```",
			SessionID: "planner-session-next",
		}, nil
	}

	planner := NewPlannerSession(planFn, 1, "")
	planner.SetSessionID("planner-session-prev")
	if _, _, _, _, err := planner.Plan(context.Background(), "implement feature", "/repo"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if gotSessionID != "planner-session-prev" {
		t.Fatalf("call session = %q, want planner-session-prev", gotSessionID)
	}
	if got := planner.SessionID(); got != "planner-session-next" {
		t.Fatalf("planner session = %q, want planner-session-next", got)
	}
}
