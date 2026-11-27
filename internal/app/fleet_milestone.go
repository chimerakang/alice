package app

import (
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// A milestone batch is one instruction to work through a GitHub milestone.
// It remembers which issues are in scope and which task handles each, and
// queues the next executable issue whenever a slot frees up. Everything it
// decides can be recomputed from GitHub and the fleet's tasks, so reading
// them again after a resend, a restart or a hand merge is always safe.

// Batch statuses.
const (
	// batchProposed: found, waiting for the owner to confirm.
	batchProposed = "proposed"
	batchActive   = "active"
	// batchDeclined: the owner chose to dispatch single issues instead.
	batchDeclined = "declined"
)

// Item states.
const (
	itemReady     = "ready"     // executable, waiting for a slot
	itemWaiting   = "waiting"   // a dependency is not finished
	itemActive    = "active"    // its task is queued, running or verifying
	itemAttention = "attention" // its task waits for the owner
	itemLanded    = "landed"    // its task is done, the issue is still open
	itemDone      = "done"      // closed as completed
	itemSkipped   = "skipped"   // closed as not planned or duplicate
	itemExcluded  = "excluded"  // an epic, or carries an excluding label
	itemBlocked   = "blocked"   // carries a blocking label, or is too vague
	itemHeld      = "held"      // will not be dispatched without the owner
	itemRemoved   = "removed"   // no longer in the milestone
)

// Item reasons.
const (
	reasonEpic             = "epic"
	reasonLabel            = "label"
	reasonDependency       = "dependency"
	reasonDependencyClosed = "dependency_dropped"
	reasonCycle            = "dependency_cycle"
	reasonCancelled        = "task_cancelled"
	reasonAwaitingMerge    = "awaiting_merge"
	reasonTargetChanged    = "target_changed"
	reasonTargetMismatch   = "target_mismatch"
	reasonNeedsDetail      = "needs_clarification"
	reasonDispatchFailed   = "dispatch_failed"
)

var errBatchNotFound = errors.New("milestone batch not found")

type milestoneBatch struct {
	ID             string
	ProjectID      string
	Milestone      int
	MilestoneTitle string
	// TargetBranch is fixed when the batch is created.
	TargetBranch string
	Status       string
	EntryIssue   int
	CreatedBy    string
	ChatID       int64
	ThreadID     int
	Model        string
	BudgetUSD    float64
	// LastError is why the last reconcile could not finish, or "".
	LastError string
}

type milestoneItem struct {
	Issue     int
	Title     string
	State     string
	Reason    string
	DependsOn []int
	// Waiting lists the dependencies not finished yet.
	Waiting []int
	TaskID  string
	Detail  string
}

type milestoneBatches struct {
	Host   fleetMilestoneHost
	Config fleetMilestoneConfig

	db    *sql.DB
	store *fleet.Store
	// dispatch queues one issue task the way /ship does.
	dispatch func(context.Context, fleet.Actor, fleet.DispatchRequest) (fleet.Task, error)
	// gate returns why an issue is too vague to dispatch, or "".
	gate     func(context.Context, fleet.Project, int) (string, error)
	isEpic   func(labels []string, title string) bool
	mu       sync.Mutex // one reconcile at a time: two must not dispatch the same issue
	hookMu   sync.Mutex
	announce func(context.Context, milestoneBatch, fleet.Task)
}

func newMilestoneBatches(db *sql.DB, store *fleet.Store, cfg fleetMilestoneConfig) (*milestoneBatches, error) {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS fleet_batches (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			milestone INTEGER NOT NULL,
			milestone_title TEXT NOT NULL,
			target_branch TEXT NOT NULL,
			status TEXT NOT NULL,
			entry_issue INTEGER NOT NULL,
			created_by TEXT NOT NULL,
			chat_id INTEGER NOT NULL DEFAULT 0,
			thread_id INTEGER NOT NULL DEFAULT 0,
			model TEXT NOT NULL DEFAULT '',
			budget_usd REAL NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE (project_id, milestone)
		)`,
		`CREATE TABLE IF NOT EXISTS fleet_batch_items (
			batch_id TEXT NOT NULL,
			issue INTEGER NOT NULL,
			title TEXT NOT NULL,
			state TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			depends_on TEXT NOT NULL DEFAULT '[]',
			waiting TEXT NOT NULL DEFAULT '[]',
			task_id TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (batch_id, issue)
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("fleet milestone schema: %w", err)
		}
	}
	return &milestoneBatches{Host: ghMilestoneHost{}, Config: cfg.withDefaults(), db: db, store: store}, nil
}

