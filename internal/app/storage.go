package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-tg-agent/internal/app/security"

	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO required)
)

// Storage interface 定義持久化操作
type Storage interface {
	UnifiedTaskStore

	// Tool Executions
	InsertToolExecution(exec ToolExecution) error
	GetToolExecutions(limit int, offset int) ([]ToolExecution, error)
	GetToolExecutionsByTimeRange(start, end time.Time, limit int) ([]ToolExecution, error)
	GetToolExecutionsByChat(chatID int64, limit int) ([]ToolExecution, error)

	// Decision Logs
	InsertDecisionLog(log DecisionLog) error
	GetDecisionLogs(limit int, offset int) ([]DecisionLog, error)
	GetDecisionLogsByTimeRange(start, end time.Time, limit int) ([]DecisionLog, error)
	GetDecisionLogsByTimeRangeWithOffset(start, end time.Time, limit int, offset int, source string) ([]DecisionLog, error)
	GetDecisionLogsByProject(projectPath string, limit int) ([]DecisionLog, error)

	// Performance Metrics
	InsertPerformanceMetric(metric PerformanceMetrics) error
	GetPerformanceMetrics(limit int, offset int) ([]PerformanceMetrics, error)
	GetPerformanceMetricsByTimeRange(start, end time.Time, limit int) ([]PerformanceMetrics, error)
	GetPerformanceMetricsByProject(projectPath string, limit int, offset int) ([]PerformanceMetrics, error)
	GetPerformanceAnalytics(hours int) (PerformanceAnalytics, error)
	GetToolExecutionStats() (map[string]map[string]interface{}, error)
	GetToolExecutionStatsByTimeRange(start, end time.Time) (map[string]map[string]interface{}, error)
	GetCostSavings(hours int) (CostSavingsReport, error)
	GetCostSavingsByProject(projectPath string, hours int) (CostSavingsReport, error)

	// Security Events
	InsertSecurityEvent(event security.SecurityEvent) error
	GetSecurityEvents(limit int, offset int) ([]security.SecurityEvent, error)
	GetSecurityEventsByTimeRange(start, end time.Time, limit int) ([]security.SecurityEvent, error)
	GetSecurityEventsBySeverity(severity string, limit int) ([]security.SecurityEvent, error)

	// Count Queries (for pagination)
	GetToolExecutionsCount() (int64, error)
	GetDecisionLogsCount() (int64, error)
	GetDecisionLogsCountByTimeRange(start, end time.Time) (int64, error)
	GetPerformanceMetricsCount() (int64, error)
	GetSecurityEventsCount() (int64, error)

	// Topic Settings
	SaveTopicSetting(chatID int64, threadID int, projectDir string) error
	GetTopicSetting(chatID int64, threadID int) (string, error)

	// Chat Language Preferences
	SaveChatLanguage(chatID int64, langCode string) error
	GetChatLanguage(chatID int64) (string, error)

	// Scheduled Tasks
	InsertScheduledTask(task ScheduledTask) error
	DeleteScheduledTask(taskID string, chatID int64) error
	GetScheduledTask(taskID string, chatID int64) (ScheduledTask, error)
	GetScheduledTasksByChat(chatID int64) ([]ScheduledTask, error)
	GetEnabledScheduledTasks() ([]ScheduledTask, error)
	UpdateScheduledTaskEnabled(taskID string, chatID int64, enabled bool) error
	UpdateScheduledTaskResult(taskID string, status string, result string, duration time.Duration) error

	// Auto Skills
	InsertAutoSkill(skill AutoSkill) error
	UpdateAutoSkill(skill AutoSkill) error
	DeleteAutoSkill(skillID string) error
	GetActiveSkills() ([]AutoSkill, error)
	GetSkillsByChat(chatID int64) ([]AutoSkill, error)

	// Data Retention
	CleanupOldData(retentionDays int) error

	// Connection Management
	Close() error
	Health() error

	// Database Access (for advanced operations like checkpoints)
	GetDB() interface{} // Returns underlying database connection
}

// SQLiteStorage SQLite 實作
type SQLiteStorage struct {
	db   *sql.DB
	path string
}

// NewSQLiteStorage 建立新的 SQLite 儲存實例
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	// 確保目錄存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// 開啟資料庫連接
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 配置連接池以避免 SQLite 並發問題
	db.SetMaxOpenConns(1)            // SQLite 只允許一個寫入連接
	db.SetMaxIdleConns(1)            // 保持一個空閒連接
	db.SetConnMaxLifetime(time.Hour) // 連接最長存活時間

	storage := &SQLiteStorage{
		db:   db,
		path: dbPath,
	}

	// 初始化資料庫表格
	if err := storage.initTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	return storage, nil
}

// execWithRetry 執行資料庫操作，遇到 SQLITE_BUSY 時會重試
func (s *SQLiteStorage) execWithRetry(operation func() error) error {
	const maxRetries = 5
	const retryDelay = 50 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		err := operation()
		if err == nil {
			return nil
		}

		// 檢查是否為 SQLite 忙碌錯誤
		if strings.Contains(err.Error(), "database is locked") ||
			strings.Contains(err.Error(), "SQLITE_BUSY") {
			if i < maxRetries-1 { // 不是最後一次重試
				time.Sleep(retryDelay * time.Duration(i+1)) // 遞增延遲
				continue
			}
		}

		return err // 非忙碌錯誤或達到最大重試次數
	}

	return fmt.Errorf("database operation failed after %d retries", maxRetries)
}

// formatTimeForSQLite formats a time.Time for SQLite string comparison.
// Timestamps are stored via time.Time.String() in local timezone (e.g. "2026-02-14 19:58:43 +0800 CST").
// SQLite BETWEEN does lexicographic comparison, so query params must use matching local timezone format.
func formatTimeForSQLite(t time.Time) string {
	// Match the format stored in database: "2006-01-02 15:04:05 -0700 MST"
	return t.Local().Format("2006-01-02 15:04:05 -0700 MST")
}

