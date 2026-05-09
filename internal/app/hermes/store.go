package hermes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// TaskStateStore defines persistence operations for Hermes task states.
type TaskStateStore interface {
	// CreateTask persists a new TaskState and returns it with CreatedAt/UpdatedAt set.
	CreateTask(task TaskState) (TaskState, error)

	// GetTask retrieves a task by ID.
	GetTask(id string) (TaskState, error)

	// GetActiveTaskForChat returns the most recent non-terminal task for a chat, or ErrNoTask.
	GetActiveTaskForChat(chatID int64) (TaskState, error)

	// StorePlan persists the Planner's sub-task list, replacing any existing plan.
	// Must be called before UpdateSubTask.
	StorePlan(taskID string, plan []SubTask) error

	// UpdateSubTask writes the result and status of a single sub-task back to the store.
	UpdateSubTask(taskID string, idx int, status SubTaskStatus, result string, tokensUsed int) error

	// MarkSubTaskStarted records that execution for a sub-task has been dispatched.
	// It must not increment Attempts; attempts are counted when a run produces a
	// result or enters a strict retry.
	MarkSubTaskStarted(taskID string, idx int) error

	// AdvanceTask increments CurrentIdx and sets the task status.
	AdvanceTask(taskID string, nextIdx int, status TaskStatus) error

	// AppendArtifact adds a file artifact record to the task.
	AppendArtifact(taskID string, artifact Artifact) error

	// UpdateAccumulated replaces the accumulated summary text.
	UpdateAccumulated(taskID string, accumulated string) error

	// UpdatePlannerSession records the Planner's CLI --resume session ID.
	UpdatePlannerSession(taskID string, sessionID string) error

	// MarkInterrupted sets task status to interrupted and records the message ID.
	MarkInterrupted(taskID string, messageID int64) error

	// MarkStatus sets an arbitrary terminal or transition status on a task.
	MarkStatus(taskID string, status TaskStatus) error

	// ResetBudgetStartedAt updates the wallclock budget start time so that a
	// user-confirmed "continue" gets a fresh time window.
	ResetBudgetStartedAt(taskID string, t time.Time) error

	// AddTokenUsage adds delta tokens to both the task budget and the current sub-task.
	AddTokenUsage(taskID string, delta int) error

	// AddModelUsage accumulates per-model token + cost usage for the #102
	// summary report. Creates a new ModelUsage entry if the model has not been
	// seen on this task. costUSD is the per-call USD value reported by the CLI
	// (or estimated for Max-sub) — see #148 1E.
	AddModelUsage(taskID, model string, inputTokens, outputTokens int, costUSD float64) error
	AddModelUsageBreakdown(taskID, model string, usage TokenUsageBreakdown) error

	// AddPhaseUsage accumulates per-phase token + cost usage for Hermes
	// observability dashboards and postmortems. Phases include planner,
	// executor, reviewer, retry, and preflight.
	AddPhaseUsage(taskID, phase, model string, inputTokens, outputTokens int, costUSD float64) error
	AddPhaseUsageBreakdown(taskID, phase, model string, usage TokenUsageBreakdown) error

	// ListTasksForChat returns recent tasks for a chat (newest first).
	ListTasksForChat(chatID int64, limit int) ([]TaskState, error)
}

// ErrNoTask is returned when no matching task exists.
var ErrNoTask = fmt.Errorf("hermes: no task found")

// normalizeStatus maps legacy DB statuses to current ones. The "validating"
// status was retired in favor of staying in TaskStatusExecuting through the
// validate-and-retry sub-loop; existing rows persisted before the retirement
// are coerced on read so the runtime never observes a removed status.
func normalizeStatus(s string) TaskStatus {
	if s == "validating" {
		return TaskStatusExecuting
	}
	return TaskStatus(s)
}

// SQLiteTaskStore implements TaskStateStore backed by SQLite.
type SQLiteTaskStore struct {
	db                  *sql.DB
	onUnifiedTaskUpdate func(taskID string)
}

var _ SnapshotStore = (*SQLiteTaskStore)(nil)
var _ RuntimeStepStore = (*SQLiteTaskStore)(nil)

// NewSQLiteTaskStore creates and migrates the hermes SQLite tables.
// db must be an open *sql.DB (typically shared with the main SQLiteStorage).
func NewSQLiteTaskStore(db *sql.DB) (*SQLiteTaskStore, error) {
	s := &SQLiteTaskStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("hermes store migration: %w", err)
	}
	return s, nil
}

// SetUnifiedTaskUpdateHook registers a best-effort callback fired after the
// Hermes legacy tables have been mirrored into the unified #114 tables.
func (s *SQLiteTaskStore) SetUnifiedTaskUpdateHook(fn func(taskID string)) {
	s.onUnifiedTaskUpdate = fn
}

