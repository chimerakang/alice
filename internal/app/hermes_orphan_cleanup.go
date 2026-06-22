package app

import (
	"context"
	"log"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
)

// staleInterruptCutoff is how long a HermesInterrupt may remain pending
// before alice startup considers it orphaned. The current pause UX bounds
// the in-process wait at 10 minutes; a 24h cutoff is conservative and
// covers cases where an alice restart happened mid-pause.
const staleInterruptCutoff = 24 * time.Hour

// staleInterruptStore is the read surface needed for the orphan-cleanup
// pass. Implemented by hermes.SQLiteTaskStore. After #169 slice 3d uses
// MarkTaskFailedDurable so the failure also lands in a snapshot — without
// it the snapshot's state_status (now authoritative) would still report
// the task as executing.
type staleInterruptStore interface {
	ListStaleInterrupts(cutoff time.Time) ([]hermes.StaleInterruptRef, error)
	MarkTaskFailedDurable(taskID, reason string) error
}

// pendingInterruptStore is the read surface needed by RemindPendingPauses.
// Implemented by hermes.SQLiteTaskStore.
type pendingInterruptStore interface {
	ListStaleInterrupts(cutoff time.Time) ([]hermes.StaleInterruptRef, error)
}

// pauseReminder is the bot-side surface RemindPendingPauses needs to
// notify the operator. Defined here (rather than reaching into
// *TelegramBot directly) so the function is testable without a live
// bot instance.
type pauseReminder interface {
	NotifyPendingPause(chatID int64, threadID int, taskID string, interrupt hermes.HermesInterrupt, pausedFor time.Duration)
}

// SweepStaleHermesInterrupts marks tasks failed when their latest snapshot
// holds a HermesInterrupt older than staleInterruptCutoff. Each cleared
// task emits a HumanInterruptTimeout runtime event so dashboards can show
// what was reaped.
//
// Called once at alice startup. Safe to call repeatedly — idempotent
// because cleared tasks no longer match the query (status moves out of
// 'executing').
func SweepStaleHermesInterrupts(ctx context.Context, store staleInterruptStore) {
	if store == nil {
		return
	}
	cutoff := time.Now().Add(-staleInterruptCutoff)
	stale, err := store.ListStaleInterrupts(cutoff)
	if err != nil {
		log.Printf("[hermes.orphan] list stale interrupts: %v", err)
		return
	}
	if len(stale) == 0 {
		return
	}
	log.Printf("[hermes.orphan] sweeping %d stale interrupt(s) older than %s", len(stale), staleInterruptCutoff)
	for _, ref := range stale {
		idleFor := time.Since(ref.Interrupt.CreatedAt).Round(time.Second)
		log.Printf("[hermes.orphan] task=%s chat=%d thread=%d interrupt_id=%s idle=%s reason=%s — marking failed",
			ref.TaskID, ref.ChatID, ref.ThreadID, ref.Interrupt.ID, idleFor, ref.Interrupt.Reason)
		if err := store.MarkTaskFailedDurable(ref.TaskID, "stale_interrupt_orphan"); err != nil {
			log.Printf("[hermes.orphan] mark failed task=%s: %v", ref.TaskID, err)
			continue
		}
		recordRuntimeEvent(ctx, appengine.Event{
			Type:      "HumanInterruptTimeout",
			Timestamp: time.Now(),
			ChatID:    ref.ChatID,
			ThreadID:  ref.ThreadID,
			TaskID:    ref.TaskID,
			Issue:     ref.State.GithubIssueNumber,
			Payload: map[string]any{
				"interrupt_id":     ref.Interrupt.ID,
				"interrupt_reason": ref.Interrupt.Reason,
				"source_step":      string(ref.Interrupt.SourceStep),
				"resume_step":      string(ref.Interrupt.ResumeStep),
				"created_at":       ref.Interrupt.CreatedAt,
				"idle_seconds":     int(idleFor.Seconds()),
				"sweep_reason":     "startup_orphan_cleanup",
			},
		})
	}
}

// RemindPendingPauses sends a Telegram reminder for each task whose
// latest snapshot still carries an active HermesInterrupt that is younger
// than staleInterruptCutoff (older ones were already failed by
// SweepStaleHermesInterrupts). The reminder lets the operator know the
// task is waiting for their decision after an alice restart, without
// auto-deciding for them.
//
// Returns the count of reminders sent.
func RemindPendingPauses(ctx context.Context, store pendingInterruptStore, reminder pauseReminder) int {
	if store == nil || reminder == nil {
		return 0
	}
	now := time.Now()
	pauses, err := store.ListStaleInterrupts(now)
	if err != nil {
		log.Printf("[hermes.resume] list pending pauses: %v", err)
		return 0
	}
	cutoff := now.Add(-staleInterruptCutoff)
	sent := 0
	for _, ref := range pauses {
		// Filter out anything older than the staleness cutoff — those
		// were already swept to failed by SweepStaleHermesInterrupts and
		// would no longer have an interrupt in the snapshot anyway, but
		// guard defensively.
		if !ref.Interrupt.CreatedAt.IsZero() && ref.Interrupt.CreatedAt.Before(cutoff) {
			continue
		}
		pausedFor := now.Sub(ref.Interrupt.CreatedAt).Round(time.Second)
		reminder.NotifyPendingPause(ref.ChatID, ref.ThreadID, ref.TaskID, ref.Interrupt, pausedFor)
		sent++
	}
	if sent > 0 {
		log.Printf("[hermes.resume] reminded %d pending pause(s) on startup", sent)
	}
	return sent
}
