package hermes

import "strings"

// IssueState represents the lifecycle stage of a GitHub issue from the
// IssueOps point of view.
type IssueState string

const (
	IssueStateDrafted           IssueState = "drafted"
	IssueStatePlanned           IssueState = "planned"
	IssueStateInProgress        IssueState = "in_progress"
	IssueStateEvidenceCollected IssueState = "evidence_collected"
	IssueStateChecklistUnsynced IssueState = "checklist_unsynced"
	IssueStateChecklistSynced   IssueState = "checklist_synced"
	IssueStateReadyToClose      IssueState = "ready_to_close"
	IssueStateClosed            IssueState = "closed"
	IssueStateBlocked           IssueState = "blocked"
)

// IssueEvent represents a lifecycle event emitted by IssueOps.
type IssueEvent string

const (
	IssueEventIssueLoaded               IssueEvent = "IssueLoaded"
	IssueEventIssuePlanned              IssueEvent = "IssuePlanned"
	IssueEventHermesTaskStarted         IssueEvent = "HermesTaskStarted"
	IssueEventSubTaskCompleted          IssueEvent = "SubTaskCompleted"
	IssueEventReviewPassed              IssueEvent = "ReviewPassed"
	IssueEventReviewPartialOrFailed     IssueEvent = "ReviewPartialOrFailed"
	IssueEventEvidenceAttached          IssueEvent = "EvidenceAttached"
	IssueEventChecklistMismatchDetected IssueEvent = "ChecklistMismatchDetected"
	IssueEventChecklistSyncRequested    IssueEvent = "ChecklistSyncRequested"
	IssueEventChecklistSynced           IssueEvent = "ChecklistSynced"
	IssueEventCloseRequested            IssueEvent = "CloseRequested"
	IssueEventIssueClosed               IssueEvent = "IssueClosed"
	IssueEventHumanDecisionRequired     IssueEvent = "HumanDecisionRequired"
	IssueEventSyncFailed                IssueEvent = "SyncFailed"
)

// IssueTransition describes one allowed edge in the Issue FSM.
type IssueTransition struct {
	From  IssueState
	Event IssueEvent
	To    IssueState
}

var issueTransitionTable = []IssueTransition{
	{From: "", Event: IssueEventIssueLoaded, To: IssueStateDrafted},
	{From: IssueStateDrafted, Event: IssueEventIssuePlanned, To: IssueStatePlanned},
	{From: IssueStateBlocked, Event: IssueEventIssuePlanned, To: IssueStatePlanned},
	{From: IssueStatePlanned, Event: IssueEventHermesTaskStarted, To: IssueStateInProgress},
	{From: IssueStateInProgress, Event: IssueEventSubTaskCompleted, To: IssueStateEvidenceCollected},
	{From: IssueStateInProgress, Event: IssueEventReviewPassed, To: IssueStateEvidenceCollected},
	{From: IssueStateInProgress, Event: IssueEventEvidenceAttached, To: IssueStateEvidenceCollected},
	{From: IssueStateEvidenceCollected, Event: IssueEventChecklistMismatchDetected, To: IssueStateChecklistUnsynced},
	{From: IssueStateEvidenceCollected, Event: IssueEventChecklistSyncRequested, To: IssueStateChecklistUnsynced},
	{From: IssueStateChecklistUnsynced, Event: IssueEventChecklistSynced, To: IssueStateChecklistSynced},
	{From: IssueStateChecklistSynced, Event: IssueEventCloseRequested, To: IssueStateReadyToClose},
	{From: IssueStateReadyToClose, Event: IssueEventIssueClosed, To: IssueStateClosed},
	{From: IssueStateDrafted, Event: IssueEventHumanDecisionRequired, To: IssueStateBlocked},
	{From: IssueStatePlanned, Event: IssueEventHumanDecisionRequired, To: IssueStateBlocked},
	{From: IssueStateInProgress, Event: IssueEventHumanDecisionRequired, To: IssueStateBlocked},
	{From: IssueStateEvidenceCollected, Event: IssueEventHumanDecisionRequired, To: IssueStateBlocked},
	{From: IssueStateChecklistUnsynced, Event: IssueEventHumanDecisionRequired, To: IssueStateBlocked},
	{From: IssueStateChecklistSynced, Event: IssueEventHumanDecisionRequired, To: IssueStateBlocked},
	{From: IssueStateReadyToClose, Event: IssueEventHumanDecisionRequired, To: IssueStateBlocked},
	{From: IssueStateDrafted, Event: IssueEventReviewPartialOrFailed, To: IssueStateBlocked},
	{From: IssueStatePlanned, Event: IssueEventReviewPartialOrFailed, To: IssueStateBlocked},
	{From: IssueStateInProgress, Event: IssueEventReviewPartialOrFailed, To: IssueStateBlocked},
	{From: IssueStateEvidenceCollected, Event: IssueEventReviewPartialOrFailed, To: IssueStateBlocked},
	{From: IssueStateChecklistUnsynced, Event: IssueEventReviewPartialOrFailed, To: IssueStateBlocked},
	{From: IssueStateChecklistSynced, Event: IssueEventReviewPartialOrFailed, To: IssueStateBlocked},
	{From: IssueStateReadyToClose, Event: IssueEventReviewPartialOrFailed, To: IssueStateBlocked},
	{From: IssueStateDrafted, Event: IssueEventSyncFailed, To: IssueStateBlocked},
	{From: IssueStatePlanned, Event: IssueEventSyncFailed, To: IssueStateBlocked},
	{From: IssueStateInProgress, Event: IssueEventSyncFailed, To: IssueStateBlocked},
	{From: IssueStateEvidenceCollected, Event: IssueEventSyncFailed, To: IssueStateBlocked},
	{From: IssueStateChecklistUnsynced, Event: IssueEventSyncFailed, To: IssueStateBlocked},
	{From: IssueStateChecklistSynced, Event: IssueEventSyncFailed, To: IssueStateBlocked},
	{From: IssueStateReadyToClose, Event: IssueEventSyncFailed, To: IssueStateBlocked},
}

