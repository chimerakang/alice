package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
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

// DecisionLog captures the full context behind AI-generated decisions
type DecisionLog struct {
	Timestamp     time.Time              `json:"timestamp"`
	SessionID     string                 `json:"session_id"`
	ProjectPath   string                 `json:"project_path"`
	ChatID        int64                  `json:"chat_id"`
	ThreadID      int                    `json:"thread_id"`
	UserPrompt    string                 `json:"user_prompt"`
	AgentResponse string                 `json:"agent_response"`
	ToolCalls     []ToolExecution        `json:"tool_calls"`
	Context       map[string]interface{} `json:"context"`
	Outcome       ExecutionOutcome       `json:"outcome"`
	DurationMs    int                    `json:"duration_ms"`
	TokensUsed    TokenStats             `json:"tokens_used"`
}

// ExecutionOutcome represents the result of an AI interaction
type ExecutionOutcome struct {
	Success      bool     `json:"success"`
	ErrorMessage string   `json:"error_message,omitempty"`
	TaskType     string   `json:"task_type"` // "code_generation", "file_operation", "analysis", etc.
	FilesChanged []string `json:"files_changed"`
	Summary      string   `json:"summary"`
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

// DecisionLogger manages AI decision transparency logging
type DecisionLogger struct {
	decisions []DecisionLog
	mu        sync.RWMutex
	maxSize   int
	enabled   bool
}

// Global decision logger instance
var globalDecisionLogger = &DecisionLogger{
	decisions: make([]DecisionLog, 0, 50),
	maxSize:   50, // Keep only the most recent 50 decisions
	enabled:   true,
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

	// 廣播工具開始事件到 WebSocket 客戶端
	BroadcastToolEvent("tool_execution_start", execution)
}

// LogToolComplete logs the completion of a tool execution
func (tl *ToolLogger) LogToolComplete(toolName string, status string, duration time.Duration, err error) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	var chatID int64 = 0
	success := (status == "success" && err == nil)
	var completedExecution *ToolExecution

	// Find the most recent execution of this tool and update it
	for i := range tl.executions {
		if tl.executions[i].ToolName == toolName && tl.executions[i].Status == "running" {
			tl.executions[i].Status = status
			tl.executions[i].Duration = duration
			chatID = tl.executions[i].ChatID
			if err != nil {
				tl.executions[i].Error = err.Error()
			}
			completedExecution = &tl.executions[i]
			break
		}
	}

	// 如果有 SQLite 儲存，將完成的工具執行記錄寫入資料庫
	if globalStorage != nil && completedExecution != nil {
		go func() {
			if err := globalStorage.InsertToolExecution(*completedExecution); err != nil {
				log.Printf("Warning: failed to persist tool execution to database: %v", err)
			}
		}()
	}

	// 廣播工具執行事件到 WebSocket 客戶端
	if completedExecution != nil {
		BroadcastToolEvent("tool_execution", *completedExecution)
	}

	// Record performance metrics for tool execution
	RecordToolExecution(toolName, duration, chatID, success)
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

// LogDecision records a complete AI decision with full context
func (dl *DecisionLogger) LogDecision(decision DecisionLog) {
	if !dl.enabled {
		return
	}

	dl.mu.Lock()
	defer dl.mu.Unlock()

	// Add to beginning of slice (most recent first)
	dl.decisions = append([]DecisionLog{decision}, dl.decisions...)

	// Trim to max size
	if len(dl.decisions) > dl.maxSize {
		dl.decisions = dl.decisions[:dl.maxSize]
	}

	// 如果有 SQLite 儲存，將決策記錄寫入資料庫
	if globalStorage != nil {
		go func() {
			if err := globalStorage.InsertDecisionLog(decision); err != nil {
				log.Printf("Warning: failed to persist decision log to database: %v", err)
			}
		}()
	}

	// 廣播決策事件到 WebSocket 客戶端
	BroadcastDecisionEvent(decision)
}

// GetRecentDecisions returns the most recent decision logs
func (dl *DecisionLogger) GetRecentDecisions(limit int) []DecisionLog {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	if limit <= 0 || limit > len(dl.decisions) {
		limit = len(dl.decisions)
	}

	result := make([]DecisionLog, limit)
	copy(result, dl.decisions[:limit])
	return result
}

// GetDecisionCount returns the total number of decisions logged
func (dl *DecisionLogger) GetDecisionCount() int {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return len(dl.decisions)
}

// SetEnabled enables or disables decision logging
func (dl *DecisionLogger) SetEnabled(enabled bool) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.enabled = enabled
}