// OnDispatch sets what to do with a task the batch queued, e.g. post its card.
func (m *milestoneBatches) OnDispatch(fn func(context.Context, milestoneBatch, fleet.Task)) {
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	m.announce = fn
}

// ── storage ──────────────────────────────────────────────────────────────────

const batchColumns = `id, project_id, milestone, milestone_title, target_branch, status, entry_issue,
	created_by, chat_id, thread_id, model, budget_usd, last_error`

func scanBatch(row interface{ Scan(...any) error }) (milestoneBatch, error) {
	var b milestoneBatch
	err := row.Scan(&b.ID, &b.ProjectID, &b.Milestone, &b.MilestoneTitle, &b.TargetBranch, &b.Status, &b.EntryIssue,
		&b.CreatedBy, &b.ChatID, &b.ThreadID, &b.Model, &b.BudgetUSD, &b.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return b, errBatchNotFound
	}
	return b, err
}

func (m *milestoneBatches) Get(ctx context.Context, id string) (milestoneBatch, error) {
	return scanBatch(m.db.QueryRowContext(ctx, `SELECT `+batchColumns+` FROM fleet_batches WHERE id = ?`, id))
}

// ByMilestone returns the project's batch for a milestone.
func (m *milestoneBatches) ByMilestone(ctx context.Context, projectID string, milestone int) (milestoneBatch, error) {
	return scanBatch(m.db.QueryRowContext(ctx, `SELECT `+batchColumns+` FROM fleet_batches WHERE project_id = ? AND milestone = ?`, projectID, milestone))
}

// ByIssue returns the batch that has the issue in scope, from what is
// stored, so it answers even when GitHub cannot be reached.
func (m *milestoneBatches) ByIssue(ctx context.Context, projectID string, issue int) (milestoneBatch, error) {
	return scanBatch(m.db.QueryRowContext(ctx, `SELECT `+batchColumns+` FROM fleet_batches b
		WHERE project_id = ? AND (
			EXISTS (SELECT 1 FROM fleet_batch_items i WHERE i.batch_id = b.id AND i.issue = ? AND i.state <> ?)
			-- A batch not started yet has no items, only the issue that led to it.
			OR (entry_issue = ? AND NOT EXISTS (SELECT 1 FROM fleet_batch_items i WHERE i.batch_id = b.id AND i.issue = ?)))
		ORDER BY updated_at DESC LIMIT 1`, projectID, issue, itemRemoved, issue, issue))
}

func (m *milestoneBatches) activeIDs(ctx context.Context) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id FROM fleet_batches WHERE status = ? ORDER BY created_at`, batchActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Create stores a batch, or returns the one the milestone already has.
func (m *milestoneBatches) Create(ctx context.Context, b milestoneBatch) (milestoneBatch, error) {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return milestoneBatch{}, err
	}
	b.ID = hex.EncodeToString(raw[:])
	now := time.Now().UnixMilli()
	if _, err := m.db.ExecContext(ctx, `INSERT OR IGNORE INTO fleet_batches (`+batchColumns+`, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.ProjectID, b.Milestone, b.MilestoneTitle, b.TargetBranch, b.Status, b.EntryIssue,
		b.CreatedBy, b.ChatID, b.ThreadID, b.Model, b.BudgetUSD, "", now, now); err != nil {
		return milestoneBatch{}, err
	}
	return m.ByMilestone(ctx, b.ProjectID, b.Milestone)
}

// Resolve settles a proposed batch. It reports false when the batch was no
// longer waiting for an answer, so a button pressed twice acts once.
func (m *milestoneBatches) Resolve(ctx context.Context, id, status string) (bool, error) {
	res, err := m.db.ExecContext(ctx, `UPDATE fleet_batches SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		status, time.Now().UnixMilli(), id, batchProposed)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Reopen takes up a batch the owner had declined.
func (m *milestoneBatches) Reopen(ctx context.Context, id string) error {
	_, err := m.db.ExecContext(ctx, `UPDATE fleet_batches SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		batchActive, time.Now().UnixMilli(), id, batchDeclined)
	return err
}

