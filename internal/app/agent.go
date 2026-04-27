package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
	"claude-tg-agent/internal/app/security"
)

// TokenStats tracks cumulative token usage for an agent session.
type TokenStats struct {
	TotalInputTokens  int64   // 累計 input tokens
	TotalOutputTokens int64   // 累計 output tokens
	TotalCostUSD      float64 // 累計費用（Max 訂閱下為 0）
	APICallCount      int     // CLI 呼叫次數
	Model             string  // NEW: 使用的模型（haiku, sonnet, opus）
}

// CostSavingsReport 成本節省報告（用於 Dashboard 展示）
type CostSavingsReport struct {
	PeriodHours       int                           `json:"period_hours"`
	StartTime         time.Time                     `json:"start_time"`
	EndTime           time.Time                     `json:"end_time"`
	ActualCost        float64                       `json:"actual_cost"`        // 實際花費
	DefaultModelCost  float64                       `json:"default_model_cost"` // 假設全用預設模型花費
	SavingsCost       float64                       `json:"savings_cost"`       // 節省金額
	SavingsPercent    float64                       `json:"savings_percent"`    // 節省百分比
	TotalRequests     int                           `json:"total_requests"`
	ByModel           map[string]ModelCostBreakdown `json:"by_model"`
	RoutingMethodStat map[string]int                `json:"routing_method_stat"`
}

// ModelCostBreakdown 按模型的成本分解
type ModelCostBreakdown struct {
	Calls         int     `json:"calls"`
	ActualCost    float64 `json:"actual_cost"`
	WouldHaveCost float64 `json:"would_have_cost"` // 假設用預設模型的成本
	Saved         float64 `json:"saved"`           // 節省金額
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
}

// ToolExecution represents a single tool execution event
type ToolExecution struct {
	Timestamp   time.Time              `json:"timestamp"`
	ToolName    string                 `json:"tool_name"`
	Input       map[string]interface{} `json:"input"`
	Status      string                 `json:"status"` // "running", "success", "error"
	Duration    time.Duration          `json:"duration_ms"`
	ChatID      int64                  `json:"chat_id"`
	ThreadID    int                    `json:"thread_id"`
	Error       string                 `json:"error,omitempty"`
	ProjectPath string                 `json:"project_path,omitempty"`
}

