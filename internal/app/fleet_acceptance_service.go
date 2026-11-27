package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/ghissue"
)

// acceptStore is the append-only log of per-item acceptance, in the fleet's
// database. It is alice's own table: the fleet package knows nothing of it.
type acceptStore struct{ db *sql.DB }

func newAcceptStore(db *sql.DB) (*acceptStore, error) {
	if db == nil {
		return nil, errors.New("acceptance log needs a database")
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS fleet_acceptance_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			run_no INTEGER NOT NULL DEFAULT 0,
			actor TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fleet_acceptance_task ON fleet_acceptance_log(task_id, id)`,
		// One agent report per run, however often the log is read.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_acceptance_run ON fleet_acceptance_log(task_id, run_no) WHERE kind = 'report' AND actor = 'agent'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("fleet acceptance schema: %w", err)
		}
	}
	return &acceptStore{db: db}, nil
}

// append stores one entry. A duplicate agent report for a run is ignored.
func (s *acceptStore) append(ctx context.Context, taskID, kind string, runNo int, actor string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO fleet_acceptance_log (task_id, kind, run_no, actor, payload, created_at) VALUES (?,?,?,?,?,?)`,
		taskID, kind, runNo, actor, string(raw), time.Now().UnixMilli())
	return err
}

func (s *acceptStore) list(ctx context.Context, taskID string) ([]acceptLogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, kind, run_no, actor, payload, created_at FROM fleet_acceptance_log WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []acceptLogEntry
	for rows.Next() {
		var e acceptLogEntry
		var payload string
		var ms int64
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Kind, &e.RunNo, &e.Actor, &payload, &ms); err != nil {
			return nil, err
		}
		e.Payload, e.CreatedAt = json.RawMessage(payload), time.UnixMilli(ms)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Errors the web layer maps onto statuses.
var (
	errAcceptInvalid  = errors.New("acceptance: invalid")
	errAcceptConflict = errors.New("acceptance: conflict")
)

func acceptInvalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errAcceptInvalid, fmt.Sprintf(format, a...))
}

// acceptGateError is returned when closing is refused.
type acceptGateError struct{ Gate acceptGate }

func (e *acceptGateError) Error() string   { return "acceptance gate: " + e.Gate.Message }
func (e *acceptGateError) Is(t error) bool { return t == errAcceptConflict }

// Acceptance returns the folded state, after taking in any run's report not
// yet recorded.
func (f *FleetService) Acceptance(ctx context.Context, taskID string) (acceptState, error) {
	if f.Accept == nil {
		return acceptState{ReportedRuns: map[int]bool{}}, nil
	}
	entries, err := f.Accept.list(ctx, taskID)
	if err != nil {
		return acceptState{}, err
	}
	st := foldAcceptance(entries)
	if !st.Structured {
		return st, nil
	}
	events, err := f.taskEvents(ctx, taskID)
	if err != nil {
		return st, err
	}
	fresh := false
	for i, run := range deliveryRuns(events, nil) {
		n := i + 1
		if st.ReportedRuns[n] || strings.TrimSpace(run.final) == "" {
			continue
		}
		items, errs, found := parseAcceptanceReport(run.final, func(id string) bool { _, ok := st.item(id); return ok })
		if !found {
			continue
		}
		rep := acceptReport{Run: n, Source: "agent", Items: items, Errors: errs}
		if err := f.Accept.append(ctx, taskID, acceptKindReport, n, "agent", rep); err != nil {
			return st, err
		}
		// Later runs are validated against the criteria as the earlier
		// reports left them, so refold between runs.
		entries, err = f.Accept.list(ctx, taskID)
		if err != nil {
			return st, err
		}
		st, fresh = foldAcceptance(entries), true
	}
	_ = fresh
	return st, nil
}

func (f *FleetService) taskEvents(ctx context.Context, taskID string) ([]fleet.Event, error) {
	var all []fleet.Event
	var after int64
	for page := 0; page < 100; page++ {
		events, next, more, err := f.Commander.TaskEvents(ctx, taskID, after, 1000)
		if err != nil {
			return nil, err
		}
		all = append(all, events...)
		after = next
		if !more {
			break
		}
	}
	return all, nil
}