func (m *milestoneBatches) Items(ctx context.Context, batchID string) ([]milestoneItem, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT issue, title, state, reason, depends_on, waiting, task_id, detail
		FROM fleet_batch_items WHERE batch_id = ? ORDER BY issue`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []milestoneItem
	for rows.Next() {
		var it milestoneItem
		var deps, waiting string
		if err := rows.Scan(&it.Issue, &it.Title, &it.State, &it.Reason, &deps, &waiting, &it.TaskID, &it.Detail); err != nil {
			return nil, err
		}
		if err := errors.Join(json.Unmarshal([]byte(deps), &it.DependsOn), json.Unmarshal([]byte(waiting), &it.Waiting)); err != nil {
			return nil, fmt.Errorf("batch %s issue %d: %w", batchID, it.Issue, err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (m *milestoneBatches) saveItem(ctx context.Context, batchID string, it milestoneItem) error {
	deps, err := json.Marshal(it.DependsOn)
	if err != nil {
		return err
	}
	waiting, err := json.Marshal(it.Waiting)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT OR REPLACE INTO fleet_batch_items
		(batch_id, issue, title, state, reason, depends_on, waiting, task_id, detail) VALUES (?,?,?,?,?,?,?,?,?)`,
		batchID, it.Issue, it.Title, it.State, it.Reason, string(deps), string(waiting), it.TaskID, it.Detail)
	return err
}

func (m *milestoneBatches) noteResult(ctx context.Context, id string, cause error) error {
	text := ""
	if cause != nil {
		text = cause.Error()
	}
	_, err := m.db.ExecContext(ctx, `UPDATE fleet_batches SET last_error = ?, updated_at = ? WHERE id = ?`, text, time.Now().UnixMilli(), id)
	return err
}

// ── reconcile ────────────────────────────────────────────────────────────────

// Reconcile reads the milestone and the fleet's tasks again, records where
// every issue stands, and queues what can start. It returns the tasks it
// queued. Only an active batch changes.
func (m *milestoneBatches) Reconcile(ctx context.Context, id string) ([]fleet.Task, error) {
	m.mu.Lock()
	batch, err := m.Get(ctx, id)
	if err != nil || batch.Status != batchActive {
		m.mu.Unlock()
		return nil, err
	}
	queued, err := m.reconcile(ctx, batch)
	if noteErr := m.noteResult(context.WithoutCancel(ctx), id, err); err == nil {
		err = noteErr
	}
	m.mu.Unlock()

	m.hookMu.Lock()
	announce := m.announce
	m.hookMu.Unlock()
	if announce != nil {
		for _, task := range queued {
			announce(ctx, batch, task)
		}
	}
	return queued, err
}

func (m *milestoneBatches) reconcile(ctx context.Context, batch milestoneBatch) ([]fleet.Task, error) {
	project, err := m.store.GetProject(ctx, batch.ProjectID)
	if err != nil {
		return nil, err
	}
	// A failed read changes nothing: a partial list would look like issues
	// having left the milestone.
	issues, err := m.Host.MilestoneIssues(ctx, project.RepoPath, batch.Milestone)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(issues, func(a, b milestoneIssue) int { return cmp.Compare(a.Number, b.Number) })
	previous, err := m.Items(ctx, batch.ID)
	if err != nil {
		return nil, err
	}
	before := map[int]milestoneItem{}
	for _, it := range previous {
		before[it.Issue] = it
	}
	tasks, err := m.store.ListTasks(ctx, fleet.TaskFilter{ProjectID: batch.ProjectID, Order: fleet.NewestFirst})
	if err != nil {
		return nil, err
	}

	inScope := map[int]milestoneIssue{}
	items := make([]*milestoneItem, 0, len(issues))
	for _, issue := range issues {
		inScope[issue.Number] = issue
		it := m.classify(batch, issue, before[issue.Number], tasks)
		items = append(items, &it)
	}
	if err := m.applyDependencies(ctx, project, items, inScope); err != nil {
		return nil, err
	}

	slots := max(project.MaxConcurrent, 1)
	for _, it := range items {
		if it.State == itemActive {
			slots--
		}
	}
	// Items are saved before anything is queued, so the scope is on record
	// even if a dispatch below never returns.
	for _, it := range items {
		if err := m.saveItem(ctx, batch.ID, *it); err != nil {
			return nil, err
		}
		delete(before, it.Issue)
	}
	for _, gone := range before {
		gone.State, gone.Reason, gone.Waiting = itemRemoved, "", nil
		if err := m.saveItem(ctx, batch.ID, gone); err != nil {
			return nil, err
		}
	}

	var queued []fleet.Task
	var failed error
	for _, it := range items {
		if it.State != itemReady {
			continue
		}
		if project.DefaultBranch != batch.TargetBranch {
			// A task takes the project's branch when it first starts.
			it.Reason, it.Detail = reasonTargetChanged, project.DefaultBranch
		} else if slots > 0 {
			task, err := m.start(ctx, batch, project, it)
			if err != nil {
				failed = errors.Join(failed, fmt.Errorf("issue #%d: %w", it.Issue, err))
			}
			if task.ID != "" {
				queued = append(queued, task)
				slots--
			}
		}
		if err := m.saveItem(ctx, batch.ID, *it); err != nil {
			return queued, errors.Join(failed, err)
		}
	}
	return queued, failed
}

