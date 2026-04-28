package hermes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

	// AddModelUsage accumulates per-model token usage for the #102 summary report.
	// Creates a new ModelUsage entry if the model has not been seen on this task.
	AddModelUsage(taskID, model string, inputTokens, outputTokens int) error

	// ListTasksForChat returns recent tasks for a chat (newest first).
	ListTasksForChat(chatID int64, limit int) ([]TaskState, error)
}

// ErrNoTask is returned when no matching task exists.
var ErrNoTask = fmt.Errorf("hermes: no task found")

// SQLiteTaskStore implements TaskStateStore backed by SQLite.
type SQLiteTaskStore struct {
	db                  *sql.DB
	onUnifiedTaskUpdate func(taskID string)
}

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
			created_at            TEXT NOT NULL,
			updated_at            TEXT NOT NULL
		)`,
		// Additive migrations for pre-existing tables.
		// SQLite does not support IF NOT EXISTS on ALTER TABLE — ignore "duplicate column" error.
		`ALTER TABLE hermes_task_states ADD COLUMN project_dir TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hermes_task_states ADD COLUMN github_issue_number INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE hermes_task_states ADD COLUMN model_usages TEXT NOT NULL DEFAULT '[]'`,
		`CREATE INDEX IF NOT EXISTS idx_hermes_tasks_chat_status
			ON hermes_task_states(chat_id, status)`,
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

	return task, s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO hermes_task_states
				(id, chat_id, planner_session, project_dir, goal, current_idx, accumulated,
				 status, interrupted_by, interrupt_policy, token_budget, plan_json,
				 github_issue_number, model_usages, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			task.ID, task.ChatID, task.PlannerSessionID, task.ProjectDir, task.Goal,
			task.CurrentIdx, task.Accumulated, string(task.Status),
			task.InterruptedBy, string(task.InterruptPolicy),
			string(budgetJSON), string(planJSON),
			task.GithubIssueNumber, string(modelUsagesJSON),
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

func (s *SQLiteTaskStore) GetTask(id string) (TaskState, error) {
	row := s.db.QueryRow(`
		SELECT id, chat_id, planner_session, project_dir, goal, current_idx, accumulated,
		       status, interrupted_by, interrupt_policy, token_budget, plan_json,
		       github_issue_number, model_usages, created_at, updated_at
		FROM hermes_task_states WHERE id = ?`, id)
	return s.scanTask(row)
}

func (s *SQLiteTaskStore) GetActiveTaskForChat(chatID int64) (TaskState, error) {
	row := s.db.QueryRow(`
		SELECT id, chat_id, planner_session, project_dir, goal, current_idx, accumulated,
		       status, interrupted_by, interrupt_policy, token_budget, plan_json,
		       github_issue_number, model_usages, created_at, updated_at
		FROM hermes_task_states
		WHERE chat_id = ? AND status NOT IN ('done','failed','interrupted')
		ORDER BY created_at DESC LIMIT 1`, chatID)
	return s.scanTask(row)
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

func (s *SQLiteTaskStore) UpdateSubTask(taskID string, idx int, status SubTaskStatus, result string, tokensUsed int) error {
	return s.execWithRetry(func() error {
		// Load current plan, patch, write back
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
		plan[idx].Status = status
		plan[idx].Result = result
		plan[idx].TokensUsed += tokensUsed
		plan[idx].Attempts++

		updated, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(
			`UPDATE hermes_task_states SET plan_json = ?, updated_at = ? WHERE id = ?`,
			string(updated), time.Now().Format(time.RFC3339), taskID,
		)
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

func (s *SQLiteTaskStore) MarkSubTaskStarted(taskID string, idx int) error {
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
		plan[idx].Status = SubTaskInProgress

		updated, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(
			`UPDATE hermes_task_states SET current_idx = ?, plan_json = ?, updated_at = ? WHERE id = ?`,
			idx, string(updated), time.Now().Format(time.RFC3339), taskID,
		)
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

func (s *SQLiteTaskStore) AdvanceTask(taskID string, nextIdx int, status TaskStatus) error {
	return s.execWithRetry(func() error {
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

func (s *SQLiteTaskStore) AddModelUsage(taskID, model string, inputTokens, outputTokens int) error {
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
		for i := range usages {
			if usages[i].Model == model {
				usages[i].InputTokens += inputTokens
				usages[i].OutputTokens += outputTokens
				usages[i].CallCount++
				found = true
				break
			}
		}
		if !found {
			usages = append(usages, ModelUsage{
				Model:        model,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				CallCount:    1,
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

func (s *SQLiteTaskStore) ListTasksForChat(chatID int64, limit int) ([]TaskState, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, planner_session, project_dir, goal, current_idx, accumulated,
		       status, interrupted_by, interrupt_policy, token_budget, plan_json,
		       github_issue_number, model_usages, created_at, updated_at
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

	// Now load artifacts for each task with the connection free.
	for i := range tasks {
		tasks[i].Artifacts, err = s.loadArtifacts(tasks[i].ID)
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

func (s *SQLiteTaskStore) scanTask(row rowScanner) (TaskState, error) {
	var task TaskState
	var statusStr, policyStr, budgetJSON, planJSON, modelUsagesJSON string
	var interruptedBy sql.NullInt64
	var createdStr, updatedStr string

	err := row.Scan(
		&task.ID, &task.ChatID, &task.PlannerSessionID, &task.ProjectDir, &task.Goal,
		&task.CurrentIdx, &task.Accumulated,
		&statusStr, &interruptedBy, &policyStr,
		&budgetJSON, &planJSON,
		&task.GithubIssueNumber, &modelUsagesJSON,
		&createdStr, &updatedStr,
	)
	if err == sql.ErrNoRows {
		return task, ErrNoTask
	}
	if err != nil {
		return task, err
	}

	task.Status = TaskStatus(statusStr)
	task.InterruptPolicy = InterruptPolicy(policyStr)
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

func (s *SQLiteTaskStore) scanTaskRow(rows *sql.Rows) (TaskState, error) {
	task, err := s.scanRowsInto(rows)
	if err != nil {
		return task, err
	}
	task.Artifacts, err = s.loadArtifacts(task.ID)
	return task, err
}

func (s *SQLiteTaskStore) scanRowsInto(rows *sql.Rows) (TaskState, error) {
	var task TaskState
	var statusStr, policyStr, budgetJSON, planJSON, modelUsagesJSON string
	var interruptedBy sql.NullInt64
	var createdStr, updatedStr string

	err := rows.Scan(
		&task.ID, &task.ChatID, &task.PlannerSessionID, &task.ProjectDir, &task.Goal,
		&task.CurrentIdx, &task.Accumulated,
		&statusStr, &interruptedBy, &policyStr,
		&budgetJSON, &planJSON,
		&task.GithubIssueNumber, &modelUsagesJSON,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return task, err
	}

	task.Status = TaskStatus(statusStr)
	task.InterruptPolicy = InterruptPolicy(policyStr)
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
		VALUES (?, ?, 0, ?, ?, ?, 'plan-execute', '', ?, ?, ?, ?, ?, 0)
		ON CONFLICT(id) DO UPDATE SET
			chat_id = excluded.chat_id,
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
		task.ID, task.ChatID, task.ProjectDir, task.GithubIssueNumber, task.Goal, string(task.Status),
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