// initTables 初始化資料庫表格
func (s *SQLiteStorage) initTables() error {
	// 啟用外鍵約束和 WAL 模式（提升並發性能）
	if _, err := s.db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	if _, err := s.db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return err
	}

	// Tool Executions 表
	toolExecutionsSQL := `
	CREATE TABLE IF NOT EXISTS tool_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		tool_name TEXT NOT NULL,
		input_json TEXT,
		status TEXT NOT NULL,
		duration_ms INTEGER,
		chat_id INTEGER,
		thread_id INTEGER,
		error TEXT,
		git_commit_hash TEXT,
		git_branch TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_tool_executions_timestamp ON tool_executions(timestamp);
	CREATE INDEX IF NOT EXISTS idx_tool_executions_chat_id ON tool_executions(chat_id);
	CREATE INDEX IF NOT EXISTS idx_tool_executions_tool_name ON tool_executions(tool_name);
	`

	// Decision Logs 表
	decisionLogsSQL := `
	CREATE TABLE IF NOT EXISTS decision_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		session_id TEXT,
		project_path TEXT,
		chat_id INTEGER,
		thread_id INTEGER,
		user_prompt TEXT,
		agent_response TEXT,
		tool_calls_json TEXT,
		context_json TEXT,
		outcome_json TEXT,
		duration_ms INTEGER,
		tokens_input INTEGER,
		tokens_output INTEGER,
		tokens_total INTEGER,
		cost_usd REAL,
		git_commit_hash TEXT,
		git_branch TEXT,
		model TEXT DEFAULT '',
		routing_reason TEXT DEFAULT '',
		routing_latency_ms INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_decision_logs_timestamp ON decision_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_decision_logs_session_id ON decision_logs(session_id);
	CREATE INDEX IF NOT EXISTS idx_decision_logs_project_path ON decision_logs(project_path);
	CREATE INDEX IF NOT EXISTS idx_decision_logs_chat_id ON decision_logs(chat_id);
	CREATE INDEX IF NOT EXISTS idx_decision_logs_model ON decision_logs(model);
	`

	// Performance Metrics 表
	performanceMetricsSQL := `
	CREATE TABLE IF NOT EXISTS performance_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		api_call_latency_ms INTEGER,
		api_call_success BOOLEAN,
		tool_execution_time_ms INTEGER,
		tool_execution_type TEXT,
		tokens_used INTEGER,
		estimated_cost REAL,
		memory_usage BIGINT,
		error_type TEXT,
		chat_id INTEGER,
		agent_type TEXT,
		model TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_performance_metrics_timestamp ON performance_metrics(timestamp);
	CREATE INDEX IF NOT EXISTS idx_performance_metrics_chat_id ON performance_metrics(chat_id);
	CREATE INDEX IF NOT EXISTS idx_performance_metrics_tool_type ON performance_metrics(tool_execution_type);
	CREATE INDEX IF NOT EXISTS idx_performance_metrics_model ON performance_metrics(model);
	`

	// Security Events 表
	securityEventsSQL := `
	CREATE TABLE IF NOT EXISTS security_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id TEXT UNIQUE,
		timestamp DATETIME NOT NULL,
		event_type TEXT NOT NULL,
		severity TEXT NOT NULL,
		description TEXT,
		user_id INTEGER,
		ip_address TEXT,
		user_agent TEXT,
		details_json TEXT,
		mitigated BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_security_events_timestamp ON security_events(timestamp);
	CREATE INDEX IF NOT EXISTS idx_security_events_event_type ON security_events(event_type);
	CREATE INDEX IF NOT EXISTS idx_security_events_severity ON security_events(severity);
	CREATE INDEX IF NOT EXISTS idx_security_events_user_id ON security_events(user_id);
	`

	// Topic Settings 表（持久化 topic → project 對應）
	topicSettingsSQL := `
	CREATE TABLE IF NOT EXISTS topic_settings (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		project_dir TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (chat_id, thread_id)
	);
	`

	// Chat Language Preferences 表
	chatLanguageSQL := `
	CREATE TABLE IF NOT EXISTS chat_language (
		chat_id INTEGER PRIMARY KEY,
		lang_code TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_chat_language_updated_at ON chat_language(updated_at);
	`

	// Auto Skills 表
	autoSkillsSQL := `
	CREATE TABLE IF NOT EXISTS auto_skills (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		tool_chain_json TEXT,
		context TEXT,
		tags_json TEXT,
		success_rate REAL DEFAULT 1.0,
		use_count INTEGER DEFAULT 0,
		version INTEGER DEFAULT 1,
		status TEXT DEFAULT 'active',
		project_path TEXT,
		chat_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_auto_skills_status ON auto_skills(status);
	CREATE INDEX IF NOT EXISTS idx_auto_skills_chat_id ON auto_skills(chat_id);
	CREATE INDEX IF NOT EXISTS idx_auto_skills_project_path ON auto_skills(project_path);
	`

	// Parallel Executions 表
	parallelExecutionsSQL := `
	CREATE TABLE IF NOT EXISTS parallel_executions (
		id TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		thread_id INTEGER DEFAULT 0,
		task_count INTEGER,
		status TEXT,
		results_json TEXT,
		total_time_ms INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_parallel_executions_chat_id ON parallel_executions(chat_id);
	CREATE INDEX IF NOT EXISTS idx_parallel_executions_created_at ON parallel_executions(created_at);
	`

	// Scheduled Tasks 表
	scheduledTasksSQL := `
	CREATE TABLE IF NOT EXISTS scheduled_tasks (
		id TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		thread_id INTEGER DEFAULT 0,
		name TEXT NOT NULL,
		cron_expr TEXT NOT NULL,
		task_type TEXT NOT NULL DEFAULT 'command',
		payload TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_run_at DATETIME,
		last_result TEXT,
		last_status TEXT,
		run_count INTEGER DEFAULT 0,
		fail_count INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_chat_id ON scheduled_tasks(chat_id);
	CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_enabled ON scheduled_tasks(enabled);
	`

	// Prompt Classifications 表 (Hermes 實驗 #104 — VS Code UserPromptSubmit 觀測)
	promptClassificationsSQL := `
	CREATE TABLE IF NOT EXISTS prompt_classifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		source TEXT NOT NULL,
		cwd TEXT,
		prompt_snippet TEXT,
		prompt_length INTEGER,
		classification TEXT NOT NULL,
		matched_rule TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_prompt_classifications_timestamp ON prompt_classifications(timestamp);
	CREATE INDEX IF NOT EXISTS idx_prompt_classifications_classification ON prompt_classifications(classification);
	CREATE INDEX IF NOT EXISTS idx_prompt_classifications_source ON prompt_classifications(source);
	`

	// 執行所有 SQL 語句
	tables := []string{
		toolExecutionsSQL,
		decisionLogsSQL,
		performanceMetricsSQL,
		securityEventsSQL,
		topicSettingsSQL,
		chatLanguageSQL,
		autoSkillsSQL,
		parallelExecutionsSQL,
		scheduledTasksSQL,
		promptClassificationsSQL,
	}

	for _, tableSQL := range tables {
		if _, err := s.db.Exec(tableSQL); err != nil {
			return fmt.Errorf("failed to create tables: %w", err)
		}
	}
	if err := s.migrateUnifiedTaskTables(); err != nil {
		return err
	}
	if err := s.backfillDecisionLogsToUnified(); err != nil {
		log.Printf("[storage] decision_logs backfill warning: %v", err)
	}

	// Migration: add thinking_content column to decision_logs
	_, err := s.db.Exec(`ALTER TABLE decision_logs ADD COLUMN thinking_content TEXT DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (thinking_content): %v", err)
	}

	// Migration: add source column to decision_logs (for Claude Code Hooks integration)
	_, err = s.db.Exec(`ALTER TABLE decision_logs ADD COLUMN source TEXT DEFAULT 'telegram'`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (source): %v", err)
	}

	// Migration: add model column to decision_logs
	_, err = s.db.Exec(`ALTER TABLE decision_logs ADD COLUMN model TEXT DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (model): %v", err)
	}

	// Migration: add routing_reason column to decision_logs
	_, err = s.db.Exec(`ALTER TABLE decision_logs ADD COLUMN routing_reason TEXT DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (routing_reason): %v", err)
	}

	// Migration: add routing_latency_ms column to decision_logs
	_, err = s.db.Exec(`ALTER TABLE decision_logs ADD COLUMN routing_latency_ms INTEGER DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (routing_latency_ms): %v", err)
	}

	// Migration: add model column to performance_metrics
	_, err = s.db.Exec(`ALTER TABLE performance_metrics ADD COLUMN model TEXT DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (performance_metrics.model): %v", err)
	}

	// Migration: add project_path column to performance_metrics
	_, err = s.db.Exec(`ALTER TABLE performance_metrics ADD COLUMN project_path TEXT DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (performance_metrics.project_path): %v", err)
	}

	// Migration: add project_path column to tool_executions
	_, err = s.db.Exec(`ALTER TABLE tool_executions ADD COLUMN project_path TEXT DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Migration warning (tool_executions.project_path): %v", err)
	}

	// Create indexes for new columns if they don't exist
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_decision_logs_model ON decision_logs(model)`)
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_performance_metrics_model ON performance_metrics(model)`)
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_performance_metrics_project_path ON performance_metrics(project_path)`)
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_tool_executions_project_path ON tool_executions(project_path)`)

	log.Printf("Database initialized successfully at: %s", s.path)
	return nil
}

// ==================== Tool Executions ====================

// InsertToolExecution 插入工具執行記錄
func (s *SQLiteStorage) InsertToolExecution(exec ToolExecution) error {
	inputJSON, _ := json.Marshal(exec.Input)

	// 從當前 Git 狀態獲取 commit hash 和 branch（如果可用）
	var gitCommitHash, gitBranch sql.NullString
	if gitManager != nil {
		if gitState, err := gitManager.GetGitState("."); err == nil && gitState != nil {
			gitCommitHash = sql.NullString{String: gitState.CommitHash, Valid: true}
			gitBranch = sql.NullString{String: gitState.Branch, Valid: true}
		}
	}

	_, err := s.db.Exec(`
		INSERT INTO tool_executions
		(timestamp, tool_name, input_json, status, duration_ms, chat_id, thread_id, error, git_commit_hash, git_branch, project_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		exec.Timestamp, exec.ToolName, string(inputJSON), exec.Status,
		exec.Duration.Milliseconds(), exec.ChatID, exec.ThreadID, exec.Error,
		gitCommitHash, gitBranch, exec.ProjectPath)

	return err
}

