package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

// UnifiedTaskStore is the transitional task-centric persistence API for #114.
// During migration it is written alongside decision_logs and Hermes' legacy
// tables so Dashboard queries can move over incrementally.
type UnifiedTaskStore interface {
	UpsertUnifiedTask(task UnifiedTask) error
	UpsertUnifiedSubTask(subTask UnifiedSubTask) error
	InsertUnifiedToolEvent(event UnifiedToolEvent) error
	InsertUnifiedArtifact(artifact UnifiedArtifact) error
	InsertUnifiedReviewResult(review UnifiedReviewResult) (int64, error)
	InsertUnifiedReviewSubTaskResult(result UnifiedReviewSubTaskResult) error
	StoreReview(ctx context.Context, taskID string, review appengine.ReviewResult) error
	ListUnifiedTaskGraphs(query UnifiedTaskQuery) ([]UnifiedTaskGraph, error)
	CountUnifiedTasks(query UnifiedTaskQuery) (int64, error)
	GetPlannerRulesWeeklyReport(windowStart, windowEnd time.Time, i18n *I18nManager, lang string) (PlannerRulesWeeklyReport, error)
	GetQualityDecompositionStats(window QualityWindow) (QualityDecompositionStats, error)
	GetQualityScoreStats(window QualityWindow) (QualityScoreStats, error)
	GetQualityInsights(window QualityWindow) ([]QualityInsight, error)
}

type UnifiedTask struct {
	ID                string     `json:"id"`
	ChatID            int64      `json:"chat_id"`
	ThreadID          int        `json:"thread_id"`
	ProjectDir        string     `json:"project_dir"`
	Goal              string     `json:"goal"`
	Engine            string     `json:"engine"`
	Backend           string     `json:"backend"`
	Status            string     `json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	TotalInputTokens  int        `json:"total_input_tokens"`
	TotalOutputTokens int        `json:"total_output_tokens"`
	TotalCostUSD      float64    `json:"total_cost_usd"`
}

type UnifiedSubTask struct {
	ID               string     `json:"id"`
	TaskID           string     `json:"task_id"`
	Idx              int        `json:"idx"`
	Description      string     `json:"description"`
	Model            string     `json:"model"`
	Status           string     `json:"status"`
	ResultText       string     `json:"result_text"`
	InputTokens      int        `json:"input_tokens"`
	OutputTokens     int        `json:"output_tokens"`
	CostUSD          float64    `json:"cost_usd"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	RoutingReason    string     `json:"routing_reason"`
	RoutingLatencyMS int        `json:"routing_latency_ms"`
}

type UnifiedToolEvent struct {
	ID         int64     `json:"id,omitempty"`
	SubTaskID  string    `json:"sub_task_id"`
	ToolName   string    `json:"tool_name"`
	InputJSON  string    `json:"input_json"`
	OutputJSON string    `json:"output_json"`
	Timestamp  time.Time `json:"ts"`
	Status     string    `json:"status"`
}

type UnifiedArtifact struct {
	ID        int64  `json:"id,omitempty"`
	SubTaskID string `json:"sub_task_id"`
	Path      string `json:"path"`
	Hash      string `json:"hash"`
}

type UnifiedReviewResult struct {
	ID             int64     `json:"id,omitempty"`
	TaskID         string    `json:"task_id"`
	ReviewerModel  string    `json:"reviewer_model"`
	Verdict        string    `json:"verdict"`
	OverallScore   int       `json:"overall_score"`
	FeedbackText   string    `json:"feedback_text"`
	IssueTags      []string  `json:"issue_tags"`
	InputTokens    int       `json:"input_tokens"`
	OutputTokens   int       `json:"output_tokens"`
	CostUSD        float64   `json:"cost_usd"`
	BlockCount     int       `json:"block_count"`
	AutoFixedCount int       `json:"auto_fixed_count"`
	Source         string    `json:"source"`
	CreatedAt      time.Time `json:"created_at"`
}

type UnifiedReviewSubTaskResult struct {
	ID        int64    `json:"id,omitempty"`
	ReviewID  int64    `json:"review_id"`
	SubTaskID string   `json:"sub_task_id"`
	Score     int      `json:"score"`
	Feedback  string   `json:"feedback"`
	IssueTags []string `json:"issue_tags"`
}

type UnifiedReviewGraph struct {
	UnifiedReviewResult
	SubTaskResults []UnifiedReviewSubTaskResult `json:"sub_task_results"`
}

