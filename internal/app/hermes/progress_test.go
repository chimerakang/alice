package hermes

import (
	"errors"
	"testing"
	"time"
)

func TestTextProgressReporterNotificationPolicy(t *testing.T) {
	type event struct {
		text   string
		notify bool
	}
	var events []event
	reporter := NewTextProgressReporterWithNotify(VerbosityNormal, func(text string, notify bool) {
		events = append(events, event{text: text, notify: notify})
	})

	reporter.OnPlanReady([]SubTask{{Description: "plan"}})
	reporter.OnSubTaskStart(0, 1, SubTask{Description: "step"})
	reporter.OnSubTaskDone(0, 1, SubTask{Description: "step", Status: SubTaskDone, Result: "ok"}, true, "ok")
	reporter.OnDone(TaskState{
		Plan: []SubTask{{Description: "step", Status: SubTaskDone, Result: "ok"}},
		TokenBudget: TokenBudget{
			StartedAt: time.Now(),
		},
	})
	reporter.OnError(errors.New("boom"))

	if len(events) != 5 {
		t.Fatalf("events len = %d, want 5", len(events))
	}
	for i := 0; i < 3; i++ {
		if events[i].notify {
			t.Fatalf("event %d should be silent: %#v", i, events[i])
		}
	}
	if !events[3].notify {
		t.Fatalf("done event should notify: %#v", events[3])
	}
	if !events[4].notify {
		t.Fatalf("error event should notify: %#v", events[4])
	}
}