// DecisionLog captures the full context behind AI-generated decisions
type DecisionLog struct {
	Timestamp       time.Time              `json:"timestamp"`
	SessionID       string                 `json:"session_id"`
	ProjectPath     string                 `json:"project_path"`
	ChatID          int64                  `json:"chat_id"`
	ThreadID        int                    `json:"thread_id"`
	UserPrompt      string                 `json:"user_prompt"`
	AgentResponse   string                 `json:"agent_response"`
	ThinkingContent string                 `json:"thinking_content"` // AI thinking/reasoning blocks
	ToolCalls       []ToolExecution        `json:"tool_calls"`
	Context         map[string]interface{} `json:"context"`
	Outcome         ExecutionOutcome       `json:"outcome"`
	DurationMs      int                    `json:"duration_ms"`
	TokensUsed      TokenStats             `json:"tokens_used"`
	GitCommitHash   string                 `json:"git_commit_hash,omitempty"`
	GitBranch       string                 `json:"git_branch,omitempty"`
	Source          string                 `json:"source"`             // "telegram", "terminal", "vscode", "unknown"
	Model           string                 `json:"model"`              // NEW: "haiku", "sonnet", "opus"
	RoutingReason   string                 `json:"routing_reason"`     // NEW: "user_command", "ai_router", "static_rule", "default"
	RoutingLatency  int                    `json:"routing_latency_ms"` // NEW: 路由判斷耗時 (ms)
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
func (tl *ToolLogger) LogToolStart(toolName string, input map[string]interface{}, chatID int64, threadID int, projectPath string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	execution := ToolExecution{
		Timestamp:   time.Now(),
		ToolName:    toolName,
		Input:       input,
		Status:      "running",
		ChatID:      chatID,
		ThreadID:    threadID,
		ProjectPath: projectPath,
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
	var projectPath string = ""
	success := (status == "success" && err == nil)
	var completedExecution *ToolExecution

	// Find the most recent execution of this tool and update it
	for i := range tl.executions {
		if tl.executions[i].ToolName == toolName && tl.executions[i].Status == "running" {
			tl.executions[i].Status = status
			tl.executions[i].Duration = duration
			chatID = tl.executions[i].ChatID
			projectPath = tl.executions[i].ProjectPath
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

	// Record performance metrics for tool execution (with projectPath now)
	RecordToolExecution(toolName, duration, chatID, projectPath, success)
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

// LoadFromDB loads recent tool executions from database into memory on startup
func (tl *ToolLogger) LoadFromDB(storage Storage) {
	executions, err := storage.GetToolExecutions(tl.maxSize, 0)
	if err != nil {
		log.Printf("[tool-logger] failed to load from DB: %v", err)
		return
	}
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.executions = executions
	log.Printf("[tool-logger] loaded %d executions from DB", len(executions))
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

// LoadFromDB loads recent decisions from database into memory on startup
func (dl *DecisionLogger) LoadFromDB(storage Storage) {
	decisions, err := storage.GetDecisionLogs(dl.maxSize, 0)
	if err != nil {
		log.Printf("[decision-logger] failed to load from DB: %v", err)
		return
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.decisions = decisions
	log.Printf("[decision-logger] loaded %d decisions from DB", len(decisions))
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
	ctx              *ChatContext
	stats            TokenStats
	lastTotalCostUSD float64 // CLI's cumulative cost from last call (for delta calculation)
}

type Agent struct {
	client               Client
	projectDir           string
	projects             map[string]*projectState // projectDir → state
	chatID               int64                    // Telegram chat ID
	threadID             int                      // Telegram thread ID (for forum topics)
	currentModelOverride string                   // Current model override for this agent (for dynamic routing)
	lastUsedModel        string                   // Last model used (for session continuity)
	chatContext          *ChatContext             // Shared conversation state across Agent/Hermes
	enablePlanMode       bool                     // OpusPlan: enable two-phase plan+execute
	planModel            string                   // OpusPlan: model for planning phase (e.g. Opus)
	executeModel         string                   // OpusPlan: model for execution phase (e.g. Sonnet)
	cliTimeoutMinutes    int                      // CLI 執行逾時（分鐘），0=無限制
	// Abort control
	cancelFunc context.CancelFunc // 取消正在執行的 CLI 子程序
	cancelMu   sync.Mutex         // 保護 cancelFunc 的併發存取
	processing bool               // 是否正在處理請求

	// Per-call metrics — populated at the end of every Run/runDirect.
	// Read via LastCallMetrics() so PlanExecuteEngine can attribute Executor
	// tokens to the actual model used (otherwise per-model breakdown only
	// captures the Planner and labels Executor work as "Unknown").
	lastCallMu      sync.Mutex
	lastCallModel   string
	lastCallInputT  int
	lastCallOutputT int
}

func NewAgent(client Client, projectDir string, chatID int64, threadID int) *Agent {
	return NewAgentWithContext(client, NewChatContext(chatID, threadID, projectDir))
}

func NewAgentWithContext(client Client, chatCtx *ChatContext) *Agent {
	if chatCtx == nil {
		chatCtx = NewChatContext(0, 0, "")
	}
	return &Agent{
		client:      client,
		projectDir:  chatCtx.ProjectDir,
		projects:    make(map[string]*projectState),
		chatID:      chatCtx.ChatID,
		threadID:    chatCtx.ThreadID,
		chatContext: chatCtx,
	}
}

// Abort 中斷正在執行的 agent 任務，回傳是否成功中斷
func (a *Agent) Abort() bool {
	a.cancelMu.Lock()
	defer a.cancelMu.Unlock()
	if a.cancelFunc != nil {
		a.cancelFunc()
		a.cancelFunc = nil
		return true
	}
	return false
}

// IsProcessing 回報 agent 是否正在執行任務
func (a *Agent) IsProcessing() bool {
	a.cancelMu.Lock()
	defer a.cancelMu.Unlock()
	return a.processing
}

// SetModelOverride 設定此 agent 的模型覆蓋（用於動態路由）
func (a *Agent) SetModelOverride(model string) {
	a.currentModelOverride = model
}

// SetPlanMode 啟用或停用 OpusPlan 兩階段模式
func (a *Agent) SetPlanMode(enabled bool, planModel, executeModel string) {
	a.enablePlanMode = enabled
	a.planModel = planModel
	a.executeModel = executeModel
}

// IsPlanMode 回報是否啟用 OpusPlan 模式
func (a *Agent) IsPlanMode() bool {
	return a.enablePlanMode
}

// opusPlanPrompt 產生計劃階段的 prompt 包裝
func opusPlanPrompt(userMessage string) string {
	return fmt.Sprintf(`You are in PLANNING MODE. Your task is to analyze the following request and produce a detailed execution plan. Do NOT execute any tools or make any changes. Only output a structured plan.

Requirements for your plan:
1. Break the task into numbered steps
2. For each step, specify which files need to be modified and what changes are needed
3. Identify potential risks or edge cases
4. Estimate complexity (simple/medium/complex) for each step

User request:
%s

Output your plan in a clear, structured format.`, userMessage)
}

// selectModel 根據使用者訊息和靜態規則選擇最合適的模型
// 返回選擇的模型名稱和路由原因
func (a *Agent) selectModel(userMessage string) (model string, routingReason string) {
	routes := GetDefaultModelRoutes()
	var bestMatch *ModelRoute

	// 遍歷所有規則，找到最優先（最低優先級數字）的匹配
	for i := range routes {
		route := &routes[i]
		// 嘗試編譯和匹配正則表達式
		if re, err := regexp.Compile(route.Pattern); err == nil {
			if re.MatchString(userMessage) {
				// 如果尚未有匹配或此規則優先級更高
				if bestMatch == nil || route.Priority < bestMatch.Priority {
					bestMatch = route
				}
			}
		}
	}

	// 如果找到匹配規則，使用該規則
	if bestMatch != nil {
		return bestMatch.Model, "static_rule"
	}

	// 返回預設模型 (Sonnet)
	return "sonnet", "default"
}

// current 取得目前專案的狀態，不存在則建立
func (a *Agent) current() *projectState {
	if ps, ok := a.projects[a.projectDir]; ok {
		return ps
	}
	if a.chatContext == nil {
		a.chatContext = NewChatContext(a.chatID, a.threadID, a.projectDir)
	}
	a.chatContext.ProjectDir = a.projectDir
	ps := &projectState{
		ctx: a.chatContext,
	}
	a.projects[a.projectDir] = ps
	return ps
}

// Run sends a message to Claude Code CLI and returns the response text.
// onUpdate(msg, silent): silent=false for initial status, silent=true for tool updates.
func (a *Agent) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	startTime := time.Now()

	// 設定 context with timeout (可透過 config 調整，預設 15 分鐘，0=無限制)
	var ctx context.Context
	var cancel context.CancelFunc
	if a.cliTimeoutMinutes > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(a.cliTimeoutMinutes)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	a.cancelMu.Lock()
	a.cancelFunc = cancel
	a.processing = true
	a.cancelMu.Unlock()
	defer func() {
		a.cancelMu.Lock()
		a.cancelFunc = nil
		a.processing = false
		a.cancelMu.Unlock()
		cancel()
	}()

	if onUpdate != nil {
		onUpdate("🔧 Claude Code processing...", false)
	}

	ps := a.current()

	// Handle model selection with three-tier routing priority
	var routingReason string
	var routingLatency int
	selectedModel := ""

	// Priority 1: User command override
	if a.currentModelOverride != "" {
		selectedModel = a.currentModelOverride
		routingReason = "user_command"
		routingLatency = 0
	} else {
		// Priority 2: Static rules-based routing
		startRouting := time.Now()
		selectedModel, routingReason = a.selectModel(userMessage)
		routingLatency = int(time.Since(startRouting).Milliseconds())
		msgPreview := userMessage
		if len(msgPreview) > 50 {
			msgPreview = msgPreview[:50]
		}
		log.Printf("[telegram] model routing: message=%q selected=%s reason=%s latency=%dms", msgPreview, selectedModel, routingReason, routingLatency)
	}

	if selectedModel == "" {
		selectedModel = a.client.GetModel() // Use default if no model selected
		routingReason = "default"
	}
	selectedBackend := BackendKindForModel(selectedModel)
	previousBackend := ps.ctx.LastBackend

	// If model changed, bridge recent messages. Native sessions are kept per
	// backend; same-backend model changes still start a fresh session.
	if selectedModel != a.lastUsedModel && a.lastUsedModel != "" {
		bridge := buildContextBridge(ps.ctx.RecentMessagesSnapshot())
		if bridge != "" {
			userMessage = bridge + userMessage
			log.Printf("[agent] context bridge injected (%d recent messages)", len(ps.ctx.RecentMsgs))
		}
		if selectedBackend == previousBackend {
			ps.ctx.ClearSession(selectedBackend)
		}
	}
	a.lastUsedModel = selectedModel
	ps.ctx.LastBackend = selectedBackend

	// Inject matching auto-skills into user message as context
	if globalSkillManager != nil {
		matchedSkills := globalSkillManager.FindRelevantSkills(userMessage, a.projectDir)
		if len(matchedSkills) > 0 {
			skillContext := FormatSkillsForPrompt(matchedSkills)
			userMessage = userMessage + "\n" + skillContext
			log.Printf("[agent] injected %d auto-skills into prompt", len(matchedSkills))
		}
	}

	sessionID := ps.ctx.Session(selectedBackend)
	log.Printf("[agent] calling CLI (stream), session=%s, project=%s, model=%s routing_reason=%s", sessionID, a.projectDir, a.lastUsedModel, routingReason)

	// Pre-generate decision ID so checkpoints created during streaming
	// can reference the decision that will be created after streaming completes.
	currentDecisionID := generateDecisionID(a.chatID, a.threadID, startTime)

	// Track tool executions for this decision
	var toolCallsForDecision []ToolExecution

	const maxRetries = 2
	var resp *CLIResponse
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*15) * time.Second
			log.Printf("[agent] retry %d/%d after %v (previous error: %v)", attempt, maxRetries, backoff, err)
			if onUpdate != nil {
				onUpdate(fmt.Sprintf("🔄 重試中 (%d/%d)...", attempt, maxRetries), false)
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
			toolCallsForDecision = nil
		}

		resp, err = a.client.CallStream(ctx, userMessage, a.projectDir, sessionID, a.lastUsedModel, func(toolName string, toolInput map[string]interface{}) {
			// Check if we should create a checkpoint before executing this tool
			a.checkAndCreateCheckpoint(toolName, toolInput, currentDecisionID)

			// Log tool execution start
			globalToolLogger.LogToolStart(toolName, toolInput, a.chatID, a.threadID, a.projectDir)

			// Track this tool call for decision logging
			toolExecution := ToolExecution{
				Timestamp:   time.Now(),
				ToolName:    toolName,
				Input:       toolInput,
				Status:      "executed", // Mark as executed immediately for decision log
				ChatID:      a.chatID,
				ThreadID:    a.threadID,
				ProjectPath: a.projectDir,
			}
			toolCallsForDecision = append(toolCallsForDecision, toolExecution)

			if onUpdate != nil {
				msg := formatToolUpdate(toolName, toolInput)
				if msg != "" {
					onUpdate(msg, true)
				}
			}
		}, func(contentType, text string) {
			if onUpdate == nil || text == "" {
				return
			}
			switch contentType {
			case "thinking", "text":
				// Don't send streaming previews - full response is sent at completion
			}
		})

		if err == nil {
			break
		}

		if isRetryableError(err) && attempt < maxRetries {
			log.Printf("[agent] retryable error detected: %v", err)
			if resp != nil && resp.SessionID != "" {
				ps.ctx.SetSession(selectedBackend, resp.SessionID)
				sessionID = resp.SessionID
			}
			continue
		}

		break
	} // end retry loop

	if err != nil {
		partialText := ""
		deltaCost := 0.0
		if resp != nil {
			partialText = resp.TextContent
			if resp.SessionID != "" {
				ps.ctx.SetSession(selectedBackend, resp.SessionID)
			}
			deltaCost = resp.TotalCostUSD - ps.lastTotalCostUSD
			ps.lastTotalCostUSD = resp.TotalCostUSD

			ps.stats.APICallCount++
			ps.stats.TotalInputTokens += int64(resp.Usage.InputTokens)
			ps.stats.TotalOutputTokens += int64(resp.Usage.OutputTokens)
			ps.stats.TotalCostUSD += deltaCost
			ps.ctx.LastActivity = time.Now()
		}
		a.logDecision(userMessage, partialText, toolCallsForDecision, startTime, resp, err, routingReason, routingLatency, deltaCost)
		return partialText, fmt.Errorf("CLI call failed: %w", err)
	}

	// 保存 session ID 以便下次 --resume
	ps.ctx.SetSession(selectedBackend, resp.SessionID)

	// Calculate cost delta (CLI's TotalCostUSD is session-cumulative)
	deltaCost := resp.TotalCostUSD - ps.lastTotalCostUSD
	ps.lastTotalCostUSD = resp.TotalCostUSD

	// 更新統計
	ps.stats.APICallCount++
	ps.stats.TotalInputTokens += int64(resp.Usage.InputTokens)
	ps.stats.TotalOutputTokens += int64(resp.Usage.OutputTokens)
	ps.stats.TotalCostUSD += deltaCost
	ps.ctx.LastActivity = time.Now()

	log.Printf("[agent] done: turns=%d tokens_in=%d tokens_out=%d cost=$%.4f (delta from $%.4f) session=%s",
		resp.NumTurns, resp.Usage.InputTokens, resp.Usage.OutputTokens,
		deltaCost, resp.TotalCostUSD, resp.SessionID)

	// Log decision for transparency (success case)
	a.logDecision(userMessage, resp.Result, toolCallsForDecision, startTime, resp, nil, routingReason, routingLatency, deltaCost)

	// Save exchange to recentMessages for context bridge on future model switches
	addToRecentMessages(ps, userMessage, resp.Result)

	a.recordLastCallMetrics(a.lastUsedModel, resp.Usage.InputTokens, resp.Usage.OutputTokens)

	return resp.Result, nil
}

// recordLastCallMetrics stores the model and token totals from the most
// recent CLI call so DirectEngine can pull them via LastCallMetrics().
func (a *Agent) recordLastCallMetrics(model string, inTokens, outTokens int) {
	a.lastCallMu.Lock()
	defer a.lastCallMu.Unlock()
	a.lastCallModel = model
	a.lastCallInputT = inTokens
	a.lastCallOutputT = outTokens
}

// LastCallMetrics returns the model and token totals from the most recent
// Agent CLI call. Returns zero values when no call has been made yet.
// Used by engine.DirectEngine via the MetricsProvider type assertion.
func (a *Agent) LastCallMetrics() (model string, inTokens, outTokens int) {
	a.lastCallMu.Lock()
	defer a.lastCallMu.Unlock()
	return a.lastCallModel, a.lastCallInputT, a.lastCallOutputT
}

// RunWithPlan executes a two-phase OpusPlan workflow:
// Phase 1: Call plan model (Opus) with --max-turns 1 to produce a plan
// Phase 2: Call execute model (Sonnet) with the plan as context to execute
func (a *Agent) RunWithPlan(userMessage string, onUpdate func(string, bool)) (string, error) {
	startTime := time.Now()

	var ctx context.Context
	var cancel context.CancelFunc
	if a.cliTimeoutMinutes > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(a.cliTimeoutMinutes)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	a.cancelMu.Lock()
	a.cancelFunc = cancel
	a.processing = true
	a.cancelMu.Unlock()
	defer func() {
		a.cancelMu.Lock()
		a.cancelFunc = nil
		a.processing = false
		a.cancelMu.Unlock()
		cancel()
	}()

	planFn := func(ctx context.Context, _ string, projectDir string) (string, string, int, int, error) {
		if onUpdate != nil {
			onUpdate("🧠 Phase 1: Planning with Opus...", false)
		}
		log.Printf("[agent] OpusPlan via PlanExecuteEngine: calling plan model=%s, project=%s", a.planModel, projectDir)
		planResp, err := a.client.CallPlan(ctx, opusPlanPrompt(userMessage), projectDir, a.planModel, func(contentType, text string) {
			if onUpdate != nil && contentType == "text" && text != "" {
				preview := text
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				onUpdate(fmt.Sprintf("🧠 Planning: %s", preview), true)
			}
		})
		if err != nil {
			return "", "", 0, 0, err
		}
		planText := planResp.Result
		if planText == "" {
			planText = planResp.TextContent
		}
		desc := fmt.Sprintf("Follow this execution plan to complete the original request.\n\nPlan:\n%s\n\nOriginal user request:\n%s", planText, userMessage)
		raw, _ := json.Marshal([]hermes.SubTask{{
			ID:          "s1",
			Description: desc,
			Status:      hermes.SubTaskPending,
		}})
		return "```json\n" + string(raw) + "\n```", planResp.SessionID, planResp.Usage.InputTokens, planResp.Usage.OutputTokens, nil
	}

	direct := appengine.NewDirectEngine(&metricsForwardingRunner{
		run: func(msg string, update func(string, bool)) (string, error) {
			if onUpdate != nil {
				onUpdate("⚡ Phase 2: Executing with Sonnet...", false)
			}
			return a.runDirect(ctx, msg, update, startTime)
		},
		metrics: a.LastCallMetrics,
	})
	store := hermes.NewMemoryTaskStore()
	engine := appengine.NewPlanExecuteEngine(appengine.PlanExecuteConfig{
		PlannerModel:          a.planModel,
		ProjectDir:            a.projectDir,
		ChatID:                a.chatID,
		MaxPlannerJSONRetries: 1,
		InterruptPolicy:       hermes.InterruptQueue,
		Budget:                hermes.TokenBudget{},
		AccumulatedCfg:        hermes.AccumulatedConfig{},
	}, planFn, direct, store, &hermes.NoopProgressReporter{})

	result, err := engine.Run(ctx, userMessage, a.chatContext, nil)
	if err != nil {
		log.Printf("[agent] OpusPlan via PlanExecuteEngine failed: %v", err)
		if onUpdate != nil {
			onUpdate("⚠️ Plan phase failed, falling back to direct execution...", false)
		}
		return a.runDirect(ctx, userMessage, onUpdate, startTime)
	}
	return result.Text, nil
}

type directRunnerFunc func(string, func(string, bool)) (string, error)

func (f directRunnerFunc) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	return f(userMessage, onUpdate)
}

// metricsForwardingRunner wraps a function-style runner together with a
// metrics provider (typically *Agent). DirectEngine uses LastCallMetrics()
// to attribute Executor tokens to the actual model — without this wrapper
// the per-model breakdown would lose every Executor call.
type metricsForwardingRunner struct {
	run     func(string, func(string, bool)) (string, error)
	metrics func() (string, int, int)
}

func (r *metricsForwardingRunner) Run(msg string, onUpdate func(string, bool)) (string, error) {
	return r.run(msg, onUpdate)
}

func (r *metricsForwardingRunner) LastCallMetrics() (string, int, int) {
	if r.metrics == nil {
		return "", 0, 0
	}
	return r.metrics()
}

// runDirect is the standard single-phase execution (fallback when plan phase fails)
func (a *Agent) runDirect(ctx context.Context, userMessage string, onUpdate func(string, bool), startTime time.Time) (string, error) {
	if onUpdate != nil {
		onUpdate("🔧 Claude Code processing...", false)
	}

	ps := a.current()

	selectedModel := a.executeModel
	if selectedModel == "" {
		selectedModel = a.client.GetModel()
	}
	a.lastUsedModel = selectedModel
	selectedBackend := BackendKindForModel(selectedModel)
	ps.ctx.LastBackend = selectedBackend

	sessionID := ps.ctx.Session(selectedBackend)
	log.Printf("[agent] runDirect: session=%s, project=%s, model=%s", sessionID, a.projectDir, selectedModel)

	currentDecisionID := generateDecisionID(a.chatID, a.threadID, startTime)
	var toolCallsForDecision []ToolExecution

	resp, err := a.client.CallStream(ctx, userMessage, a.projectDir, sessionID, selectedModel, func(toolName string, toolInput map[string]interface{}) {
		a.checkAndCreateCheckpoint(toolName, toolInput, currentDecisionID)
		globalToolLogger.LogToolStart(toolName, toolInput, a.chatID, a.threadID, a.projectDir)

		toolExecution := ToolExecution{
			Timestamp:   time.Now(),
			ToolName:    toolName,
			Input:       toolInput,
			Status:      "executed",
			ChatID:      a.chatID,
			ThreadID:    a.threadID,
			ProjectPath: a.projectDir,
		}
		toolCallsForDecision = append(toolCallsForDecision, toolExecution)

		if onUpdate != nil {
			msg := formatToolUpdate(toolName, toolInput)
			if msg != "" {
				onUpdate(msg, true)
			}
		}
	}, func(contentType, text string) {})

	if err != nil {
		partialText := ""
		deltaCost := 0.0
		if resp != nil {
			partialText = resp.TextContent
			if resp.SessionID != "" {
				ps.ctx.SetSession(selectedBackend, resp.SessionID)
			}
			deltaCost = resp.TotalCostUSD - ps.lastTotalCostUSD
			ps.lastTotalCostUSD = resp.TotalCostUSD
			ps.stats.APICallCount++
			ps.stats.TotalInputTokens += int64(resp.Usage.InputTokens)
			ps.stats.TotalOutputTokens += int64(resp.Usage.OutputTokens)
			ps.stats.TotalCostUSD += deltaCost
			ps.ctx.LastActivity = time.Now()
		}
		a.logDecision(userMessage, partialText, toolCallsForDecision, startTime, resp, err, "opus_plan_fallback", 0, deltaCost)
		return partialText, fmt.Errorf("CLI call failed: %w", err)
	}

	ps.ctx.SetSession(selectedBackend, resp.SessionID)
	deltaCost := resp.TotalCostUSD - ps.lastTotalCostUSD
	ps.lastTotalCostUSD = resp.TotalCostUSD
	ps.stats.APICallCount++
	ps.stats.TotalInputTokens += int64(resp.Usage.InputTokens)
	ps.stats.TotalOutputTokens += int64(resp.Usage.OutputTokens)
	ps.stats.TotalCostUSD += deltaCost
	ps.ctx.LastActivity = time.Now()

	a.logDecision(userMessage, resp.Result, toolCallsForDecision, startTime, resp, nil, "opus_plan_fallback", 0, deltaCost)
	addToRecentMessages(ps, userMessage, resp.Result)

	a.recordLastCallMetrics(selectedModel, resp.Usage.InputTokens, resp.Usage.OutputTokens)

	return resp.Result, nil
}

// isRetryableError determines if a CLI error is transient and worth retrying.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "rate limit") ||
		strings.Contains(s, "429") ||
		strings.Contains(s, "overloaded") ||
		strings.Contains(s, "529") ||
		strings.Contains(s, "temporary") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "EOF")
}

// addToRecentMessages 儲存最近一輪對話（最多保留 5 輪）
func addToRecentMessages(ps *projectState, userMsg, assistantMsg string) {
	if ps == nil || ps.ctx == nil {
		return
	}
	ps.ctx.AddRecentMessage(userMsg, assistantMsg)
}

// buildContextBridge 從最近對話記錄產生上下文摘要
// 在 model 切換時注入到新 session 的第一條訊息，避免上下文丟失
func buildContextBridge(messages []contextMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Context from previous conversation — continuing from where we left off]\n")
	for _, msg := range messages {
		role := "User"
		if msg.Role == "assistant" {
			role = "Assistant"
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", role, msg.Content))
	}
	sb.WriteString("\n---\n\n")
	return sb.String()
}

func formatToolUpdate(name string, input map[string]interface{}) string {
	switch name {
	case "Read":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("📖 Read %s", filepath.Base(path))
		}
		return "📖 Read file"
	case "Write":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("✍️ Write %s", filepath.Base(path))
		}
		return "✍️ Write file"
	case "Edit":
		if path, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("✏️ Edit %s", filepath.Base(path))
		}
		return "✏️ Edit file"
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			if len(cmd) > 60 {
				cmd = cmd[:60] + "..."
			}
			return fmt.Sprintf("💻 %s", cmd)
		}
		return "💻 Execute command"
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("🔍 Find %s", pattern)
		}
		return "🔍 Find files"
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("🔎 Search %s", pattern)
		}
		return "🔎 Search content"
	case "command_execution":
		// Codex CLI's only tool — shell execution. Mirrors Bash formatting.
		if cmd, ok := input["command"].(string); ok {
			if len(cmd) > 60 {
				cmd = cmd[:60] + "..."
			}
			return fmt.Sprintf("💻 %s", cmd)
		}
		return "💻 Execute command"
	default:
		return fmt.Sprintf("🔧 %s", name)
	}
}