// migrate creates all hermes tables if they don't exist.
func (s *SQLiteTaskStore) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hermes_task_states (
			id                    TEXT PRIMARY KEY,
			chat_id               INTEGER NOT NULL,
			thread_id             INTEGER NOT NULL DEFAULT 0,
			planner_session       TEXT NOT NULL DEFAULT '',
			project_dir           TEXT NOT NULL DEFAULT '',
			goal                  TEXT NOT NULL,
			current_idx           INTEGER NOT NULL DEFAULT 0,
			accumulated           TEXT NOT NULL DEFAULT '',
			status                TEXT NOT NULL DEFAULT 'planning',
			interrupted_by        INTEGER,
			interrupt_policy      TEXT NOT NULL DEFAULT 'queue',
			token_budget          TEXT NOT NULL DEFAULT '{}',
			plan_json             TEXT NOT NULL DEFAULT '[]',
			github_issue_number   INTEGER NOT NULL DEFAULT 0,
			model_usages          TEXT NOT NULL DEFAULT '[]',
			phase_usages          TEXT NOT NULL DEFAULT '[]',
			created_at            TEXT NOT NULL,
			updated_at            TEXT NOT NULL
		)`,
		// Additive migrations for pre-existing tables.
		// SQLite does not support IF NOT EXISTS on ALTER TABLE — ignore "duplicate column" error.
		`ALTER TABLE hermes_task_states ADD COLUMN thread_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE hermes_task_states ADD COLUMN project_dir TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hermes_task_states ADD COLUMN github_issue_number INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE hermes_task_states ADD COLUMN model_usages TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE hermes_task_states ADD COLUMN phase_usages TEXT NOT NULL DEFAULT '[]'`,
		`CREATE INDEX IF NOT EXISTS idx_hermes_tasks_chat_status
			ON hermes_task_states(chat_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_hermes_tasks_chat_thread
			ON hermes_task_states(chat_id, thread_id)`,
		`CREATE TABLE IF NOT EXISTS hermes_task_artifacts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id     TEXT NOT NULL REFERENCES hermes_task_states(id),
			path        TEXT NOT NULL,
			hash        TEXT NOT NULL DEFAULT '',
			sub_task_id TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hermes_artifacts_task
			ON hermes_task_artifacts(task_id)`,
		`CREATE TABLE IF NOT EXISTS hermes_snapshots (
			id                    TEXT PRIMARY KEY,
			task_id               TEXT NOT NULL REFERENCES hermes_task_states(id),
			chat_id               INTEGER NOT NULL DEFAULT 0,
			thread_id             INTEGER NOT NULL DEFAULT 0,
			step                  INTEGER NOT NULL,
			state_json            TEXT NOT NULL,
			next_step             TEXT NOT NULL DEFAULT '',
			source_node           TEXT NOT NULL DEFAULT '',
			writes_json           TEXT NOT NULL DEFAULT '{}',
			metadata_json         TEXT NOT NULL DEFAULT '{}',
			parent_snapshot_id    TEXT NOT NULL DEFAULT '',
			channel_versions_json TEXT NOT NULL DEFAULT '{}',
			state_status          TEXT NOT NULL DEFAULT '',
			created_at            TEXT NOT NULL
		)`,
		// Additive migration: pre-existing snapshots tables get the
		// state_status column. SQLite does not support IF NOT EXISTS on
		// ALTER TABLE — duplicate-column error is ignored by execIgnore.
		`ALTER TABLE hermes_snapshots ADD COLUMN state_status TEXT NOT NULL DEFAULT ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_hermes_snapshots_task_step
			ON hermes_snapshots(task_id, step)`,
		`CREATE INDEX IF NOT EXISTS idx_hermes_snapshots_task_created
			ON hermes_snapshots(task_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_hermes_snapshots_thread_created
			ON hermes_snapshots(chat_id, thread_id, created_at)`,
		// Index covers the slice 3d filter "latest snapshot per task with
		// non-terminal status" used by GetActiveTaskForChat, web health
		// check, and the startup zombie sweep.
		`CREATE INDEX IF NOT EXISTS idx_hermes_snapshots_task_step_status
			ON hermes_snapshots(task_id, step DESC, state_status)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id                  TEXT PRIMARY KEY,
			chat_id             INTEGER NOT NULL DEFAULT 0,
			thread_id           INTEGER NOT NULL DEFAULT 0,
			project_dir         TEXT NOT NULL DEFAULT '',
			github_issue_number INTEGER NOT NULL DEFAULT 0,
			goal                TEXT NOT NULL DEFAULT '',
			engine              TEXT NOT NULL DEFAULT '',
			backend             TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT '',
			started_at          TEXT NOT NULL,
			ended_at            TEXT,
			total_input_tokens  INTEGER NOT NULL DEFAULT 0,
			total_output_tokens INTEGER NOT NULL DEFAULT 0,
			total_cost_usd      REAL NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_started_at ON tasks(started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_chat_thread ON tasks(chat_id, thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_project_dir ON tasks(project_dir)`,
		`ALTER TABLE tasks ADD COLUMN github_issue_number INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_github_issue_number ON tasks(github_issue_number)`,
		`CREATE TABLE IF NOT EXISTS sub_tasks (
			id                 TEXT PRIMARY KEY,
			task_id            TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			idx                INTEGER NOT NULL DEFAULT 0,
			description        TEXT NOT NULL DEFAULT '',
			model              TEXT NOT NULL DEFAULT '',
			status             TEXT NOT NULL DEFAULT '',
			result_text        TEXT NOT NULL DEFAULT '',
			input_tokens       INTEGER NOT NULL DEFAULT 0,
			output_tokens      INTEGER NOT NULL DEFAULT 0,
			cost_usd           REAL NOT NULL DEFAULT 0,
			started_at         TEXT NOT NULL,
			ended_at           TEXT,
			routing_reason     TEXT NOT NULL DEFAULT '',
			routing_latency_ms INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sub_tasks_task_idx ON sub_tasks(task_id, idx)`,
		`CREATE TABLE IF NOT EXISTS tool_events (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			sub_task_id TEXT NOT NULL REFERENCES sub_tasks(id) ON DELETE CASCADE,
			tool_name   TEXT NOT NULL DEFAULT '',
			input_json  TEXT NOT NULL DEFAULT '{}',
			output_json TEXT NOT NULL DEFAULT '{}',
			ts          TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_events_sub_task ON tool_events(sub_task_id)`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			sub_task_id TEXT NOT NULL REFERENCES sub_tasks(id) ON DELETE CASCADE,
			path        TEXT NOT NULL,
			hash        TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_sub_task ON artifacts(sub_task_id)`,
		`CREATE TABLE IF NOT EXISTS review_results (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			reviewer_model  TEXT NOT NULL DEFAULT '',
			verdict         TEXT NOT NULL DEFAULT '',
			overall_score   INTEGER NOT NULL DEFAULT 0,
			feedback_text   TEXT NOT NULL DEFAULT '',
			issue_tags      TEXT NOT NULL DEFAULT '[]',
			input_tokens    INTEGER NOT NULL DEFAULT 0,
			output_tokens   INTEGER NOT NULL DEFAULT 0,
			cost_usd        REAL NOT NULL DEFAULT 0,
			created_at      TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_review_results_task ON review_results(task_id)`,
		`CREATE TABLE IF NOT EXISTS review_subtask_results (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			review_id   INTEGER NOT NULL REFERENCES review_results(id) ON DELETE CASCADE,
			sub_task_id TEXT NOT NULL REFERENCES sub_tasks(id) ON DELETE CASCADE,
			score       INTEGER NOT NULL DEFAULT 0,
			feedback    TEXT NOT NULL DEFAULT '',
			issue_tags  TEXT NOT NULL DEFAULT '[]'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_review_subtask_results_review ON review_subtask_results(review_id)`,
	}

	return s.execWithRetry(func() error {
		for _, stmt := range stmts {
			if _, err := s.db.Exec(stmt); err != nil {
				// ALTER TABLE ADD COLUMN fails with "duplicate column name" when the
				// column already exists (e.g. fresh installs that hit CREATE TABLE first).
				if strings.Contains(err.Error(), "duplicate column name") {
					continue
				}
				return fmt.Errorf("migrate: %w", err)
			}
		}
		return nil
	})
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

func (s *SQLiteTaskStore) CreateTask(task TaskState) (TaskState, error) {
	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now

	planJSON, err := json.Marshal(task.Plan)
	if err != nil {
		return task, err
	}
	budgetJSON, err := json.Marshal(task.TokenBudget)
	if err != nil {
		return task, err
	}
	modelUsagesJSON, err := json.Marshal(task.ModelUsages)
	if err != nil {
		return task, err
	}
	phaseUsagesJSON, err := json.Marshal(task.PhaseUsages)
	if err != nil {
		return task, err
	}

	return task, s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO hermes_task_states
				(id, chat_id, thread_id, planner_session, project_dir, goal, current_idx, accumulated,
				 status, interrupted_by, interrupt_policy, token_budget, plan_json,
				 github_issue_number, model_usages, phase_usages, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			task.ID, task.ChatID, task.ThreadID, task.PlannerSessionID, task.ProjectDir, task.Goal,
			task.CurrentIdx, task.Accumulated, string(task.Status),
			task.InterruptedBy, "inject",
			string(budgetJSON), string(planJSON),
			task.GithubIssueNumber, string(modelUsagesJSON), string(phaseUsagesJSON),
			task.CreatedAt.Format(time.RFC3339),
			task.UpdatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
		if err := s.upsertUnifiedTask(task, nil); err != nil {
			return err
		}
		s.broadcastUnifiedTask(task.ID)
		return nil
	})
}

// CreateSnapshot persists one durable Hermes runtime checkpoint. The caller is
// responsible for choosing a monotonic per-task Step; the database enforces
// uniqueness on (task_id, step).
func (s *SQLiteTaskStore) CreateSnapshot(snapshot Snapshot) (Snapshot, error) {
	if strings.TrimSpace(snapshot.TaskID) == "" {
		return snapshot, fmt.Errorf("snapshot task_id is required")
	}
	if snapshot.ID == "" {
		snapshot.ID = uuid.NewString()
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now()
	}
	if snapshot.State.TaskID == "" {
		snapshot.State.TaskID = snapshot.TaskID
	}
	if snapshot.ChatID == 0 {
		snapshot.ChatID = snapshot.State.ChatID
	}
	if snapshot.ThreadID == 0 {
		snapshot.ThreadID = snapshot.State.ThreadID
	}
	if snapshot.ChannelVersions == nil {
		snapshot.ChannelVersions = map[string]int64{}
	}

	stateJSON, err := json.Marshal(snapshot.State)
	if err != nil {
		return snapshot, fmt.Errorf("marshal snapshot state: %w", err)
	}
	writesJSON, err := json.Marshal(snapshot.Writes)
	if err != nil {
		return snapshot, fmt.Errorf("marshal snapshot writes: %w", err)
	}
	metadataJSON, err := json.Marshal(snapshot.Metadata)
	if err != nil {
		return snapshot, fmt.Errorf("marshal snapshot metadata: %w", err)
	}
	channelVersionsJSON, err := json.Marshal(snapshot.ChannelVersions)
	if err != nil {
		return snapshot, fmt.Errorf("marshal snapshot channel versions: %w", err)
	}

	return snapshot, s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO hermes_snapshots
				(id, task_id, chat_id, thread_id, step, state_json, next_step, source_node,
				 writes_json, metadata_json, parent_snapshot_id, channel_versions_json,
				 state_status, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshot.ID, snapshot.TaskID, snapshot.ChatID, snapshot.ThreadID, snapshot.Step,
			string(stateJSON), string(snapshot.NextStep), string(snapshot.SourceNode),
			string(writesJSON), string(metadataJSON), snapshot.ParentSnapshotID,
			string(channelVersionsJSON),
			string(snapshot.State.Status),
			snapshot.CreatedAt.Format(time.RFC3339),
		)
		return err
	})
}

// CommitRuntimeStep atomically applies reducer updates, refreshes the legacy
// task/sub-task views, and writes one durable snapshot.
func (s *SQLiteTaskStore) CommitRuntimeStep(commit RuntimeCommit) (Snapshot, error) {
	if strings.TrimSpace(commit.TaskID) == "" {
		return Snapshot{}, fmt.Errorf("runtime commit task_id is required")
	}
	currentTask, err := s.GetTask(commit.TaskID)
	if err != nil {
		return Snapshot{}, err
	}
	current := HermesStateFromTaskState(currentTask)
	if prev, prevErr := s.GetLatestSnapshot(commit.TaskID); prevErr == nil {
		current = CarryForwardSnapshotFields(current, prev.State)
	}
	nextState, err := ApplyStateUpdates(current, commit.Updates)
	if err != nil {
		return Snapshot{}, err
	}

	var latest Snapshot
	var parentSnapshotID string
	nextStepNumber := 1
	if latest, err = s.GetLatestSnapshot(commit.TaskID); err == nil {
		parentSnapshotID = latest.ID
		nextStepNumber = latest.Step + 1
	} else if err != ErrNoTask {
		return Snapshot{}, err
	}
	if commit.ParentSnapshotID != "" {
		parentSnapshotID = commit.ParentSnapshotID
	}
	if commit.ChannelVersions == nil {
		commit.ChannelVersions = map[string]int64{}
	}
	if commit.CreatedAt.IsZero() {
		commit.CreatedAt = time.Now()
	}
	snapshot := Snapshot{
		ID:               commit.SnapshotID,
		TaskID:           commit.TaskID,
		ChatID:           nextState.ChatID,
		ThreadID:         nextState.ThreadID,
		Step:             nextStepNumber,
		State:            nextState,
		NextStep:         commit.NextStep,
		SourceNode:       commit.SourceNode,
		Writes:           collapseStateUpdates(commit.Updates),
		Metadata:         commit.Metadata,
		ParentSnapshotID: parentSnapshotID,
		ChannelVersions:  commit.ChannelVersions,
		CreatedAt:        commit.CreatedAt,
	}
	if snapshot.ID == "" {
		snapshot.ID = uuid.NewString()
	}

	planJSON, err := json.Marshal(nextState.Plan)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime plan: %w", err)
	}
	modelUsagesJSON, err := json.Marshal(nextState.ModelUsages)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime model usages: %w", err)
	}
	phaseUsagesJSON, err := json.Marshal(nextState.PhaseUsages)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime phase usages: %w", err)
	}
	tokenBudgetJSON, err := json.Marshal(nextState.TokenBudget)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime token budget: %w", err)
	}
	stateJSON, err := json.Marshal(snapshot.State)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime snapshot state: %w", err)
	}
	writesJSON, err := json.Marshal(snapshot.Writes)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime snapshot writes: %w", err)
	}
	metadataJSON, err := json.Marshal(snapshot.Metadata)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime snapshot metadata: %w", err)
	}
	channelVersionsJSON, err := json.Marshal(snapshot.ChannelVersions)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal runtime channel versions: %w", err)
	}

	err = s.execWithRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		var interruptedBy any
		if nextState.Interrupt != nil && nextState.Interrupt.MessageID != 0 {
			interruptedBy = nextState.Interrupt.MessageID
		}
		now := commit.CreatedAt.Format(time.RFC3339)
		// After #169 slice 3d the snapshot is the canonical source for
		// reducer-managed fields; this UPDATE is the bare minimum needed to
		// keep two pre-existing legacy paths working:
		//
		//   - status: GetActiveTaskForChat / web health / zombie sweep all
		//     filter via COALESCE(snapshot.state_status, task.status), so
		//     the legacy column is the fallback for tasks that have no
		//     snapshot yet (CreateTask without any commit). Keeping it
		//     fresh costs nothing and avoids edge-case divergence.
		//   - interrupted_by: dashboard SQL JOINs and a few legacy code
		//     paths still consume this column directly.
		//   - updated_at: row-touch timestamp used by ListTasksForChat
		//     ORDER BY for tie-breaking.
		//
		// The bulky JSON columns (plan_json, model_usages, phase_usages,
		// accumulated, token_budget) and reducer-managed scalars
		// (current_idx, planner_session, github_issue_number) are no
		// longer mirrored — the snapshot is read instead.
		if _, err := tx.Exec(`
			UPDATE hermes_task_states
			SET status = ?, interrupted_by = ?, updated_at = ?
			WHERE id = ?`,
			string(nextState.Status), interruptedBy, now, commit.TaskID,
		); err != nil {
			return err
		}
		_ = planJSON
		_ = modelUsagesJSON
		_ = phaseUsagesJSON
		_ = tokenBudgetJSON
		if err := replaceUnifiedSubTasksTx(tx, commit.TaskID, nextState.Plan); err != nil {
			return err
		}
		if err := upsertUnifiedTaskTx(tx, hermesStateToTaskState(currentTask, nextState), terminalEndedAt(nextState.Status, commit.CreatedAt)); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO hermes_snapshots
				(id, task_id, chat_id, thread_id, step, state_json, next_step, source_node,
				 writes_json, metadata_json, parent_snapshot_id, channel_versions_json,
				 state_status, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshot.ID, snapshot.TaskID, snapshot.ChatID, snapshot.ThreadID, snapshot.Step,
			string(stateJSON), string(snapshot.NextStep), string(snapshot.SourceNode),
			string(writesJSON), string(metadataJSON), snapshot.ParentSnapshotID,
			string(channelVersionsJSON),
			string(nextState.Status),
			snapshot.CreatedAt.Format(time.RFC3339),
		); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	s.broadcastUnifiedTask(commit.TaskID)
	return snapshot, nil
}