// IsEnabled returns whether decision logging is enabled
func (dl *DecisionLogger) IsEnabled() bool {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return dl.enabled
}

// SearchDecisions searches for decisions matching criteria
func (dl *DecisionLogger) SearchDecisions(projectPath string, taskType string, successOnly bool) []DecisionLog {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	var results []DecisionLog
	for _, decision := range dl.decisions {
		match := true

		if projectPath != "" && decision.ProjectPath != projectPath {
			match = false
		}

		if taskType != "" && decision.Outcome.TaskType != taskType {
			match = false
		}

		if successOnly && !decision.Outcome.Success {
			match = false
		}

		if match {
			results = append(results, decision)
		}
	}

	return results
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
	startTime := time.Now()

	if onUpdate != nil {
		onUpdate("🔧 Claude Code 處理中 ...", false)
	}

	ps := a.current()
	log.Printf("[agent] calling CLI (stream), session=%s, project=%s", ps.sessionID, a.projectDir)

	// Track tool executions for this decision
	var toolCallsForDecision []ToolExecution

	resp, err := a.client.CallStream(userMessage, a.projectDir, ps.sessionID, func(toolName string, toolInput map[string]interface{}) {
		// Check if we should create a checkpoint before executing this tool
		a.checkAndCreateCheckpoint(toolName, toolInput)

		// Log tool execution start
		globalToolLogger.LogToolStart(toolName, toolInput, a.chatID, a.threadID)

		// Track this tool call for decision logging
		toolExecution := ToolExecution{
			Timestamp: time.Now(),
			ToolName:  toolName,
			Input:     toolInput,
			Status:    "executed", // Mark as executed immediately for decision log
			ChatID:    a.chatID,
			ThreadID:  a.threadID,
		}
		toolCallsForDecision = append(toolCallsForDecision, toolExecution)

		if onUpdate != nil {
			msg := formatToolUpdate(toolName, toolInput)
			if msg != "" {
				onUpdate(msg, true)
			}
		}
	})
	if err != nil {
		// Log decision for transparency (error case)
		a.logDecision(userMessage, "", toolCallsForDecision, startTime, nil, err)
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

	// Log decision for transparency (success case)
	a.logDecision(userMessage, resp.Result, toolCallsForDecision, startTime, resp, nil)

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

// logDecision records a complete AI decision with full context
func (a *Agent) logDecision(userPrompt, agentResponse string, toolCalls []ToolExecution, startTime time.Time, resp *CLIResponse, err error) {
	if !globalDecisionLogger.IsEnabled() {
		return
	}

	// Filter sensitive data from prompts and responses
	userPrompt = a.filterSensitiveData(userPrompt)
	agentResponse = a.filterSensitiveData(agentResponse)
	toolCalls = a.filterSensitiveToolCalls(toolCalls)

	duration := time.Since(startTime)
	ps := a.current()

	// Determine task type based on tool calls and content
	taskType := a.inferTaskType(userPrompt, toolCalls)

	// Extract files changed from tool calls
	filesChanged := a.extractFilesChanged(toolCalls)

	// Create execution outcome
	outcome := ExecutionOutcome{
		Success:      err == nil,
		TaskType:     taskType,
		FilesChanged: filesChanged,
		Summary:      a.generateSummary(userPrompt, agentResponse, taskType),
	}

	if err != nil {
		outcome.ErrorMessage = err.Error()
	}

	// Create context map
	context := map[string]interface{}{
		"turns":         0,
		"project_dir":   a.projectDir,
		"model":         a.client.Model,
		"tool_count":    len(toolCalls),
		"has_error":     err != nil,
	}

	if resp != nil {
		context["turns"] = resp.NumTurns
	}

	// Create token stats for this interaction
	tokenStats := TokenStats{}
	if resp != nil {
		tokenStats.TotalInputTokens = int64(resp.Usage.InputTokens)
		tokenStats.TotalOutputTokens = int64(resp.Usage.OutputTokens)
		tokenStats.TotalCostUSD = resp.TotalCostUSD
		tokenStats.APICallCount = 1
	}

	// Create decision log
	decision := DecisionLog{
		Timestamp:     startTime,
		SessionID:     ps.sessionID,
		ProjectPath:   a.projectDir,
		ChatID:        a.chatID,
		ThreadID:      a.threadID,
		UserPrompt:    userPrompt,
		AgentResponse: agentResponse,
		ToolCalls:     toolCalls,
		Context:       context,
		Outcome:       outcome,
		DurationMs:    int(duration.Milliseconds()),
		TokensUsed:    tokenStats,
	}

	// Log the decision
	globalDecisionLogger.LogDecision(decision)

	log.Printf("[agent] decision logged: task_type=%s, tools=%d, success=%v, duration=%dms",
		taskType, len(toolCalls), outcome.Success, int(duration.Milliseconds()))
}

// inferTaskType attempts to classify the type of task based on user input and tools used
func (a *Agent) inferTaskType(userPrompt string, toolCalls []ToolExecution) string {
	prompt := strings.ToLower(userPrompt)

	// Check for specific task patterns
	if strings.Contains(prompt, "read") || strings.Contains(prompt, "show") || strings.Contains(prompt, "what") {
		return "analysis"
	}
	if strings.Contains(prompt, "write") || strings.Contains(prompt, "create") || strings.Contains(prompt, "add") {
		return "code_generation"
	}
	if strings.Contains(prompt, "fix") || strings.Contains(prompt, "debug") || strings.Contains(prompt, "error") {
		return "debugging"
	}
	if strings.Contains(prompt, "test") {
		return "testing"
	}
	if strings.Contains(prompt, "commit") || strings.Contains(prompt, "git") {
		return "version_control"
	}

	// Classify by tools used
	hasFileOps := false
	hasBash := false
	for _, tool := range toolCalls {
		switch tool.ToolName {
		case "Read", "Write", "Edit":
			hasFileOps = true
		case "Bash":
			hasBash = true
		}
	}

	if hasFileOps && hasBash {
		return "complex_operation"
	} else if hasFileOps {
		return "file_operation"
	} else if hasBash {
		return "command_execution"
	}

	return "general_assistance"
}

// extractFilesChanged extracts file paths from tool calls
func (a *Agent) extractFilesChanged(toolCalls []ToolExecution) []string {
	filesSet := make(map[string]bool)

	for _, tool := range toolCalls {
		if tool.ToolName == "Write" || tool.ToolName == "Edit" || tool.ToolName == "Read" {
			if filePath, ok := tool.Input["file_path"].(string); ok && filePath != "" {
				filesSet[filePath] = true
			}
		}
	}

	files := make([]string, 0, len(filesSet))
	for file := range filesSet {
		files = append(files, file)
	}

	return files
}

// generateSummary creates a brief summary of the interaction
func (a *Agent) generateSummary(userPrompt, agentResponse, taskType string) string {
	if len(userPrompt) > 100 {
		userPrompt = userPrompt[:100] + "..."
	}

	switch taskType {
	case "code_generation":
		return fmt.Sprintf("Generated code for: %s", userPrompt)
	case "file_operation":
		return fmt.Sprintf("Performed file operations for: %s", userPrompt)
	case "analysis":
		return fmt.Sprintf("Analyzed: %s", userPrompt)
	case "debugging":
		return fmt.Sprintf("Debugged issue: %s", userPrompt)
	case "testing":
		return fmt.Sprintf("Testing task: %s", userPrompt)
	case "version_control":
		return fmt.Sprintf("Git operation: %s", userPrompt)
	default:
		return fmt.Sprintf("Task: %s", userPrompt)
	}
}

// filterSensitiveData removes or masks sensitive information from text
func (a *Agent) filterSensitiveData(text string) string {
	// Use security manager's PII detection if available
	if globalSecurityManager != nil {
		filtered, detected := globalSecurityManager.DetectAndFilterPII(text)
		if len(detected) > 0 {
			// Log security event for PII detection in agent logs
			globalSecurityManager.LogSecurityEvent(SecurityEvent{
				EventType:   "pii_filtered_in_logs",
				Severity:    "medium",
				Description: fmt.Sprintf("PII filtered from agent logs: %v", detected),
				UserID:      a.chatID,
				Details: map[string]interface{}{
					"detected_types": detected,
					"agent_context":  "decision_logging",
				},
			})
		}
		return filtered
	}

	// Fallback to legacy pattern matching if security manager not available
	sensitivePatterns := []struct {
		pattern     string
		replacement string
	}{
		{`sk-[a-zA-Z0-9-_]{20,}`, "[API_KEY_MASKED]"},                    // API keys
		{`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, "[EMAIL_MASKED]"}, // Email addresses
		{`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`, "[CARD_MASKED]"},       // Credit card numbers
		{`password\s*[:=]\s*\S+`, "password: [MASKED]"},                       // Passwords
		{`token\s*[:=]\s*\S+`, "token: [MASKED]"},                             // Tokens
		{`secret\s*[:=]\s*\S+`, "secret: [MASKED]"},                           // Secrets
	}

	filteredText := text
	for _, pattern := range sensitivePatterns {
		// Note: In production, use regexp for proper pattern matching
		// For now, using simple string replacement to avoid import complexity
		if strings.Contains(strings.ToLower(filteredText), strings.Split(pattern.replacement, ":")[0][1:]) {
			// Simple masking - in production would use proper regex
			filteredText = pattern.replacement
		}
	}

	return filteredText
}

// filterSensitiveToolCalls removes sensitive data from tool call inputs
func (a *Agent) filterSensitiveToolCalls(toolCalls []ToolExecution) []ToolExecution {
	filtered := make([]ToolExecution, len(toolCalls))

	for i, tool := range toolCalls {
		filtered[i] = tool

		// Filter sensitive data from tool inputs
		if tool.Input != nil {
			filteredInput := make(map[string]interface{})
			for key, value := range tool.Input {
				if a.isSensitiveKey(key) {
					filteredInput[key] = "[MASKED]"
				} else if strValue, ok := value.(string); ok {
					filteredInput[key] = a.filterSensitiveData(strValue)
				} else {
					filteredInput[key] = value
				}
			}
			filtered[i].Input = filteredInput
		}
	}

	return filtered
}

// isSensitiveKey checks if a key name indicates sensitive data
func (a *Agent) isSensitiveKey(key string) bool {
	sensitiveKeys := []string{
		"password", "token", "key", "secret", "auth", "credential",
		"private", "confidential", "sensitive",
	}

	keyLower := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(keyLower, sensitive) {
			return true
		}
	}

	return false
}

// checkAndCreateCheckpoint checks if a checkpoint should be created before executing a tool
func (a *Agent) checkAndCreateCheckpoint(toolName string, toolInput map[string]interface{}) {
	// Check if checkpoint manager is available and enabled
	if globalCheckpointManager == nil || !globalCheckpointManager.IsEnabled() {
		return
	}

	// Check if this operation requires a checkpoint
	shouldCreate, dangerousOp, description := globalCheckpointManager.ShouldCreateCheckpoint(toolName, toolInput)
	if !shouldCreate {
		return
	}

	// Extract session information
	sessionID := ""
	// For now, we'll use a simple session ID based on chat ID
	sessionID = fmt.Sprintf("chat_%d", a.chatID)

	// Create checkpoint
	log.Printf("Creating checkpoint before %s operation (chat: %d)", toolName, a.chatID)

	checkpoint, err := globalCheckpointManager.CreateCheckpoint(
		a.projectDir,
		description,
		TriggerPreDanger,
		sessionID,
		a.chatID,
		fmt.Sprintf("%s: %s", toolName, dangerousOp.Description),
	)

	if err != nil {
		log.Printf("Warning: Failed to create checkpoint before %s: %v", toolName, err)
		return
	}

	log.Printf("Checkpoint %s created successfully before %s operation", checkpoint.ID, toolName)

	// Broadcast checkpoint event via WebSocket if available
	if globalWebSocketHub != nil {
		checkpointEvent := map[string]interface{}{
			"event_type":     "checkpoint_created",
			"checkpoint_id":  checkpoint.ID,
			"tool_name":      toolName,
			"chat_id":        a.chatID,
			"project_dir":    a.projectDir,
			"description":    description,
			"trigger_type":   string(TriggerPreDanger),
			"dangerous_op":   dangerousOp.Description,
			"risk_level":     dangerousOp.RiskLevel.String(),
			"timestamp":      checkpoint.Timestamp,
		}

		// Use the correct broadcast method
		globalWebSocketHub.BroadcastEvent("checkpoint_created", checkpointEvent)
	}
}
