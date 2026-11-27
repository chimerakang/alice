package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// The commands a client may send for a task. The web API takes them as they
// are; Telegram buttons map onto them (see fleetCallbackOp).
const (
	fleetOpCancel         = "cancel"
	fleetOpRetry          = "retry"
	fleetOpReply          = "reply"
	fleetOpApprove        = "approve"
	fleetOpMerge          = "merge"
	fleetOpPass           = "pass"
	fleetOpDiscard        = "discard"
	fleetOpFinishEvidence = "finish_evidence"
)

// Why a command is not possible now.
const (
	fleetWhyStatus           = "status"              // the task's status does not take it
	fleetWhyNotShip          = "not_ship"            // a scout task lands nothing
	fleetWhyNoPullRequest    = "no_pull_request"     // the trust mode lands through a PR that does not exist
	fleetWhyNoApprovalStep   = "no_approval_step"    // direct-pr is landed by its pull request, not by approval
	fleetWhyItemsOpen        = "items_not_approved"  // acceptance items are still open
	fleetWhyDeliveryPending  = "delivery_incomplete" // the project's delivery flow is not complete
	fleetWhyAwaitingExternal = "awaiting_external"   // an item waits on an outside answer
	fleetWhyAlreadyLanded    = "already_landed"      // the pull request was merged already
)

// fleetActionBlock is why one command cannot be carried out.
type fleetActionBlock struct {
	Action string
	Reason string
	Items  []string
}

// fleetActionSet is what a task allows right now. It is the one source the
// web dashboard and the Telegram buttons draw from, and the check every
// command goes through before it runs.
type fleetActionSet struct {
	Allowed []string
	Blocked []fleetActionBlock
}

func (s fleetActionSet) Can(action string) bool {
	for _, a := range s.Allowed {
		if a == action {
			return true
		}
	}
	return false
}

// Why says why action is not allowed. An action the task's status never
// offers is blocked by the status.
func (s fleetActionSet) Why(action string) fleetActionBlock {
	for _, b := range s.Blocked {
		if b.Action == action {
			return b
		}
	}
	return fleetActionBlock{Action: action, Reason: fleetWhyStatus}
}

// fleetActionError is a command refused because the task does not allow it.
type fleetActionError struct {
	Block  fleetActionBlock
	Status fleet.Status
}

var errFleetActionUnavailable = errors.New("fleet action unavailable")

func (e *fleetActionError) Error() string {
	msg := fmt.Sprintf("%s is not possible for a %s task: %s", e.Block.Action, e.Status, e.Block.Reason)
	if len(e.Block.Items) > 0 {
		msg += " (" + strings.Join(e.Block.Items, ", ") + ")"
	}
	return msg
}

func (e *fleetActionError) Is(t error) bool { return t == errFleetActionUnavailable }

// fleetActionInput is everything the rules look at, gathered beforehand so
// the rules themselves are pure.
type fleetActionInput struct {
	Task fleet.Task
	// Accept is the task's folded acceptance state.
	Accept acceptState
	// EvidenceOnly: the task changed no file and is closed on its evidence.
	EvidenceOnly bool
	// Landed: the task's pull request has been merged by the fleet.
	Landed bool
}

