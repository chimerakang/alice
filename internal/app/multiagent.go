package app

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// AgentType represents different types of specialized agents
type AgentType int

const (
	GeneralAgent AgentType = iota
	CodeReviewAgent
	TestingAgent
	DocumentationAgent
	DeploymentAgent
	DebugAgent
)

var agentTypeNames = map[AgentType]string{
	GeneralAgent:       "General",
	CodeReviewAgent:    "CodeReview",
	TestingAgent:       "Testing",
	DocumentationAgent: "Documentation",
	DeploymentAgent:    "Deployment",
	DebugAgent:         "Debug",
}

// String returns the string representation of AgentType
func (at AgentType) String() string {
	if name, ok := agentTypeNames[at]; ok {
		return name
	}
	return "Unknown"
}

// SpecializedAgent wraps an Agent with specialization capabilities
type SpecializedAgent struct {
	Type        AgentType
	Skills      []string
	Context     AgentContext
	*Agent      // Embed base agent
	lastUsed    time.Time
	taskCount   int
}

// AgentContext holds context information for specialized agents
type AgentContext struct {
	Capabilities []string              `json:"capabilities"`
	Preferences  map[string]interface{} `json:"preferences"`
	History      []TaskExecution       `json:"history"`
}

// TaskExecution represents a completed task by a specialized agent
type TaskExecution struct {
	TaskID      string    `json:"task_id"`
	Type        AgentType `json:"type"`
	Description string    `json:"description"`
	Success     bool      `json:"success"`
	Duration    int64     `json:"duration_ms"`
	Timestamp   time.Time `json:"timestamp"`
}

// CoordinatedTask represents a task that may require multiple agents
type CoordinatedTask struct {
	ID          string                    `json:"id"`
	Description string                    `json:"description"`
	SubTasks    []SubTask                 `json:"sub_tasks"`
	Assignments map[AgentType][]SubTask   `json:"assignments"`
	Status      TaskStatus                `json:"status"`
	CreatedAt   time.Time                 `json:"created_at"`
	CompletedAt *time.Time                `json:"completed_at,omitempty"`
}

// SubTask represents a portion of a coordinated task
type SubTask struct {
	ID           string     `json:"id"`
	Description  string     `json:"description"`
	AgentType    AgentType  `json:"agent_type"`
	Dependencies []string   `json:"dependencies"`
	Status       TaskStatus `json:"status"`
	Result       string     `json:"result,omitempty"`
}

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusAssigned   TaskStatus = "assigned"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
)

// AgentCoordinator manages multiple specialized agents
type AgentCoordinator struct {
	agents      map[AgentType]*SpecializedAgent
	activeTask  *CoordinatedTask
	sharedState *SharedContext
	mu          sync.RWMutex
	enabled     bool
}

// SharedContext holds shared information between agents
type SharedContext struct {
	Variables map[string]interface{} `json:"variables"`
	Files     map[string]FileInfo    `json:"files"`
	Messages  []InterAgentMessage    `json:"messages"`
	mu        sync.RWMutex
}

// FileInfo represents information about files in the shared context
type FileInfo struct {
	Path         string    `json:"path"`
	LastModified time.Time `json:"last_modified"`
	ModifiedBy   AgentType `json:"modified_by"`
	Purpose      string    `json:"purpose"`
}

// InterAgentMessage represents communication between agents
type InterAgentMessage struct {
	From    AgentType `json:"from"`
	To      AgentType `json:"to"`
	Message string    `json:"message"`
	Type    string    `json:"type"`
	Time    time.Time `json:"time"`
}

// Global agent coordinator instance
var globalAgentCoordinator = &AgentCoordinator{
	agents:      make(map[AgentType]*SpecializedAgent),
	sharedState: &SharedContext{
		Variables: make(map[string]interface{}),
		Files:     make(map[string]FileInfo),
		Messages:  make([]InterAgentMessage, 0),
	},
	enabled: false, // Disabled by default
}

// NewSpecializedAgent creates a new specialized agent
func NewSpecializedAgent(agentType AgentType, baseAgent *Agent) *SpecializedAgent {
	specialized := &SpecializedAgent{
		Type:      agentType,
		Skills:    getSkillsForAgentType(agentType),
		Context:   AgentContext{
			Capabilities: getCapabilitiesForAgentType(agentType),
			Preferences:  make(map[string]interface{}),
			History:      make([]TaskExecution, 0),
		},
		Agent:     baseAgent,
		lastUsed:  time.Now(),
		taskCount: 0,
	}

	return specialized
}

