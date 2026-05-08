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
