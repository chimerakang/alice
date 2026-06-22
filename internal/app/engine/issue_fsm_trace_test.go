package engine

import (
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func TestIssueFSMTransitionEvent(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		issue   int
		payload IssueFSMTransitionPayload
	}{
		{
			name:  "checklist_synced",
			issue: 153,
			payload: IssueFSMTransitionPayload{
				From:                   hermes.IssueStateChecklistUnsynced,
				Event:                  hermes.IssueEventChecklistSynced,
				To:                     hermes.IssueStateChecklistSynced,
				Reason:                 "sync applied",
				Source:                 "engine.on_done",
				ChecklistTotal:         5,
				CheckedCount:           4,
				UncheckedCount:         1,
				HasBlockingLabel:       false,
				HasRequiredLabel:       true,
				ReviewAccepted:         true,
				ValidationPassed:       true,
				ChecklistSynced:        true,
				NeedsHumanConfirmation: false,
				WouldWrite:             true,
				Wrote:                  true,
				DryRun:                 false,
				CanAutoClose:           false,
			},
		},
		{
			name:  "ready_to_close",
			issue: 154,
			payload: IssueFSMTransitionPayload{
				From:                   hermes.IssueStateChecklistSynced,
				Event:                  hermes.IssueEventCloseRequested,
				To:                     hermes.IssueStateReadyToClose,
				Reason:                 "all guards passed",
				Source:                 "telegram.reconcile",
				ChecklistTotal:         3,
				CheckedCount:           3,
				UncheckedCount:         0,
				HasBlockingLabel:       false,
				HasRequiredLabel:       true,
				ReviewAccepted:         true,
				ValidationPassed:       true,
				ChecklistSynced:        true,
				NeedsHumanConfirmation: false,
				WouldWrite:             false,
				Wrote:                  false,
				DryRun:                 true,
				CanAutoClose:           true,
			},
		},
		{
			name:  "blocked",
			issue: 155,
			payload: IssueFSMTransitionPayload{
				From:                   hermes.IssueStateReadyToClose,
				Event:                  hermes.IssueEventHumanDecisionRequired,
				To:                     hermes.IssueStateBlocked,
				Reason:                 "manual decision required",
				Source:                 "telegram.reconcile",
				ChecklistTotal:         3,
				CheckedCount:           2,
				UncheckedCount:         1,
				HasBlockingLabel:       true,
				HasRequiredLabel:       true,
				ReviewAccepted:         false,
				ValidationPassed:       false,
				ChecklistSynced:        false,
				NeedsHumanConfirmation: true,
				WouldWrite:             false,
				Wrote:                  false,
				DryRun:                 false,
				CanAutoClose:           false,
				RetryAction:            "retry-close",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := IssueFSMTransitionEvent(tt.issue, at, tt.payload)
			if event.Type != "IssueFSMTransition" || !event.Timestamp.Equal(at) || event.Issue != tt.issue {
				t.Fatalf("event metadata = %+v", event)
			}
			payload, ok := event.Payload.(IssueFSMTransitionPayload)
			if !ok {
				t.Fatalf("payload type = %T, want IssueFSMTransitionPayload", event.Payload)
			}
			if payload.From != tt.payload.From || payload.Event != tt.payload.Event || payload.To != tt.payload.To {
				t.Fatalf("payload transition = %+v", payload)
			}
			if payload.Source != tt.payload.Source || payload.CheckedCount != tt.payload.CheckedCount || payload.UncheckedCount != tt.payload.UncheckedCount {
				t.Fatalf("payload details = %+v", payload)
			}
			if payload.CanAutoClose != tt.payload.CanAutoClose || payload.NeedsHumanConfirmation != tt.payload.NeedsHumanConfirmation || payload.RetryAction != tt.payload.RetryAction {
				t.Fatalf("payload guard = %+v", payload)
			}
		})
	}
}