// GetToolExecutions 獲取工具執行記錄（分頁）
func (s *SQLiteStorage) GetToolExecutions(limit int, offset int) ([]ToolExecution, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, tool_name, input_json, status, duration_ms, chat_id, thread_id, error, project_path
		FROM tool_executions
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanToolExecutions(rows)
}

// GetToolExecutionsByTimeRange 按時間範圍獲取工具執行記錄
func (s *SQLiteStorage) GetToolExecutionsByTimeRange(start, end time.Time, limit int) ([]ToolExecution, error) {
	startStr := formatTimeForSQLite(start)
	endStr := formatTimeForSQLite(end)
	rows, err := s.db.Query(`
		SELECT timestamp, tool_name, input_json, status, duration_ms, chat_id, thread_id, error, project_path
		FROM tool_executions
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp DESC
		LIMIT ?`, startStr, endStr, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanToolExecutions(rows)
}

// GetToolExecutionsByChat 按聊天 ID 獲取工具執行記錄
func (s *SQLiteStorage) GetToolExecutionsByChat(chatID int64, limit int) ([]ToolExecution, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, tool_name, input_json, status, duration_ms, chat_id, thread_id, error
		FROM tool_executions
		WHERE chat_id = ?
		ORDER BY timestamp DESC
		LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanToolExecutions(rows)
}

// scanToolExecutions 掃描工具執行記錄結果
func (s *SQLiteStorage) scanToolExecutions(rows *sql.Rows) ([]ToolExecution, error) {
	var executions []ToolExecution
	for rows.Next() {
		var exec ToolExecution
		var inputJSON string
		var durationMs int64
		var projectPath sql.NullString

		err := rows.Scan(&exec.Timestamp, &exec.ToolName, &inputJSON, &exec.Status,
			&durationMs, &exec.ChatID, &exec.ThreadID, &exec.Error, &projectPath)
		if err != nil {
			return nil, err
		}

		// 解析 JSON 輸入
		json.Unmarshal([]byte(inputJSON), &exec.Input)
		exec.Duration = time.Duration(durationMs) * time.Millisecond
		if projectPath.Valid {
			exec.ProjectPath = projectPath.String
		}

		executions = append(executions, exec)
	}

	return executions, rows.Err()
}

// ==================== Decision Logs ====================

// InsertDecisionLog 插入決策記錄
func (s *SQLiteStorage) InsertDecisionLog(log DecisionLog) error {
	toolCallsJSON, _ := json.Marshal(log.ToolCalls)
	contextJSON, _ := json.Marshal(log.Context)
	outcomeJSON, _ := json.Marshal(log.Outcome)

	// 從當前 Git 狀態獲取 commit hash 和 branch（如果可用）
	var gitCommitHash, gitBranch sql.NullString
	if gitManager != nil {
		if gitState, err := gitManager.GetGitState(log.ProjectPath); err == nil && gitState != nil {
			gitCommitHash = sql.NullString{String: gitState.CommitHash, Valid: true}
			gitBranch = sql.NullString{String: gitState.Branch, Valid: true}
		}
	}

	// Default source to "telegram" if not set
	source := log.Source
	if source == "" {
		source = "telegram"
	}

	_, err := s.db.Exec(`
		INSERT INTO decision_logs
		(timestamp, session_id, project_path, chat_id, thread_id, user_prompt, agent_response,
		 tool_calls_json, context_json, outcome_json, duration_ms, tokens_input, tokens_output,
		 tokens_total, cost_usd, git_commit_hash, git_branch, thinking_content, source,
		 model, routing_reason, routing_latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.Timestamp, log.SessionID, log.ProjectPath, log.ChatID, log.ThreadID,
		log.UserPrompt, log.AgentResponse, string(toolCallsJSON), string(contextJSON),
		string(outcomeJSON), log.DurationMs, log.TokensUsed.TotalInputTokens, log.TokensUsed.TotalOutputTokens,
		log.TokensUsed.TotalInputTokens+log.TokensUsed.TotalOutputTokens, log.TokensUsed.TotalCostUSD,
		gitCommitHash, gitBranch, log.ThinkingContent, source,
		log.Model, log.RoutingReason, log.RoutingLatency)

	if err != nil {
		return err
	}
	if err := s.insertDecisionLogUnified(log); err != nil {
		return fmt.Errorf("insert unified task: %w", err)
	}
	return nil
}

// GetDecisionLogs 獲取決策記錄（分頁）
func (s *SQLiteStorage) GetDecisionLogs(limit int, offset int) ([]DecisionLog, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, session_id, project_path, chat_id, thread_id, user_prompt,
			   agent_response, tool_calls_json, context_json, outcome_json, duration_ms,
			   tokens_input, tokens_output, COALESCE(cost_usd, 0) as cost_usd,
			   COALESCE(thinking_content, '') as thinking_content,
			   COALESCE(git_commit_hash, '') as git_commit_hash, COALESCE(git_branch, '') as git_branch,
			   COALESCE(source, 'telegram') as source,
			   COALESCE(model, '') as model, COALESCE(routing_reason, '') as routing_reason,
			   COALESCE(routing_latency_ms, 0) as routing_latency_ms
		FROM decision_logs
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDecisionLogs(rows)
}

// GetDecisionLogsByTimeRange 按時間範圍獲取決策記錄
func (s *SQLiteStorage) GetDecisionLogsByTimeRange(start, end time.Time, limit int) ([]DecisionLog, error) {
	startStr := formatTimeForSQLite(start)
	endStr := formatTimeForSQLite(end)
	rows, err := s.db.Query(`
		SELECT timestamp, session_id, project_path, chat_id, thread_id, user_prompt,
			   agent_response, tool_calls_json, context_json, outcome_json, duration_ms,
			   tokens_input, tokens_output, COALESCE(cost_usd, 0) as cost_usd,
			   COALESCE(thinking_content, '') as thinking_content,
			   COALESCE(git_commit_hash, '') as git_commit_hash, COALESCE(git_branch, '') as git_branch,
			   COALESCE(source, 'telegram') as source,
			   COALESCE(model, '') as model, COALESCE(routing_reason, '') as routing_reason,
			   COALESCE(routing_latency_ms, 0) as routing_latency_ms
		FROM decision_logs
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp DESC
		LIMIT ?`, startStr, endStr, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDecisionLogs(rows)
}

// GetDecisionLogsByTimeRangeWithOffset 按時間範圍和偏移量獲取決策記錄，支持可選的source過濾
// Uses datetime function to parse timestamps and compare them reliably
func (s *SQLiteStorage) GetDecisionLogsByTimeRangeWithOffset(start, end time.Time, limit int, offset int, source string) ([]DecisionLog, error) {
	// Convert times to ISO8601 format for SQLite datetime() function to parse
	startISO := start.UTC().Format("2006-01-02T15:04:05Z")
	endISO := end.UTC().Format("2006-01-02T15:04:05Z")

	query := `
		SELECT timestamp, session_id, project_path, chat_id, thread_id, user_prompt,
			   agent_response, tool_calls_json, context_json, outcome_json, duration_ms,
			   tokens_input, tokens_output, COALESCE(cost_usd, 0) as cost_usd,
			   COALESCE(thinking_content, '') as thinking_content,
			   COALESCE(git_commit_hash, '') as git_commit_hash, COALESCE(git_branch, '') as git_branch,
			   COALESCE(source, 'telegram') as source,
			   COALESCE(model, '') as model, COALESCE(routing_reason, '') as routing_reason,
			   COALESCE(routing_latency_ms, 0) as routing_latency_ms
		FROM decision_logs
		WHERE datetime(substr(timestamp, 1, 19)) BETWEEN datetime(?) AND datetime(?)`

	var args []interface{}
	args = append(args, startISO, endISO)

	// Add source filter if provided (non-empty)
	if source != "" {
		query += ` AND source = ?`
		args = append(args, source)
	}

	query += ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDecisionLogs(rows)
}

// GetDecisionLogsByProject 按專案路徑獲取決策記錄
func (s *SQLiteStorage) GetDecisionLogsByProject(projectPath string, limit int) ([]DecisionLog, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, session_id, project_path, chat_id, thread_id, user_prompt,
			   agent_response, tool_calls_json, context_json, outcome_json, duration_ms,
			   tokens_input, tokens_output, COALESCE(cost_usd, 0) as cost_usd,
			   COALESCE(thinking_content, '') as thinking_content,
			   COALESCE(git_commit_hash, '') as git_commit_hash, COALESCE(git_branch, '') as git_branch,
			   COALESCE(source, 'telegram') as source
		FROM decision_logs
		WHERE project_path = ?
		ORDER BY timestamp DESC
		LIMIT ?`, projectPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDecisionLogs(rows)
}

// scanDecisionLogs 掃描決策記錄結果
func (s *SQLiteStorage) scanDecisionLogs(rows *sql.Rows) ([]DecisionLog, error) {
	var logs []DecisionLog
	for rows.Next() {
		var log DecisionLog
		var toolCallsJSON, contextJSON, outcomeJSON string
		var gitCommitHash, gitBranch string

		err := rows.Scan(&log.Timestamp, &log.SessionID, &log.ProjectPath,
			&log.ChatID, &log.ThreadID, &log.UserPrompt, &log.AgentResponse,
			&toolCallsJSON, &contextJSON, &outcomeJSON, &log.DurationMs,
			&log.TokensUsed.TotalInputTokens, &log.TokensUsed.TotalOutputTokens,
			&log.TokensUsed.TotalCostUSD,
			&log.ThinkingContent, &gitCommitHash, &gitBranch, &log.Source,
			&log.Model, &log.RoutingReason, &log.RoutingLatency)
		if err != nil {
			return nil, err
		}

		// 解析 JSON 欄位
		json.Unmarshal([]byte(toolCallsJSON), &log.ToolCalls)
		json.Unmarshal([]byte(contextJSON), &log.Context)
		json.Unmarshal([]byte(outcomeJSON), &log.Outcome)

		// 設定 Git 狀態
		log.GitCommitHash = gitCommitHash
		log.GitBranch = gitBranch

		logs = append(logs, log)
	}

	return logs, rows.Err()
}

// GetDecisionLogCountBySessionID checks if a session_id already exists (for deduplication)
func (s *SQLiteStorage) GetDecisionLogCountBySessionID(sessionID string) (int64, error) {
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM decision_logs WHERE session_id = ?`, sessionID).Scan(&count)
	return count, err
}

// DeleteDecisionLogsBySessionID removes all decision logs for a given session_id (used for upsert on hook re-trigger)
func (s *SQLiteStorage) DeleteDecisionLogsBySessionID(sessionID string) error {
	if _, err := s.db.Exec(`DELETE FROM decision_logs WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, "decision:"+sessionID)
	return err
}

// ==================== Performance Metrics ====================

// InsertPerformanceMetric 插入效能指標
func (s *SQLiteStorage) InsertPerformanceMetric(metric PerformanceMetrics) error {
	_, err := s.db.Exec(`
		INSERT INTO performance_metrics
		(timestamp, api_call_latency_ms, api_call_success, tool_execution_time_ms,
		 tool_execution_type, tokens_used, estimated_cost, memory_usage, error_type,
		 chat_id, project_path, agent_type, model)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metric.Timestamp, metric.APICallLatency.Milliseconds(), metric.APICallSuccess,
		metric.ToolExecutionTime.Milliseconds(), metric.ToolExecutionType,
		metric.TokensUsed, metric.EstimatedCost, metric.MemoryUsage,
		metric.ErrorType, metric.ChatID, metric.ProjectPath, metric.AgentType, metric.Model)

	return err
}

// GetPerformanceMetrics 獲取效能指標（分頁）
func (s *SQLiteStorage) GetPerformanceMetrics(limit int, offset int) ([]PerformanceMetrics, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, api_call_latency_ms, api_call_success, tool_execution_time_ms,
			   tool_execution_type, tokens_used, estimated_cost, memory_usage, error_type,
			   chat_id, COALESCE(project_path, '') as project_path, agent_type, COALESCE(model, '') as model
		FROM performance_metrics
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanPerformanceMetrics(rows)
}

// GetPerformanceMetricsByTimeRange 按時間範圍獲取效能指標
func (s *SQLiteStorage) GetPerformanceMetricsByTimeRange(start, end time.Time, limit int) ([]PerformanceMetrics, error) {
	startStr := formatTimeForSQLite(start)
	endStr := formatTimeForSQLite(end)
	rows, err := s.db.Query(`
		SELECT timestamp, api_call_latency_ms, api_call_success, tool_execution_time_ms,
			   tool_execution_type, tokens_used, estimated_cost, memory_usage, error_type,
			   chat_id, COALESCE(project_path, '') as project_path, agent_type, COALESCE(model, '') as model
		FROM performance_metrics
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp DESC
		LIMIT ?`, startStr, endStr, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanPerformanceMetrics(rows)
}

// GetPerformanceMetricsByProject 按項目路徑獲取效能指標
func (s *SQLiteStorage) GetPerformanceMetricsByProject(projectPath string, limit int, offset int) ([]PerformanceMetrics, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, api_call_latency_ms, api_call_success, tool_execution_time_ms,
			   tool_execution_type, tokens_used, estimated_cost, memory_usage, error_type,
			   chat_id, COALESCE(project_path, '') as project_path, agent_type, COALESCE(model, '') as model
		FROM performance_metrics
		WHERE project_path = ?
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`, projectPath, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanPerformanceMetrics(rows)
}

// GetPerformanceAnalytics 獲取效能分析數據
func (s *SQLiteStorage) GetPerformanceAnalytics(hours int) (PerformanceAnalytics, error) {
	var analytics PerformanceAnalytics
	sinceStr := formatTimeForSQLite(time.Now().Add(-time.Duration(hours) * time.Hour))

	// 基礎統計查詢
	row := s.db.QueryRow(`
		SELECT
			COUNT(*) as total_requests,
			AVG(CASE WHEN api_call_success THEN 1.0 ELSE 0.0 END) * 100.0 as success_rate,
			AVG(api_call_latency_ms) as avg_api_latency_ms,
			AVG(tool_execution_time_ms) as avg_tool_execution_ms,
			SUM(tokens_used) as total_tokens,
			SUM(estimated_cost) as total_cost,
			MAX(memory_usage) as peak_memory,
			COUNT(*) / CAST(? as REAL) as requests_per_hour
		FROM performance_metrics
		WHERE timestamp >= ?`, hours, sinceStr)

	var avgAPILatencyMs, avgToolExecutionMs float64
	err := row.Scan(&analytics.TotalRequests, &analytics.SuccessRate,
		&avgAPILatencyMs, &avgToolExecutionMs, &analytics.TotalTokens,
		&analytics.TotalCost, &analytics.PeakMemoryUsage, &analytics.RequestsPerHour)

	if err != nil {
		return analytics, err
	}

	analytics.AvgAPILatency = time.Duration(avgAPILatencyMs) * time.Millisecond
	analytics.AvgToolExecution = time.Duration(avgToolExecutionMs) * time.Millisecond

	// 計算 Cost Per Request
	if analytics.TotalRequests > 0 {
		analytics.CostPerRequest = analytics.TotalCost / float64(analytics.TotalRequests)
	}

	// 錯誤類型統計
	analytics.ErrorsByType = make(map[string]int64)
	rows, err := s.db.Query(`
		SELECT error_type, COUNT(*) as count
		FROM performance_metrics
		WHERE timestamp >= ? AND error_type IS NOT NULL AND error_type != ''
		GROUP BY error_type`, sinceStr)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var errorType string
			var count int64
			if rows.Scan(&errorType, &count) == nil {
				analytics.ErrorsByType[errorType] = count
			}
		}
	}

	// 工具使用統計
	analytics.ToolUsageStats = make(map[string]int64)
	rows, err = s.db.Query(`
		SELECT tool_execution_type, COUNT(*) as count
		FROM performance_metrics
		WHERE timestamp >= ? AND tool_execution_type IS NOT NULL AND tool_execution_type != ''
		GROUP BY tool_execution_type`, sinceStr)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var toolType string
			var count int64
			if rows.Scan(&toolType, &count) == nil {
				analytics.ToolUsageStats[toolType] = count
			}
		}
	}

	return analytics, nil
}

