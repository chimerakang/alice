package app

// Read-only access to the records the removed Hermes task system left behind
// (hermes_task_states, hermes_snapshots). Nothing here creates, migrates or
// writes those tables: a database that never had them simply has no history.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var errNoHermesTables = errors.New("no hermes history in this database")

type hermesHistoryFilter struct {
	Status string
	Limit  int
	Offset int
}

type hermesInterrupt struct {
	ID         string     `json:"id,omitempty"`
	MessageID  int64      `json:"message_id,omitempty"`
	SourceStep string     `json:"source_step,omitempty"`
	ResumeStep string     `json:"resume_step,omitempty"`
	Payload    any        `json:"payload,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	CreatedAt  time.Time  `json:"created_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type hermesTaskRef struct {
	TaskID            string           `json:"task_id"`
	GithubURL         string           `json:"github_url,omitempty"`
	ChatID            int64            `json:"chat_id"`
	ThreadID          int              `json:"thread_id"`
	Goal              string           `json:"goal"`
	ProjectDir        string           `json:"project_dir"`
	GithubIssueNumber int              `json:"github_issue_number,omitempty"`
	Status            string           `json:"status"`
	NextStep          string           `json:"next_step,omitempty"`
	CurrentIdx        int              `json:"current_idx"`
	PlanLength        int              `json:"plan_length"`
	UsedTokens        int              `json:"used_tokens"`
	MaxTotalTokens    int              `json:"max_total_tokens,omitempty"`
	UpdatedAt         time.Time        `json:"updated_at"`
	Interrupt         *hermesInterrupt `json:"interrupt,omitempty"`
}

type hermesSubTask struct {
	ID                  string `json:"id"`
	Description         string `json:"description"`
	Status              string `json:"status"`
	Result              string `json:"result,omitempty"`
	Attempts            int    `json:"attempts"`
	TokensUsed          int    `json:"tokens_used"`
	StrictRetryFeedback string `json:"strict_retry_feedback,omitempty"`
}

// hermesSnapshotState is the part of a stored snapshot the history shows.
type hermesSnapshotState struct {
	ThreadID    int              `json:"thread_id"`
	ProjectDir  string           `json:"project_dir"`
	Status      string           `json:"status"`
	CurrentIdx  int              `json:"current_idx"`
	Plan        []hermesSubTask  `json:"plan"`
	Accumulated string           `json:"accumulated"`
	Interrupt   *hermesInterrupt `json:"interrupt"`
}

type hermesSnapshot struct {
	ID               string
	ParentSnapshotID string
	Step             int
	NextStep         string
	SourceNode       string
	Reason           string
	State            hermesSnapshotState
	RawState         string
	CreatedAt        time.Time
}

type hermesHistory struct{ db *sql.DB }

// globalHermesHistory reads the history from the app database, if it is open.
func globalHermesHistory() (hermesHistory, bool) {
	ss, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return hermesHistory{}, false
	}
	db, ok := ss.GetDB().(*sql.DB)
	if !ok {
		return hermesHistory{}, false
	}
	return hermesHistory{db: db}, true
}

