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

// ExecuteCoordinatedTask breaks down a complex task and assigns it to multiple agents
func (ac *AgentCoordinator) ExecuteCoordinatedTask(task string, baseAgent *Agent, onUpdate func(string, bool)) (string, error) {
	if !ac.enabled {
		// Fall back to single agent
		return baseAgent.Run(task, onUpdate)
	}

	// Create coordinated task
	coordinatedTask := ac.createCoordinatedTask(task)
	ac.activeTask = coordinatedTask

	if onUpdate != nil {
		onUpdate(fmt.Sprintf("🤖 協調執行任務: %s", task), false)
	}

	// Execute subtasks in order
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

	// Mark task as completed
	now := time.Now()
	coordinatedTask.Status = TaskStatusCompleted
	coordinatedTask.CompletedAt = &now

	// Combine results
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