// getSkillsForAgentType returns the skills associated with each agent type
func getSkillsForAgentType(agentType AgentType) []string {
	switch agentType {
	case CodeReviewAgent:
		return []string{"code_analysis", "security_review", "performance_review", "best_practices"}
	case TestingAgent:
		return []string{"unit_testing", "integration_testing", "test_automation", "coverage_analysis"}
	case DocumentationAgent:
		return []string{"api_documentation", "readme_writing", "code_comments", "user_guides"}
	case DeploymentAgent:
		return []string{"ci_cd", "docker", "kubernetes", "cloud_deployment", "monitoring"}
	case DebugAgent:
		return []string{"error_analysis", "log_analysis", "performance_debugging", "troubleshooting"}
	case GeneralAgent:
		fallthrough
	default:
		return []string{"general_assistance", "code_generation", "file_operations"}
	}
}

// getCapabilitiesForAgentType returns capabilities for each agent type
func getCapabilitiesForAgentType(agentType AgentType) []string {
	switch agentType {
	case CodeReviewAgent:
		return []string{"can_analyze_code_quality", "can_identify_security_issues", "can_suggest_improvements"}
	case TestingAgent:
		return []string{"can_write_tests", "can_run_tests", "can_analyze_coverage"}
	case DocumentationAgent:
		return []string{"can_generate_docs", "can_update_readme", "can_write_comments"}
	case DeploymentAgent:
		return []string{"can_build_images", "can_deploy_apps", "can_manage_infrastructure"}
	case DebugAgent:
		return []string{"can_analyze_errors", "can_debug_issues", "can_trace_problems"}
	case GeneralAgent:
		fallthrough
	default:
		return []string{"can_read_files", "can_write_files", "can_execute_commands"}
	}
}

// IsEnabled returns whether multi-agent coordination is enabled
func (ac *AgentCoordinator) IsEnabled() bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.enabled
}

// SetEnabled enables or disables multi-agent coordination
func (ac *AgentCoordinator) SetEnabled(enabled bool) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.enabled = enabled

	if enabled {
		log.Println("🤖 Multi-agent coordination enabled")
	} else {
		log.Println("🤖 Multi-agent coordination disabled")
	}
}

// GetOrCreateAgent gets an existing specialized agent or creates a new one
func (ac *AgentCoordinator) GetOrCreateAgent(agentType AgentType, baseAgent *Agent) *SpecializedAgent {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if specialized, exists := ac.agents[agentType]; exists {
		specialized.lastUsed = time.Now()
		return specialized
	}

	// Create new specialized agent
	specialized := NewSpecializedAgent(agentType, baseAgent)
	ac.agents[agentType] = specialized

	log.Printf("🤖 Created new %s agent", agentType.String())
	return specialized
}

// RouteTask determines which agent should handle a task based on content analysis
func (ac *AgentCoordinator) RouteTask(task string) AgentType {
	if !ac.enabled {
		return GeneralAgent
	}

	taskLower := strings.ToLower(task)

	// Code review keywords
	if containsAny(taskLower, []string{"review", "check code", "analyze", "security", "performance", "quality"}) {
		return CodeReviewAgent
	}

	// Testing keywords
	if containsAny(taskLower, []string{"test", "unittest", "coverage", "spec", "验证", "測試"}) {
		return TestingAgent
	}

	// Documentation keywords
	if containsAny(taskLower, []string{"document", "readme", "comment", "docs", "api doc", "文檔", "文件"}) {
		return DocumentationAgent
	}

	// Deployment keywords
	if containsAny(taskLower, []string{"deploy", "docker", "kubernetes", "ci/cd", "build", "release", "部署"}) {
		return DeploymentAgent
	}

	// Debug keywords
	if containsAny(taskLower, []string{"debug", "error", "bug", "fix", "trace", "troubleshoot", "除錯", "錯誤"}) {
		return DebugAgent
	}

	return GeneralAgent
}

