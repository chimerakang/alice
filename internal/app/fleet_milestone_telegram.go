package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/chimerakang/alice-core/fleet"
)

// bindingMilestoneAuto, set on a project, lets /ship #n take on the whole
// milestone without asking first.
const (
	bindingMilestoneAuto = "milestone_auto"
	bindingOn            = "on"
)

// Buttons of the confirmation a batch asks for.
const (
	fleetActBatchGo  = "msgo"
	fleetActBatchOne = "msone"
)

// A Telegram message holds 4096 characters.
const fleetBatchSummaryLimit = 3500

// attachMilestones builds the batch runner on the service's database.
func (f *FleetService) attachMilestones(db *sql.DB) error {
	batches, err := newMilestoneBatches(db, f.Store, f.Config.Milestone)
	if err != nil {
		return err
	}
	batches.dispatch = func(ctx context.Context, actor fleet.Actor, req fleet.DispatchRequest) (fleet.Task, error) {
		return fleetCommanderDispatch(ctx, f, actor, req)
	}
	batches.gate = fleetMilestoneGate
	epicLabels, epicPattern := resolvedEpicLabels(f.Config.EpicLabels), compileEpicPattern(f.Config.EpicTitlePattern)
	batches.isEpic = func(labels []string, title string) bool { return isEpicIssue(labels, title, epicLabels, epicPattern) }
	f.Milestones = batches
	return nil
}

// fleetMilestoneGate applies /ship's issue quality check to a batch's issue.
func fleetMilestoneGate(ctx context.Context, project fleet.Project, number int) (string, error) {
	issue, err := fleetFetchIssue(ctx, project.RepoPath, number)
	if err != nil {
		return "", fmt.Errorf("read issue #%d: %w", number, err)
	}
	switch decision := decideIssueQualityGate(issue, false); decision.Action {
	case issueQualityGateSkip, issueQualityGateNeedsClarification:
		return decision.Reason, nil
	}
	return "", nil
}

// attachFleetMilestones posts a card for every task a batch queues, in the
// chat the batch was started from.
func (t *TelegramBot) attachFleetMilestones(svc *FleetService) {
	svc.Milestones.OnDispatch(func(ctx context.Context, batch milestoneBatch, task fleet.Task) {
		if batch.ChatID != 0 {
			t.fleetPostCard(ctx, chatKey{chatID: batch.ChatID, threadID: batch.ThreadID}, task)
		}
	})
}

// fleetMilestoneShip handles /ship #n for an issue that belongs to a
// milestone. It reports whether the command has been dealt with; when not,
// the caller dispatches the single issue as usual.
//
// Sending the command again leads back to the same batch and queues nothing
// twice. The one exception is an issue the batch has stopped working on, which
// can be sent again by hand.
func (t *TelegramBot) fleetMilestoneShip(ctx context.Context, key chatKey, userID int64, project fleet.Project, args fleetDispatchArgs) bool {
	batches := t.fleet.Milestones
	if batches == nil {
		return false
	}
	fail := func(err error) bool {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return true
	}

	// Look in what is stored first: it answers when GitHub cannot.
	batch, err := batches.ByIssue(ctx, project.ID, args.Issue)
	var issue milestoneIssue
	if errors.Is(err, errBatchNotFound) {
		issue, err = batches.Host.Issue(ctx, project.RepoPath, args.Issue)
		if err != nil {
			// The single dispatch reads the issue itself and reports a
			// failure to do so.
			log.Printf("[fleet] milestone of #%d: %v", args.Issue, err)
			return false
		}
		if issue.Milestone == 0 {
			return false
		}
		batch, err = batches.ByMilestone(ctx, project.ID, issue.Milestone)
	}

	switch {
	case errors.Is(err, errBatchNotFound):
		return t.fleetMilestoneNew(ctx, key, userID, project, issue, args)
	case err != nil:
		return fail(err)
	case batch.Status == batchDeclined:
		// Authorising the project afterwards takes the milestone up again.
		if !t.fleetMayTakeOver(project, userID) {
			return false
		}
		if err := batches.Reopen(ctx, batch.ID); err != nil {
			return fail(err)
		}
		batch.Status = batchActive
	case batch.Status == batchProposed:
		t.fleetAskBatch(key, batch)
		return true
	}

	_, reconcileErr := batches.Reconcile(ctx, batch.ID)
	items, err := batches.Items(ctx, batch.ID)
	if err != nil {
		return fail(err)
	}
	for _, it := range items {
		if it.Issue == args.Issue && (it.State == itemHeld || it.State == itemLanded) {
			return false
		}
	}
	t.fleetBatchSummary(key, batch, items, reconcileErr)
	return true
}

