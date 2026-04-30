package hermes

import (
	"errors"
	"testing"
	"time"
)

func TestTextProgressReporterMinimalEmitsPlanFailureAndDone(t *testing.T) {
	type event struct {
		text   string
		notify bool
	}
	var events []event
	reporter := NewTextProgressReporterWithNotify(func(text string, notify bool) {
		events = append(events, event{text: text, notify: notify})
	})

	// Successful sub-task is silent.
	reporter.OnPlanReady([]SubTask{{Description: "plan"}})
	reporter.OnSubTaskStart(0, 2, SubTask{Description: "step"})
	reporter.OnSubTaskDone(0, 2, SubTask{Description: "step", Status: SubTaskDone, Result: "ok"}, true, "ok")
	reporter.OnRetry(0, 1, 3, "validator failed")

	// Failure surfaces immediately.
	reporter.OnSubTaskDone(1, 2, SubTask{Description: "broken", Status: SubTaskFailed}, false, "compile error")

	reporter.OnDone(TaskState{
		Plan: []SubTask{
			{Description: "step", Status: SubTaskDone, Result: "ok"},
			{Description: "broken", Status: SubTaskFailed, Result: "compile error"},
		},
		TokenBudget: TokenBudget{StartedAt: time.Now()},
	})
	reporter.OnError(errors.New("boom"))

	if len(events) != 4 {
		t.Fatalf("events len = %d, want 4 (plan, failure, done, error): %#v", len(events), events)
	}
	if events[0].notify {
		t.Fatalf("plan event should be silent: %#v", events[0])
	}
	if !events[1].notify {
		t.Fatalf("failure event should notify: %#v", events[1])
	}
	if !events[2].notify {
		t.Fatalf("done event should notify: %#v", events[2])
	}
	if !events[3].notify {
		t.Fatalf("error event should notify: %#v", events[3])
	}
}