// fleetTaskActions applies the rules. The order of Allowed is the order
// buttons are shown in.
func fleetTaskActions(in fleetActionInput) fleetActionSet {
	task := in.Task
	var s fleetActionSet
	allow := func(a ...string) { s.Allowed = append(s.Allowed, a...) }
	block := func(action, reason string, items []string) {
		s.Blocked = append(s.Blocked, fleetActionBlock{Action: action, Reason: reason, Items: items})
	}
	retry := func() {
		if in.Accept.ExternalWait() {
			block(fleetOpRetry, fleetWhyAwaitingExternal, nil)
			return
		}
		allow(fleetOpRetry)
	}

	switch task.Status {
	case fleet.StatusQueued, fleet.StatusRunning, fleet.StatusVerifying:
		allow(fleetOpCancel)
	case fleet.StatusReviewReady:
		if in.EvidenceOnly && in.Accept.Structured && task.PRNumber == 0 && task.TrustMode.NeedsRemote() {
			block(fleetOpApprove, fleetWhyNoPullRequest, nil)
			gate := evaluateAcceptGate(in.Accept, true)
			if gate.OK {
				allow(fleetOpFinishEvidence)
			} else {
				block(fleetOpFinishEvidence, fleetWhyItemsOpen, gate.Open)
			}
		} else if b, ok := fleetApproveBlock(in); ok {
			block(fleetOpApprove, b.Reason, b.Items)
		} else {
			allow(fleetOpApprove)
		}
		if task.TrustMode == fleet.TrustDirectPR && task.PRNumber > 0 {
			gate := evaluateAcceptGate(in.Accept, task.PRURL != "")
			switch {
			case gate.OK:
				allow(fleetOpMerge)
			case gate.Code == acceptGateItemsOpen:
				block(fleetOpMerge, fleetWhyItemsOpen, gate.Open)
			default:
				block(fleetOpMerge, fleetWhyDeliveryPending, nil)
			}
		}
		allow(fleetOpReply, fleetOpDiscard)
	case fleet.StatusNeedsDecision:
		allow(fleetOpReply)
		retry()
		allow(fleetOpPass, fleetOpDiscard)
	case fleet.StatusFailed:
		retry()
		allow(fleetOpReply, fleetOpDiscard)
	case fleet.StatusCancelled:
		allow(fleetOpDiscard) // Cancelled checkouts still need an explicit cleanup action.
	case fleet.StatusDone:
		// A direct-pr task is done once its pull request is open; merging
		// it is still the owner's call.
		if task.TrustMode == fleet.TrustDirectPR && task.PRNumber > 0 {
			if in.Landed {
				block(fleetOpMerge, fleetWhyAlreadyLanded, nil)
			} else if gate := evaluateAcceptGate(in.Accept, true); !gate.OK {
				block(fleetOpMerge, fleetWhyItemsOpen, gate.Open)
			} else {
				allow(fleetOpMerge)
			}
		}
	}
	return s
}

// fleetApproveBlock says why a review_ready task cannot be approved, if it
// cannot. It follows FleetService.Approve and Deliverer.Approve.
func fleetApproveBlock(in fleetActionInput) (fleetActionBlock, bool) {
	task := in.Task
	if task.Shape != fleet.ShapeShip {
		return fleetActionBlock{Reason: fleetWhyNotShip}, true
	}
	// Evidence-only delivery does not waive the project's PR workflow.
	switch task.TrustMode {
	case fleet.TrustLocalOnly:
	case fleet.TrustNoMistakes:
		if task.PRNumber == 0 {
			return fleetActionBlock{Reason: fleetWhyNoPullRequest}, true
		}
	case fleet.TrustDirectPR:
		if task.PRNumber == 0 {
			return fleetActionBlock{Reason: fleetWhyNoPullRequest}, true
		}
		return fleetActionBlock{Reason: fleetWhyNoApprovalStep}, true
	default:
		return fleetActionBlock{Reason: fleetWhyNoApprovalStep}, true
	}
	delivered := !task.TrustMode.NeedsRemote() || task.PRURL != ""
	gate := evaluateAcceptGate(in.Accept, delivered)
	switch {
	case gate.OK:
		return fleetActionBlock{}, false
	case gate.Code == acceptGateItemsOpen:
		return fleetActionBlock{Reason: fleetWhyItemsOpen, Items: gate.Open}, true
	default:
		return fleetActionBlock{Reason: fleetWhyDeliveryPending}, true
	}
}

// TaskActions works out what a task allows right now.
func (f *FleetService) TaskActions(ctx context.Context, task fleet.Task) fleetActionSet {
	in := fleetActionInput{Task: task}
	switch task.Status {
	case fleet.StatusReviewReady, fleet.StatusNeedsDecision, fleet.StatusFailed:
		st, err := f.Acceptance(ctx, task.ID)
		if err != nil {
			return fleetActionSet{Blocked: []fleetActionBlock{{Action: fleetOpApprove, Reason: fleetWhyDeliveryPending}}}
		}
		in.Accept = st
		if task.Status == fleet.StatusReviewReady && task.PRNumber == 0 && in.Accept.Structured && task.TrustMode.NeedsRemote() {
			in.EvidenceOnly = f.noChanges(ctx, task)
		}
	case fleet.StatusDone:
		if task.TrustMode == fleet.TrustDirectPR && task.PRNumber > 0 && f.Store != nil {
			st, err := f.Acceptance(ctx, task.ID)
			if err != nil {
				return fleetActionSet{Blocked: []fleetActionBlock{{Action: fleetOpMerge, Reason: fleetWhyDeliveryPending}}}
			}
			in.Accept = st
			ev, ok, err := f.Store.LastEvent(ctx, task.ID, fleet.EventDeliveryApproved)
			in.Landed = err == nil && ok && ev.ID > 0
		}
	}
	return fleetTaskActions(in)
}

