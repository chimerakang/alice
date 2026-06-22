package hermes

import "testing"

func TestValidIssueTransition(t *testing.T) {
	tests := []struct {
		name  string
		from  IssueState
		event IssueEvent
		to    IssueState
		want  bool
	}{
		{
			name:  "happy_path_drafted_to_planned",
			from:  IssueStateDrafted,
			event: IssueEventIssuePlanned,
			to:    IssueStatePlanned,
			want:  true,
		},
		{
			name:  "happy_path_planned_to_in_progress",
			from:  IssueStatePlanned,
			event: IssueEventHermesTaskStarted,
			to:    IssueStateInProgress,
			want:  true,
		},
		{
			name:  "happy_path_in_progress_to_evidence_collected",
			from:  IssueStateInProgress,
			event: IssueEventSubTaskCompleted,
			to:    IssueStateEvidenceCollected,
			want:  true,
		},
		{
			name:  "checklist_unsynced_detected",
			from:  IssueStateEvidenceCollected,
			event: IssueEventChecklistMismatchDetected,
			to:    IssueStateChecklistUnsynced,
			want:  true,
		},
		{
			name:  "checklist_unsynced_sync_done",
			from:  IssueStateChecklistUnsynced,
			event: IssueEventChecklistSynced,
			to:    IssueStateChecklistSynced,
			want:  true,
		},
		{
			name:  "ready_to_close",
			from:  IssueStateChecklistSynced,
			event: IssueEventCloseRequested,
			to:    IssueStateReadyToClose,
			want:  true,
		},
		{
			name:  "closed",
			from:  IssueStateReadyToClose,
			event: IssueEventIssueClosed,
			to:    IssueStateClosed,
			want:  true,
		},
		{
			name:  "blocked_from_in_progress",
			from:  IssueStateInProgress,
			event: IssueEventHumanDecisionRequired,
			to:    IssueStateBlocked,
			want:  true,
		},
		{
			name:  "blocked_recovery_to_planned",
			from:  IssueStateBlocked,
			event: IssueEventIssuePlanned,
			to:    IssueStatePlanned,
			want:  true,
		},
		{
			name:  "closed_is_terminal",
			from:  IssueStateClosed,
			event: IssueEventIssuePlanned,
			to:    IssueStatePlanned,
			want:  false,
		},
		{
			name:  "checklist_unsynced_cannot_close_directly",
			from:  IssueStateChecklistUnsynced,
			event: IssueEventIssueClosed,
			to:    IssueStateClosed,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidIssueTransition(tt.from, tt.event, tt.to); got != tt.want {
				t.Fatalf("ValidIssueTransition(%q, %q, %q) = %v, want %v", tt.from, tt.event, tt.to, got, tt.want)
			}
		})
	}
}

func TestIssueStateHelpers(t *testing.T) {
	if !IssueStateClosed.IsTerminal() {
		t.Fatal("closed should be terminal")
	}
	if IssueStateBlocked.IsTerminal() {
		t.Fatal("blocked should not be terminal")
	}
	if !IssueStateBlocked.NeedsHumanDecision() {
		t.Fatal("blocked should require human decision")
	}
	if IssueStateChecklistUnsynced.NeedsHumanDecision() {
		t.Fatal("checklist_unsynced should not require human decision by itself")
	}
}

func TestIssueCloseReadinessCanAutoClose(t *testing.T) {
	tests := []struct {
		name string
		g    IssueCloseReadiness
		want bool
	}{
		{
			name: "happy_path",
			g: IssueCloseReadiness{
				State:                     IssueStateReadyToClose,
				ChecklistSynced:           true,
				ReviewAccepted:            true,
				ValidationPassed:          true,
				HasUncheckedRequiredItems: false,
			},
			want: true,
		},
		{
			name: "checklist_unsynced",
			g: IssueCloseReadiness{
				State:            IssueStateReadyToClose,
				ChecklistSynced:  false,
				ReviewAccepted:   true,
				ValidationPassed: true,
			},
			want: false,
		},
		{
			name: "blocked",
			g: IssueCloseReadiness{
				State:                     IssueStateBlocked,
				ChecklistSynced:           true,
				ReviewAccepted:            true,
				ValidationPassed:          true,
				HasBlockingLabel:          true,
				HasUncheckedRequiredItems: false,
			},
			want: false,
		},
		{
			name: "closed",
			g: IssueCloseReadiness{
				State:            IssueStateClosed,
				ChecklistSynced:  true,
				ReviewAccepted:   true,
				ValidationPassed: true,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.g.CanAutoClose(); got != tt.want {
				t.Fatalf("CanAutoClose() = %v, want %v", got, tt.want)
			}
		})
	}
}