// start queues the item's issue, recording on the item how that went.
func (m *milestoneBatches) start(ctx context.Context, batch milestoneBatch, project fleet.Project, it *milestoneItem) (fleet.Task, error) {
	fail := func(err error) (fleet.Task, error) {
		it.Reason, it.Detail = reasonDispatchFailed, err.Error()
		return fleet.Task{}, err
	}
	if m.gate != nil {
		why, err := m.gate(ctx, project, it.Issue)
		if err != nil {
			return fail(err)
		}
		if why != "" {
			it.State, it.Reason, it.Detail = itemBlocked, reasonNeedsDetail, why
			return fleet.Task{}, nil
		}
	}
	task, err := m.dispatch(ctx, fleet.Actor{ID: batch.CreatedBy, Via: "milestone"}, fleet.DispatchRequest{
		ProjectID: batch.ProjectID, Issue: it.Issue, Shape: fleet.ShapeShip,
		Model: batch.Model, Budget: fleet.Limit{CostUSD: batch.BudgetUSD},
	})
	// A task can be queued and still report an error from a later step.
	if task.ID == "" {
		if err == nil {
			err = errors.New("dispatch returned no task")
		}
		return fail(err)
	}
	it.State, it.Reason, it.Detail, it.TaskID = itemActive, "", "", task.ID
	return task, err
}

// classify places one issue, leaving dependencies to applyDependencies.
// tasks are the project's, newest first.
func (m *milestoneBatches) classify(batch milestoneBatch, issue milestoneIssue, prev milestoneItem, tasks []fleet.Task) milestoneItem {
	it := milestoneItem{Issue: issue.Number, Title: issue.Title, TaskID: prev.TaskID,
		DependsOn: parseDependencies(issue.Body, m.Config.DependencyMarkers)}

	// The newest ship task that was not thrown away handles the issue,
	// whoever dispatched it.
	var task, linked *fleet.Task
	for i := range tasks {
		t := &tasks[i]
		if t.Source.Kind != fleet.SourceIssue || t.Source.IssueNumber != issue.Number || t.Shape != fleet.ShapeShip {
			continue
		}
		if t.ID == prev.TaskID {
			linked = t
		}
		if task == nil && t.Status != fleet.StatusCancelled {
			task = t
		}
	}
	if task != nil {
		it.TaskID = task.ID
	}

	switch {
	case m.isEpic != nil && m.isEpic(issue.Labels, issue.Title):
		it.State, it.Reason = itemExcluded, reasonEpic
	case issue.completed():
		it.State = itemDone
	case issue.closed():
		it.State, it.Reason = itemSkipped, issue.StateReason
	case hasAnyLabel(issue.Labels, m.Config.ExcludeLabels):
		it.State, it.Reason = itemExcluded, reasonLabel
	case task != nil:
		switch task.Status {
		case fleet.StatusQueued, fleet.StatusRunning, fleet.StatusVerifying:
			it.State = itemActive
		case fleet.StatusDone:
			// A direct-pr task is done once its pull request is open.
			if task.TrustMode == fleet.TrustDirectPR {
				it.State, it.Reason = itemAttention, reasonAwaitingMerge
			} else {
				it.State = itemLanded
			}
		default:
			it.State, it.Reason = itemAttention, string(task.Status)
		}
		if task.Status != fleet.StatusDone && task.TargetBranch != "" && task.TargetBranch != batch.TargetBranch {
			it.Reason, it.Detail = reasonTargetMismatch, task.TargetBranch
		}
	case linked != nil:
		// The owner threw this batch's task away; queueing it again would
		// undo that.
		it.State, it.Reason = itemHeld, reasonCancelled
	case hasAnyLabel(issue.Labels, m.Config.BlockedLabels):
		it.State, it.Reason = itemBlocked, reasonLabel
	default:
		it.State = itemReady
	}
	return it
}