type UnifiedSubTaskGraph struct {
	UnifiedSubTask
	ToolEvents []UnifiedToolEvent `json:"tool_events"`
	Artifacts  []UnifiedArtifact  `json:"artifacts"`
}

type UnifiedTaskGraph struct {
	UnifiedTask
	SubTasks []UnifiedSubTaskGraph `json:"sub_tasks"`
	Reviews  []UnifiedReviewGraph  `json:"reviews"`
}

type UnifiedTaskQuery struct {
	Limit      int
	Offset     int
	ID         string
	StartTime  *time.Time
	EndTime    *time.Time
	ProjectDir string
	Status     string
	HasReview  *bool
}

func unifiedTaskTablesSQL() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			id                  TEXT PRIMARY KEY,
			chat_id             INTEGER NOT NULL DEFAULT 0,
			thread_id           INTEGER NOT NULL DEFAULT 0,
			project_dir         TEXT NOT NULL DEFAULT '',
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
			source          TEXT NOT NULL DEFAULT 'initial',
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
}

func (s *SQLiteStorage) migrateUnifiedTaskTables() error {
	for _, stmt := range unifiedTaskTablesSQL() {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("unified task migration: %w", err)
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE review_results ADD COLUMN source TEXT NOT NULL DEFAULT 'initial'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("unified task source migration: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE review_results ADD COLUMN block_count INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("unified task block_count migration: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE review_results ADD COLUMN auto_fixed_count INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("unified task auto_fixed_count migration: %w", err)
	}
	return nil
}

// stripLegacyLanguagePrefixes removes leading language directives
// ("請用繁體中文回應。\n\n", English equivalent) from historical
// decision_logs.user_prompt and tasks.goal columns. New writes go through
// stripLanguageDirective in agent.go; this catches pre-existing rows so
// Dashboard / Timeline titles don't keep showing the prefix.
// Idempotent — uses LIKE to match only rows that still carry the prefix.
func (s *SQLiteStorage) stripLegacyLanguagePrefixes() error {
	pairs := []struct{ table, column, prefix string }{
		{"decision_logs", "user_prompt", "請用繁體中文回應。\n\n"},
		{"decision_logs", "user_prompt", "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n"},
		{"tasks", "goal", "請用繁體中文回應。\n\n"},
		{"tasks", "goal", "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n"},
		{"sub_tasks", "description", "請用繁體中文回應。\n\n"},
		{"sub_tasks", "description", "Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n"},
	}
	total := int64(0)
	for _, p := range pairs {
		stmt := fmt.Sprintf(
			`UPDATE %s SET %s = substr(%s, length(?) + 1) WHERE %s LIKE ?`,
			p.table, p.column, p.column, p.column,
		)
		res, err := s.db.Exec(stmt, p.prefix, p.prefix+"%")
		if err != nil {
			return fmt.Errorf("strip %s.%s: %w", p.table, p.column, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			total += n
		}
	}
	if total > 0 {
		log.Printf("[storage] stripped language directive prefix from %d historical row(s)", total)
	}
	return nil
}

// backfillDecisionLogsToUnified copies any historical decision_logs that have
// not yet been mirrored into the unified task tables. Idempotent — UpsertUnifiedTask
// uses INSERT OR REPLACE, so re-running on the same record is a no-op.
// Skips when count(tasks where id LIKE 'decision:%') ≥ count(decision_logs).
func (s *SQLiteStorage) backfillDecisionLogsToUnified() error {
	var existing int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id LIKE 'decision:%'`).Scan(&existing); err != nil {
		return fmt.Errorf("backfill count tasks: %w", err)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM decision_logs`).Scan(&total); err != nil {
		return fmt.Errorf("backfill count decision_logs: %w", err)
	}
	if existing >= total {
		return nil
	}
	log.Printf("[storage] backfilling decision_logs → unified tasks (mirrored=%d, total=%d)", existing, total)

	const batchSize = 500
	processed := 0
	for offset := 0; ; offset += batchSize {
		decisions, err := s.GetDecisionLogs(batchSize, offset)
		if err != nil {
			return fmt.Errorf("backfill page offset=%d: %w", offset, err)
		}
		if len(decisions) == 0 {
			break
		}
		for _, d := range decisions {
			if err := s.insertDecisionLogUnified(d); err != nil {
				log.Printf("[storage] backfill skipped session=%s ts=%s: %v", d.SessionID, d.Timestamp.Format(time.RFC3339), err)
				continue
			}
			processed++
		}
	}
	log.Printf("[storage] backfill complete: %d records mirrored", processed)
	return nil
}

func (s *SQLiteStorage) UpsertUnifiedTask(task UnifiedTask) error {
	if task.StartedAt.IsZero() {
		task.StartedAt = time.Now()
	}
	_, err := s.db.Exec(`
		INSERT INTO tasks
			(id, chat_id, thread_id, project_dir, goal, engine, backend, status,
			 started_at, ended_at, total_input_tokens, total_output_tokens, total_cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			chat_id = excluded.chat_id,
			thread_id = excluded.thread_id,
			project_dir = excluded.project_dir,
			goal = excluded.goal,
			engine = excluded.engine,
			backend = excluded.backend,
			status = excluded.status,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			total_input_tokens = excluded.total_input_tokens,
			total_output_tokens = excluded.total_output_tokens,
			total_cost_usd = excluded.total_cost_usd`,
		task.ID, task.ChatID, task.ThreadID, task.ProjectDir, task.Goal,
		task.Engine, task.Backend, task.Status, task.StartedAt.Format(time.RFC3339Nano),
		formatNullableTime(task.EndedAt), task.TotalInputTokens, task.TotalOutputTokens,
		task.TotalCostUSD,
	)
	return err
}

func (s *SQLiteStorage) UpsertUnifiedSubTask(subTask UnifiedSubTask) error {
	if subTask.StartedAt.IsZero() {
		subTask.StartedAt = time.Now()
	}
	_, err := s.db.Exec(`
		INSERT INTO sub_tasks
			(id, task_id, idx, description, model, status, result_text, input_tokens,
			 output_tokens, cost_usd, started_at, ended_at, routing_reason, routing_latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task_id = excluded.task_id,
			idx = excluded.idx,
			description = excluded.description,
			model = excluded.model,
			status = excluded.status,
			result_text = excluded.result_text,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			routing_reason = excluded.routing_reason,
			routing_latency_ms = excluded.routing_latency_ms`,
		subTask.ID, subTask.TaskID, subTask.Idx, subTask.Description, subTask.Model,
		subTask.Status, subTask.ResultText, subTask.InputTokens, subTask.OutputTokens,
		subTask.CostUSD, subTask.StartedAt.Format(time.RFC3339Nano),
		formatNullableTime(subTask.EndedAt), subTask.RoutingReason, subTask.RoutingLatencyMS,
	)
	return err
}

func (s *SQLiteStorage) InsertUnifiedToolEvent(event UnifiedToolEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.InputJSON == "" {
		event.InputJSON = "{}"
	}
	if event.OutputJSON == "" {
		event.OutputJSON = "{}"
	}
	_, err := s.db.Exec(`
		INSERT INTO tool_events (sub_task_id, tool_name, input_json, output_json, ts, status)
		VALUES (?, ?, ?, ?, ?, ?)`,
		event.SubTaskID, event.ToolName, event.InputJSON, event.OutputJSON,
		event.Timestamp.Format(time.RFC3339Nano), event.Status,
	)
	return err
}

func (s *SQLiteStorage) InsertUnifiedArtifact(artifact UnifiedArtifact) error {
	_, err := s.db.Exec(`
		INSERT INTO artifacts (sub_task_id, path, hash)
		VALUES (?, ?, ?)`,
		artifact.SubTaskID, artifact.Path, artifact.Hash,
	)
	return err
}

func (s *SQLiteStorage) InsertUnifiedReviewResult(review UnifiedReviewResult) (int64, error) {
	if review.CreatedAt.IsZero() {
		review.CreatedAt = time.Now()
	}
	if strings.TrimSpace(review.Source) == "" {
		review.Source = "initial"
	}
	tagsJSON, err := json.Marshal(review.IssueTags)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`
		INSERT INTO review_results
			(task_id, reviewer_model, verdict, overall_score, feedback_text, issue_tags,
			 input_tokens, output_tokens, cost_usd, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		review.TaskID, review.ReviewerModel, review.Verdict, review.OverallScore,
		review.FeedbackText, string(tagsJSON), review.InputTokens, review.OutputTokens,
		review.CostUSD, review.Source, review.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStorage) InsertUnifiedReviewSubTaskResult(result UnifiedReviewSubTaskResult) error {
	tagsJSON, err := json.Marshal(result.IssueTags)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO review_subtask_results
			(review_id, sub_task_id, score, feedback, issue_tags)
		VALUES (?, ?, ?, ?, ?)`,
		result.ReviewID, result.SubTaskID, result.Score, result.Feedback, string(tagsJSON),
	)
	return err
}