// GetToolExecutionStats 獲取工具執行統計數據 (默認過去24小時)
func (s *SQLiteStorage) GetToolExecutionStats() (map[string]map[string]interface{}, error) {
	return s.GetToolExecutionStatsByTimeRange(time.Now().Add(-24*time.Hour), time.Now())
}

// GetToolExecutionStatsByTimeRange 獲取指定時間範圍的工具執行統計數據
func (s *SQLiteStorage) GetToolExecutionStatsByTimeRange(start, end time.Time) (map[string]map[string]interface{}, error) {
	stats := make(map[string]map[string]interface{})

	startStr := formatTimeForSQLite(start)
	endStr := formatTimeForSQLite(end)

	// 首先嘗試從 performance_metrics 表獲取數據
	rows, err := s.db.Query(`
		SELECT
			tool_execution_type,
			COUNT(*) as count,
			AVG(tool_execution_time_ms) as avg_execution_time
		FROM performance_metrics
		WHERE timestamp BETWEEN ? AND ?
			AND tool_execution_type IS NOT NULL
			AND tool_execution_type != ''
			AND tool_execution_time_ms > 0
		GROUP BY tool_execution_type
		ORDER BY count DESC`, startStr, endStr)

	if err != nil {
		return stats, err
	}
	defer rows.Close()

	hasData := false
	for rows.Next() {
		var toolType string
		var count int64
		var avgExecutionTime float64

		if err := rows.Scan(&toolType, &count, &avgExecutionTime); err != nil {
			continue
		}

		stats[toolType] = map[string]interface{}{
			"count":              count,
			"avg_execution_time": avgExecutionTime,
		}
		hasData = true
	}

	// 如果 performance_metrics 表沒有數據，回退到 tool_executions 表
	if !hasData {
		rows2, err2 := s.db.Query(`
			SELECT
				tool_name,
				COUNT(*) as count,
				AVG(duration_ms) as avg_execution_time
			FROM tool_executions
			WHERE timestamp BETWEEN ? AND ?
				AND tool_name IS NOT NULL
				AND tool_name != ''
				AND duration_ms > 0
			GROUP BY tool_name
			ORDER BY count DESC`, startStr, endStr)

		if err2 != nil {
			return stats, err2
		}
		defer rows2.Close()

		for rows2.Next() {
			var toolType string
			var count int64
			var avgExecutionTime float64

			if err := rows2.Scan(&toolType, &count, &avgExecutionTime); err != nil {
				continue
			}

			stats[toolType] = map[string]interface{}{
				"count":              count,
				"avg_execution_time": avgExecutionTime,
			}
		}
		return stats, rows2.Err()
	}

	return stats, rows.Err()
}