// GetLatestSnapshot returns the latest committed snapshot for taskID.
func (s *SQLiteTaskStore) GetLatestSnapshot(taskID string) (Snapshot, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, chat_id, thread_id, step, state_json, next_step, source_node,
		       writes_json, metadata_json, parent_snapshot_id, channel_versions_json, created_at
		FROM hermes_snapshots
		WHERE task_id = ?
		ORDER BY step DESC, created_at DESC
		LIMIT 1`, taskID)
	return scanSnapshot(row)
}

// GetLatestSnapshotForThread returns the most recent snapshot for a Telegram
// chat/thread pair regardless of task id.
func (s *SQLiteTaskStore) GetLatestSnapshotForThread(chatID int64, threadID int) (Snapshot, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, chat_id, thread_id, step, state_json, next_step, source_node,
		       writes_json, metadata_json, parent_snapshot_id, channel_versions_json, created_at
		FROM hermes_snapshots
		WHERE chat_id = ? AND thread_id = ?
		ORDER BY created_at DESC, step DESC
		LIMIT 1`, chatID, threadID)
	return scanSnapshot(row)
}

// StaleInterruptRef describes one task that holds a HermesInterrupt older
// than the cutoff supplied to ListStaleInterrupts. Returned by the
// startup-orphan-cleanup pass so callers can mark the task failed and
// emit a HumanInterruptTimeout runtime event.
type StaleInterruptRef struct {
	TaskID    string
	ChatID    int64
	ThreadID  int
	Step      int
	State     HermesState
	Interrupt HermesInterrupt
}

// ListStaleInterrupts returns active tasks (status not terminal) whose
// latest snapshot carries an Interrupt with CreatedAt older than the
// cutoff. The query joins hermes_task_states (for status filter) with the
// latest snapshot per task; rows whose state JSON has no interrupt or
// whose interrupt is younger than the cutoff are filtered in Go.
//
// Used at alice startup to fail tasks whose engine goroutine died while
// blocking on a human-in-the-loop pause (e.g. process restart). With the
// engine still using an in-process channel for the actual block, we cannot
// resume those tasks; cleanup is the safe default.
func (s *SQLiteTaskStore) ListStaleInterrupts(cutoff time.Time) ([]StaleInterruptRef, error) {
	rows, err := s.db.Query(`
		SELECT snap.task_id, snap.chat_id, snap.thread_id, snap.step, snap.state_json
		FROM hermes_snapshots AS snap
		INNER JOIN hermes_task_states AS task ON task.id = snap.task_id
		INNER JOIN (
			SELECT task_id, MAX(step) AS max_step
			FROM hermes_snapshots
			GROUP BY task_id
		) latest ON latest.task_id = snap.task_id AND latest.max_step = snap.step
		WHERE task.status NOT IN ('done','failed','interrupted')`)
	if err != nil {
		return nil, fmt.Errorf("ListStaleInterrupts query: %w", err)
	}
	defer rows.Close()

	var stale []StaleInterruptRef
	for rows.Next() {
		var (
			taskID    string
			chatID    int64
			threadID  int
			step      int
			stateJSON string
		)
		if err := rows.Scan(&taskID, &chatID, &threadID, &step, &stateJSON); err != nil {
			return nil, fmt.Errorf("ListStaleInterrupts scan: %w", err)
		}
		var state HermesState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			return nil, fmt.Errorf("ListStaleInterrupts unmarshal task=%s: %w", taskID, err)
		}
		if state.Interrupt == nil {
			continue
		}
		if state.Interrupt.CreatedAt.IsZero() || state.Interrupt.CreatedAt.After(cutoff) {
			continue
		}
		stale = append(stale, StaleInterruptRef{
			TaskID:    taskID,
			ChatID:    chatID,
			ThreadID:  threadID,
			Step:      step,
			State:     state,
			Interrupt: *state.Interrupt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListStaleInterrupts rows: %w", err)
	}
	return stale, nil
}

// ListSnapshotHistory returns all snapshots for taskID in chronological step
// order. This is the read side needed for audit/debug timelines.
func (s *SQLiteTaskStore) ListSnapshotHistory(taskID string) ([]Snapshot, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, chat_id, thread_id, step, state_json, next_step, source_node,
		       writes_json, metadata_json, parent_snapshot_id, channel_versions_json, created_at
		FROM hermes_snapshots
		WHERE task_id = ?
		ORDER BY step ASC, created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []Snapshot
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, ErrNoTask
	}
	return snapshots, nil
}