// ShouldUseMultiAgent determines if a task should be handled by multiple agents
func (ac *AgentCoordinator) ShouldUseMultiAgent(task string) bool {
	if !ac.enabled {
		return false
	}

	taskLower := strings.ToLower(task)

	// Complex tasks that benefit from multiple agents
	multiAgentKeywords := []string{
		"implement", "create feature", "build system", "full stack", "end to end",
		"complete solution", "整個系統", "完整功能", "全棧", "端到端",
	}

	return containsAny(taskLower, multiAgentKeywords)
}

// ExecuteCoordinatedTask breaks down a complex task and assigns it to multiple agents.
// Uses parallel execution when the orchestrator is available and multiple subtasks exist.
func (ac *AgentCoordinator) ExecuteCoordinatedTask(task string, baseAgent *Agent, onUpdate func(string, bool)) (string, error) {
	if !ac.enabled {
		// Fall back to single agent
		return baseAgent.Run(task, onUpdate)
	}

	// Create coordinated task
	coordinatedTask := ac.createCoordinatedTask(task)
	ac.activeTask = coordinatedTask

	// Collect all subtasks
	var allSubTasks []SubTask
	for _, subTasks := range coordinatedTask.Assignments {
		allSubTasks = append(allSubTasks, subTasks...)
	}

	// If orchestrator is available and multiple subtasks, use parallel execution
	if globalOrchestrator != nil && len(allSubTasks) > 1 {
		if onUpdate != nil {
			onUpdate(fmt.Sprintf("🚀 並行執行 %d 個子任務...", len(allSubTasks)), false)
		}

		// Convert SubTasks to SubAgentTasks
		tasks := make([]SubAgentTask, len(allSubTasks))
		for i, st := range allSubTasks {
			tasks[i] = SubAgentTask{
				ID:          st.ID,
				Description: st.Description,
				DependsOn:   st.Dependencies,
			}
		}

		execution := globalOrchestrator.ExecuteParallel(
			tasks,
			baseAgent.client,
			baseAgent.projectDir,
			baseAgent.chatID,
			baseAgent.threadID,
			func(taskID, status, result string) {
				if onUpdate != nil {
					onUpdate(fmt.Sprintf("🤖 [%s] %s", taskID, status), true)
				}
			},
			defaultParallelTimeout,
		)

		// Mark coordinated task done
		now := time.Now()
		coordinatedTask.Status = TaskStatusCompleted
		coordinatedTask.CompletedAt = &now

		return FormatParallelResults(execution), nil
	}

	// Fallback: sequential execution
	if onUpdate != nil {
		onUpdate(fmt.Sprintf("🤖 協調執行任務: %s", task), false)
	}

	var results []string
	for agentType, subTasks := range coordinatedTask.Assignments {
		specialized := ac.GetOrCreateAgent(agentType, baseAgent)

		for _, subTask := range subTasks {
			if onUpdate != nil {
				onUpdate(fmt.Sprintf("🤖 %s 代理處理: %s", agentType.String(), subTask.Description), true)
			}

			result, err := specialized.ExecuteSubTask(subTask, onUpdate)
			if err != nil {
				log.Printf("❌ %s agent failed subtask: %v", agentType.String(), err)
				subTask.Status = TaskStatusFailed
			} else {
				subTask.Status = TaskStatusCompleted
				subTask.Result = result
				results = append(results, result)
			}
		}
	}

	now := time.Now()
	coordinatedTask.Status = TaskStatusCompleted
	coordinatedTask.CompletedAt = &now

	finalResult := strings.Join(results, "\n\n")
	return finalResult, nil
}

// createCoordinatedTask analyzes a task and creates subtasks for different agents
func (ac *AgentCoordinator) createCoordinatedTask(task string) *CoordinatedTask {
	taskID := fmt.Sprintf("task_%d", time.Now().Unix())

	coordinatedTask := &CoordinatedTask{
		ID:          taskID,
		Description: task,
		Status:      TaskStatusPending,
		CreatedAt:   time.Now(),
		Assignments: make(map[AgentType][]SubTask),
	}

	// Simple task breakdown logic (can be enhanced with AI analysis)
	subTasks := ac.analyzeAndBreakdownTask(task)
	coordinatedTask.SubTasks = subTasks

	// Group subtasks by agent type
	for _, subTask := range subTasks {
		agentType := subTask.AgentType
		coordinatedTask.Assignments[agentType] = append(coordinatedTask.Assignments[agentType], subTask)
	}

	return coordinatedTask
}