// check returns errNoHermesTables when either table is absent.
func (h hermesHistory) check() error {
	var n int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name IN ('hermes_task_states','hermes_snapshots')`).Scan(&n); err != nil {
		return fmt.Errorf("check hermes tables: %w", err)
	}
	if n != 2 {
		return errNoHermesTables
	}
	return nil
}

// list returns tasks, newest first. A task's status is that of its latest
// snapshot where there is one.
func (h hermesHistory) list(filter hermesHistoryFilter) ([]hermesTaskRef, int, error) {
	if err := h.check(); errors.Is(err, errNoHermesTables) {
		return []hermesTaskRef{}, 0, nil
	} else if err != nil {
		return nil, 0, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where, args := "", []any{}
	if status := strings.TrimSpace(filter.Status); status != "" {
		where, args = "WHERE t.status = ?", append(args, status)
	}
	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM hermes_task_states AS t "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count hermes tasks: %w", err)
	}
	// The correlated subquery reads only the listed tasks' snapshots.
	rows, err := h.db.Query(`
		SELECT t.id, t.chat_id, t.goal, t.github_issue_number, t.status,
		       t.current_idx, t.token_budget, t.updated_at, t.plan_json,
		       snap.state_json, snap.next_step
		FROM hermes_task_states AS t
		LEFT JOIN hermes_snapshots AS snap
		       ON snap.task_id = t.id
		      AND snap.step = (SELECT MAX(step) FROM hermes_snapshots WHERE task_id = t.id)
		`+where+`
		ORDER BY t.updated_at DESC
		LIMIT ? OFFSET ?`, append(args, limit, filter.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list hermes tasks: %w", err)
	}
	defer rows.Close()
	out := []hermesTaskRef{}
	for rows.Next() {
		var ref hermesTaskRef
		var budgetJSON, planJSON, updated string
		var stateJSON, nextStep sql.NullString
		if err := rows.Scan(&ref.TaskID, &ref.ChatID, &ref.Goal, &ref.GithubIssueNumber, &ref.Status,
			&ref.CurrentIdx, &budgetJSON, &updated, &planJSON, &stateJSON, &nextStep); err != nil {
			return nil, 0, fmt.Errorf("scan hermes task: %w", err)
		}
		ref.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		// Old rows can hold JSON this code does not understand; show the
		// row with what can be read.
		var budget struct {
			Used int `json:"used_tokens"`
			Max  int `json:"max_total_tokens"`
		}
		_ = json.Unmarshal([]byte(budgetJSON), &budget)
		ref.UsedTokens, ref.MaxTotalTokens = budget.Used, budget.Max
		var plan []hermesSubTask
		_ = json.Unmarshal([]byte(planJSON), &plan)
		ref.PlanLength = len(plan)
		var state hermesSnapshotState
		if stateJSON.Valid && json.Unmarshal([]byte(stateJSON.String), &state) == nil {
			ref.ThreadID, ref.ProjectDir, ref.Interrupt = state.ThreadID, state.ProjectDir, state.Interrupt
		}
		ref.NextStep = nextStep.String
		out = append(out, ref)
	}
	return out, total, rows.Err()
}

// snapshots returns a task's snapshots in step order.
func (h hermesHistory) snapshots(taskID string) ([]hermesSnapshot, error) {
	if err := h.check(); errors.Is(err, errNoHermesTables) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	rows, err := h.db.Query(`
		SELECT id, parent_snapshot_id, step, state_json, next_step, source_node, metadata_json, created_at
		FROM hermes_snapshots WHERE task_id = ? ORDER BY step ASC, created_at ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list hermes snapshots: %w", err)
	}
	defer rows.Close()
	var out []hermesSnapshot
	for rows.Next() {
		var s hermesSnapshot
		var metadata, created string
		if err := rows.Scan(&s.ID, &s.ParentSnapshotID, &s.Step, &s.RawState, &s.NextStep,
			&s.SourceNode, &metadata, &created); err != nil {
			return nil, fmt.Errorf("scan hermes snapshot: %w", err)
		}
		if err := json.Unmarshal([]byte(s.RawState), &s.State); err != nil {
			return nil, fmt.Errorf("read hermes snapshot %s: %w", s.ID, err)
		}
		var meta struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal([]byte(metadata), &meta)
		s.Reason = meta.Reason
		s.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, s)
	}
	return out, rows.Err()
}

// task returns the stored task with its latest snapshot's state laid over it,
// keyed as the dashboard expects. It returns nil when the task is unknown.
func (h hermesHistory) task(taskID, latestState string) (map[string]any, error) {
	if err := h.check(); errors.Is(err, errNoHermesTables) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var (
		id, planner, projectDir, goal, accumulated, status, budget, plan, models, phases, created, updated string
		chatID                                                                                             int64
		threadID, currentIdx, issue                                                                        int
	)
	err := h.db.QueryRow(`
		SELECT id, chat_id, thread_id, planner_session, project_dir, goal, current_idx, accumulated,
		       status, token_budget, plan_json, github_issue_number, model_usages, phase_usages,
		       created_at, updated_at
		FROM hermes_task_states WHERE id = ?`, taskID).Scan(&id, &chatID, &threadID, &planner, &projectDir,
		&goal, &currentIdx, &accumulated, &status, &budget, &plan, &issue, &models, &phases, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hermes task: %w", err)
	}
	out := map[string]any{
		"id": id, "chat_id": chatID, "thread_id": threadID, "planner_session_id": planner,
		"project_dir": projectDir, "goal": goal, "current_idx": currentIdx, "accumulated": accumulated,
		"status": status, "github_issue_number": issue, "created_at": created, "updated_at": updated,
	}
	for key, raw := range map[string]string{"token_budget": budget, "plan": plan, "model_usages": models, "phase_usages": phases} {
		if json.Valid([]byte(raw)) {
			out[key] = json.RawMessage(raw)
		}
	}
	var state map[string]json.RawMessage
	if json.Unmarshal([]byte(latestState), &state) == nil {
		for key, value := range state {
			if _, known := out[key]; known || key == "phase_usages" || key == "model_usages" {
				out[key] = value
			}
		}
	}
	return out, nil
}
