package app

import (
	"errors"
	"testing"
)

func TestBackgroundJobTrackerSummary(t *testing.T) {
	tracker := NewBackgroundJobTracker(2)
	done := tracker.Start("retry")

	summary := tracker.Summary()
	if summary.ActiveCount != 1 || summary.TotalStarted != 1 {
		t.Fatalf("active summary = %+v", summary)
	}

	done(nil)
	summary = tracker.Summary()
	if summary.ActiveCount != 0 || summary.TotalCompleted != 1 || summary.RecentCount != 1 {
		t.Fatalf("completed summary = %+v", summary)
	}
	if summary.Recent[0].Status != "completed" || summary.Recent[0].Name != "retry" {
		t.Fatalf("recent job = %+v", summary.Recent[0])
	}
}

func TestBackgroundJobTrackerFailure(t *testing.T) {
	tracker := NewBackgroundJobTracker(2)
	done := tracker.Start("hermes")
	done(errors.New("boom"))
	done(nil)

	summary := tracker.Summary()
	if summary.TotalFailed != 1 || summary.TotalCompleted != 0 {
		t.Fatalf("failure summary = %+v", summary)
	}
	if summary.Recent[0].Status != "failed" || summary.Recent[0].Error != "boom" {
		t.Fatalf("recent failed job = %+v", summary.Recent[0])
	}
}