// GetCostSavings 計算指定時間範圍內的成本節省報告
func (s *SQLiteStorage) GetCostSavings(hours int) (CostSavingsReport, error) {
	startTime := time.Now().Add(-time.Duration(hours) * time.Hour)
	endTime := time.Now()
	report := CostSavingsReport{
		PeriodHours:       hours,
		StartTime:         startTime,
		EndTime:           endTime,
		ByModel:           make(map[string]ModelCostBreakdown),
		RoutingMethodStat: make(map[string]int),
	}

	// 查詢 decision_logs 按模型分類 - 規範化模型名稱為簡稱
	// 只包含有有效成本的記錄（cost > 0 或有 tokens）
	rows, err := s.db.Query(`
		SELECT
			CASE
				WHEN model LIKE '%haiku%' THEN 'haiku'
				WHEN model LIKE '%opus%' THEN 'opus'
				WHEN model LIKE '%sonnet%' OR model = '' OR model IS NULL THEN 'sonnet'
				ELSE 'sonnet'
			END as model,
			COALESCE(NULLIF(routing_reason, ''), 'default') as routing_reason,
			COUNT(*) as calls,
			SUM(CASE WHEN tokens_input IS NOT NULL THEN tokens_input ELSE 0 END) as total_input_tokens,
			SUM(CASE WHEN tokens_output IS NOT NULL THEN tokens_output ELSE 0 END) as total_output_tokens,
			SUM(CASE WHEN cost_usd IS NOT NULL THEN cost_usd ELSE 0.0 END) as total_cost
		FROM decision_logs
		WHERE timestamp BETWEEN ? AND ?
		  AND (cost_usd > 0 OR tokens_input > 0 OR tokens_output > 0)
		GROUP BY
			CASE
				WHEN model LIKE '%haiku%' THEN 'haiku'
				WHEN model LIKE '%opus%' THEN 'opus'
				WHEN model LIKE '%sonnet%' OR model = '' OR model IS NULL THEN 'sonnet'
				ELSE 'sonnet'
			END,
			routing_reason
		ORDER BY total_cost DESC`,
		startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	if err != nil {
		return report, fmt.Errorf("failed to query decision_logs: %w", err)
	}
	defer rows.Close()

	actualCostByModel := make(map[string]float64)
	callsByModel := make(map[string]int)
	tokensByModel := make(map[string][2]int) // [input, output]

	for rows.Next() {
		var model, routingReason string
		var calls int
		var inputTokens, outputTokens int64
		var cost float64

		if err := rows.Scan(&model, &routingReason, &calls, &inputTokens, &outputTokens, &cost); err != nil {
			continue
		}

		// 累計實際成本和請求數
		report.TotalRequests += calls
		report.ActualCost += cost
		actualCostByModel[model] += cost // 累加而不是覆盖！
		callsByModel[model] += calls
		report.RoutingMethodStat[routingReason] += calls

		// 保存 token 用量
		tokens := tokensByModel[model]
		tokens[0] += int(inputTokens)
		tokens[1] += int(outputTokens)
		tokensByModel[model] = tokens
	}

	if err = rows.Err(); err != nil {
		return report, fmt.Errorf("error scanning decision_logs: %w", err)
	}

	// 計算假設全用預設模型（Sonnet）的成本
	defaultModel := "sonnet"
	defaultPricing := ModelPricing[defaultModel]

	for model, cost := range actualCostByModel {
		tokens := tokensByModel[model]
		inputTokens := tokens[0]
		outputTokens := tokens[1]
		calls := callsByModel[model]

		// 假設此模型的請求用 Sonnet 處理的成本
		wouldHaveCost := (float64(inputTokens) * defaultPricing.InputPerMTok / 1_000_000) +
			(float64(outputTokens) * defaultPricing.OutputPerMTok / 1_000_000)

		saved := wouldHaveCost - cost

		// 累計到預設模型成本
		report.DefaultModelCost += wouldHaveCost

		// 按模型分解
		report.ByModel[model] = ModelCostBreakdown{
			Calls:         calls,
			ActualCost:    cost,
			WouldHaveCost: wouldHaveCost,
			Saved:         saved,
			InputTokens:   inputTokens,
			OutputTokens:  outputTokens,
		}
	}

	// 計算節省統計
	report.SavingsCost = report.DefaultModelCost - report.ActualCost
	if report.DefaultModelCost > 0 {
		report.SavingsPercent = (report.SavingsCost / report.DefaultModelCost) * 100
	}

	return report, nil
}

// GetCostSavingsByProject 計算指定項目的成本節省報告
func (s *SQLiteStorage) GetCostSavingsByProject(projectPath string, hours int) (CostSavingsReport, error) {
	startTime := time.Now().Add(-time.Duration(hours) * time.Hour)
	endTime := time.Now()
	report := CostSavingsReport{
		PeriodHours:       hours,
		StartTime:         startTime,
		EndTime:           endTime,
		ByModel:           make(map[string]ModelCostBreakdown),
		RoutingMethodStat: make(map[string]int),
	}

	// 查詢 decision_logs 按項目和模型分類 - 規範化模型名稱為簡稱
	rows, err := s.db.Query(`
		SELECT
			CASE
				WHEN model LIKE '%haiku%' THEN 'haiku'
				WHEN model LIKE '%opus%' THEN 'opus'
				WHEN model LIKE '%sonnet%' OR model = '' OR model IS NULL THEN 'sonnet'
				ELSE 'sonnet'
			END as model,
			COALESCE(NULLIF(routing_reason, ''), 'default') as routing_reason,
			COUNT(*) as calls,
			SUM(CASE WHEN tokens_input IS NOT NULL THEN tokens_input ELSE 0 END) as total_input_tokens,
			SUM(CASE WHEN tokens_output IS NOT NULL THEN tokens_output ELSE 0 END) as total_output_tokens,
			SUM(CASE WHEN cost_usd IS NOT NULL THEN cost_usd ELSE 0.0 END) as total_cost
		FROM decision_logs
		WHERE project_path = ?
		  AND timestamp BETWEEN ? AND ?
		  AND (cost_usd > 0 OR tokens_input > 0 OR tokens_output > 0)
		GROUP BY
			CASE
				WHEN model LIKE '%haiku%' THEN 'haiku'
				WHEN model LIKE '%opus%' THEN 'opus'
				WHEN model LIKE '%sonnet%' OR model = '' OR model IS NULL THEN 'sonnet'
				ELSE 'sonnet'
			END,
			routing_reason
		ORDER BY total_cost DESC`,
		projectPath, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	if err != nil {
		return report, fmt.Errorf("failed to query decision_logs by project: %w", err)
	}
	defer rows.Close()

	actualCostByModel := make(map[string]float64)
	callsByModel := make(map[string]int)
	tokensByModel := make(map[string][2]int)

	for rows.Next() {
		var model, routingReason string
		var calls int
		var inputTokens, outputTokens int64
		var cost float64

		if err := rows.Scan(&model, &routingReason, &calls, &inputTokens, &outputTokens, &cost); err != nil {
			continue
		}

		report.TotalRequests += calls
		report.ActualCost += cost
		actualCostByModel[model] += cost
		callsByModel[model] += calls
		report.RoutingMethodStat[routingReason] += calls

		tokens := tokensByModel[model]
		tokens[0] += int(inputTokens)
		tokens[1] += int(outputTokens)
		tokensByModel[model] = tokens
	}

	if err = rows.Err(); err != nil {
		return report, fmt.Errorf("error scanning decision_logs by project: %w", err)
	}

	// 計算假設全用預設模型（Sonnet）的成本
	defaultModel := "sonnet"
	defaultPricing := ModelPricing[defaultModel]

	for model, cost := range actualCostByModel {
		tokens := tokensByModel[model]
		inputTokens := tokens[0]
		outputTokens := tokens[1]
		calls := callsByModel[model]

		wouldHaveCost := (float64(inputTokens) * defaultPricing.InputPerMTok / 1_000_000) +
			(float64(outputTokens) * defaultPricing.OutputPerMTok / 1_000_000)

		saved := wouldHaveCost - cost
		report.DefaultModelCost += wouldHaveCost

		report.ByModel[model] = ModelCostBreakdown{
			Calls:         calls,
			ActualCost:    cost,
			WouldHaveCost: wouldHaveCost,
			Saved:         saved,
			InputTokens:   inputTokens,
			OutputTokens:  outputTokens,
		}
	}

	report.SavingsCost = report.DefaultModelCost - report.ActualCost
	if report.DefaultModelCost > 0 {
		report.SavingsPercent = (report.SavingsCost / report.DefaultModelCost) * 100
	}

	return report, nil
}

// scanPerformanceMetrics 掃描效能指標結果
func (s *SQLiteStorage) scanPerformanceMetrics(rows *sql.Rows) ([]PerformanceMetrics, error) {
	var metrics []PerformanceMetrics
	for rows.Next() {
		var metric PerformanceMetrics
		var apiLatencyMs, toolExecutionMs int64

		err := rows.Scan(&metric.Timestamp, &apiLatencyMs, &metric.APICallSuccess,
			&toolExecutionMs, &metric.ToolExecutionType, &metric.TokensUsed,
			&metric.EstimatedCost, &metric.MemoryUsage, &metric.ErrorType,
			&metric.ChatID, &metric.ProjectPath, &metric.AgentType, &metric.Model)
		if err != nil {
			return nil, err
		}

		metric.APICallLatency = time.Duration(apiLatencyMs) * time.Millisecond
		metric.ToolExecutionTime = time.Duration(toolExecutionMs) * time.Millisecond

		metrics = append(metrics, metric)
	}

	return metrics, rows.Err()
}

// ==================== Security Events ====================

// InsertSecurityEvent 插入安全事件
func (s *SQLiteStorage) InsertSecurityEvent(event security.SecurityEvent) error {
	return s.execWithRetry(func() error {
		detailsJSON, _ := json.Marshal(event.Details)

		_, err := s.db.Exec(`
			INSERT INTO security_events
			(event_id, timestamp, event_type, severity, description, user_id,
			 ip_address, user_agent, details_json, mitigated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.ID, event.Timestamp, event.EventType, event.Severity,
			event.Description, event.UserID, event.IP, event.UserAgent,
			string(detailsJSON), event.Mitigated)

		return err
	})
}

// GetSecurityEvents 獲取安全事件（分頁）
func (s *SQLiteStorage) GetSecurityEvents(limit int, offset int) ([]security.SecurityEvent, error) {
	rows, err := s.db.Query(`
		SELECT event_id, timestamp, event_type, severity, description, user_id,
			   ip_address, user_agent, details_json, mitigated
		FROM security_events
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanSecurityEvents(rows)
}

// GetSecurityEventsByTimeRange 按時間範圍獲取安全事件
func (s *SQLiteStorage) GetSecurityEventsByTimeRange(start, end time.Time, limit int) ([]security.SecurityEvent, error) {
	startStr := formatTimeForSQLite(start)
	endStr := formatTimeForSQLite(end)
	rows, err := s.db.Query(`
		SELECT event_id, timestamp, event_type, severity, description, user_id,
			   ip_address, user_agent, details_json, mitigated
		FROM security_events
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp DESC
		LIMIT ?`, startStr, endStr, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanSecurityEvents(rows)
}

// GetSecurityEventsBySeverity 按嚴重性獲取安全事件
func (s *SQLiteStorage) GetSecurityEventsBySeverity(severity string, limit int) ([]security.SecurityEvent, error) {
	rows, err := s.db.Query(`
		SELECT event_id, timestamp, event_type, severity, description, user_id,
			   ip_address, user_agent, details_json, mitigated
		FROM security_events
		WHERE severity = ?
		ORDER BY timestamp DESC
		LIMIT ?`, severity, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanSecurityEvents(rows)
}

// scanSecurityEvents 掃描安全事件結果
func (s *SQLiteStorage) scanSecurityEvents(rows *sql.Rows) ([]security.SecurityEvent, error) {
	var events []security.SecurityEvent
	for rows.Next() {
		var event security.SecurityEvent
		var detailsJSON string
		var userID sql.NullInt64

		err := rows.Scan(&event.ID, &event.Timestamp, &event.EventType,
			&event.Severity, &event.Description, &userID, &event.IP,
			&event.UserAgent, &detailsJSON, &event.Mitigated)
		if err != nil {
			return nil, err
		}

		if userID.Valid {
			event.UserID = userID.Int64
		}

		// 同步 ID → EventID 供前端使用
		event.EventID = event.ID

		// 解析 JSON 詳情
		json.Unmarshal([]byte(detailsJSON), &event.Details)

		events = append(events, event)
	}

	return events, rows.Err()
}

// ==================== Topic Settings ====================

// SaveTopicSetting 儲存 topic 對應的專案目錄
func (s *SQLiteStorage) SaveTopicSetting(chatID int64, threadID int, projectDir string) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO topic_settings (chat_id, thread_id, project_dir, updated_at)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(chat_id, thread_id) DO UPDATE SET
				project_dir = excluded.project_dir,
				updated_at = CURRENT_TIMESTAMP`,
			chatID, threadID, projectDir)
		return err
	})
}

// GetTopicSetting 讀取 topic 對應的專案目錄，找不到時回傳空字串
func (s *SQLiteStorage) GetTopicSetting(chatID int64, threadID int) (string, error) {
	var projectDir string
	err := s.db.QueryRow(`
		SELECT project_dir FROM topic_settings
		WHERE chat_id = ? AND thread_id = ?`,
		chatID, threadID).Scan(&projectDir)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return projectDir, err
}

// ==================== Chat Language Preferences ====================

// SaveChatLanguage 儲存 chat 的語言偏好
func (s *SQLiteStorage) SaveChatLanguage(chatID int64, langCode string) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO chat_language (chat_id, lang_code, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(chat_id) DO UPDATE SET
				lang_code = excluded.lang_code,
				updated_at = CURRENT_TIMESTAMP`,
			chatID, langCode)
		return err
	})
}

// GetChatLanguage 讀取 chat 的語言偏好，找不到時回傳空字串
func (s *SQLiteStorage) GetChatLanguage(chatID int64) (string, error) {
	var langCode string
	err := s.db.QueryRow(`
		SELECT lang_code FROM chat_language
		WHERE chat_id = ?`,
		chatID).Scan(&langCode)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return langCode, err
}

// ==================== Count Queries (for pagination) ====================

// GetToolExecutionsCount 獲取工具執行記錄總數
func (s *SQLiteStorage) GetToolExecutionsCount() (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM tool_executions").Scan(&count)
	return count, err
}

// GetDecisionLogsCount 獲取決策記錄總數
func (s *SQLiteStorage) GetDecisionLogsCount() (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM decision_logs").Scan(&count)
	return count, err
}

// GetDecisionLogsCountByTimeRange 獲取指定時間範圍內的決策記錄總數
func (s *SQLiteStorage) GetDecisionLogsCountByTimeRange(start, end time.Time) (int64, error) {
	startStr := formatTimeForSQLite(start)
	endStr := formatTimeForSQLite(end)
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM decision_logs WHERE timestamp BETWEEN ? AND ?", startStr, endStr).Scan(&count)
	return count, err
}

// GetPerformanceMetricsCount 獲取效能指標總數
func (s *SQLiteStorage) GetPerformanceMetricsCount() (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM performance_metrics").Scan(&count)
	return count, err
}

// GetSecurityEventsCount 獲取安全事件總數
func (s *SQLiteStorage) GetSecurityEventsCount() (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM security_events").Scan(&count)
	return count, err
}

// ==================== Data Retention & Cleanup ====================

// CleanupOldData 清理舊資料
func (s *SQLiteStorage) CleanupOldData(retentionDays int) error {
	cutoffStr := formatTimeForSQLite(time.Now().AddDate(0, 0, -retentionDays))

	// 清理各個表格的舊資料
	tables := []string{
		"tool_executions",
		"decision_logs",
		"performance_metrics",
		"security_events",
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	var totalDeleted int64
	for _, table := range tables {
		result, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE timestamp < ?", table), cutoffStr)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to cleanup %s: %w", table, err)
		}
		if deleted, _ := result.RowsAffected(); deleted > 0 {
			totalDeleted += deleted
			log.Printf("Cleaned up %d old records from %s", deleted, table)
		}
	}

	// Deactivate expired auto_skills (last_used_at older than retention period)
	skillResult, err := tx.Exec(`UPDATE auto_skills SET status='inactive' WHERE status='active' AND last_used_at < ?`, cutoffStr)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to cleanup auto_skills: %w", err)
	}
	if deactivated, _ := skillResult.RowsAffected(); deactivated > 0 {
		log.Printf("Deactivated %d expired auto_skills", deactivated)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if totalDeleted > 0 {
		log.Printf("Data cleanup completed: removed %d total records older than %d days", totalDeleted, retentionDays)

		// 執行 VACUUM 來回收空間
		if _, err := s.db.Exec("VACUUM"); err != nil {
			log.Printf("Warning: failed to vacuum database: %v", err)
		}
	}

	return nil
}

// ==================== Connection Management ====================

// Close 關閉資料庫連接
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// Health 檢查資料庫健康狀態
func (s *SQLiteStorage) Health() error {
	return s.db.Ping()
}

// GetDB 返回底層資料庫連接
func (s *SQLiteStorage) GetDB() interface{} {
	return s.db
}

// GetDatabaseStats 獲取資料庫統計資訊
func (s *SQLiteStorage) GetDatabaseStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// 各表記錄數量
	tables := []string{"tool_executions", "decision_logs", "performance_metrics", "security_events"}
	for _, table := range tables {
		var count int64
		err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err == nil {
			stats[table+"_count"] = count
		}
	}

	// 資料庫大小
	var pageCount, pageSize int64
	s.db.QueryRow("PRAGMA page_count").Scan(&pageCount)
	s.db.QueryRow("PRAGMA page_size").Scan(&pageSize)
	stats["database_size_bytes"] = pageCount * pageSize

	// 最早和最新記錄時間
	var earliest, latest sql.NullString
	s.db.QueryRow("SELECT MIN(timestamp) FROM tool_executions").Scan(&earliest)
	s.db.QueryRow("SELECT MAX(timestamp) FROM tool_executions").Scan(&latest)
	if earliest.Valid {
		stats["earliest_record"] = earliest.String
	}
	if latest.Valid {
		stats["latest_record"] = latest.String
	}

	return stats, nil
}

// ==================== Scheduled Tasks ====================

// InsertScheduledTask 插入排程任務
func (s *SQLiteStorage) InsertScheduledTask(task ScheduledTask) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO scheduled_tasks (id, chat_id, thread_id, name, cron_expr, task_type,
				payload, enabled, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			task.ID, task.ChatID, task.ThreadID, task.Name, task.CronExpr,
			task.TaskType, task.Payload, task.Enabled, task.CreatedAt,
		)
		return err
	})
}