// IsTerminal reports whether the issue has reached a final state.
func (s IssueState) IsTerminal() bool {
	return s == IssueStateClosed
}

// NeedsHumanDecision reports whether the current issue state requires a human
// to unblock the workflow before IssueOps can continue.
func (s IssueState) NeedsHumanDecision() bool {
	return s == IssueStateBlocked
}

// CanAutoClose reports whether the issue may be closed automatically.
//
// The caller must still perform any higher-level GitHub write guard checks
// before mutating remote state. This method only answers the FSM-level readiness
// question.
func (r IssueCloseReadiness) CanAutoClose() bool {
	return r.State == IssueStateReadyToClose &&
		!r.HasBlockingLabel &&
		r.ChecklistSynced &&
		r.ReviewAccepted &&
		r.ValidationPassed &&
		!r.HasUncheckedRequiredItems
}

// IssueCloseReadiness captures the guards that must pass before IssueOps may
// auto-close an issue.
type IssueCloseReadiness struct {
	State                     IssueState
	HasBlockingLabel          bool
	ChecklistSynced           bool
	ReviewAccepted            bool
	ValidationPassed          bool
	HasUncheckedRequiredItems bool
}

// ValidIssueTransition reports whether the FSM can move from `from` to `to`
// when handling `event`.
func ValidIssueTransition(from IssueState, event IssueEvent, to IssueState) bool {
	for _, candidate := range issueTransitionTable {
		if candidate.From == from && candidate.Event == event && candidate.To == to {
			return true
		}
	}
	return false
}

// IssueStateFromGitHub normalizes a GitHub issue state or IssueOps state label
// into the closest IssueOps lifecycle state.
func IssueStateFromGitHub(raw string) IssueState {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "closed":
		return IssueStateClosed
	case "blocked":
		return IssueStateBlocked
	case "drafted":
		return IssueStateDrafted
	case "planned":
		return IssueStatePlanned
	case "in_progress":
		return IssueStateInProgress
	case "evidence_collected":
		return IssueStateEvidenceCollected
	case "checklist_unsynced":
		return IssueStateChecklistUnsynced
	case "checklist_synced":
		return IssueStateChecklistSynced
	case "ready_to_close":
		return IssueStateReadyToClose
	default:
		return IssueStateDrafted
	}
}

// IssueEventForState returns the event that best describes arriving at state.
func IssueEventForState(state IssueState) IssueEvent {
	switch state {
	case IssueStateClosed:
		return IssueEventIssueClosed
	case IssueStateReadyToClose:
		return IssueEventCloseRequested
	case IssueStateChecklistSynced:
		return IssueEventChecklistSynced
	case IssueStateChecklistUnsynced:
		return IssueEventChecklistMismatchDetected
	case IssueStateBlocked:
		return IssueEventHumanDecisionRequired
	case IssueStateEvidenceCollected:
		return IssueEventEvidenceAttached
	case IssueStateInProgress:
		return IssueEventHermesTaskStarted
	case IssueStatePlanned:
		return IssueEventIssuePlanned
	default:
		return IssueEventIssueLoaded
	}
}