// openCriteria is the issue's open acceptance items, as the fleet dispatches
// them: unchecked and under an acceptance heading.
func openCriteria(issue *ghissue.IssueContext) []acceptCriterion {
	items := []acceptCriterion{}
	for _, c := range issue.Checklist {
		if c.Checked || !ghissue.IsAcceptanceSection(c.Section) {
			continue
		}
		items = append(items, acceptCriterion{ID: c.ID, Text: c.Text})
	}
	return items
}

// SeedCriteria records version 1 of a task's criteria.
func (f *FleetService) SeedCriteria(ctx context.Context, taskID, source string, items []acceptCriterion) error {
	if f.Accept == nil || len(items) == 0 {
		return nil
	}
	return f.Accept.append(ctx, taskID, acceptKindCriteria, 0, "system",
		acceptCriteriaEntry{Version: 1, Source: source, Items: items})
}

// DispatchIssue queues a ship or scout task for an issue like the fleet's
// own dispatcher, and also keeps the criteria with their IDs and tells the
// agent how to report on them. Anything but an issue goes to the Commander.
func (f *FleetService) DispatchIssue(ctx context.Context, actor fleet.Actor, req fleet.DispatchRequest) (fleet.Task, error) {
	if req.Issue <= 0 || f.Accept == nil {
		return f.Commander.Dispatch(ctx, actor, req)
	}
	if strings.TrimSpace(actor.ID) == "" {
		return fleet.Task{}, fleet.ErrNoActor
	}
	project, err := f.Store.GetProject(ctx, req.ProjectID)
	if err != nil {
		return fleet.Task{}, err
	}
	issue, err := f.issueFetcher().Fetch(ctx, project.RepoPath, req.Issue)
	if err != nil {
		return fleet.Task{}, fmt.Errorf("fleet: load issue #%d: %w", req.Issue, err)
	}
	if strings.EqualFold(strings.TrimSpace(issue.State), "closed") {
		return fleet.Task{}, fmt.Errorf("%w: #%d", fleet.ErrIssueClosed, req.Issue)
	}
	busy, err := f.Store.ListTasks(ctx, fleet.TaskFilter{ProjectID: req.ProjectID, Statuses: []fleet.Status{
		fleet.StatusQueued, fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady, fleet.StatusNeedsDecision,
	}})
	if err != nil {
		return fleet.Task{}, err
	}
	for _, o := range busy {
		if o.Source.Kind == fleet.SourceIssue && o.Source.IssueNumber == req.Issue {
			return fleet.Task{}, fmt.Errorf("%w: #%d is being handled by task %s (%s)", fleet.ErrIssueBusy, req.Issue, o.ID, o.Status)
		}
	}
	criteria := openCriteria(issue)
	task, err := f.Store.CreateTask(ctx, fleet.NewTask{
		ProjectID: req.ProjectID, Shape: req.Shape, Model: req.Model, Priority: req.Priority, Budget: req.Budget,
		Goal:   acceptanceGoal(req.Issue, issue, criteria),
		Source: fleet.Source{Kind: fleet.SourceIssue, IssueNumber: req.Issue},
	})
	if err != nil {
		return fleet.Task{}, err
	}
	items := make([]map[string]string, 0, len(criteria))
	for _, c := range criteria {
		items = append(items, map[string]string{"id": c.ID, "text": c.Text})
	}
	_, _ = f.Store.AppendEvent(ctx, task.ID, fleet.EventIssueLinked, map[string]any{"number": req.Issue, "title": issue.Title, "items": items})
	if err := f.SeedCriteria(ctx, task.ID, "dispatch", criteria); err != nil {
		return task, err
	}
	f.Scheduler.Kick()
	return task, nil
}

func acceptanceGoal(number int, issue *ghissue.IssueContext, items []acceptCriterion) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Work on GitHub issue #%d: %s\n", number, strings.TrimSpace(issue.Title))
	if body := strings.TrimSpace(issue.Body); body != "" {
		sb.WriteString("\n" + body + "\n")
	}
	if len(items) == 0 {
		sb.WriteString("\nDo not edit the issue yourself.")
		return sb.String()
	}
	sb.WriteString("\nAcceptance items still open. Each has an ID:\n\n")
	ids := []string{}
	for i, it := range items {
		fmt.Fprintf(&sb, "- [%s] %s\n", it.ID, it.Text)
		if i < 2 {
			ids = append(ids, it.ID)
		}
	}
	sb.WriteString("\nComplete these. Items already checked in the issue are done; do not redo them.\n\n")
	sb.WriteString("End your final message with one line naming the items you fully completed, in exactly this form:\n\n")
	fmt.Fprintf(&sb, "Completed items: %s\n\n", strings.Join(ids, ", "))
	sb.WriteString("List an item only if it is completely done. If you completed none, write \"Completed items: none\". ")
	sb.WriteString("Do not edit the issue yourself; its checklist is updated for you once your work has been verified.\n\n")
	sb.WriteString(acceptReportInstructions)
	return sb.String()
}