// fleetMilestoneNew starts a batch, or asks first when the project has not
// authorised that.
func (t *TelegramBot) fleetMilestoneNew(ctx context.Context, key chatKey, userID int64, project fleet.Project, issue milestoneIssue, args fleetDispatchArgs) bool {
	batches := t.fleet.Milestones
	all, err := batches.Host.MilestoneIssues(ctx, project.RepoPath, issue.Milestone)
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return true
	}
	others := 0
	for _, other := range all {
		if other.Number != issue.Number && !other.closed() &&
			!batches.isEpic(other.Labels, other.Title) && !hasAnyLabel(other.Labels, batches.Config.ExcludeLabels) {
			others++
		}
	}
	// With nothing else to do there is no batch to speak of.
	if others == 0 {
		return false
	}

	status := batchProposed
	if t.fleetMayTakeOver(project, userID) {
		status = batchActive
	}
	batch, err := batches.Create(ctx, milestoneBatch{
		ProjectID: project.ID, Milestone: issue.Milestone, MilestoneTitle: issue.MilestoneTitle,
		TargetBranch: project.DefaultBranch, Status: status, EntryIssue: issue.Number,
		CreatedBy: strconv.FormatInt(userID, 10), ChatID: key.chatID, ThreadID: key.threadID,
		Model: args.Model, BudgetUSD: args.BudgetUSD,
	})
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return true
	}
	if batch.Status != batchActive {
		t.fleetAskBatch(key, batch)
		return true
	}
	t.fleetStartBatch(ctx, key, batch)
	return true
}

// fleetMayTakeOver reports whether a milestone may be taken on unasked.
func (t *TelegramBot) fleetMayTakeOver(project fleet.Project, userID int64) bool {
	return project.Bindings[bindingMilestoneAuto] == bindingOn && t.fleetIsOwner(userID)
}

func (t *TelegramBot) fleetStartBatch(ctx context.Context, key chatKey, batch milestoneBatch) {
	_, reconcileErr := t.fleet.Milestones.Reconcile(ctx, batch.ID)
	items, err := t.fleet.Milestones.Items(ctx, batch.ID)
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.fleetBatchSummary(key, batch, items, reconcileErr)
}

func (t *TelegramBot) fleetAskBatch(key chatKey, batch milestoneBatch) {
	text := t.fleetMsg(key.chatID, "fleet_batch_confirm", map[string]string{
		"{title}":  batch.MilestoneTitle,
		"{issue}":  strconv.Itoa(batch.EntryIssue),
		"{branch}": batch.TargetBranch,
	})
	buttons := [][]fleetButton{
		{{Text: t.fleetMsg(key.chatID, "fleet_batch_btn_all", nil), Data: fleetCallbackData(fleetActBatchGo, batch.ID)}},
		{{Text: t.fleetMsg(key.chatID, "fleet_batch_btn_one", map[string]string{"{issue}": strconv.Itoa(batch.EntryIssue)}), Data: fleetCallbackData(fleetActBatchOne, batch.ID)}},
	}
	var messenger fleetMessenger = t
	if t.fleetCards != nil && t.fleetCards.Messenger != nil {
		messenger = t.fleetCards.Messenger
	}
	if _, err := messenger.SendCard(key, text, buttons); err != nil {
		log.Printf("[fleet] ask about milestone batch %s: %v", batch.ID, err)
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
	}
}

