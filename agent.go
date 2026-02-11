package main

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"
)

// TokenStats tracks cumulative token usage for an agent session.
type TokenStats struct {
	TotalInputTokens  int64   // 累計 input tokens
	TotalOutputTokens int64   // 累計 output tokens
	TotalCostUSD      float64 // 累計費用（Max 訂閱下為 0）
	APICallCount      int     // CLI 呼叫次數
}

// ToolExecution represents a single tool execution event
type ToolExecution struct {
	Timestamp time.Time              `json:"timestamp"`
	ToolName  string                 `json:"tool_name"`
	Input     map[string]interface{} `json:"input"`
	Status    string                 `json:"status"` // "running", "success", "error"
	Duration  time.Duration          `json:"duration_ms"`
	ChatID    int64                  `json:"chat_id"`
	ThreadID  int                    `json:"thread_id"`
	Error     string                 `json:"error,omitempty"`
}

// ToolLogger manages tool execution logging
type ToolLogger struct {
	executions []ToolExecution
	mu         sync.RWMutex
	maxSize    int
}

// Global tool logger instance
var globalToolLogger = &ToolLogger{
	executions: make([]ToolExecution, 0, 100),
	maxSize:    100, // Keep only the most recent 100 executions
}

// LogToolStart logs the beginning of a tool execution
func (tl *ToolLogger) LogToolStart(toolName string, input map[string]interface{}, chatID int64, threadID int) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	execution := ToolExecution{
		Timestamp: time.Now(),
		ToolName:  toolName,
		Input:     input,
		Status:    "running",
		ChatID:    chatID,
		ThreadID:  threadID,
	}

	// Add to beginning of slice (most recent first)
	tl.executions = append([]ToolExecution{execution}, tl.executions...)

	// Trim to max size
	if len(tl.executions) > tl.maxSize {
		tl.executions = tl.executions[:tl.maxSize]
	}
}

// LogToolComplete logs the completion of a tool execution
func (tl *ToolLogger) LogToolComplete(toolName string, status string, duration time.Duration, err error) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	// Find the most recent execution of this tool and update it
	for i := range tl.executions {
		if tl.executions[i].ToolName == toolName && tl.executions[i].Status == "running" {
			tl.executions[i].Status = status
			tl.executions[i].Duration = duration
			if err != nil {
				tl.executions[i].Error = err.Error()
			}
			break
		}
	}
}

// GetRecentExecutions returns the most recent tool executions
func (tl *ToolLogger) GetRecentExecutions(limit int) []ToolExecution {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	if limit <= 0 || limit > len(tl.executions) {
		limit = len(tl.executions)
	}

	result := make([]ToolExecution, limit)
	copy(result, tl.executions[:limit])
	return result
}

// GetExecutionCount returns the total number of executions logged
func (tl *ToolLogger) GetExecutionCount() int {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	return len(tl.executions)
}

// projectState 保存單一專案的對話狀態
type projectState struct {
	sessionID    string
	stats        TokenStats
	lastActivity time.Time
	createdAt    time.Time
}

type Agent struct {
	client     *CLIClient
	projectDir string
	projects   map[string]*projectState // projectDir → state
	chatID     int64                    // Telegram chat ID
	threadID   int                      // Telegram thread ID (for forum topics)
}

func NewAgent(client *CLIClient, projectDir string, chatID int64, threadID int) *Agent {
	return &Agent{
		client:     client,
		projectDir: projectDir,
		projects:   make(map[string]*projectState),
		chatID:     chatID,
		threadID:   threadID,
	}
}

// current 取得目前專案的狀態，不存在則建立
func (a *Agent) current() *projectState {
	if ps, ok := a.projects[a.projectDir]; ok {
		return ps
	}
	now := time.Now()
	ps := &projectState{
		createdAt:    now,
		lastActivity: now,
	}
	a.projects[a.projectDir] = ps
	return ps
}

// Run sends a message to Claude Code CLI and returns the response text.
// onUpdate(msg, silent): silent=false for initial status, silent=true for tool updates.
func (a *Agent) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	if onUpdate != nil {
		onUpdate("🔧 Claude Code 處理中 ...", false)
	}

	ps := a.current()
	log.Printf("[agent] calling CLI (stream), session=%s, project=%s", ps.sessionID, a.projectDir)

	resp, err := a.client.CallStream(userMessage, a.projectDir, ps.sessionID, func(toolName string, toolInput map[string]interface{}) {
		// Log tool execution start
		globalToolLogger.LogToolStart(toolName, toolInput, a.chatID, a.threadID)

		if onUpdate != nil {
			msg := formatToolUpdate(toolName, toolInput)
			if msg != "" {
				onUpdate(msg, true)
			}
		}
	})
	if err != nil {
		return "", fmt.Errorf("CLI call failed: %w", err)
	}

	// 保存 session ID 以便下次 --resume
	ps.sessionID = resp.SessionID

	// 更新統計
	ps.stats.APICallCount++
	ps.stats.TotalInputTokens += int64(resp.Usage.InputTokens)
	ps.stats.TotalOutputTokens += int64(resp.Usage.OutputTokens)
	ps.stats.TotalCostUSD += resp.TotalCostUSD
	ps.lastActivity = time.Now()

	log.Printf("[agent] done: turns=%d tokens_in=%d tokens_out=%d cost=$%.4f session=%s",
		resp.NumTurns, resp.Usage.InputTokens, resp.Usage.OutputTokens,
		resp.TotalCostUSD, resp.SessionID)

	return resp.Result, nil
}

func formatToolUpdate(name string, input map[string]interface{}) string {
	switch name {
	case "Read":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("📖 讀取 %s", filepath.Base(path))
		}
		return "📖 讀取檔案"
	case "Write":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("✏️ 寫入 %s", filepath.Base(path))
		}
		return "✏️ 寫入檔案"
	case "Edit":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("✏️ 編輯 %s", filepath.Base(path))
		}
		return "✏️ 編輯檔案"
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			if len(cmd) > 60 {
				cmd = cmd[:60] + "..."
			}
			return fmt.Sprintf("💻 %s", cmd)
		}
		return "💻 執行指令"
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("🔍 搜尋 %s", pattern)
		}
		return "🔍 搜尋檔案"
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("🔍 搜尋 %s", pattern)
		}
		return "🔍 搜尋程式碼"
	default:
		return fmt.Sprintf("🔧 %s", name)
	}
}

// Reset clears the current project's session and stats
func (a *Agent) Reset() {
	delete(a.projects, a.projectDir)
}

// SetProject switches the working directory (preserves all project sessions)
func (a *Agent) SetProject(dir string) {
	a.projectDir = dir
}

// Stats returns the current project's token usage statistics
func (a *Agent) Stats() TokenStats {
	return a.current().stats
}

// SessionID returns the current project's CLI session ID
func (a *Agent) SessionID() string {
	return a.current().sessionID
}

// ProjectDir returns the current project directory
func (a *Agent) ProjectDir() string {
	return a.projectDir
}

// LastActivity returns when the agent was last active
func (a *Agent) LastActivity() time.Time {
	return a.current().lastActivity
}

// CreatedAt returns when the agent session was created
func (a *Agent) CreatedAt() time.Time {
	return a.current().createdAt
}

// IsActive returns true if the agent has been active within the last hour
func (a *Agent) IsActive() bool {
	return time.Since(a.current().lastActivity) < time.Hour
}

// ProjectCount returns the number of projects this agent has worked with
func (a *Agent) ProjectCount() int {
	return len(a.projects)
}