// DeleteScheduledTask 刪除排程任務（需驗證 chatID 擁有權）
func (s *SQLiteStorage) DeleteScheduledTask(taskID string, chatID int64) error {
	return s.execWithRetry(func() error {
		result, err := s.db.Exec(`DELETE FROM scheduled_tasks WHERE id=? AND chat_id=?`, taskID, chatID)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("task not found or access denied")
		}
		return nil
	})
}

// GetScheduledTask 取得單一排程任務
func (s *SQLiteStorage) GetScheduledTask(taskID string, chatID int64) (ScheduledTask, error) {
	row := s.db.QueryRow(`
		SELECT id, chat_id, thread_id, name, cron_expr, task_type, payload, enabled,
			created_at, COALESCE(last_run_at,''), COALESCE(last_result,''),
			COALESCE(last_status,''), run_count, fail_count
		FROM scheduled_tasks WHERE id=? AND chat_id=?`, taskID, chatID)
	return scanScheduledTask(row)
}

// GetScheduledTasksByChat 取得特定聊天的所有排程任務
func (s *SQLiteStorage) GetScheduledTasksByChat(chatID int64) ([]ScheduledTask, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, thread_id, name, cron_expr, task_type, payload, enabled,
			created_at, COALESCE(last_run_at,''), COALESCE(last_result,''),
			COALESCE(last_status,''), run_count, fail_count
		FROM scheduled_tasks WHERE chat_id=? ORDER BY created_at DESC`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledTasks(rows)
}

// GetEnabledScheduledTasks 取得所有啟用的排程任務
func (s *SQLiteStorage) GetEnabledScheduledTasks() ([]ScheduledTask, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, thread_id, name, cron_expr, task_type, payload, enabled,
			created_at, COALESCE(last_run_at,''), COALESCE(last_result,''),
			COALESCE(last_status,''), run_count, fail_count
		FROM scheduled_tasks WHERE enabled=1 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledTasks(rows)
}

// UpdateScheduledTaskEnabled 更新排程任務的啟用狀態
func (s *SQLiteStorage) UpdateScheduledTaskEnabled(taskID string, chatID int64, enabled bool) error {
	return s.execWithRetry(func() error {
		enabledInt := 0
		if enabled {
			enabledInt = 1
		}
		_, err := s.db.Exec(`UPDATE scheduled_tasks SET enabled=? WHERE id=? AND chat_id=?`,
			enabledInt, taskID, chatID)
		return err
	})
}

// UpdateScheduledTaskResult 更新排程任務的執行結果
func (s *SQLiteStorage) UpdateScheduledTaskResult(taskID string, status string, result string, duration time.Duration) error {
	return s.execWithRetry(func() error {
		failIncr := 0
		if status == "failed" {
			failIncr = 1
		}
		_, err := s.db.Exec(`
			UPDATE scheduled_tasks
			SET last_run_at=?, last_status=?, last_result=?,
				run_count=run_count+1, fail_count=fail_count+?
			WHERE id=?`,
			time.Now(), status, result, failIncr, taskID)
		return err
	})
}

// scanScheduledTask scans a single row into ScheduledTask
func scanScheduledTask(row *sql.Row) (ScheduledTask, error) {
	var task ScheduledTask
	var createdAt, lastRunAt, lastResult, lastStatus string
	var enabled int

	err := row.Scan(&task.ID, &task.ChatID, &task.ThreadID, &task.Name, &task.CronExpr,
		&task.TaskType, &task.Payload, &enabled, &createdAt, &lastRunAt,
		&lastResult, &lastStatus, &task.RunCount, &task.FailCount)
	if err != nil {
		return task, err
	}

	task.Enabled = enabled == 1
	task.LastResult = lastResult
	task.LastStatus = lastStatus
	task.CreatedAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", createdAt)
	if lastRunAt != "" {
		task.LastRunAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", lastRunAt)
	}
	return task, nil
}

// scanScheduledTasks scans multiple rows into ScheduledTask slice
func scanScheduledTasks(rows *sql.Rows) ([]ScheduledTask, error) {
	var tasks []ScheduledTask
	for rows.Next() {
		var task ScheduledTask
		var createdAt, lastRunAt, lastResult, lastStatus string
		var enabled int

		err := rows.Scan(&task.ID, &task.ChatID, &task.ThreadID, &task.Name, &task.CronExpr,
			&task.TaskType, &task.Payload, &enabled, &createdAt, &lastRunAt,
			&lastResult, &lastStatus, &task.RunCount, &task.FailCount)
		if err != nil {
			return nil, err
		}

		task.Enabled = enabled == 1
		task.LastResult = lastResult
		task.LastStatus = lastStatus
		task.CreatedAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", createdAt)
		if lastRunAt != "" {
			task.LastRunAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", lastRunAt)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// ==================== Parallel Executions ====================

// InsertParallelExecution 插入並行執行記錄
func (s *SQLiteStorage) InsertParallelExecution(exec *ParallelExecution) error {
	resultsJSON, _ := json.Marshal(exec.Results)
	var completedAt interface{}
	if exec.CompletedAt != nil {
		completedAt = *exec.CompletedAt
	}

	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO parallel_executions (id, chat_id, thread_id, task_count, status,
				results_json, total_time_ms, created_at, completed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			exec.ID, exec.ChatID, exec.ThreadID, len(exec.Tasks),
			string(exec.Status), string(resultsJSON),
			exec.TotalTime.Milliseconds(), exec.CreatedAt, completedAt,
		)
		return err
	})
}

// ==================== Auto Skills ====================

// InsertAutoSkill 插入新的自動技能
func (s *SQLiteStorage) InsertAutoSkill(skill AutoSkill) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO auto_skills (id, name, description, tool_chain_json, context, tags_json,
				success_rate, use_count, version, status, project_path, chat_id,
				created_at, updated_at, last_used_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			skill.ID, skill.Name, skill.Description, MarshalToolChain(skill.ToolChain),
			skill.Context, MarshalTags(skill.Tags), skill.SuccessRate, skill.UseCount,
			skill.Version, skill.Status, skill.ProjectPath, skill.ChatID,
			skill.CreatedAt, skill.UpdatedAt, skill.LastUsedAt,
		)
		return err
	})
}

// UpdateAutoSkill 更新自動技能
func (s *SQLiteStorage) UpdateAutoSkill(skill AutoSkill) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			UPDATE auto_skills SET name=?, description=?, tool_chain_json=?, context=?,
				tags_json=?, success_rate=?, use_count=?, version=?, status=?,
				updated_at=?, last_used_at=?
			WHERE id=?`,
			skill.Name, skill.Description, MarshalToolChain(skill.ToolChain),
			skill.Context, MarshalTags(skill.Tags), skill.SuccessRate, skill.UseCount,
			skill.Version, skill.Status, skill.UpdatedAt, skill.LastUsedAt, skill.ID,
		)
		return err
	})
}