// Reset clears the current project's session and stats
func (a *Agent) Reset() {
	delete(a.projects, a.projectDir)
	if a.chatContext != nil {
		a.chatContext.Sessions = make(map[BackendKind]string)
		a.chatContext.RecentMsgs = nil
		a.chatContext.LastActivity = time.Now()
	}
}

// ClearSession resets only the session ID, forcing fresh Claude Code context on next run while preserving usage stats
func (a *Agent) ClearSession() {
	ps := a.current()
	ps.ctx.ClearSession(ps.ctx.LastBackend)
}

func (a *Agent) ClearSessionForModel(model string) {
	a.current().ctx.ClearSession(BackendKindForModel(model))
}

// SetProject switches the working directory (preserves all project sessions)
func (a *Agent) SetProject(dir string) {
	a.projectDir = dir
	if a.chatContext != nil {
		a.chatContext.ProjectDir = dir
	}
}

// Stats returns the current project's token usage statistics
func (a *Agent) Stats() TokenStats {
	return a.current().stats
}

// SessionID returns the current project's CLI session ID
func (a *Agent) SessionID() string {
	ps := a.current()
	return ps.ctx.Session(ps.ctx.LastBackend)
}

// SessionIDForModel returns the native session for a model's backend.
func (a *Agent) SessionIDForModel(model string) string {
	return a.current().ctx.Session(BackendKindForModel(model))
}