// analyzeAndBreakdownTask breaks down a complex task into subtasks
func (ac *AgentCoordinator) analyzeAndBreakdownTask(task string) []SubTask {
	var subTasks []SubTask
	taskLower := strings.ToLower(task)

	// Create subtasks based on task content
	if strings.Contains(taskLower, "implement") || strings.Contains(taskLower, "create") {
		// Implementation task - needs code review and testing
		subTasks = append(subTasks, SubTask{
			ID:          "design_" + fmt.Sprintf("%d", len(subTasks)+1),
			Description: "設計和規劃實作方案",
			AgentType:   CodeReviewAgent,
			Status:      TaskStatusPending,
		})

		subTasks = append(subTasks, SubTask{
			ID:          "implement_" + fmt.Sprintf("%d", len(subTasks)+1),
			Description: task, // Original task for general agent
			AgentType:   GeneralAgent,
			Status:      TaskStatusPending,
		})

		subTasks = append(subTasks, SubTask{
			ID:          "test_" + fmt.Sprintf("%d", len(subTasks)+1),
			Description: "為實作的功能編寫測試",
			AgentType:   TestingAgent,
			Status:      TaskStatusPending,
		})

		subTasks = append(subTasks, SubTask{
			ID:          "document_" + fmt.Sprintf("%d", len(subTasks)+1),
			Description: "更新相關文件",
			AgentType:   DocumentationAgent,
			Status:      TaskStatusPending,
		})
	} else {
		// Simple task - assign to most appropriate agent
		agentType := ac.RouteTask(task)
		subTasks = append(subTasks, SubTask{
			ID:          "main_" + fmt.Sprintf("%d", len(subTasks)+1),
			Description: task,
			AgentType:   agentType,
			Status:      TaskStatusPending,
		})
	}

	return subTasks
}

// ExecuteSubTask executes a subtask with the specialized agent
func (sa *SpecializedAgent) ExecuteSubTask(subTask SubTask, onUpdate func(string, bool)) (string, error) {
	sa.lastUsed = time.Now()
	sa.taskCount++

	// Create specialized prompt based on agent type
	specializedPrompt := sa.createSpecializedPrompt(subTask.Description)

	// Execute using the base agent
	result, err := sa.Agent.Run(specializedPrompt, onUpdate)

	// Record task execution
	execution := TaskExecution{
		TaskID:      subTask.ID,
		Type:        sa.Type,
		Description: subTask.Description,
		Success:     err == nil,
		Duration:    0, // Will be calculated
		Timestamp:   time.Now(),
	}
	sa.Context.History = append(sa.Context.History, execution)

	return result, err
}

// createSpecializedPrompt creates a prompt tailored to the agent's specialization
func (sa *SpecializedAgent) createSpecializedPrompt(originalPrompt string) string {
	switch sa.Type {
	case CodeReviewAgent:
		return fmt.Sprintf("作為程式碼審查專家，請專注於程式碼品質、安全性和最佳實務。%s", originalPrompt)
	case TestingAgent:
		return fmt.Sprintf("作為測試專家，請專注於測試策略、測試覆蓋率和品質保證。%s", originalPrompt)
	case DocumentationAgent:
		return fmt.Sprintf("作為技術文件專家，請專注於清晰的文件撰寫和使用者體驗。%s", originalPrompt)
	case DeploymentAgent:
		return fmt.Sprintf("作為部署和 DevOps 專家，請專注於可靠的部署流程和基礎設施。%s", originalPrompt)
	case DebugAgent:
		return fmt.Sprintf("作為除錯專家，請專注於錯誤分析、根因識別和解決方案。%s", originalPrompt)
	default:
		return originalPrompt
	}
}