// RefreshCriteria reads the issue again. When its acceptance items differ
// from the ones on record it files a proposal; nothing changes until a
// reviewer applies it.
func (f *FleetService) RefreshCriteria(ctx context.Context, actor fleet.Actor, taskID string) (acceptState, error) {
	task, err := f.Store.GetTask(ctx, taskID)
	if err != nil {
		return acceptState{}, err
	}
	st, err := f.Acceptance(ctx, taskID)
	if err != nil {
		return st, err
	}
	if !st.Structured {
		return st, acceptInvalid("task has no criteria on record")
	}
	if task.Source.Kind != fleet.SourceIssue {
		return st, acceptInvalid("task is not from an issue")
	}
	project, err := f.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return st, err
	}
	issue, err := f.issueFetcher().Fetch(ctx, project.RepoPath, task.Source.IssueNumber)
	if err != nil {
		return st, fmt.Errorf("read issue #%d: %w", task.Source.IssueNumber, err)
	}
	var current []acceptCriterion
	for _, it := range st.active() {
		current = append(current, acceptCriterion{ID: it.ID, Text: it.Text})
	}
	merged, diff := reconcileCriteria(current, openCriteria(issue))
	if diff.empty() {
		if st.Proposal != nil {
			_ = f.Accept.append(ctx, taskID, acceptKindDismissed, 0, actor.ID, map[string]string{"reason": "issue matches the criteria again"})
		}
		return f.Acceptance(ctx, taskID)
	}
	if err := f.Accept.append(ctx, taskID, acceptKindProposal, 0, actor.ID,
		acceptProposal{Version: st.Version + 1, Items: merged, Diff: diff}); err != nil {
		return st, err
	}
	return f.Acceptance(ctx, taskID)
}

// ResolveCriteria applies or dismisses the pending proposal.
func (f *FleetService) ResolveCriteria(ctx context.Context, actor fleet.Actor, taskID string, apply bool) (acceptState, error) {
	st, err := f.Acceptance(ctx, taskID)
	if err != nil {
		return st, err
	}
	if st.Proposal == nil {
		return st, fmt.Errorf("%w: no criteria update is waiting", errAcceptConflict)
	}
	if apply {
		err = f.Accept.append(ctx, taskID, acceptKindCriteria, 0, actor.ID,
			acceptCriteriaEntry{Version: st.Proposal.Version, Source: "issue", Items: st.Proposal.Items})
	} else {
		err = f.Accept.append(ctx, taskID, acceptKindDismissed, 0, actor.ID, map[string]string{})
	}
	if err != nil {
		return st, err
	}
	return f.Acceptance(ctx, taskID)
}

var acceptDecisions = map[string]bool{
	acceptDecisionPending: true, acceptDecisionApproved: true, acceptDecisionNeeds: true, acceptDecisionExternal: true,
}

// Decide records a reviewer's call on one item.
func (f *FleetService) Decide(ctx context.Context, actor fleet.Actor, taskID, itemID, decision, note string) (acceptState, error) {
	task, err := f.Store.GetTask(ctx, taskID)
	if err != nil {
		return acceptState{}, err
	}
	st, err := f.Acceptance(ctx, taskID)
	if err != nil {
		return st, err
	}
	itemID, note = strings.ToLower(strings.TrimSpace(itemID)), strings.TrimSpace(note)
	it, ok := st.item(itemID)
	switch {
	case !st.Structured:
		return st, acceptInvalid("task has no criteria on record; add them first")
	case !ok:
		return st, acceptInvalid("unknown item %q", itemID)
	case !acceptDecisions[decision]:
		return st, acceptInvalid("unknown decision %q", decision)
	case task.Status != fleet.StatusReviewReady && task.Status != fleet.StatusNeedsDecision:
		return st, fmt.Errorf("%w: task is %s; items can be reviewed when it awaits the owner", errAcceptConflict, task.Status)
	case decision == acceptDecisionApproved && it.Report != acceptReportEvidence:
		return st, acceptInvalid("item %s has no submitted evidence to approve", itemID)
	case (decision == acceptDecisionNeeds || decision == acceptDecisionExternal) && note == "":
		return st, acceptInvalid("a note is required: say what is missing")
	}
	if err := f.Accept.append(ctx, taskID, acceptKindDecision, 0, actor.ID,
		acceptDecision{ItemID: itemID, Decision: decision, Note: note}); err != nil {
		return st, err
	}
	return f.Acceptance(ctx, taskID)
}