func (a *Agent) LastBackend() BackendKind {
	return a.current().ctx.LastBackend
}

// ProjectDir returns the current project directory
func (a *Agent) ProjectDir() string {
	return a.projectDir
}

// LastActivity returns when the agent was last active
func (a *Agent) LastActivity() time.Time {
	return a.current().ctx.LastActivity
}

// AddRecentMessage appends one user/assistant exchange to the conversation
// bridge so Hermes follow-up turns can reference the previous task result.
func (a *Agent) AddRecentMessage(userMsg, assistantMsg string) {
	ps := a.current()
	addToRecentMessages(ps, userMsg, assistantMsg)
}

// RecentMessagesSnapshot returns a copy of the recent conversation bridge state.
func (a *Agent) RecentMessagesSnapshot() []contextMessage {
	ps := a.current()
	return ps.ctx.RecentMessagesSnapshot()
}

// CreatedAt returns when the agent session was created
func (a *Agent) CreatedAt() time.Time {
	return a.current().ctx.CreatedAt
}

// IsActive returns true if the agent has been active within the last hour
func (a *Agent) IsActive() bool {
	return time.Since(a.current().ctx.LastActivity) < time.Hour
}

// ProjectCount returns the number of projects this agent has worked with
func (a *Agent) ProjectCount() int {
	return len(a.projects)
}

