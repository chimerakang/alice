package hermes

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPaginateForTelegram(t *testing.T) {
	// Single short message: one page, no footer.
	pages := paginateForTelegram([]string{"hello", "world"}, 100)
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	if !strings.Contains(pages[0], "（") == false { // expect no pagination footer
		// already correct; placeholder for clarity
	}
	if strings.Contains(pages[0], "/2") {
		t.Errorf("single page should not carry a page footer: %q", pages[0])
	}

	// Long content forces multi-page; each page must stay under cap and
	// carry a (n/m) footer.
	bigLine := strings.Repeat("a", 30)
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, bigLine)
	}
	pages = paginateForTelegram(lines, 100)
	if len(pages) < 2 {
		t.Fatalf("expected pagination, got %d pages", len(pages))
	}
	for i, p := range pages {
		footer := fmt.Sprintf("（%d/%d）", i+1, len(pages))
		if !strings.Contains(p, footer) {
			t.Errorf("page %d missing footer %q: %q", i, footer, p)
		}
	}
}

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