func (s *SQLiteStorage) ListUnifiedTaskGraphs(query UnifiedTaskQuery) ([]UnifiedTaskGraph, error) {
	where, args := unifiedTaskWhere(query)
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, chat_id, thread_id, project_dir, goal, engine, backend, status,
		       started_at, ended_at, total_input_tokens, total_output_tokens, total_cost_usd
		FROM tasks`+where+`
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?`, append(args, limit, maxInt(query.Offset, 0))...)
	if err != nil {
		return nil, err
	}

	tasks := make([]UnifiedTaskGraph, 0)
	for rows.Next() {
		task, err := scanUnifiedTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, UnifiedTaskGraph{UnifiedTask: task})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range tasks {
		subTasks, err := s.listUnifiedSubTaskGraphs(tasks[i].ID)
		if err != nil {
			return nil, err
		}
		reviews, err := s.listUnifiedReviewGraphs(tasks[i].ID)
		if err != nil {
			return nil, err
		}
		tasks[i].SubTasks = subTasks
		tasks[i].Reviews = reviews
	}
	return tasks, nil
}

func (s *SQLiteStorage) CountUnifiedTasks(query UnifiedTaskQuery) (int64, error) {
	where, args := unifiedTaskWhere(query)
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`+where, args...).Scan(&count)
	return count, err
}