// languagePromptPrefixes are the per-language directives Alice prepends to
// every user message before sending to the CLI (see telegram.go). Stripping
// them before logging keeps Dashboard / Timeline titles readable instead of
// showing "請用繁體中文回應。…" on every entry.
var languagePromptPrefixes = []string{
	"請用繁體中文回應。\n\n",
	"Please respond in English. Do NOT use Chinese characters or Chinese formatting in your response.\n\n",
}

// stripLanguageDirective removes any leading language directive from a
// user-facing prompt copy. Used only when surfacing the prompt for display
// (DecisionLog / Dashboard / Telegram echoes); the CLI request still receives
// the directive.
func stripLanguageDirective(prompt string) string {
	for _, prefix := range languagePromptPrefixes {
		if strings.HasPrefix(prompt, prefix) {
			return prompt[len(prefix):]
		}
	}
	return prompt
}

// logDecision records a complete AI decision with full context
func (a *Agent) logDecision(userPrompt, agentResponse string, toolCalls []ToolExecution, startTime time.Time, resp *CLIResponse, err error, routingReason string, routingLatency int, deltaCost float64) {
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
	taskType := inferTaskType(userPrompt, toolCalls)

	// Extract files changed from tool calls
	filesChanged := extractFilesChanged(toolCalls)

	// Create execution outcome
	outcome := ExecutionOutcome{
		Success:      err == nil,
		TaskType:     taskType,
		FilesChanged: filesChanged,
		Summary:      generateSummary(userPrompt, agentResponse, taskType),
	}

	if err != nil {
		outcome.ErrorMessage = err.Error()
	}

	// Create context map
	context := map[string]interface{}{
		"turns":       0,
		"project_dir": a.projectDir,
		"model":       a.client.GetModel(),
		"tool_count":  len(toolCalls),
		"has_error":   err != nil,
	}

	if resp != nil {
		context["turns"] = resp.NumTurns
	}

	// Create token stats for this interaction
	tokenStats := TokenStats{
		Model: ExtractModelShortName(a.client.GetModel()), // NEW: 記錄使用的模型
	}
	if resp != nil {
		tokenStats.TotalInputTokens = int64(resp.Usage.InputTokens)
		tokenStats.TotalOutputTokens = int64(resp.Usage.OutputTokens)
		tokenStats.TotalCostUSD = deltaCost // Use delta cost instead of cumulative session cost
		tokenStats.APICallCount = 1
	}

	// Extract thinking content from CLI response
	thinkingContent := ""
	if resp != nil {
		thinkingContent = resp.ThinkingContent
	}

	// Create decision log
	decision := DecisionLog{
		Timestamp:       startTime,
		SessionID:       ps.ctx.Session(ps.ctx.LastBackend),
		ProjectPath:     a.projectDir,
		ChatID:          a.chatID,
		ThreadID:        a.threadID,
		UserPrompt:      stripLanguageDirective(userPrompt),
		AgentResponse:   agentResponse,
		ThinkingContent: thinkingContent,
		ToolCalls:       toolCalls,
		Context:         context,
		Outcome:         outcome,
		DurationMs:      int(duration.Milliseconds()),
		TokensUsed:      tokenStats,
		Source:          "telegram",
		Model:           ExtractModelShortName(a.lastUsedModel), // 使用的模型
		RoutingReason:   routingReason,                          // 路由原因
		RoutingLatency:  routingLatency,                         // 路由延遲 (ms)
	}

	// Log the decision
	globalDecisionLogger.LogDecision(decision)

	// Evaluate decision for auto-skill generation
	if globalSkillManager != nil {
		go globalSkillManager.EvaluateDecision(decision)
	}

	log.Printf("[agent] decision logged: task_type=%s, tools=%d, success=%v, duration=%dms",
		taskType, len(toolCalls), outcome.Success, int(duration.Milliseconds()))
}