// DeleteAutoSkill 刪除自動技能
func (s *SQLiteStorage) DeleteAutoSkill(skillID string) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`DELETE FROM auto_skills WHERE id=?`, skillID)
		return err
	})
}

// GetActiveSkills 獲取所有活躍的技能
func (s *SQLiteStorage) GetActiveSkills() ([]AutoSkill, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, tool_chain_json, context, tags_json,
			success_rate, use_count, version, status, project_path, chat_id,
			created_at, updated_at, last_used_at
		FROM auto_skills WHERE status='active'
		ORDER BY use_count DESC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAutoSkills(rows)
}

// GetSkillsByChat 獲取特定聊天的所有技能
func (s *SQLiteStorage) GetSkillsByChat(chatID int64) ([]AutoSkill, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, tool_chain_json, context, tags_json,
			success_rate, use_count, version, status, project_path, chat_id,
			created_at, updated_at, last_used_at
		FROM auto_skills WHERE chat_id=?
		ORDER BY updated_at DESC`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAutoSkills(rows)
}

// scanAutoSkills scans rows into AutoSkill slice
func scanAutoSkills(rows *sql.Rows) ([]AutoSkill, error) {
	var skills []AutoSkill
	for rows.Next() {
		var skill AutoSkill
		var toolChainJSON, tagsJSON string
		var createdAt, updatedAt, lastUsedAt string

		err := rows.Scan(&skill.ID, &skill.Name, &skill.Description, &toolChainJSON,
			&skill.Context, &tagsJSON, &skill.SuccessRate, &skill.UseCount,
			&skill.Version, &skill.Status, &skill.ProjectPath, &skill.ChatID,
			&createdAt, &updatedAt, &lastUsedAt)
		if err != nil {
			return nil, err
		}

		skill.ToolChain = UnmarshalToolChain(toolChainJSON)
		skill.Tags = UnmarshalTags(tagsJSON)
		skill.CreatedAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", createdAt)
		skill.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", updatedAt)
		skill.LastUsedAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", lastUsedAt)

		skills = append(skills, skill)
	}
	return skills, rows.Err()
}

// 全域儲存實例
var globalStorage Storage

// InitStorage 初始化儲存系統
func InitStorage(dbPath string) error {
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	globalStorage = storage
	log.Printf("Storage system initialized successfully")
	return nil
}

// PromptClassificationRecord is the row written by RecordPromptClassification.
// It mirrors the prompt_classifications table, used by the Hermes UserPromptSubmit
// observation experiment (#104).
type PromptClassificationRecord struct {
	SessionID      string
	Timestamp      time.Time
	Source         string
	CWD            string
	PromptSnippet  string
	PromptLength   int
	Classification string
	MatchedRule    string
}

// RecordPromptClassification inserts one row into prompt_classifications.
// Failures are surfaced to the caller so hook handlers can log them.
func (s *SQLiteStorage) RecordPromptClassification(rec PromptClassificationRecord) error {
	return s.execWithRetry(func() error {
		_, err := s.db.Exec(`
			INSERT INTO prompt_classifications
				(session_id, timestamp, source, cwd, prompt_snippet, prompt_length, classification, matched_rule)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, rec.SessionID, rec.Timestamp, rec.Source, rec.CWD, rec.PromptSnippet, rec.PromptLength, rec.Classification, rec.MatchedRule)
		return err
	})
}

// ListPromptClassifications returns the most recent rows from prompt_classifications, newest first.
func (s *SQLiteStorage) ListPromptClassifications(limit int) ([]PromptClassificationRecord, error) {
	rows, err := s.db.Query(`
		SELECT session_id, timestamp, source, cwd, prompt_snippet, prompt_length, classification, matched_rule
		FROM prompt_classifications
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recs []PromptClassificationRecord
	for rows.Next() {
		var r PromptClassificationRecord
		if err := rows.Scan(&r.SessionID, &r.Timestamp, &r.Source, &r.CWD, &r.PromptSnippet, &r.PromptLength, &r.Classification, &r.MatchedRule); err != nil {
			return nil, err
		}
		recs = append(recs, r)
	}
	return recs, rows.Err()
}