// GetAgentStats returns statistics about all specialized agents
func (ac *AgentCoordinator) GetAgentStats() map[string]interface{} {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	stats := make(map[string]interface{})
	agentStats := make(map[string]interface{})

	for agentType, agent := range ac.agents {
		agentInfo := map[string]interface{}{
			"type":        agentType.String(),
			"skills":      agent.Skills,
			"task_count":  agent.taskCount,
			"last_used":   agent.lastUsed,
			"capabilities": agent.Context.Capabilities,
		}
		agentStats[agentType.String()] = agentInfo
	}

	stats["agents"] = agentStats
	stats["enabled"] = ac.enabled
	stats["total_agents"] = len(ac.agents)

	if ac.activeTask != nil {
		stats["active_task"] = map[string]interface{}{
			"id":          ac.activeTask.ID,
			"description": ac.activeTask.Description,
			"status":      ac.activeTask.Status,
			"sub_tasks":   len(ac.activeTask.SubTasks),
		}
	}

	return stats
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, substrings []string) bool {
	for _, substring := range substrings {
		if strings.Contains(s, substring) {
			return true
		}
	}
	return false
}

// ==================== SubAgent Orchestrator (Parallel Execution) ====================

const (
	defaultMaxConcurrent = 3
	defaultParallelTimeout = 10 * time.Minute
)

// SubAgentOrchestrator manages parallel execution of multiple agent tasks
type SubAgentOrchestrator struct {
	maxConcurrent int
	semaphore     chan struct{}
	mu            sync.Mutex
}

// SubAgentTask represents a single task for parallel execution
type SubAgentTask struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on"` // task IDs this depends on
}

// SubAgentResult holds the result of a single parallel task
type SubAgentResult struct {
	TaskID      string        `json:"task_id"`
	Description string        `json:"description"`
	Success     bool          `json:"success"`
	Result      string        `json:"result"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration_ms"`
}