// applyDependencies holds back ready items whose dependencies are not
// finished. A dependency is finished only when its issue is closed as
// completed: a done task may have delivered only part of it.
func (m *milestoneBatches) applyDependencies(ctx context.Context, project fleet.Project, items []*milestoneItem, inScope map[int]milestoneIssue) error {
	outside := map[int]milestoneIssue{}
	lookup := func(n int) (milestoneIssue, error) {
		if issue, ok := inScope[n]; ok {
			return issue, nil
		}
		if issue, ok := outside[n]; ok {
			return issue, nil
		}
		issue, err := m.Host.Issue(ctx, project.RepoPath, n)
		if err != nil {
			return issue, fmt.Errorf("read dependency #%d: %w", n, err)
		}
		outside[n] = issue
		return issue, nil
	}
	// reaches reports whether following open dependencies from n leads to goal.
	var reaches func(n, goal int, seen map[int]bool) bool
	reaches = func(n, goal int, seen map[int]bool) bool {
		issue, ok := inScope[n]
		if !ok || issue.closed() || seen[n] {
			return false
		}
		seen[n] = true
		for _, dep := range parseDependencies(issue.Body, m.Config.DependencyMarkers) {
			if dep == goal || reaches(dep, goal, seen) {
				return true
			}
		}
		return false
	}

	for _, it := range items {
		if it.State != itemReady {
			continue
		}
		var dropped []int
		for _, n := range it.DependsOn {
			if n == it.Issue {
				continue
			}
			dep, err := lookup(n)
			if err != nil {
				return err
			}
			switch {
			case dep.completed():
			case dep.closed():
				dropped = append(dropped, n)
			default:
				it.Waiting = append(it.Waiting, n)
			}
		}
		switch {
		case reaches(it.Issue, it.Issue, map[int]bool{}):
			it.State, it.Reason = itemHeld, reasonCycle
		case len(dropped) > 0:
			it.State, it.Reason, it.Waiting = itemHeld, reasonDependencyClosed, dropped
		case len(it.Waiting) > 0:
			it.State, it.Reason = itemWaiting, reasonDependency
		}
	}
	return nil
}

// ── background ───────────────────────────────────────────────────────────────

func (m *milestoneBatches) reconcileActive(ctx context.Context) {
	ids, err := m.activeIDs(ctx)
	if err != nil {
		log.Printf("[fleet] list milestone batches: %v", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if _, err := m.Reconcile(ctx, id); err != nil && ctx.Err() == nil {
			log.Printf("[fleet] reconcile milestone batch %s: %v", id, err)
		}
	}
}

// Run reconciles every active batch at start, whenever a task changes
// status, and at a steady interval for what happens on GitHub alone.
func (m *milestoneBatches) Run(ctx context.Context, bus *fleet.Bus) error {
	events, err := bus.Subscribe(ctx, fleet.EventFilter{Types: []string{fleet.EventStatus}})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(time.Duration(m.Config.ReconcileSeconds) * time.Second)
	defer ticker.Stop()
	for {
		m.reconcileActive(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case _, ok := <-events:
			if !ok {
				return nil
			}
			// Several changes arriving together need one pass.
			for drained := false; !drained; {
				select {
				case _, ok := <-events:
					drained = !ok
				default:
					drained = true
				}
			}
		}
	}
}