// CheckAction refuses a command the task does not allow now, so a stale
// button cannot act. It returns the task as it stands.
func (f *FleetService) CheckAction(ctx context.Context, taskID, action string) (fleet.Task, fleetActionSet, error) {
	task, err := f.Store.GetTask(ctx, taskID)
	if err != nil {
		return task, fleetActionSet{}, err
	}
	set := f.TaskActions(ctx, task)
	if !set.Can(action) {
		return task, set, &fleetActionError{Block: set.Why(action), Status: task.Status}
	}
	return task, set, nil
}

// fleetCallbackOp is the command a Telegram button stands for, or "" for a
// button that acts on nothing (going back from a confirmation).
func fleetCallbackOp(action string) string {
	switch action {
	case fleetActCancel:
		return fleetOpCancel
	case fleetActRetry:
		return fleetOpRetry
	case fleetActApprove:
		return fleetOpApprove
	case fleetOpFinishEvidence:
		return fleetOpFinishEvidence
	case fleetActMerge:
		return fleetOpMerge
	case fleetActPass:
		return fleetOpPass
	case fleetActDiscard, fleetActDiscardConfirm, fleetActPromptDiscard:
		return fleetOpDiscard
	case fleetActGoOn, fleetActHint:
		return fleetOpReply
	}
	return ""
}

// fleetFilterButtons drops the buttons for commands the task does not allow
// now, and any row left empty. Links and buttons that act on nothing stay.
func fleetFilterButtons(rows [][]fleetButton, set fleetActionSet) [][]fleetButton {
	var out [][]fleetButton
	for _, row := range rows {
		var kept []fleetButton
		for _, b := range row {
			if b.Data != "" {
				if action, _, ok := parseFleetCallback(b.Data); ok {
					if op := fleetCallbackOp(action); op != "" && !set.Can(op) {
						continue
					}
				}
			}
			kept = append(kept, b)
		}
		if len(kept) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// fleetBlockVars are the template variables that say why a command is not
// possible: {action} and {reason}.
func fleetBlockVars(loc func(string, map[string]string) string, b fleetActionBlock, status fleet.Status) map[string]string {
	reason := loc("fleet_why_"+b.Reason, map[string]string{
		"{status}": loc("fleet_status_"+string(status), nil),
		"{items}":  fleetClip(strings.Join(b.Items, ", "), 80),
	})
	return map[string]string{"{action}": loc("fleet_op_"+b.Action, nil), "{reason}": reason}
}

// CheckActorAction retains the audit trail when an adapter refuses a stale
// command before Commander runs. The core currently exposes audit reads only;
// use the shared audit schema rather than invoking a refused command for its
// audit side effect (which could accidentally perform the command).
func (f *FleetService) CheckActorAction(ctx context.Context, actor fleet.Actor, taskID, action string) (fleet.Task, fleetActionSet, error) {
	task, set, err := f.CheckAction(ctx, taskID, action)
	if err != nil && task.ID != "" && f.Accept != nil {
		_, auditErr := f.Accept.db.ExecContext(ctx, `INSERT INTO fleet_audit
   (actor,via,command,project_id,task_id,detail,ok,error,created_at)
   VALUES (?,?,?,?,?,?,?,?,?)`, actor.ID, actor.Via, action, task.ProjectID, taskID, "", false, err.Error(), time.Now().UnixMilli())
		if auditErr != nil {
			log.Printf("[fleet] record refused action audit: %v", auditErr)
		}
	}
	return task, set, err
}