// ManualEntry lets a reviewer record what the agent did not report: for a
// task with no criteria on record, the criteria themselves (Criteria); then
// the evidence, blockers or replies from outside (Items).
func (f *FleetService) ManualEntry(ctx context.Context, actor fleet.Actor, taskID string, criteria []acceptCriterion, items []acceptReportItem) (acceptState, error) {
	if _, err := f.Store.GetTask(ctx, taskID); err != nil {
		return acceptState{}, err
	}
	st, err := f.Acceptance(ctx, taskID)
	if err != nil {
		return st, err
	}
	if !st.Structured {
		seen := map[string]bool{}
		for i := range criteria {
			criteria[i].ID, criteria[i].Text = strings.ToLower(strings.TrimSpace(criteria[i].ID)), strings.TrimSpace(criteria[i].Text)
			if criteria[i].ID == "" {
				criteria[i].ID = fmt.Sprintf("item-%d", i+1)
			}
			if criteria[i].Text == "" || seen[criteria[i].ID] {
				return st, acceptInvalid("criteria need distinct IDs and text")
			}
			seen[criteria[i].ID] = true
		}
		if len(criteria) == 0 {
			return st, acceptInvalid("this task has no criteria on record; give them with the entry")
		}
		if err := f.SeedCriteria(ctx, taskID, "manual", criteria); err != nil {
			return st, err
		}
		if st, err = f.Acceptance(ctx, taskID); err != nil {
			return st, err
		}
	}
	if len(items) == 0 {
		return st, nil
	}
	clean, errs := validateReportItems(items, func(id string) bool { _, ok := st.item(id); return ok })
	if len(errs) > 0 {
		return st, acceptInvalid("%s", strings.Join(errs, "; "))
	}
	if err := f.Accept.append(ctx, taskID, acceptKindReport, 0, actor.ID, acceptReport{Source: "reviewer", Items: clean}); err != nil {
		return st, err
	}
	return f.Acceptance(ctx, taskID)
}

// RequestEvidence sends the items marked "needs evidence" back to the agent
// of the same task: only those items, with the reviewer's notes. Approved
// items stay as they are.
func (f *FleetService) RequestEvidence(ctx context.Context, actor fleet.Actor, taskID string) (fleet.Task, error) {
	st, err := f.Acceptance(ctx, taskID)
	if err != nil {
		return fleet.Task{}, err
	}
	var ids []string
	var sb strings.Builder
	for _, it := range st.active() {
		if it.Decision != acceptDecisionNeeds {
			continue
		}
		ids = append(ids, it.ID)
		fmt.Fprintf(&sb, "- [%s] %s\n  Reviewer: %s\n", it.ID, it.Text, it.Note)
	}
	if len(ids) == 0 {
		return fleet.Task{}, acceptInvalid("no item is marked as needing evidence")
	}
	instruction := "The reviewer needs more evidence for these acceptance items. Work only on them; the other items are approved and stay as they are.\n\n" +
		sb.String() + "\n" + acceptReportInstructions + "\nReport only the items above."
	task, err := f.Commander.Reply(ctx, actor, fleet.ReplyRequest{TaskID: taskID, Text: instruction})
	if err != nil {
		return task, err
	}
	_ = f.Accept.append(ctx, taskID, acceptKindRequest, 0, actor.ID, acceptRequest{Items: ids})
	return task, nil
}