func unifiedTaskWhere(query UnifiedTaskQuery) (string, []any) {
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 6)
	if query.ID != "" {
		clauses = append(clauses, "id = ?")
		args = append(args, query.ID)
	}
	if query.StartTime != nil {
		clauses = append(clauses, "started_at >= ?")
		args = append(args, query.StartTime.Format(time.RFC3339Nano))
	}
	if query.EndTime != nil {
		clauses = append(clauses, "started_at <= ?")
		args = append(args, query.EndTime.Format(time.RFC3339Nano))
	}
	if query.ProjectDir != "" {
		clauses = append(clauses, "project_dir = ?")
		args = append(args, query.ProjectDir)
	}
	if query.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, query.Status)
	}
	if query.HasReview != nil {
		if *query.HasReview {
			clauses = append(clauses, "EXISTS (SELECT 1 FROM review_results rr WHERE rr.task_id = tasks.id)")
		} else {
			clauses = append(clauses, "NOT EXISTS (SELECT 1 FROM review_results rr WHERE rr.task_id = tasks.id)")
		}
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

type sqlScanner interface {
	Scan(dest ...any) error
}

func scanUnifiedTask(scanner sqlScanner) (UnifiedTask, error) {
	var task UnifiedTask
	var startedAt, endedAt sql.NullString
	err := scanner.Scan(
		&task.ID, &task.ChatID, &task.ThreadID, &task.ProjectDir, &task.Goal,
		&task.Engine, &task.Backend, &task.Status, &startedAt, &endedAt,
		&task.TotalInputTokens, &task.TotalOutputTokens, &task.TotalCostUSD,
	)
	if err != nil {
		return task, err
	}
	task.StartedAt = parseDBTime(startedAt.String)
	task.EndedAt = parseNullableDBTime(endedAt)
	return task, nil
}

func scanUnifiedSubTask(scanner sqlScanner) (UnifiedSubTask, error) {
	var subTask UnifiedSubTask
	var startedAt, endedAt sql.NullString
	err := scanner.Scan(
		&subTask.ID, &subTask.TaskID, &subTask.Idx, &subTask.Description,
		&subTask.Model, &subTask.Status, &subTask.ResultText,
		&subTask.InputTokens, &subTask.OutputTokens, &subTask.CostUSD,
		&startedAt, &endedAt, &subTask.RoutingReason, &subTask.RoutingLatencyMS,
	)
	if err != nil {
		return subTask, err
	}
	subTask.StartedAt = parseDBTime(startedAt.String)
	subTask.EndedAt = parseNullableDBTime(endedAt)
	return subTask, nil
}

func (s *SQLiteStorage) listUnifiedSubTaskGraphs(taskID string) ([]UnifiedSubTaskGraph, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, idx, description, model, status, result_text,
		       input_tokens, output_tokens, cost_usd, started_at, ended_at,
		       routing_reason, routing_latency_ms
		FROM sub_tasks
		WHERE task_id = ?
		ORDER BY idx ASC`, taskID)
	if err != nil {
		return nil, err
	}

	subTasks := make([]UnifiedSubTaskGraph, 0)
	for rows.Next() {
		subTask, err := scanUnifiedSubTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		subTasks = append(subTasks, UnifiedSubTaskGraph{
			UnifiedSubTask: subTask,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range subTasks {
		events, err := s.listUnifiedToolEvents(subTasks[i].ID)
		if err != nil {
			return nil, err
		}
		artifacts, err := s.listUnifiedArtifacts(subTasks[i].ID)
		if err != nil {
			return nil, err
		}
		subTasks[i].ToolEvents = events
		subTasks[i].Artifacts = artifacts
	}
	return subTasks, nil
}

func (s *SQLiteStorage) listUnifiedToolEvents(subTaskID string) ([]UnifiedToolEvent, error) {
	rows, err := s.db.Query(`
		SELECT id, sub_task_id, tool_name, input_json, output_json, ts, status
		FROM tool_events
		WHERE sub_task_id = ?
		ORDER BY ts ASC, id ASC`, subTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]UnifiedToolEvent, 0)
	for rows.Next() {
		var event UnifiedToolEvent
		var ts string
		if err := rows.Scan(&event.ID, &event.SubTaskID, &event.ToolName, &event.InputJSON, &event.OutputJSON, &ts, &event.Status); err != nil {
			return nil, err
		}
		event.Timestamp = parseDBTime(ts)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLiteStorage) listUnifiedArtifacts(subTaskID string) ([]UnifiedArtifact, error) {
	rows, err := s.db.Query(`
		SELECT id, sub_task_id, path, hash
		FROM artifacts
		WHERE sub_task_id = ?
		ORDER BY id ASC`, subTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	artifacts := make([]UnifiedArtifact, 0)
	for rows.Next() {
		var artifact UnifiedArtifact
		if err := rows.Scan(&artifact.ID, &artifact.SubTaskID, &artifact.Path, &artifact.Hash); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (s *SQLiteStorage) listUnifiedReviewGraphs(taskID string) ([]UnifiedReviewGraph, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, reviewer_model, verdict, overall_score, feedback_text,
		       issue_tags, input_tokens, output_tokens, cost_usd, block_count, auto_fixed_count, source, created_at
		FROM review_results
		WHERE task_id = ?
		ORDER BY created_at DESC, id DESC`, taskID)
	if err != nil {
		return nil, err
	}

	reviews := make([]UnifiedReviewGraph, 0)
	for rows.Next() {
		var review UnifiedReviewGraph
		var tagsJSON, createdAt string
		if err := rows.Scan(
			&review.ID, &review.TaskID, &review.ReviewerModel, &review.Verdict,
			&review.OverallScore, &review.FeedbackText, &tagsJSON,
			&review.InputTokens, &review.OutputTokens, &review.CostUSD,
			&review.BlockCount, &review.AutoFixedCount, &review.Source, &createdAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		review.IssueTags = parseStringListJSON(tagsJSON)
		review.CreatedAt = parseDBTime(createdAt)
		reviews = append(reviews, review)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range reviews {
		subTaskResults, err := s.listUnifiedReviewSubTaskResults(reviews[i].ID)
		if err != nil {
			return nil, err
		}
		reviews[i].SubTaskResults = subTaskResults
	}
	return reviews, nil
}

func (s *SQLiteStorage) listUnifiedReviewSubTaskResults(reviewID int64) ([]UnifiedReviewSubTaskResult, error) {
	rows, err := s.db.Query(`
		SELECT id, review_id, sub_task_id, score, feedback, issue_tags
		FROM review_subtask_results
		WHERE review_id = ?
		ORDER BY id ASC`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]UnifiedReviewSubTaskResult, 0)
	for rows.Next() {
		var result UnifiedReviewSubTaskResult
		var tagsJSON string
		if err := rows.Scan(&result.ID, &result.ReviewID, &result.SubTaskID, &result.Score, &result.Feedback, &tagsJSON); err != nil {
			return nil, err
		}
		result.IssueTags = parseStringListJSON(tagsJSON)
		results = append(results, result)
	}
	return results, rows.Err()
}

func parseStringListJSON(raw string) []string {
	if raw == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func parseNullableDBTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := parseDBTime(value.String)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func parseDBTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *SQLiteStorage) insertDecisionLogUnified(decision DecisionLog) error {
	taskID := unifiedDecisionTaskID(decision)
	subTaskID := taskID + ":1"
	startedAt := decision.Timestamp
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	endedAt := startedAt.Add(time.Duration(decision.DurationMs) * time.Millisecond)
	status := "done"
	if !decision.Outcome.Success {
		status = "failed"
	}
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:                taskID,
		ChatID:            decision.ChatID,
		ThreadID:          decision.ThreadID,
		ProjectDir:        decision.ProjectPath,
		Goal:              decision.UserPrompt,
		Engine:            "direct",
		Backend:           backendForDecisionModel(decision.Model),
		Status:            status,
		StartedAt:         startedAt,
		EndedAt:           &endedAt,
		TotalInputTokens:  int(decision.TokensUsed.TotalInputTokens),
		TotalOutputTokens: int(decision.TokensUsed.TotalOutputTokens),
		TotalCostUSD:      decision.TokensUsed.TotalCostUSD,
	}); err != nil {
		return err
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:               subTaskID,
		TaskID:           taskID,
		Idx:              0,
		Description:      decision.UserPrompt,
		Model:            decision.Model,
		Status:           status,
		ResultText:       decision.AgentResponse,
		InputTokens:      int(decision.TokensUsed.TotalInputTokens),
		OutputTokens:     int(decision.TokensUsed.TotalOutputTokens),
		CostUSD:          decision.TokensUsed.TotalCostUSD,
		StartedAt:        startedAt,
		EndedAt:          &endedAt,
		RoutingReason:    decision.RoutingReason,
		RoutingLatencyMS: decision.RoutingLatency,
	}); err != nil {
		return err
	}
	for _, call := range decision.ToolCalls {
		inputJSON, _ := json.Marshal(call.Input)
		if err := s.InsertUnifiedToolEvent(UnifiedToolEvent{
			SubTaskID:  subTaskID,
			ToolName:   call.ToolName,
			InputJSON:  string(inputJSON),
			OutputJSON: "{}",
			Timestamp:  call.Timestamp,
			Status:     call.Status,
		}); err != nil {
			return err
		}
	}
	for _, path := range decision.Outcome.FilesChanged {
		if err := s.InsertUnifiedArtifact(UnifiedArtifact{
			SubTaskID: subTaskID,
			Path:      path,
		}); err != nil {
			return err
		}
	}
	s.broadcastUnifiedTask(taskID)
	return nil
}