// ParallelExecution represents a complete parallel execution session
type ParallelExecution struct {
	ID          string            `json:"id"`
	ChatID      int64             `json:"chat_id"`
	ThreadID    int               `json:"thread_id"`
	Tasks       []SubAgentTask    `json:"tasks"`
	Results     []SubAgentResult  `json:"results"`
	Status      TaskStatus        `json:"status"`
	TotalTime   time.Duration     `json:"total_time_ms"`
	CreatedAt   time.Time         `json:"created_at"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
}

// Global orchestrator instance
var globalOrchestrator *SubAgentOrchestrator

// InitOrchestrator initializes the global SubAgentOrchestrator
func InitOrchestrator(maxConcurrent int) {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrent
	}
	globalOrchestrator = &SubAgentOrchestrator{
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
	}
	log.Printf("[orchestrator] initialized with max_concurrent=%d", maxConcurrent)
}

// ExecuteParallel runs multiple tasks in parallel using separate Agent instances
func (o *SubAgentOrchestrator) ExecuteParallel(
	tasks []SubAgentTask,
	client Client,
	projectDir string,
	chatID int64,
	threadID int,
	onProgress func(taskID string, status string, result string),
	timeout time.Duration,
) *ParallelExecution {
	if timeout <= 0 {
		timeout = defaultParallelTimeout
	}

	execID := fmt.Sprintf("parallel_%d_%d", chatID, time.Now().UnixMilli())
	execution := &ParallelExecution{
		ID:        execID,
		ChatID:    chatID,
		ThreadID:  threadID,
		Tasks:     tasks,
		Results:   make([]SubAgentResult, len(tasks)),
		Status:    TaskStatusInProgress,
		CreatedAt: time.Now(),
	}

	startTime := time.Now()

	// Build dependency graph
	taskIndex := make(map[string]int) // task ID → index
	for i, task := range tasks {
		taskIndex[task.ID] = i
	}

	// Identify independent tasks (no dependencies)
	// and dependent tasks (need to wait)
	var independent []int
	dependents := make(map[int][]string) // index → list of dependency task IDs

	for i, task := range tasks {
		if len(task.DependsOn) == 0 {
			independent = append(independent, i)
		} else {
			dependents[i] = task.DependsOn
		}
	}

	// Execute independent tasks in parallel
	var wg sync.WaitGroup
	completed := make(map[string]bool)
	var completedMu sync.Mutex

	// Channel to signal task completion
	done := make(chan int, len(tasks))

	// Execute a single task
	executeTask := func(idx int) {
		defer wg.Done()

		task := tasks[idx]

		// Acquire semaphore
		o.semaphore <- struct{}{}
		defer func() { <-o.semaphore }()

		if onProgress != nil {
			onProgress(task.ID, "started", "")
		}

		taskStart := time.Now()

		// Create independent agent for this task
		agent := NewAgent(client, projectDir, chatID, threadID)
		result, err := agent.Run(task.Description, nil)

		subResult := SubAgentResult{
			TaskID:      task.ID,
			Description: task.Description,
			Success:     err == nil,
			Result:      result,
			Duration:    time.Since(taskStart),
		}
		if err != nil {
			subResult.Error = err.Error()
		}

		execution.Results[idx] = subResult

		completedMu.Lock()
		completed[task.ID] = true
		completedMu.Unlock()

		status := "completed"
		if err != nil {
			status = "failed"
		}
		if onProgress != nil {
			onProgress(task.ID, status, truncateResult(result, 200))
		}

		done <- idx

		log.Printf("[orchestrator] task %s %s (%.1fs)", task.ID, status, subResult.Duration.Seconds())
	}

	// Start independent tasks
	for _, idx := range independent {
		wg.Add(1)
		go executeTask(idx)
	}

	// Monitor and start dependent tasks when ready
	if len(dependents) > 0 {
		go func() {
			pending := make(map[int]bool)
			for idx := range dependents {
				pending[idx] = true
			}

			for range done {
				// Check if any pending task's dependencies are all met
				for idx := range pending {
					deps := dependents[idx]
					allMet := true
					completedMu.Lock()
					for _, dep := range deps {
						if !completed[dep] {
							allMet = false
							break
						}
					}
					completedMu.Unlock()

					if allMet {
						delete(pending, idx)
						wg.Add(1)
						go executeTask(idx)
					}
				}
			}
		}()
	}

	// Wait with timeout
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
		close(done)
	}()

	select {
	case <-waitDone:
		execution.Status = TaskStatusCompleted
	case <-time.After(timeout):
		execution.Status = TaskStatusFailed
		log.Printf("[orchestrator] parallel execution %s timed out after %v", execID, timeout)
	}

	now := time.Now()
	execution.CompletedAt = &now
	execution.TotalTime = time.Since(startTime)

	// Persist to database
	if globalStorage != nil {
		go persistParallelExecution(execution)
	}

	log.Printf("[orchestrator] execution %s finished: %d tasks, %s, %.1fs total",
		execID, len(tasks), execution.Status, execution.TotalTime.Seconds())

	return execution
}

// FormatParallelResults formats execution results for display
func FormatParallelResults(exec *ParallelExecution) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 **並行執行結果** (%s)\n", exec.Status))
	sb.WriteString(fmt.Sprintf("⏱ 總耗時: %.1f 秒\n\n", exec.TotalTime.Seconds()))

	for i, result := range exec.Results {
		icon := "✅"
		if !result.Success {
			icon = "❌"
		}
		sb.WriteString(fmt.Sprintf("%s **任務 %d**: %s\n", icon, i+1, result.Description))
		sb.WriteString(fmt.Sprintf("   ⏱ %.1fs\n", result.Duration.Seconds()))

		if result.Success {
			// Show truncated result
			text := truncateResult(result.Result, 300)
			if text != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", text))
			}
		} else {
			sb.WriteString(fmt.Sprintf("   ❌ %s\n", result.Error))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ParseParallelTasks parses user input into parallel tasks
// Format: numbered list "1. task one\n2. task two\n3. task three"
func ParseParallelTasks(input string) []SubAgentTask {
	var tasks []SubAgentTask
	lines := strings.Split(input, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Strip number prefix like "1. ", "2. ", "- ", etc.
		desc := line
		for i, c := range line {
			if c == '.' || c == ')' {
				if i > 0 && i < 4 {
					desc = strings.TrimSpace(line[i+1:])
					break
				}
			}
			if c == '-' && i == 0 {
				desc = strings.TrimSpace(line[1:])
				break
			}
		}

		if desc == "" {
			continue
		}

		taskID := fmt.Sprintf("task_%d", len(tasks)+1)
		tasks = append(tasks, SubAgentTask{
			ID:          taskID,
			Description: desc,
		})
	}

	return tasks
}

func truncateResult(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func persistParallelExecution(exec *ParallelExecution) {
	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return
	}
	if err := sqliteStorage.InsertParallelExecution(exec); err != nil {
		log.Printf("[orchestrator] failed to persist execution: %v", err)
	}
}