// AutoRerunPaused reports whether automatic reruns must hold off: an item
// waits for an answer from outside.
func (f *FleetService) AutoRerunPaused(ctx context.Context, taskID string) bool {
	st, err := f.Acceptance(ctx, taskID)
	return err == nil && st.ExternalWait()
}

// ErrAwaitingExternal is returned for an automatic rerun held back.
var ErrAwaitingExternal = fmt.Errorf("%w: an acceptance item is awaiting external confirmation; record the reply first", errAcceptConflict)

// Retry reruns a failed or blocked task, unless it waits on an outside answer.
func (f *FleetService) Retry(ctx context.Context, actor fleet.Actor, taskID string) (fleet.Task, error) {
	if f.AutoRerunPaused(ctx, taskID) {
		return fleet.Task{}, ErrAwaitingExternal
	}
	return f.Commander.Retry(ctx, actor, taskID)
}

// noChanges reports whether a task's branch holds no work at all.
func (f *FleetService) noChanges(ctx context.Context, task fleet.Task) bool {
	if task.Shape != fleet.ShapeShip {
		return false
	}
	d, err := f.Commander.TaskDiff(ctx, task.ID)
	if err != nil {
		return false
	}
	_, _, _, ok := parseDiffStat(d.Stat)
	return !ok && len(d.Untracked) == 0 && strings.TrimSpace(d.Patch) == ""
}

// Pass sends a blocked task to review without its automatic verification.
// It approves no item. A task that changed nothing has nothing to deliver, so
// it goes straight to review rather than into a delivery that refuses it.
func (f *FleetService) Pass(ctx context.Context, actor fleet.Actor, taskID string) (fleet.Task, error) {
	task, err := f.Store.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	st, err := f.Acceptance(ctx, taskID)
	if err != nil {
		return task, err
	}
	if st.Structured && task.Status == fleet.StatusNeedsDecision && f.noChanges(ctx, task) {
		task, err = f.Store.Transition(ctx, taskID, fleet.StatusReviewReady,
			"waved through by "+actor.ID+": no file changes, evidence only")
		if err == nil {
			_, _ = f.Store.AppendEvent(ctx, taskID, "acceptance.passed", map[string]any{"actor": actor.ID, "evidence_only": true})
		}
		return task, err
	}
	return f.Commander.Pass(ctx, actor, taskID)
}

// CheckClose applies the closing rule to a task.
func (f *FleetService) CheckClose(ctx context.Context, taskID string) (acceptGate, bool, error) {
	task, err := f.Store.GetTask(ctx, taskID)
	if err != nil {
		return acceptGate{}, false, err
	}
	st, err := f.Acceptance(ctx, taskID)
	if err != nil {
		return acceptGate{}, false, err
	}
	evidenceOnly := st.Structured && f.noChanges(ctx, task)
	done := evidenceOnly || !task.TrustMode.NeedsRemote() || task.PRURL != ""
	return evaluateAcceptGate(st, done), evidenceOnly, nil
}

// Approve lands a task, once every required item is approved and the
// delivery flow the project asks for is complete. A task with no file
// changes is closed on its evidence alone.
func (f *FleetService) Approve(ctx context.Context, actor fleet.Actor, taskID string, merge bool) (fleet.Task, error) {
	gate, evidenceOnly, err := f.CheckClose(ctx, taskID)
	if err != nil {
		return fleet.Task{}, err
	}
	if !gate.OK {
		return fleet.Task{}, &acceptGateError{Gate: gate}
	}
	task, err := f.Store.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	if evidenceOnly && task.Status == fleet.StatusReviewReady {
		_, _ = f.Store.AppendEvent(ctx, taskID, fleet.EventDeliveryApproved, map[string]any{"approver": actor.ID, "landed": "evidence only"})
		return f.Store.Transition(ctx, taskID, fleet.StatusDone, "approved by "+actor.ID+": evidence only, no file changes")
	}
	if merge {
		return f.Commander.Merge(ctx, actor, taskID)
	}
	return f.Commander.Approve(ctx, actor, taskID)
}

// issueFetcher is where issues are read: the host the Commander's issue
// tracker uses, so dispatch and criteria refresh see the same issue.
func (f *FleetService) issueFetcher() fleet.IssueHost {
	if f.Commander != nil && f.Commander.Issues != nil && f.Commander.Issues.Host != nil {
		return f.Commander.Issues.Host
	}
	return fleet.GitHubIssues{}
}