// handleFleetBatchCallback answers the batch confirmation.
func (t *TelegramBot) handleFleetBatchCallback(key chatKey, userID int64, queryID, action, batchID string) {
	answer := func(msgKey string) {
		t.answerCallbackQuery(queryID, fleetClip(t.fleetMsg(key.chatID, msgKey, nil), 190))
	}
	if t.fleet == nil || t.fleet.Milestones == nil {
		answer("fleet_disabled")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetDispatchTimeout)
	defer cancel()
	batch, err := t.fleet.Milestones.Get(ctx, batchID)
	if err != nil || batch.ChatID != key.chatID {
		answer("fleet_cb_unknown")
		return
	}
	if !t.fleetIsOwner(userID) {
		answer("fleet_cb_owner_only")
		return
	}
	status := batchActive
	if action == fleetActBatchOne {
		status = batchDeclined
	}
	changed, err := t.fleet.Milestones.Resolve(ctx, batch.ID, status)
	if err != nil {
		t.answerCallbackQuery(queryID, fleetClip(err.Error(), 190))
		return
	}
	if !changed {
		answer("fleet_batch_already_answered")
		return
	}
	t.answerCallbackQuery(queryID, "")
	batch.Status = status
	if status == batchActive {
		t.fleetStartBatch(ctx, key, batch)
		return
	}
	project, err := t.fleet.Store.GetProject(ctx, batch.ProjectID)
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.fleetDispatchOne(ctx, key, userID, fleet.ShapeShip, project,
		fleetDispatchArgs{Issue: batch.EntryIssue, Model: batch.Model, BudgetUSD: batch.BudgetUSD})
}

// fleetBatchSummary says where every issue of the batch stands.
func (t *TelegramBot) fleetBatchSummary(key chatKey, batch milestoneBatch, items []milestoneItem, reconcileErr error) {
	msg := func(k string, v map[string]string) string { return t.fleetMsg(key.chatID, k, v) }
	t.send(key, fleetBatchText(msg, batch, items, reconcileErr))
}

func fleetBatchText(msg func(string, map[string]string) string, batch milestoneBatch, items []milestoneItem, reconcileErr error) string {
	var sb strings.Builder
	sb.WriteString(msg("fleet_batch_header", map[string]string{"{title}": batch.MilestoneTitle, "{branch}": batch.TargetBranch}))
	if reconcileErr != nil {
		sb.WriteString("\n" + msg("fleet_batch_incomplete", map[string]string{"{error}": fleetClip(reconcileErr.Error(), 300)}))
	}
	for _, it := range items {
		line := fmt.Sprintf("#%d %s", it.Issue, msg("fleet_batch_state_"+it.State, nil))
		if it.Reason != "" {
			refs := make([]string, 0, len(it.Waiting))
			for _, n := range it.Waiting {
				refs = append(refs, "#"+strconv.Itoa(n))
			}
			line += "｜" + msg("fleet_batch_reason_"+it.Reason, map[string]string{
				"{issues}": strings.Join(refs, " "),
				"{detail}": fleetClip(it.Detail, 120),
			})
		}
		if it.TaskID != "" && (it.State == itemActive || it.State == itemAttention) {
			line += " [" + it.TaskID + "]"
		}
		line += " " + fleetClip(it.Title, 60)
		if sb.Len()+len(line) > fleetBatchSummaryLimit {
			sb.WriteString("\n…")
			break
		}
		sb.WriteString("\n" + line)
	}
	return sb.String()
}

// fleetSetProjectMilestone handles /project milestone <auto|ask>.
func (t *TelegramBot) fleetSetProjectMilestone(ctx context.Context, key chatKey, userID int64, args []string) {
	usage := func() { t.send(key, t.fleetMsg(key.chatID, "fleet_project_milestone_usage", nil)) }
	if len(args) != 1 || (args[0] != "auto" && args[0] != "ask") {
		usage()
		return
	}
	if !t.fleetIsOwner(userID) {
		t.send(key, t.fleetMsg(key.chatID, "fleet_cb_owner_only", nil))
		return
	}
	project, ok := t.fleetRequireProject(ctx, key)
	if !ok {
		return
	}
	project, err := t.fleet.Commander.UpdateProject(ctx, fleetActor(userID), project.ID, "milestone takeover: "+args[0],
		func(p *fleet.Project) error {
			if args[0] == "auto" {
				p.Bindings[bindingMilestoneAuto] = bindingOn
			} else {
				delete(p.Bindings, bindingMilestoneAuto)
			}
			return nil
		})
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_project_milestone_"+args[0], map[string]string{"{name}": project.Name}))
}
