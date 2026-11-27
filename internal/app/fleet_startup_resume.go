package app

import (
	"context"

	"github.com/chimerakang/alice-core/fleet"
)

func fleetStartupResumeState(ctx context.Context, store *fleet.Store, task fleet.Task) (recovered, capped bool) {
	_, recovered, _ = store.LastEvent(ctx, task.ID, fleet.EventStartupResumed)
	if task.Status != fleet.StatusNeedsDecision {
		return recovered, false
	}
	cap, ok, err := store.LastEvent(ctx, task.ID, fleet.EventStartupResumeCapped)
	if err != nil || !ok {
		return recovered, false
	}
	status, ok, err := store.LastEvent(ctx, task.ID, fleet.EventStatus)
	return recovered, err == nil && ok && cap.ID > status.ID
}