func (s *SQLiteStorage) broadcastUnifiedTask(taskID string) {
	if globalWebSocketHub == nil {
		return
	}
	graphs, err := s.ListUnifiedTaskGraphs(UnifiedTaskQuery{Limit: 1, ID: taskID})
	if err != nil {
		return
	}
	for _, graph := range graphs {
		if graph.ID == taskID {
			globalWebSocketHub.BroadcastEvent("task_updated", graph)
			return
		}
	}
}

// unifiedDecisionTaskID derives a stable, per-row task ID.
// A single Claude session spans many user turns (each producing its own
// decision_logs row), so SessionID alone is insufficient — keying on it would
// cause INSERT OR REPLACE to collapse all turns into one. Include the
// timestamp to make each decision_log row a distinct task in the unified store.
func unifiedDecisionTaskID(decision DecisionLog) string {
	ts := decision.Timestamp.UTC().Format("20060102T150405.000000000Z")
	if decision.SessionID != "" {
		return "decision:" + decision.SessionID + ":" + ts
	}
	return "decision:" + ts
}

func backendForDecisionModel(model string) string {
	switch {
	case model == "":
		return ""
	case containsInsensitive(model, "gpt"), containsInsensitive(model, "codex"):
		return "codex"
	default:
		return "claude"
	}
}

func containsInsensitive(s, substr string) bool {
	return len(s) >= len(substr) && sqlContainsFold(s, substr)
}

func sqlContainsFold(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if equalFoldASCII(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func formatNullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return sql.NullString{}
	}
	return t.Format(time.RFC3339Nano)
}