// inferTaskType attempts to classify the type of task based on user input and tools used
func inferTaskType(userPrompt string, toolCalls []ToolExecution) string {
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
func extractFilesChanged(toolCalls []ToolExecution) []string {
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
func generateSummary(userPrompt, agentResponse, taskType string) string {
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
	if sm := security.Global(); sm != nil {
		// Don't log events here to avoid double-logging (the original detection point should log)
		filtered, _ := sm.DetectAndFilterPII(text, false, nil)
		return filtered
	}

	// Fallback to legacy pattern matching if security manager not available
	sensitivePatterns := []struct {
		pattern     string
		replacement string
	}{
		{`sk-[a-zA-Z0-9-_]{20,}`, "[API_KEY_MASKED]"},                        // API keys
		{`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, "[EMAIL_MASKED]"}, // Email addresses
		{`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`, "[CARD_MASKED]"},      // Credit card numbers
		{`password\s*[:=]\s*\S+`, "password: [MASKED]"},                      // Passwords
		{`token\s*[:=]\s*\S+`, "token: [MASKED]"},                            // Tokens
		{`secret\s*[:=]\s*\S+`, "secret: [MASKED]"},                          // Secrets
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
func (a *Agent) checkAndCreateCheckpoint(toolName string, toolInput map[string]interface{}, decisionLogID string) {
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
		decisionLogID,
	)

	if err != nil {
		log.Printf("Warning: Failed to create checkpoint before %s: %v", toolName, err)
		return
	}

	log.Printf("Checkpoint %s created successfully before %s operation", checkpoint.ID, toolName)

	// Broadcast checkpoint event via WebSocket if available
	if globalWebSocketHub != nil {
		checkpointEvent := map[string]interface{}{
			"event_type":      "checkpoint_created",
			"checkpoint_id":   checkpoint.ID,
			"decision_log_id": checkpoint.DecisionLogID,
			"tool_name":       toolName,
			"chat_id":         a.chatID,
			"project_dir":     a.projectDir,
			"description":     description,
			"trigger_type":    string(TriggerPreDanger),
			"dangerous_op":    dangerousOp.Description,
			"risk_level":      dangerousOp.RiskLevel.String(),
			"timestamp":       checkpoint.Timestamp,
		}

		// Use the correct broadcast method
		globalWebSocketHub.BroadcastEvent("checkpoint_created", checkpointEvent)
	}
}
