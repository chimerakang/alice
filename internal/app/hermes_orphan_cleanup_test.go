package app

import (
	"context"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

type fakePendingStore struct {
	refs []hermes.StaleInterruptRef
	err  error
}

func (s *fakePendingStore) ListStaleInterrupts(cutoff time.Time) ([]hermes.StaleInterruptRef, error) {
	return s.refs, s.err
}

type fakeReminder struct {
	calls []reminderCall
}

type reminderCall struct {
	chatID    int64
	threadID  int
	taskID    string
	pausedFor time.Duration
}

func (r *fakeReminder) NotifyPendingPause(chatID int64, threadID int, taskID string, interrupt hermes.HermesInterrupt, pausedFor time.Duration) {
	r.calls = append(r.calls, reminderCall{
		chatID:    chatID,
		threadID:  threadID,
		taskID:    taskID,
		pausedFor: pausedFor,
	})
}

func TestRemindPendingPauses_NotifiesEachLivePause(t *testing.T) {
	now := time.Now()
	store := &fakePendingStore{
		refs: []hermes.StaleInterruptRef{
			{
				TaskID:    "task-fresh",
				ChatID:    100,
				ThreadID:  5,
				Interrupt: hermes.HermesInterrupt{ID: "iv1", CreatedAt: now.Add(-2 * time.Hour)},
			},
			{
				TaskID:    "task-recent",
				ChatID:    101,
				ThreadID:  6,
				Interrupt: hermes.HermesInterrupt{ID: "iv2", CreatedAt: now.Add(-15 * time.Minute)},
			},
		},
	}
	reminder := &fakeReminder{}
	sent := RemindPendingPauses(context.Background(), store, reminder)
	if sent != 2 {
		t.Fatalf("sent = %d, want 2", sent)
	}
	if len(reminder.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(reminder.calls))
	}
	if reminder.calls[0].taskID != "task-fresh" || reminder.calls[1].taskID != "task-recent" {
		t.Errorf("unexpected order or task ids: %+v", reminder.calls)
	}
}

func TestRemindPendingPauses_SkipsStaleAlreadyHandled(t *testing.T) {
	// SweepStaleHermesInterrupts already failed >24h interrupts; defensively
	// skip them in the reminder pass too, so a corner-case where the sweep
	// did not run doesn't cause RemindPendingPauses to also nudge dead
	// pauses.
	now := time.Now()
	store := &fakePendingStore{
		refs: []hermes.StaleInterruptRef{
			{
				TaskID:    "task-stale",
				ChatID:    200,
				Interrupt: hermes.HermesInterrupt{ID: "old", CreatedAt: now.Add(-48 * time.Hour)},
			},
		},
	}
	reminder := &fakeReminder{}
	sent := RemindPendingPauses(context.Background(), store, reminder)
	if sent != 0 {
		t.Errorf("sent = %d, want 0 (stale should be skipped)", sent)
	}
}

func TestRemindPendingPauses_NilArgsAreSafe(t *testing.T) {
	if got := RemindPendingPauses(context.Background(), nil, &fakeReminder{}); got != 0 {
		t.Errorf("nil store should return 0, got %d", got)
	}
	if got := RemindPendingPauses(context.Background(), &fakePendingStore{}, nil); got != 0 {
		t.Errorf("nil reminder should return 0, got %d", got)
	}
}