func (s *SQLiteTaskStore) GetTask(id string) (TaskState, error) {
	row := s.db.QueryRow(`
		SELECT id, chat_id, thread_id, planner_session, project_dir, goal, current_idx, accumulated,
		       status, interrupted_by, interrupt_policy, token_budget, plan_json,
		       github_issue_number, model_usages, phase_usages, created_at, updated_at
		FROM hermes_task_states WHERE id = ?`, id)
	base, err := s.scanTask(row)
	if err != nil {
		return base, err
	}
	return s.overlayLatestSnapshot(id, base)
}

// activeStatusFilter is the SQL fragment used by all "find active task"
// queries. Uses the latest snapshot's denormalized state_status when one
// exists; falls back to the legacy table's status column for tasks that
// were created (CreateTask) but have not yet committed any snapshot.
//
// COALESCE prefers snap.state_status (which #169 slice 3d makes the
// canonical source) and only reads task.status when no snapshot exists.
const activeStatusFilter = `
	COALESCE((
		SELECT s.state_status
		FROM hermes_snapshots s
		WHERE s.task_id = task.id
		ORDER BY s.step DESC
		LIMIT 1
	), task.status) NOT IN ('done','failed','interrupted')
`

func (s *SQLiteTaskStore) GetActiveTaskForChat(chatID int64) (TaskState, error) {
	row := s.db.QueryRow(`
		SELECT task.id, task.chat_id, task.thread_id, task.planner_session, task.project_dir, task.goal,
		       task.current_idx, task.accumulated,
		       task.status, task.interrupted_by, task.interrupt_policy, task.token_budget, task.plan_json,
		       task.github_issue_number, task.model_usages, task.phase_usages, task.created_at, task.updated_at
		FROM hermes_task_states AS task
		WHERE task.chat_id = ? AND `+activeStatusFilter+`
		ORDER BY task.created_at DESC LIMIT 1`, chatID)
	base, err := s.scanTask(row)
	if err != nil {
		return base, err
	}
	return s.overlayLatestSnapshot(base.ID, base)
}

// MarkTaskFailedDurable marks the task as failed at both the legacy
// status column and the snapshot layer, so post-#169-slice-3d filters
// (which read state_status from the latest snapshot) see the change.
//
// Used by startup orphan-recovery paths (zombie sweep + stale-interrupt
// sweep) where a task's owning goroutine died with the previous process.
// The snapshot insert carries metadata.Reason so dashboards can tell
// the failure came from recovery rather than normal flow.
func (s *SQLiteTaskStore) MarkTaskFailedDurable(taskID, reason string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("MarkTaskFailedDurable: task_id is required")
	}
	if reason == "" {
		reason = "startup_orphan_failed"
	}
	failed := TaskStatusFailed
	_, err := s.CommitRuntimeStep(RuntimeCommit{
		TaskID:     taskID,
		Updates:    []StateUpdate{{Status: &failed}},
		NextStep:   RuntimeStepTerminal,
		SourceNode: RuntimeStepExecutor,
		Metadata: SnapshotMetadata{
			Source: "orphan_recovery",
			Reason: reason,
		},
	})
	return err
}

// InterruptResolution names the operator's choice for a paused task.
// Mirrors engine.FailureDecision but lives at the storage layer so the
// resolution can be applied durably without dragging the engine package
// into the storage package.
type InterruptResolution string

const (
	InterruptResolutionRetry InterruptResolution = "retry"
	InterruptResolutionSkip  InterruptResolution = "skip"
	InterruptResolutionAbort InterruptResolution = "abort"
)

// ApplyInterruptResolution persists the operator's pause decision into
// the snapshot so resume can pick up correctly even after alice restart.
//
//	retry → clear Interrupt; RunFromState will re-execute the paused
//	        sub-task on resume (its Status is still in_progress).
//	skip  → mark plan[idx] as Skipped + advance CurrentIdx + clear
//	        Interrupt. RunFromState advances past the sub-task without
//	        re-running it.
//	abort → mark task failed durably via MarkTaskFailedDurable; no
//	        resume goroutine should be launched.
//
// Returns an error if the task has no pending Interrupt or the
// Interrupt's Payload does not carry a valid sub_task_idx for skip.
func (s *SQLiteTaskStore) ApplyInterruptResolution(taskID string, decision InterruptResolution) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("ApplyInterruptResolution: task_id is required")
	}
	snap, err := s.GetLatestSnapshot(taskID)
	if err != nil {
		return fmt.Errorf("ApplyInterruptResolution: load snapshot: %w", err)
	}
	if snap.State.Interrupt == nil {
		return fmt.Errorf("ApplyInterruptResolution: task %s has no pending interrupt", taskID)
	}

	switch decision {
	case InterruptResolutionAbort:
		return s.MarkTaskFailedDurable(taskID, "user_abort_after_pause")

	case InterruptResolutionSkip:
		idx, ok := InterruptSubTaskIdx(snap.State.Interrupt)
		if !ok {
			return fmt.Errorf("ApplyInterruptResolution: skip needs sub_task_idx in interrupt payload")
		}
		if idx < 0 || idx >= len(snap.State.Plan) {
			return fmt.Errorf("ApplyInterruptResolution: sub_task_idx %d out of range (plan size %d)", idx, len(snap.State.Plan))
		}
		plan := append([]SubTask(nil), snap.State.Plan...)
		plan[idx].Status = SubTaskSkipped
		nextIdx := idx + 1
		_, err := s.CommitRuntimeStep(RuntimeCommit{
			TaskID: taskID,
			Updates: []StateUpdate{{
				Plan:           plan,
				CurrentIdx:     &nextIdx,
				ClearInterrupt: true,
			}},
			NextStep:   RuntimeStepExecutor,
			SourceNode: RuntimeStepApproval,
			Metadata: SnapshotMetadata{
				Source: "interrupt_resolution",
				Reason: "user_skip_after_pause",
			},
		})
		return err

	case InterruptResolutionRetry:
		// Budget interrupts route differently from failure-pause:
		// resume at the SnappedStep the Walker recorded (could be
		// planner / executor / reviewer) and reset BudgetStartedAt so
		// wallclock-bounded budgets restart fresh. UsedTokens is not
		// reset — if the user hits "continue" on a token-exhausted
		// budget, the gate will re-trip on the next iteration and they
		// can choose abort or expand MaxTotalTokens. See #171 Class B
		// follow-up.
		if snap.State.Interrupt.Reason == "budget_exceeded" {
			resumeStep := snap.State.Interrupt.ResumeStep
			if resumeStep == "" {
				resumeStep = RuntimeStepExecutor
			}
			now := time.Now()
			_, err := s.CommitRuntimeStep(RuntimeCommit{
				TaskID: taskID,
				Updates: []StateUpdate{{
					ClearInterrupt:  true,
					BudgetStartedAt: &now,
				}},
				NextStep:   resumeStep,
				SourceNode: RuntimeStepApproval,
				Metadata: SnapshotMetadata{
					Source: "interrupt_resolution",
					Reason: "user_continue_after_budget",
				},
			})
			return err
		}
		_, err := s.CommitRuntimeStep(RuntimeCommit{
			TaskID:     taskID,
			Updates:    []StateUpdate{{ClearInterrupt: true}},
			NextStep:   RuntimeStepExecutor,
			SourceNode: RuntimeStepApproval,
			Metadata: SnapshotMetadata{
				Source: "interrupt_resolution",
				Reason: "user_retry_after_pause",
			},
		})
		return err

	default:
		return fmt.Errorf("ApplyInterruptResolution: unknown decision %q", decision)
	}
}

// InterruptSubTaskIdx extracts the sub-task index that a HermesInterrupt
// was created for. JSON unmarshalling of `Payload any` produces
// map[string]any with float64 numeric fields, so handle both float64 and
// int paths defensively.
func InterruptSubTaskIdx(interrupt *HermesInterrupt) (int, bool) {
	if interrupt == nil {
		return 0, false
	}
	payload, ok := interrupt.Payload.(map[string]any)
	if !ok {
		return 0, false
	}
	switch v := payload["sub_task_idx"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	return 0, false
}

// SweepZombieTasks marks tasks whose latest snapshot is non-terminal as
// failed, recording a recovery reason. Returns the count swept. Replaces
// the legacy startup UPDATE on hermes_task_states.status which after
// #169 slice 3d is no longer authoritative.
//
// Tasks whose latest snapshot has an active HermesInterrupt (i.e. the
// task is legitimately paused waiting for the operator's pause-decision
// click) are excluded — killing them on every restart would defeat the
// β1/β2 cold-restart resume path. SweepStaleHermesInterrupts handles
// abandoning paused tasks via its own 24h cutoff.
func (s *SQLiteTaskStore) SweepZombieTasks(reason string) (int, error) {
	rows, err := s.db.Query(`
		SELECT task.id, snap.state_json FROM hermes_task_states AS task
		LEFT JOIN (
			SELECT s.task_id, s.state_json, s.state_status FROM hermes_snapshots s
			INNER JOIN (
				SELECT task_id, MAX(step) AS max_step
				FROM hermes_snapshots
				GROUP BY task_id
			) latest ON latest.task_id = s.task_id AND latest.max_step = s.step
		) AS snap ON snap.task_id = task.id
		WHERE COALESCE(snap.state_status, task.status) IN ('planning','executing','validating')`)
	if err != nil {
		return 0, fmt.Errorf("SweepZombieTasks query: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		var stateJSON sql.NullString
		if err := rows.Scan(&id, &stateJSON); err != nil {
			return 0, fmt.Errorf("SweepZombieTasks scan: %w", err)
		}
		// Skip tasks with an active interrupt — they are legitimately
		// paused waiting for the operator's pause-decision click. The
		// stale-interrupt sweeper (24h cutoff) handles abandonment.
		if stateJSON.Valid && stateJSON.String != "" {
			var state HermesState
			if err := json.Unmarshal([]byte(stateJSON.String), &state); err == nil && state.Interrupt != nil {
				continue
			}
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("SweepZombieTasks rows: %w", err)
	}

	swept := 0
	for _, id := range ids {
		if err := s.MarkTaskFailedDurable(id, reason); err != nil {
			// Best-effort: log via caller (no logger here); skip and continue.
			continue
		}
		swept++
	}
	return swept, nil
}

// overlayLatestSnapshot applies the latest committed snapshot's State to
// the base TaskState loaded from the legacy hermes_task_states row. After
// #169 slice 3a + 3b every reducer-managed mutation produces a snapshot,
// so the snapshot is the canonical source for fields it covers (Status,
// CurrentIdx, Plan, Accumulated, ModelUsages, PhaseUsages, TokenBudget,
// PlannerSessionID, ExecutorSessionID, Interrupt).
//
// Non-reducer fields (CreatedAt, UpdatedAt, InterruptPolicy, ChatID,
// ProjectDir, Goal) stay from the legacy row. The base's Artifacts are
// also preserved when the snapshot has none — Artifacts are reducer-aware
// but AppendArtifact is rarely used in production, so the legacy
// artifacts table remains the practical truth for now.
//
// When no snapshot exists yet (CreateTask just ran, no commit yet) the
// legacy row is returned unchanged. This keeps reads correct during the
// transition window before slice 3d removes the legacy UPDATE inside
// CommitRuntimeStep.
func (s *SQLiteTaskStore) overlayLatestSnapshot(taskID string, base TaskState) (TaskState, error) {
	snap, err := s.GetLatestSnapshot(taskID)
	if err == ErrNoTask {
		return base, nil
	}
	if err != nil {
		return base, err
	}
	legacyArtifacts := base.Artifacts
	merged := hermesStateToTaskState(base, snap.State)
	if len(merged.Artifacts) == 0 && len(legacyArtifacts) > 0 {
		merged.Artifacts = legacyArtifacts
	}
	return merged, nil
}

func (s *SQLiteTaskStore) StorePlan(taskID string, plan []SubTask) error {
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("StorePlan marshal: %w", err)
	}
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(
			`UPDATE hermes_task_states SET plan_json = ?, updated_at = ? WHERE id = ?`,
			string(planJSON), time.Now().Format(time.RFC3339), taskID,
		)
		if err != nil {
			return err
		}
		if err := s.replaceUnifiedSubTasks(taskID, plan); err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

// mutateSubTask is the single entry point that load-modifies-saves a sub-task
// and runs the unified-table mirror + broadcast. Both UpdateSubTask and
// MarkSubTaskStarted route through this helper; the only difference is whether
// current_idx is also written.
func (s *SQLiteTaskStore) mutateSubTask(taskID string, idx int, setCurrentIdx bool, mutate func(*SubTask)) error {
	return s.execWithRetry(func() error {
		var planJSON string
		if err := s.db.QueryRow(`SELECT plan_json FROM hermes_task_states WHERE id = ?`, taskID).
			Scan(&planJSON); err != nil {
			return err
		}
		var plan []SubTask
		if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
			return err
		}
		if idx < 0 || idx >= len(plan) {
			return fmt.Errorf("sub-task index %d out of range", idx)
		}
		mutate(&plan[idx])

		updated, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		now := time.Now().Format(time.RFC3339)
		if setCurrentIdx {
			_, err = s.db.Exec(
				`UPDATE hermes_task_states SET current_idx = ?, plan_json = ?, updated_at = ? WHERE id = ?`,
				idx, string(updated), now, taskID,
			)
		} else {
			_, err = s.db.Exec(
				`UPDATE hermes_task_states SET plan_json = ?, updated_at = ? WHERE id = ?`,
				string(updated), now, taskID,
			)
		}
		if err != nil {
			return err
		}
		if err := s.upsertUnifiedSubTask(taskID, idx, plan[idx]); err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

func (s *SQLiteTaskStore) UpdateSubTask(taskID string, idx int, status SubTaskStatus, result string, tokensUsed int) error {
	return s.mutateSubTask(taskID, idx, false, func(st *SubTask) {
		st.Status = status
		st.Result = result
		st.TokensUsed += tokensUsed
		st.Attempts++
	})
}

func (s *SQLiteTaskStore) MarkSubTaskStarted(taskID string, idx int) error {
	return s.mutateSubTask(taskID, idx, true, func(st *SubTask) {
		st.Status = SubTaskInProgress
	})
}

func (s *SQLiteTaskStore) AdvanceTask(taskID string, nextIdx int, status TaskStatus) error {
	return s.execWithRetry(func() error {
		var current string
		if err := s.db.QueryRow(`SELECT status FROM hermes_task_states WHERE id = ?`, taskID).Scan(&current); err != nil {
			if err == sql.ErrNoRows {
				return ErrNoTask
			}
			return err
		}
		if err := ValidateTaskStatusTransition(taskID, TaskStatus(current), status); err != nil {
			return err
		}
		_, err := s.db.Exec(
			`UPDATE hermes_task_states SET current_idx = ?, status = ?, updated_at = ? WHERE id = ?`,
			nextIdx, string(status), time.Now().Format(time.RFC3339), taskID,
		)
		if err != nil {
			return err
		}
		if err := s.updateUnifiedTaskStatus(taskID, status); err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

func (s *SQLiteTaskStore) AppendArtifact(taskID string, artifact Artifact) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO hermes_task_artifacts (task_id, path, hash, sub_task_id, created_at)
			VALUES (?,?,?,?,?)`,
			taskID, artifact.Path, artifact.Hash, artifact.SubTaskID,
			time.Now().Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
		if err := s.insertUnifiedArtifact(taskID, artifact); err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

func (s *SQLiteTaskStore) UpdateAccumulated(taskID string, accumulated string) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(
			`UPDATE hermes_task_states SET accumulated = ?, updated_at = ? WHERE id = ?`,
			accumulated, time.Now().Format(time.RFC3339), taskID,
		)
		return err
	})
}

func (s *SQLiteTaskStore) UpdatePlannerSession(taskID string, sessionID string) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(
			`UPDATE hermes_task_states SET planner_session = ?, updated_at = ? WHERE id = ?`,
			sessionID, time.Now().Format(time.RFC3339), taskID,
		)
		return err
	})
}

func (s *SQLiteTaskStore) MarkInterrupted(taskID string, messageID int64) error {
	return s.execWithRetry(func() error {
		var current string
		if err := s.db.QueryRow(`SELECT status FROM hermes_task_states WHERE id = ?`, taskID).Scan(&current); err != nil {
			if err == sql.ErrNoRows {
				return ErrNoTask
			}
			return err
		}
		if err := ValidateTaskStatusTransition(taskID, TaskStatus(current), TaskStatusInterrupted); err != nil {
			return err
		}
		_, err := s.db.Exec(
			`UPDATE hermes_task_states SET status = 'interrupted', interrupted_by = ?, updated_at = ? WHERE id = ?`,
			messageID, time.Now().Format(time.RFC3339), taskID,
		)
		if err != nil {
			return err
		}
		if err := s.updateUnifiedTaskStatus(taskID, TaskStatusInterrupted); err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

func (s *SQLiteTaskStore) MarkStatus(taskID string, status TaskStatus) error {
	return s.execWithRetry(func() error {
		var current string
		if err := s.db.QueryRow(`SELECT status FROM hermes_task_states WHERE id = ?`, taskID).Scan(&current); err != nil {
			if err == sql.ErrNoRows {
				return ErrNoTask
			}
			return err
		}
		if err := ValidateTaskStatusTransition(taskID, TaskStatus(current), status); err != nil {
			return err
		}
		_, err := s.db.Exec(
			`UPDATE hermes_task_states SET status = ?, updated_at = ? WHERE id = ?`,
			string(status), time.Now().Format(time.RFC3339), taskID,
		)
		if err != nil {
			return err
		}
		if err := s.updateUnifiedTaskStatus(taskID, status); err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

func (s *SQLiteTaskStore) ResetBudgetStartedAt(taskID string, t time.Time) error {
	return s.execWithRetry(func() error {
		var raw string
		err := s.db.QueryRow(`SELECT token_budget FROM hermes_task_states WHERE id = ?`, taskID).Scan(&raw)
		if err != nil {
			return err
		}
		var budget TokenBudget
		if err := json.Unmarshal([]byte(raw), &budget); err != nil {
			return err
		}
		budget.StartedAt = t
		updated, err := json.Marshal(budget)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(
			`UPDATE hermes_task_states SET token_budget = ?, updated_at = ? WHERE id = ?`,
			string(updated), time.Now().Format(time.RFC3339), taskID,
		)
		return err
	})
}

func (s *SQLiteTaskStore) AddTokenUsage(taskID string, delta int) error {
	return s.execWithRetry(func() error {
		var budgetJSON string
		if err := s.db.QueryRow(`SELECT token_budget FROM hermes_task_states WHERE id = ?`, taskID).
			Scan(&budgetJSON); err != nil {
			return err
		}
		var budget TokenBudget
		if err := json.Unmarshal([]byte(budgetJSON), &budget); err != nil {
			return err
		}
		budget.UsedTokens += delta

		updated, err := json.Marshal(budget)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(
			`UPDATE hermes_task_states SET token_budget = ?, updated_at = ? WHERE id = ?`,
			string(updated), time.Now().Format(time.RFC3339), taskID,
		)
		return err
	})
}

func (s *SQLiteTaskStore) AddModelUsage(taskID, model string, inputTokens, outputTokens int, costUSD float64) error {
	return s.AddModelUsageBreakdown(taskID, model, TokenUsageBreakdown{
		UncachedInputTokens: inputTokens,
		OutputTokens:        outputTokens,
		CostUSD:             costUSD,
	})
}

func (s *SQLiteTaskStore) AddModelUsageBreakdown(taskID, model string, usage TokenUsageBreakdown) error {
	return s.execWithRetry(func() error {
		var modelUsagesJSON string
		if err := s.db.QueryRow(`SELECT model_usages FROM hermes_task_states WHERE id = ?`, taskID).
			Scan(&modelUsagesJSON); err != nil {
			return err
		}
		var usages []ModelUsage
		if modelUsagesJSON != "" {
			if err := json.Unmarshal([]byte(modelUsagesJSON), &usages); err != nil {
				return err
			}
		}
		// Merge: bump existing row if model already seen, otherwise append.
		found := false
		inputTokens := usage.InputVolume()
		for i := range usages {
			if usages[i].Model == model {
				usages[i].InputTokens += inputTokens
				usages[i].UncachedInputTokens += usage.UncachedInputTokens
				usages[i].CacheReadInputTokens += usage.CacheReadInputTokens
				usages[i].CacheCreationInputTokens += usage.CacheCreationInputTokens
				usages[i].OutputTokens += usage.OutputTokens
				usages[i].CostUSD += usage.CostUSD
				usages[i].CallCount++
				found = true
				break
			}
		}
		if !found {
			usages = append(usages, ModelUsage{
				Model:                    model,
				InputTokens:              inputTokens,
				UncachedInputTokens:      usage.UncachedInputTokens,
				CacheReadInputTokens:     usage.CacheReadInputTokens,
				CacheCreationInputTokens: usage.CacheCreationInputTokens,
				OutputTokens:             usage.OutputTokens,
				CostUSD:                  usage.CostUSD,
				CallCount:                1,
			})
		}
		updated, err := json.Marshal(usages)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(
			`UPDATE hermes_task_states SET model_usages = ?, updated_at = ? WHERE id = ?`,
			string(updated), time.Now().Format(time.RFC3339), taskID,
		)
		if err != nil {
			return err
		}
		if err := s.updateUnifiedTaskUsage(taskID, usages); err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

func (s *SQLiteTaskStore) AddPhaseUsage(taskID, phase, model string, inputTokens, outputTokens int, costUSD float64) error {
	return s.AddPhaseUsageBreakdown(taskID, phase, model, TokenUsageBreakdown{
		UncachedInputTokens: inputTokens,
		OutputTokens:        outputTokens,
		CostUSD:             costUSD,
	})
}

func (s *SQLiteTaskStore) AddPhaseUsageBreakdown(taskID, phase, model string, usage TokenUsageBreakdown) error {
	return s.execWithRetry(func() error {
		var phaseUsagesJSON string
		if err := s.db.QueryRow(`SELECT phase_usages FROM hermes_task_states WHERE id = ?`, taskID).
			Scan(&phaseUsagesJSON); err != nil {
			return err
		}
		var usages []PhaseUsage
		if phaseUsagesJSON != "" {
			if err := json.Unmarshal([]byte(phaseUsagesJSON), &usages); err != nil {
				return err
			}
		}
		found := false
		inputTokens := usage.InputVolume()
		for i := range usages {
			if usages[i].Phase == phase && usages[i].Model == model {
				usages[i].InputTokens += inputTokens
				usages[i].UncachedInputTokens += usage.UncachedInputTokens
				usages[i].CacheReadInputTokens += usage.CacheReadInputTokens
				usages[i].CacheCreationInputTokens += usage.CacheCreationInputTokens
				usages[i].OutputTokens += usage.OutputTokens
				usages[i].CostUSD += usage.CostUSD
				usages[i].CallCount++
				found = true
				break
			}
		}
		if !found {
			usages = append(usages, PhaseUsage{
				Phase:                    phase,
				Model:                    model,
				InputTokens:              inputTokens,
				UncachedInputTokens:      usage.UncachedInputTokens,
				CacheReadInputTokens:     usage.CacheReadInputTokens,
				CacheCreationInputTokens: usage.CacheCreationInputTokens,
				OutputTokens:             usage.OutputTokens,
				CostUSD:                  usage.CostUSD,
				CallCount:                1,
			})
		}
		updated, err := json.Marshal(usages)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(
			`UPDATE hermes_task_states SET phase_usages = ?, updated_at = ? WHERE id = ?`,
			string(updated), time.Now().Format(time.RFC3339), taskID,
		)
		if err != nil {
			return err
		}
		s.broadcastUnifiedTask(taskID)
		return nil
	})
}

func (s *SQLiteTaskStore) ListTasksForChat(chatID int64, limit int) ([]TaskState, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, thread_id, planner_session, project_dir, goal, current_idx, accumulated,
		       status, interrupted_by, interrupt_policy, token_budget, plan_json,
		       github_issue_number, model_usages, phase_usages, created_at, updated_at
		FROM hermes_task_states
		WHERE chat_id = ?
		ORDER BY created_at DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}

	// Collect base task data first, then close rows before loading artifacts.
	// Loading artifacts requires a second query on the same connection; keeping
	// rows open while issuing another query deadlocks on MaxOpenConns(1).
	var tasks []TaskState
	for rows.Next() {
		task, err := s.scanTaskRowNoArtifacts(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	// Now load artifacts and overlay the latest snapshot for each task with
	// the connection free. After #169 slice 3c the snapshot is the canonical
	// source for reducer-managed fields; the legacy row contributes only
	// CreatedAt / UpdatedAt / non-reducer columns.
	for i := range tasks {
		tasks[i].Artifacts, err = s.loadArtifacts(tasks[i].ID)
		if err != nil {
			return nil, err
		}
		tasks[i], err = s.overlayLatestSnapshot(tasks[i].ID, tasks[i])
		if err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

// ── scanning helpers ──────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(row rowScanner) (Snapshot, error) {
	var snapshot Snapshot
	var stateJSON, writesJSON, metadataJSON, channelVersionsJSON string
	var nextStep, sourceNode string
	var createdStr string

	err := row.Scan(
		&snapshot.ID, &snapshot.TaskID, &snapshot.ChatID, &snapshot.ThreadID, &snapshot.Step,
		&stateJSON, &nextStep, &sourceNode, &writesJSON, &metadataJSON,
		&snapshot.ParentSnapshotID, &channelVersionsJSON, &createdStr,
	)
	if err == sql.ErrNoRows {
		return snapshot, ErrNoTask
	}
	if err != nil {
		return snapshot, err
	}
	if err := json.Unmarshal([]byte(stateJSON), &snapshot.State); err != nil {
		return snapshot, fmt.Errorf("unmarshal snapshot state: %w", err)
	}
	if strings.TrimSpace(writesJSON) != "" {
		if err := json.Unmarshal([]byte(writesJSON), &snapshot.Writes); err != nil {
			return snapshot, fmt.Errorf("unmarshal snapshot writes: %w", err)
		}
	}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &snapshot.Metadata); err != nil {
			return snapshot, fmt.Errorf("unmarshal snapshot metadata: %w", err)
		}
	}
	if strings.TrimSpace(channelVersionsJSON) != "" {
		if err := json.Unmarshal([]byte(channelVersionsJSON), &snapshot.ChannelVersions); err != nil {
			return snapshot, fmt.Errorf("unmarshal snapshot channel versions: %w", err)
		}
	}
	if snapshot.ChannelVersions == nil {
		snapshot.ChannelVersions = map[string]int64{}
	}
	snapshot.NextStep = RuntimeStep(nextStep)
	snapshot.SourceNode = RuntimeStep(sourceNode)
	if snapshot.CreatedAt, err = time.Parse(time.RFC3339, createdStr); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (s *SQLiteTaskStore) scanTask(row rowScanner) (TaskState, error) {
	var task TaskState
	var statusStr, policyStr, budgetJSON, planJSON, modelUsagesJSON, phaseUsagesJSON string
	_ = policyStr // legacy interrupt_policy column; field removed, value ignored
	var interruptedBy sql.NullInt64
	var createdStr, updatedStr string

	err := row.Scan(
		&task.ID, &task.ChatID, &task.ThreadID, &task.PlannerSessionID, &task.ProjectDir, &task.Goal,
		&task.CurrentIdx, &task.Accumulated,
		&statusStr, &interruptedBy, &policyStr,
		&budgetJSON, &planJSON,
		&task.GithubIssueNumber, &modelUsagesJSON, &phaseUsagesJSON,
		&createdStr, &updatedStr,
	)
	if err == sql.ErrNoRows {
		return task, ErrNoTask
	}
	if err != nil {
		return task, err
	}

	task.Status = normalizeStatus(statusStr)
	if interruptedBy.Valid {
		task.InterruptedBy = &interruptedBy.Int64
	}
	if err := json.Unmarshal([]byte(planJSON), &task.Plan); err != nil {
		return task, err
	}
	if err := json.Unmarshal([]byte(budgetJSON), &task.TokenBudget); err != nil {
		return task, err
	}
	if modelUsagesJSON != "" {
		if err := json.Unmarshal([]byte(modelUsagesJSON), &task.ModelUsages); err != nil {
			return task, err
		}
	}
	if phaseUsagesJSON != "" {
		if err := json.Unmarshal([]byte(phaseUsagesJSON), &task.PhaseUsages); err != nil {
			return task, err
		}
	}
	if task.CreatedAt, err = time.Parse(time.RFC3339, createdStr); err != nil {
		return task, err
	}
	if task.UpdatedAt, err = time.Parse(time.RFC3339, updatedStr); err != nil {
		return task, err
	}

	// Load artifacts separately
	task.Artifacts, err = s.loadArtifacts(task.ID)
	return task, err
}

// scanTaskRowNoArtifacts scans a task row without loading its artifacts.
// Use this inside a rows.Next() loop to avoid opening a second query on the same connection.
func (s *SQLiteTaskStore) scanTaskRowNoArtifacts(rows *sql.Rows) (TaskState, error) {
	return s.scanRowsInto(rows)
}

func (s *SQLiteTaskStore) scanRowsInto(rows *sql.Rows) (TaskState, error) {
	var task TaskState
	var statusStr, policyStr, budgetJSON, planJSON, modelUsagesJSON, phaseUsagesJSON string
	_ = policyStr // legacy interrupt_policy column; field removed, value ignored
	var interruptedBy sql.NullInt64
	var createdStr, updatedStr string

	err := rows.Scan(
		&task.ID, &task.ChatID, &task.ThreadID, &task.PlannerSessionID, &task.ProjectDir, &task.Goal,
		&task.CurrentIdx, &task.Accumulated,
		&statusStr, &interruptedBy, &policyStr,
		&budgetJSON, &planJSON,
		&task.GithubIssueNumber, &modelUsagesJSON, &phaseUsagesJSON,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return task, err
	}

	task.Status = normalizeStatus(statusStr)
	if interruptedBy.Valid {
		task.InterruptedBy = &interruptedBy.Int64
	}
	if err := json.Unmarshal([]byte(planJSON), &task.Plan); err != nil {
		return task, err
	}
	if err := json.Unmarshal([]byte(budgetJSON), &task.TokenBudget); err != nil {
		return task, err
	}
	if modelUsagesJSON != "" {
		if err := json.Unmarshal([]byte(modelUsagesJSON), &task.ModelUsages); err != nil {
			return task, err
		}
	}
	if phaseUsagesJSON != "" {
		if err := json.Unmarshal([]byte(phaseUsagesJSON), &task.PhaseUsages); err != nil {
			return task, err
		}
	}
	if task.CreatedAt, err = time.Parse(time.RFC3339, createdStr); err != nil {
		return task, err
	}
	if task.UpdatedAt, err = time.Parse(time.RFC3339, updatedStr); err != nil {
		return task, err
	}
	return task, nil
}

func (s *SQLiteTaskStore) loadArtifacts(taskID string) ([]Artifact, error) {
	rows, err := s.db.Query(`
		SELECT path, hash, sub_task_id FROM hermes_task_artifacts
		WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifacts []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.Path, &a.Hash, &a.SubTaskID); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, rows.Err()
}

// ── unified task mirror (#114) ────────────────────────────────────────────────

func (s *SQLiteTaskStore) upsertUnifiedTask(task TaskState, endedAt *time.Time) error {
	startedAt := task.CreatedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	totalIn, totalOut := totalModelUsage(task.ModelUsages)
	_, err := s.db.Exec(`
		INSERT INTO tasks
			(id, chat_id, thread_id, project_dir, github_issue_number, goal, engine, backend, status,
			 started_at, ended_at, total_input_tokens, total_output_tokens, total_cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, 'plan-execute', '', ?, ?, ?, ?, ?, 0)
		ON CONFLICT(id) DO UPDATE SET
			chat_id = excluded.chat_id,
			thread_id = excluded.thread_id,
			project_dir = CASE
				WHEN excluded.project_dir != '' THEN excluded.project_dir
				ELSE tasks.project_dir
			END,
			github_issue_number = CASE
				WHEN excluded.github_issue_number > 0 THEN excluded.github_issue_number
				ELSE tasks.github_issue_number
			END,
			goal = excluded.goal,
			engine = excluded.engine,
			status = excluded.status,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			total_input_tokens = excluded.total_input_tokens,
			total_output_tokens = excluded.total_output_tokens`,
		task.ID, task.ChatID, task.ThreadID, task.ProjectDir, task.GithubIssueNumber, task.Goal, string(task.Status),
		startedAt.Format(time.RFC3339Nano), formatUnifiedTime(endedAt), totalIn, totalOut,
	)
	return err
}

func upsertUnifiedTaskTx(tx *sql.Tx, task TaskState, endedAt *time.Time) error {
	startedAt := task.CreatedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	totalIn, totalOut := totalModelUsage(task.ModelUsages)
	_, err := tx.Exec(`
		INSERT INTO tasks
			(id, chat_id, thread_id, project_dir, github_issue_number, goal, engine, backend, status,
			 started_at, ended_at, total_input_tokens, total_output_tokens, total_cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, 'plan-execute', '', ?, ?, ?, ?, ?, 0)
		ON CONFLICT(id) DO UPDATE SET
			chat_id = excluded.chat_id,
			thread_id = excluded.thread_id,
			project_dir = CASE
				WHEN excluded.project_dir != '' THEN excluded.project_dir
				ELSE tasks.project_dir
			END,
			github_issue_number = CASE
				WHEN excluded.github_issue_number > 0 THEN excluded.github_issue_number
				ELSE tasks.github_issue_number
			END,
			goal = excluded.goal,
			engine = excluded.engine,
			status = excluded.status,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			total_input_tokens = excluded.total_input_tokens,
			total_output_tokens = excluded.total_output_tokens`,
		task.ID, task.ChatID, task.ThreadID, task.ProjectDir, task.GithubIssueNumber, task.Goal, string(task.Status),
		startedAt.Format(time.RFC3339Nano), formatUnifiedTime(endedAt), totalIn, totalOut,
	)
	return err
}

func (s *SQLiteTaskStore) replaceUnifiedSubTasks(taskID string, plan []SubTask) error {
	if _, err := s.db.Exec(`DELETE FROM sub_tasks WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for idx, subTask := range plan {
		if err := s.upsertUnifiedSubTask(taskID, idx, subTask); err != nil {
			return err
		}
	}
	return nil
}

func replaceUnifiedSubTasksTx(tx *sql.Tx, taskID string, plan []SubTask) error {
	if _, err := tx.Exec(`DELETE FROM sub_tasks WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for idx, subTask := range plan {
		if err := upsertUnifiedSubTaskTx(tx, taskID, idx, subTask); err != nil {
			return err
		}
	}
	return nil
}

// UnifiedSubTaskID returns the globally unique sub_tasks.id for a given task.
// Planner-supplied SubTask.ID values (e.g. "s1") are not unique across tasks,
// so the storage layer namespaces them under the parent task ID. Callers that
// need to reference these rows from review_subtask_results must use the same
// composite form.
func UnifiedSubTaskID(taskID string, idx int, plannerID string) string {
	if strings.TrimSpace(plannerID) == "" {
		return fmt.Sprintf("%s:%d", taskID, idx+1)
	}
	return fmt.Sprintf("%s:%s", taskID, plannerID)
}

func (s *SQLiteTaskStore) upsertUnifiedSubTask(taskID string, idx int, subTask SubTask) error {
	subTaskID := UnifiedSubTaskID(taskID, idx, subTask.ID)
	now := time.Now()
	var endedAt *time.Time
	if isTerminalSubTask(subTask.Status) {
		endedAt = &now
	}
	_, err := s.db.Exec(`
		INSERT INTO sub_tasks
			(id, task_id, idx, description, model, status, result_text, input_tokens,
			 output_tokens, cost_usd, started_at, ended_at, routing_reason, routing_latency_ms)
		VALUES (?, ?, ?, ?, '', ?, ?, ?, 0, 0, ?, ?, '', 0)
		ON CONFLICT(id) DO UPDATE SET
			idx = excluded.idx,
			description = excluded.description,
			status = excluded.status,
			result_text = excluded.result_text,
			input_tokens = excluded.input_tokens,
			ended_at = excluded.ended_at`,
		subTaskID, taskID, idx, subTask.Description, string(subTask.Status),
		subTask.Result, subTask.TokensUsed, now.Format(time.RFC3339Nano),
		formatUnifiedTime(endedAt),
	)
	return err
}

func upsertUnifiedSubTaskTx(tx *sql.Tx, taskID string, idx int, subTask SubTask) error {
	subTaskID := UnifiedSubTaskID(taskID, idx, subTask.ID)
	now := time.Now()
	var endedAt *time.Time
	if isTerminalSubTask(subTask.Status) {
		endedAt = &now
	}
	_, err := tx.Exec(`
		INSERT INTO sub_tasks
			(id, task_id, idx, description, model, status, result_text, input_tokens,
			 output_tokens, cost_usd, started_at, ended_at, routing_reason, routing_latency_ms)
		VALUES (?, ?, ?, ?, '', ?, ?, ?, 0, 0, ?, ?, '', 0)
		ON CONFLICT(id) DO UPDATE SET
			idx = excluded.idx,
			description = excluded.description,
			status = excluded.status,
			result_text = excluded.result_text,
			input_tokens = excluded.input_tokens,
			ended_at = excluded.ended_at`,
		subTaskID, taskID, idx, subTask.Description, string(subTask.Status),
		subTask.Result, subTask.TokensUsed, now.Format(time.RFC3339Nano),
		formatUnifiedTime(endedAt),
	)
	return err
}

func (s *SQLiteTaskStore) insertUnifiedArtifact(taskID string, artifact Artifact) error {
	if strings.TrimSpace(artifact.SubTaskID) == "" {
		return nil
	}
	subTaskID := UnifiedSubTaskID(taskID, 0, artifact.SubTaskID)
	_, err := s.db.Exec(`
		INSERT INTO artifacts (sub_task_id, path, hash)
		VALUES (?, ?, ?)`,
		subTaskID, artifact.Path, artifact.Hash,
	)
	return err
}

func (s *SQLiteTaskStore) updateUnifiedTaskStatus(taskID string, status TaskStatus) error {
	var endedAt *time.Time
	if isTerminalTask(status) {
		now := time.Now()
		endedAt = &now
	}
	_, err := s.db.Exec(`
		UPDATE tasks SET status = ?, ended_at = COALESCE(?, ended_at)
		WHERE id = ?`,
		string(status), formatUnifiedTime(endedAt), taskID,
	)
	return err
}

func (s *SQLiteTaskStore) updateUnifiedTaskUsage(taskID string, usages []ModelUsage) error {
	totalIn, totalOut := totalModelUsage(usages)
	_, err := s.db.Exec(`
		UPDATE tasks SET total_input_tokens = ?, total_output_tokens = ?
		WHERE id = ?`,
		totalIn, totalOut, taskID,
	)
	return err
}

func totalModelUsage(usages []ModelUsage) (int, int) {
	var totalIn, totalOut int
	for _, usage := range usages {
		totalIn += usage.InputTokens
		totalOut += usage.OutputTokens
	}
	return totalIn, totalOut
}

func isTerminalTask(status TaskStatus) bool {
	switch status {
	case TaskStatusDone, TaskStatusFailed, TaskStatusInterrupted:
		return true
	default:
		return false
	}
}

func isTerminalSubTask(status SubTaskStatus) bool {
	switch status {
	case SubTaskDone, SubTaskFailed, SubTaskSkipped:
		return true
	default:
		return false
	}
}

func terminalEndedAt(status TaskStatus, t time.Time) *time.Time {
	if !isTerminalTask(status) {
		return nil
	}
	if t.IsZero() {
		t = time.Now()
	}
	return &t
}

func hermesStateToTaskState(base TaskState, state HermesState) TaskState {
	base.ID = state.TaskID
	base.ChatID = state.ChatID
	base.ThreadID = state.ThreadID
	base.PlannerSessionID = state.PlannerSessionID
	base.ExecutorSessionID = state.ExecutorSessionID
	base.ProjectDir = state.ProjectDir
	base.Goal = state.Goal
	base.Plan = append([]SubTask(nil), state.Plan...)
	base.CurrentIdx = state.CurrentIdx
	base.Accumulated = state.Accumulated
	base.Artifacts = append([]Artifact(nil), state.Artifacts...)
	base.Status = state.Status
	base.TokenBudget = state.TokenBudget
	base.GithubIssueNumber = state.GithubIssueNumber
	base.ModelUsages = append([]ModelUsage(nil), state.ModelUsages...)
	base.PhaseUsages = append([]PhaseUsage(nil), state.PhaseUsages...)
	if state.Interrupt != nil && state.Interrupt.MessageID != 0 {
		messageID := state.Interrupt.MessageID
		base.InterruptedBy = &messageID
	} else {
		base.InterruptedBy = nil
	}
	return base
}

func formatUnifiedTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return sql.NullString{}
	}
	return t.Format(time.RFC3339Nano)
}

func (s *SQLiteTaskStore) broadcastUnifiedTask(taskID string) {
	if s.onUnifiedTaskUpdate != nil {
		s.onUnifiedTaskUpdate(taskID)
	}
}

// ── execWithRetry ─────────────────────────────────────────────────────────────

const (
	maxRetries = 5
	retryDelay = 50 * time.Millisecond
)

func (s *SQLiteTaskStore) execWithRetry(op func() error) error {
	for i := 0; i < maxRetries; i++ {
		err := op()
		if err == nil {
			return nil
		}
		if strings.Contains(err.Error(), "database is locked") ||
			strings.Contains(err.Error(), "SQLITE_BUSY") {
			if i < maxRetries-1 {
				time.Sleep(retryDelay * time.Duration(i+1))
				continue
			}
		}
		return err
	}
	return fmt.Errorf("hermes store: operation failed after %d retries", maxRetries)
}
