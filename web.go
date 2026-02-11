package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AgentInfo represents agent information for the API
type AgentInfo struct {
	ChatID       int64     `json:"chat_id"`
	ThreadID     int       `json:"thread_id"`
	ProjectDir   string    `json:"project_dir"`
	SessionID    string    `json:"session_id"`
	IsActive     bool      `json:"is_active"`
	LastActivity time.Time `json:"last_activity"`
	CreatedAt    time.Time `json:"created_at"`
	ProjectCount int       `json:"project_count"`
	Stats        TokenStats `json:"stats"`
}

// DetailedStats represents enhanced statistics
type DetailedStats struct {
	ActiveSessions    int                   `json:"active_sessions"`
	TotalSessions     int                   `json:"total_sessions"`
	ToolsExecuted     int64                 `json:"tools_executed"`
	TotalProjects     int                   `json:"total_projects"`
	SuccessRate       float64               `json:"success_rate"`
	UptimeSeconds     int64                 `json:"uptime_seconds"`
	Timestamp         time.Time             `json:"timestamp"`
	RecentAgents      []AgentInfo           `json:"recent_agents"`
	TotalTokensUsed   int64                 `json:"total_tokens_used"`
	TotalCostUSD      float64               `json:"total_cost_usd"`
}

// WebInterface manages the HTTP server and web dashboard functionality
type WebInterface struct {
	bot       *TelegramBot
	staticDir string
	server    *http.Server
	mu        sync.RWMutex
}

// NewWebInterface creates a new web interface instance
func NewWebInterface(bot *TelegramBot, port, staticDir string) *WebInterface {
	wi := &WebInterface{
		bot:       bot,
		staticDir: staticDir,
	}

	// Create HTTP server
	wi.server = &http.Server{
		Addr:              ":" + port,
		Handler:           wi.CreateRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       15 * time.Second,
	}

	return wi
}

// CreateRouter sets up the HTTP routes and handlers
func (wi *WebInterface) CreateRouter() http.Handler {
	mux := http.NewServeMux()

	// Serve dashboard at root
	mux.HandleFunc("/", wi.handleDashboard)

	// API endpoints
	mux.HandleFunc("/api/health", wi.handleHealth)
	mux.HandleFunc("/api/stats", wi.handleStats)
	mux.HandleFunc("/api/agents", wi.handleAgentsProto)
	mux.HandleFunc("/api/agents/", wi.handleAgentDetailProto)
	mux.HandleFunc("/api/tools/recent", wi.handleRecentToolsProto)
	mux.HandleFunc("/api/tools/executions", wi.handleToolExecutionsProto)
	mux.HandleFunc("/api/decisions", wi.handleDecisionsProto)
	mux.HandleFunc("/api/decisions/recent", wi.handleRecentDecisionsProto)
	mux.HandleFunc("/api/decisions/search", wi.handleSearchDecisions)
	mux.HandleFunc("/api/decisions/export", wi.handleExportDecisions)
	mux.HandleFunc("/api/multiagent/status", wi.handleMultiAgentStatusProto)
	mux.HandleFunc("/api/multiagent/stats", wi.handleMultiAgentStatsProto)
	mux.HandleFunc("/api/multiagent/agents", wi.handleMultiAgentAgentsProto)

	// Performance Monitoring APIs
	mux.HandleFunc("/api/performance/analytics", wi.handlePerformanceAnalyticsProto)
	mux.HandleFunc("/api/performance/metrics", wi.handlePerformanceMetricsProto)
	mux.HandleFunc("/api/performance/trends", wi.handlePerformanceTrendsProto)
	mux.HandleFunc("/api/performance/recommendations", wi.handlePerformanceRecommendationsProto)
	mux.HandleFunc("/api/performance/export", wi.handlePerformanceExportProto)

	// Security APIs
	mux.HandleFunc("/api/security/events", wi.handleSecurityEventsProto)
	mux.HandleFunc("/api/security/stats", wi.handleSecurityStatsProto)
	mux.HandleFunc("/api/security/audit", wi.handleSecurityAuditProto)

	// Prometheus metrics endpoint
	mux.HandleFunc("/metrics", wi.handlePrometheusMetrics)

	// 應用安全中間件
	var handler http.Handler = mux

	if globalSecurityManager != nil {
		// 依序應用中間件 (注意順序)
		handler = globalSecurityManager.SecurityHeadersMiddleware(handler)
		handler = globalSecurityManager.IPFilterMiddleware(handler)
		handler = globalSecurityManager.RateLimitMiddleware(handler)
	}

	return handler
}

// Start begins the HTTP server
func (wi *WebInterface) Start() error {
	log.Printf("🌐 Web interface starting on %s", wi.server.Addr)

	// Create static directory if it doesn't exist
	if wi.staticDir != "" {
		if err := os.MkdirAll(wi.staticDir, 0755); err != nil {
			log.Printf("⚠️  Could not create static directory: %v", err)
		}
	}

	if err := wi.server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

// Shutdown gracefully stops the HTTP server
func (wi *WebInterface) Shutdown(ctx context.Context) error {
	log.Println("🛑 Shutting down web interface...")
	return wi.server.Shutdown(ctx)
}

// handleDashboard serves the main dashboard page
func (wi *WebInterface) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Try to serve dashboard.html from static directory
	dashboardPath := filepath.Join(wi.staticDir, "dashboard.html")
	if _, err := os.Stat(dashboardPath); err == nil {
		http.ServeFile(w, r, dashboardPath)
		return
	}

	// Fall back to serving dashboard.html from current directory
	if _, err := os.Stat("dashboard.html"); err == nil {
		http.ServeFile(w, r, "dashboard.html")
		return
	}

	// If no dashboard file found, serve a simple status page
	wi.handleSimpleDashboard(w, r)
}

// handleSimpleDashboard serves a basic status page when dashboard.html is not available
func (wi *WebInterface) handleSimpleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html>
<head>
    <title>Alice AI Agent - Web Interface</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; margin: 40px; }
        .status { background: #f0f9ff; padding: 20px; border-radius: 8px; margin: 20px 0; }
        .api-link { color: #2563eb; text-decoration: none; }
        .api-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <h1>🤖 Alice AI Agent Dashboard</h1>
    <div class="status">
        <h2>✅ Web Interface Active</h2>
        <p>The Alice Telegram bot web interface is running successfully.</p>
    </div>

    <h3>📊 Available APIs</h3>
    <ul>
        <li><a href="/api/health" class="api-link">Health Check</a></li>
        <li><a href="/api/stats" class="api-link">Statistics</a></li>
    </ul>

    <p><em>To use the full dashboard, place dashboard.html in the static directory or current directory.</em></p>
</body>
</html>`

	fmt.Fprint(w, html)
}

// handleHealth returns the health status of the service
func (wi *WebInterface) handleHealth(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		status := map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now(),
			"telegram":  "active",
		}

		json.NewEncoder(w).Encode(status)
	})(w, r)
}

// handleStats returns basic statistics about the agent
func (wi *WebInterface) handleStats(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		stats := wi.getBasicStats()
		json.NewEncoder(w).Encode(stats)
	})(w, r)
}

// handleAgents returns a list of all active agents
func (wi *WebInterface) handleAgents(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		agents := wi.getAllAgentsInfo()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agents": agents,
			"total":  len(agents),
		})
	})(w, r)
}

// handleAgentDetail returns detailed information about a specific agent
func (wi *WebInterface) handleAgentDetail(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Parse chat_id and thread_id from URL path or query
		chatIDStr := r.URL.Query().Get("chat_id")
		threadIDStr := r.URL.Query().Get("thread_id")

		if chatIDStr == "" {
			// Try to extract from URL path like /api/agents/123456
			urlParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/agents/"), "/")
			if len(urlParts) > 0 && urlParts[0] != "" {
				chatIDStr = urlParts[0]
			}
		}

		if chatIDStr == "" {
			http.Error(w, "Missing chat_id parameter", http.StatusBadRequest)
			return
		}

		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid chat_id", http.StatusBadRequest)
			return
		}

		threadID := 0
		if threadIDStr != "" {
			threadID, _ = strconv.Atoi(threadIDStr)
		}

		agent := wi.getAgentInfo(chatID, threadID)
		if agent == nil {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(agent)
	})(w, r)
}

// getAllAgentsInfo gets information about all agents
func (wi *WebInterface) getAllAgentsInfo() []AgentInfo {
	wi.mu.RLock()
	defer wi.mu.RUnlock()

	if wi.bot == nil {
		return []AgentInfo{}
	}

	agents := wi.bot.GetAgentsSafely()
	var agentInfos []AgentInfo

	for key, agent := range agents {
		if agent == nil {
			continue
		}

		info := AgentInfo{
			ChatID:       key.chatID,
			ThreadID:     key.threadID,
			ProjectDir:   agent.ProjectDir(),
			SessionID:    agent.SessionID(),
			IsActive:     agent.IsActive(),
			LastActivity: agent.LastActivity(),
			CreatedAt:    agent.CreatedAt(),
			ProjectCount: agent.ProjectCount(),
			Stats:        agent.Stats(),
		}
		agentInfos = append(agentInfos, info)
	}

	return agentInfos
}

// getAgentInfo gets information about a specific agent
func (wi *WebInterface) getAgentInfo(chatID int64, threadID int) *AgentInfo {
	wi.mu.RLock()
	defer wi.mu.RUnlock()

	if wi.bot == nil {
		return nil
	}

	agents := wi.bot.GetAgentsSafely()
	key := chatKey{chatID: chatID, threadID: threadID}

	agent, exists := agents[key]
	if !exists || agent == nil {
		return nil
	}

	return &AgentInfo{
		ChatID:       chatID,
		ThreadID:     threadID,
		ProjectDir:   agent.ProjectDir(),
		SessionID:    agent.SessionID(),
		IsActive:     agent.IsActive(),
		LastActivity: agent.LastActivity(),
		CreatedAt:    agent.CreatedAt(),
		ProjectCount: agent.ProjectCount(),
		Stats:        agent.Stats(),
	}
}

// getBasicStats collects basic statistics from the Telegram bot
func (wi *WebInterface) getBasicStats() map[string]interface{} {
	stats := wi.getDetailedStats()

	// Convert DetailedStats to map[string]interface{}
	return map[string]interface{}{
		"active_sessions":   stats.ActiveSessions,
		"tools_executed":    stats.ToolsExecuted,
		"projects":         stats.TotalProjects,
		"success_rate":     stats.SuccessRate,
		"uptime_seconds":   stats.UptimeSeconds,
		"timestamp":        stats.Timestamp,
		"total_sessions":   stats.TotalSessions,
		"total_tokens_used": stats.TotalTokensUsed,
		"total_cost_usd":   stats.TotalCostUSD,
	}
}

// getDetailedStats collects comprehensive statistics
func (wi *WebInterface) getDetailedStats() DetailedStats {
	wi.mu.RLock()
	defer wi.mu.RUnlock()

	if wi.bot == nil {
		return DetailedStats{
			Timestamp: time.Now(),
		}
	}

	agents := wi.bot.GetAgentsSafely()

	activeSessions := 0
	totalTokensUsed := int64(0)
	totalCostUSD := 0.0
	totalProjects := 0
	var recentAgents []AgentInfo

	for key, agent := range agents {
		if agent == nil {
			continue
		}

		if agent.IsActive() {
			activeSessions++
		}

		stats := agent.Stats()
		totalTokensUsed += stats.TotalInputTokens + stats.TotalOutputTokens
		totalCostUSD += stats.TotalCostUSD
		totalProjects += agent.ProjectCount()

		// Add to recent agents (limit to 5 most recent)
		if len(recentAgents) < 5 {
			info := AgentInfo{
				ChatID:       key.chatID,
				ThreadID:     key.threadID,
				ProjectDir:   agent.ProjectDir(),
				SessionID:    agent.SessionID(),
				IsActive:     agent.IsActive(),
				LastActivity: agent.LastActivity(),
				CreatedAt:    agent.CreatedAt(),
				ProjectCount: agent.ProjectCount(),
				Stats:        stats,
			}
			recentAgents = append(recentAgents, info)
		}
	}

	// Get tool execution count from global logger
	toolExecutionCount := int64(globalToolLogger.GetExecutionCount())

	return DetailedStats{
		ActiveSessions:  activeSessions,
		TotalSessions:   len(agents),
		ToolsExecuted:   toolExecutionCount,
		TotalProjects:   totalProjects,
		SuccessRate:     100.0, // Will calculate based on tool execution success/failure
		UptimeSeconds:   time.Now().Unix(),
		Timestamp:       time.Now(),
		RecentAgents:    recentAgents,
		TotalTokensUsed: totalTokensUsed,
		TotalCostUSD:    totalCostUSD,
	}
}

// handleRecentTools returns recent tool executions
func (wi *WebInterface) handleRecentTools(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Parse limit parameter
		limitStr := r.URL.Query().Get("limit")
		limit := 20 // default limit
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		executions := globalToolLogger.GetRecentExecutions(limit)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"executions": executions,
			"total":      len(executions),
			"timestamp":  time.Now(),
		})
	})(w, r)
}

// handleToolExecutions returns detailed tool execution statistics
func (wi *WebInterface) handleToolExecutions(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		executions := globalToolLogger.GetRecentExecutions(0) // Get all

		// Calculate statistics
		toolCounts := make(map[string]int)
		statusCounts := map[string]int{
			"running": 0,
			"success": 0,
			"error":   0,
		}

		for _, exec := range executions {
			toolCounts[exec.ToolName]++
			statusCounts[exec.Status]++
		}

		// Calculate success rate
		total := statusCounts["success"] + statusCounts["error"]
		successRate := 100.0
		if total > 0 {
			successRate = float64(statusCounts["success"]) / float64(total) * 100
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_executions": len(executions),
			"tool_counts":      toolCounts,
			"status_counts":    statusCounts,
			"success_rate":     successRate,
			"recent_executions": globalToolLogger.GetRecentExecutions(10), // Last 10
			"timestamp":        time.Now(),
		})
	})(w, r)
}

// handleDecisions returns comprehensive decision statistics
func (wi *WebInterface) handleDecisions(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		decisions := globalDecisionLogger.GetRecentDecisions(0) // Get all

		// Calculate statistics
		taskTypeCounts := make(map[string]int)
		outcomeStats := map[string]int{
			"success": 0,
			"error":   0,
		}
		totalDuration := int64(0)

		for _, decision := range decisions {
			taskTypeCounts[decision.Outcome.TaskType]++
			if decision.Outcome.Success {
				outcomeStats["success"]++
			} else {
				outcomeStats["error"]++
			}
			totalDuration += int64(decision.DurationMs)
		}

		avgDuration := int64(0)
		if len(decisions) > 0 {
			avgDuration = totalDuration / int64(len(decisions))
		}

		// Calculate success rate
		total := outcomeStats["success"] + outcomeStats["error"]
		successRate := 100.0
		if total > 0 {
			successRate = float64(outcomeStats["success"]) / float64(total) * 100
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_decisions":     len(decisions),
			"task_type_counts":    taskTypeCounts,
			"outcome_stats":       outcomeStats,
			"success_rate":        successRate,
			"avg_duration_ms":     avgDuration,
			"recent_decisions":    globalDecisionLogger.GetRecentDecisions(5),
			"transparency_enabled": globalDecisionLogger.IsEnabled(),
			"timestamp":           time.Now(),
		})
	})(w, r)
}

// handleRecentDecisions returns recent decision logs
func (wi *WebInterface) handleRecentDecisions(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Parse limit parameter
		limitStr := r.URL.Query().Get("limit")
		limit := 10 // default limit
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		decisions := globalDecisionLogger.GetRecentDecisions(limit)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decisions": decisions,
			"total":     len(decisions),
			"limit":     limit,
			"timestamp": time.Now(),
		})
	})(w, r)
}

// handleSearchDecisions searches decisions based on criteria
func (wi *WebInterface) handleSearchDecisions(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Parse search parameters
		projectPath := r.URL.Query().Get("project_path")
		taskType := r.URL.Query().Get("task_type")
		successOnlyStr := r.URL.Query().Get("success_only")

		successOnly := false
		if successOnlyStr == "true" {
			successOnly = true
		}

		decisions := globalDecisionLogger.SearchDecisions(projectPath, taskType, successOnly)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decisions":    decisions,
			"total":        len(decisions),
			"filters": map[string]interface{}{
				"project_path": projectPath,
				"task_type":    taskType,
				"success_only": successOnly,
			},
			"timestamp": time.Now(),
		})
	})(w, r)
}

// handleExportDecisions exports decisions in various formats
func (wi *WebInterface) handleExportDecisions(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "json"
		}

		decisions := globalDecisionLogger.GetRecentDecisions(0) // Get all

		switch format {
		case "csv":
			wi.exportDecisionsCSV(w, decisions)
		case "json":
			fallthrough
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", `attachment; filename="alice_decisions.json"`)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"exported_at": time.Now(),
				"total":       len(decisions),
				"decisions":   decisions,
			})
		}
	})(w, r)
}

// exportDecisionsCSV exports decisions in CSV format
func (wi *WebInterface) exportDecisionsCSV(w http.ResponseWriter, decisions []DecisionLog) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="alice_decisions.csv"`)

	// CSV header
	fmt.Fprintln(w, "timestamp,session_id,chat_id,project_path,task_type,success,duration_ms,user_prompt,tools_count,files_changed")

	// CSV data
	for _, decision := range decisions {
		// Escape and truncate user prompt for CSV
		userPrompt := strings.ReplaceAll(decision.UserPrompt, "\"", "\"\"")
		if len(userPrompt) > 100 {
			userPrompt = userPrompt[:100] + "..."
		}

		filesChanged := strings.Join(decision.Outcome.FilesChanged, ";")

		fmt.Fprintf(w, "\"%s\",\"%s\",%d,\"%s\",\"%s\",%t,%d,\"%s\",%d,\"%s\"\n",
			decision.Timestamp.Format(time.RFC3339),
			decision.SessionID,
			decision.ChatID,
			decision.ProjectPath,
			decision.Outcome.TaskType,
			decision.Outcome.Success,
			decision.DurationMs,
			userPrompt,
			len(decision.ToolCalls),
			filesChanged,
		)
	}
}

// handleWithRecovery wraps handlers with panic recovery
func (wi *WebInterface) handleWithRecovery(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[web] panic in %s: %v", r.URL.Path, err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		handler(w, r)
	}
}

// handleMultiAgentStatus returns multi-agent system status
func (wi *WebInterface) handleMultiAgentStatus(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		stats := globalAgentCoordinator.GetAgentStats()
		stats["enabled"] = globalAgentCoordinator.IsEnabled()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"multi_agent_status": stats,
			"timestamp":          time.Now(),
		})
	})(w, r)
}

// handleMultiAgentStats returns detailed multi-agent statistics
func (wi *WebInterface) handleMultiAgentStats(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		stats := globalAgentCoordinator.GetAgentStats()

		// Calculate additional statistics
		agents := stats["agents"].(map[string]interface{})
		totalTasks := 0
		activeAgents := 0

		for _, agentInfo := range agents {
			info := agentInfo.(map[string]interface{})
			if taskCount, ok := info["task_count"].(int); ok {
				totalTasks += taskCount
			}
			if lastUsed, ok := info["last_used"].(time.Time); ok {
				if time.Since(lastUsed) < time.Hour {
					activeAgents++
				}
			}
		}

		response := map[string]interface{}{
			"enabled":           globalAgentCoordinator.IsEnabled(),
			"total_agents":      len(agents),
			"active_agents":     activeAgents,
			"total_tasks":       totalTasks,
			"agent_details":     agents,
			"available_types": []string{
				"General", "CodeReview", "Testing", "Documentation", "Deployment", "Debug",
			},
			"timestamp": time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleMultiAgentAgents returns available agent types and their capabilities
func (wi *WebInterface) handleMultiAgentAgents(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		agentTypes := []map[string]interface{}{
			{
				"type":         "General",
				"description":  "通用代理，處理一般任務",
				"skills":       []string{"一般協助", "程式碼生成", "檔案操作"},
				"capabilities": []string{"can_read_files", "can_write_files", "can_execute_commands"},
			},
			{
				"type":         "CodeReview",
				"description":  "程式碼審查專家",
				"skills":       []string{"程式碼分析", "安全審查", "效能審查", "最佳實務"},
				"capabilities": []string{"can_analyze_code_quality", "can_identify_security_issues", "can_suggest_improvements"},
			},
			{
				"type":         "Testing",
				"description":  "測試專家",
				"skills":       []string{"單元測試", "整合測試", "測試自動化", "覆蓋率分析"},
				"capabilities": []string{"can_write_tests", "can_run_tests", "can_analyze_coverage"},
			},
			{
				"type":         "Documentation",
				"description":  "文件撰寫專家",
				"skills":       []string{"API 文件", "README 撰寫", "程式碼註解", "使用指南"},
				"capabilities": []string{"can_generate_docs", "can_update_readme", "can_write_comments"},
			},
			{
				"type":         "Deployment",
				"description":  "部署和 DevOps 專家",
				"skills":       []string{"CI/CD", "Docker", "Kubernetes", "雲端部署", "監控"},
				"capabilities": []string{"can_build_images", "can_deploy_apps", "can_manage_infrastructure"},
			},
			{
				"type":         "Debug",
				"description":  "除錯專家",
				"skills":       []string{"錯誤分析", "日誌分析", "效能除錯", "問題排解"},
				"capabilities": []string{"can_analyze_errors", "can_debug_issues", "can_trace_problems"},
			},
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"agent_types": agentTypes,
			"enabled":     globalAgentCoordinator.IsEnabled(),
			"timestamp":   time.Now(),
		})
	})(w, r)
}

// === Performance Monitoring API Handlers ===

// handlePerformanceAnalytics returns comprehensive performance analytics
func (wi *WebInterface) handlePerformanceAnalytics(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if performanceMonitor == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Performance monitoring is disabled",
			})
			return
		}

		analytics := performanceMonitor.GetAnalytics()
		recommendations := performanceMonitor.GenerateRecommendations()

		response := map[string]interface{}{
			"enabled":         true,
			"analytics":       analytics,
			"recommendations": recommendations,
			"timestamp":       time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handlePerformanceMetrics returns recent performance metrics
func (wi *WebInterface) handlePerformanceMetrics(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if performanceMonitor == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Performance monitoring is disabled",
			})
			return
		}

		// Parse limit parameter
		limitStr := r.URL.Query().Get("limit")
		limit := 100 // default limit
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		metrics := performanceMonitor.GetRecentMetrics(limit)

		response := map[string]interface{}{
			"enabled":   true,
			"metrics":   metrics,
			"count":     len(metrics),
			"limit":     limit,
			"timestamp": time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handlePerformanceTrends returns performance trend data
func (wi *WebInterface) handlePerformanceTrends(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if performanceMonitor == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Performance monitoring is disabled",
			})
			return
		}

		// Parse hours parameter
		hoursStr := r.URL.Query().Get("hours")
		hours := 24 // default 24 hours
		if hoursStr != "" {
			if parsedHours, err := strconv.Atoi(hoursStr); err == nil && parsedHours > 0 {
				hours = parsedHours
			}
		}

		trends := performanceMonitor.GetPerformanceTrends(hours)

		response := map[string]interface{}{
			"enabled":   true,
			"trends":    trends,
			"timestamp": time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handlePerformanceRecommendations returns optimization recommendations
func (wi *WebInterface) handlePerformanceRecommendations(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if performanceMonitor == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Performance monitoring is disabled",
			})
			return
		}

		recommendations := performanceMonitor.GenerateRecommendations()

		response := map[string]interface{}{
			"enabled":         true,
			"recommendations": recommendations,
			"count":           len(recommendations),
			"timestamp":       time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handlePerformanceExport exports performance data
func (wi *WebInterface) handlePerformanceExport(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		if performanceMonitor == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Performance monitoring is disabled",
			})
			return
		}

		// Export performance data as JSON
		data, err := performanceMonitor.ExportMetrics()
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Export failed: %v", err),
			})
			return
		}

		// Set headers for file download
		filename := fmt.Sprintf("alice-performance-metrics-%s.json", time.Now().Format("20060102-150405"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))

		w.Write(data)
	})(w, r)
}

// === Security API Handlers ===

// handleSecurityEvents returns recent security events
func (wi *WebInterface) handleSecurityEvents(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalSecurityManager == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Security management is disabled",
			})
			return
		}

		// Parse query parameters
		limitStr := r.URL.Query().Get("limit")
		limit := 50 // default limit
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		severity := r.URL.Query().Get("severity")
		events := globalSecurityManager.GetSecurityEvents(limit, severity)

		response := map[string]interface{}{
			"enabled":   true,
			"events":    events,
			"count":     len(events),
			"limit":     limit,
			"severity":  severity,
			"timestamp": time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleSecurityStats returns security statistics
func (wi *WebInterface) handleSecurityStats(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalSecurityManager == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Security management is disabled",
			})
			return
		}

		stats := globalSecurityManager.GetSecurityStats()
		stats["enabled"] = true
		stats["timestamp"] = time.Now()

		json.NewEncoder(w).Encode(stats)
	})(w, r)
}

// handleSecurityAudit provides detailed security audit information
func (wi *WebInterface) handleSecurityAudit(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalSecurityManager == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Security management is disabled",
			})
			return
		}

		// Get events by severity
		critical := globalSecurityManager.GetSecurityEvents(10, "critical")
		high := globalSecurityManager.GetSecurityEvents(20, "high")
		medium := globalSecurityManager.GetSecurityEvents(30, "medium")
		low := globalSecurityManager.GetSecurityEvents(10, "low")

		stats := globalSecurityManager.GetSecurityStats()

		response := map[string]interface{}{
			"enabled":    true,
			"summary":    stats,
			"events": map[string]interface{}{
				"critical": critical,
				"high":     high,
				"medium":   medium,
				"low":      low,
			},
			"recommendations": generateSecurityRecommendations(stats),
			"timestamp":       time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// generateSecurityRecommendations generates security recommendations based on stats
func generateSecurityRecommendations(stats map[string]interface{}) []map[string]interface{} {
	recommendations := []map[string]interface{}{}

	// Check for high severity events
	if severities, ok := stats["severities"].(map[string]int); ok {
		if critical := severities["critical"]; critical > 0 {
			recommendations = append(recommendations, map[string]interface{}{
				"priority":    "high",
				"title":       "Critical Security Events Detected",
				"description": fmt.Sprintf("%d critical security events found. Immediate attention required.", critical),
				"action":      "Review critical events and take immediate corrective action",
			})
		}

		if high := severities["high"]; high > 5 {
			recommendations = append(recommendations, map[string]interface{}{
				"priority":    "medium",
				"title":       "Multiple High Severity Events",
				"description": fmt.Sprintf("%d high severity security events detected.", high),
				"action":      "Investigate patterns and strengthen security measures",
			})
		}
	}

	// Check rate limiting status
	if rateLimiting, ok := stats["rate_limiting"].(bool); ok && !rateLimiting {
		recommendations = append(recommendations, map[string]interface{}{
			"priority":    "medium",
			"title":       "Rate Limiting Disabled",
			"description": "Rate limiting is currently disabled, making the service vulnerable to abuse",
			"action":      "Enable rate limiting with appropriate RPM limits",
		})
	}

	// Check PII detection
	if piiDetection, ok := stats["pii_detection"].(bool); ok && !piiDetection {
		recommendations = append(recommendations, map[string]interface{}{
			"priority":    "low",
			"title":       "PII Detection Disabled",
			"description": "PII detection is disabled, potential privacy compliance risk",
			"action":      "Enable PII detection to protect sensitive information",
		})
	}

	// Check encryption status
	if encryption, ok := stats["encryption"].(bool); ok && !encryption {
		recommendations = append(recommendations, map[string]interface{}{
			"priority":    "high",
			"title":       "Encryption Not Configured",
			"description": "Sensitive data encryption is not configured",
			"action":      "Configure encryption keys for sensitive data protection",
		})
	}

	return recommendations
}

// handlePrometheusMetrics exports metrics in Prometheus format
func (wi *WebInterface) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	metrics := generatePrometheusMetrics()
	w.Write([]byte(metrics))
}

// generatePrometheusMetrics generates Prometheus-format metrics
func generatePrometheusMetrics() string {
	var metrics strings.Builder

	// Basic service information
	metrics.WriteString("# HELP alice_info Information about Alice bot\n")
	metrics.WriteString("# TYPE alice_info gauge\n")
	metrics.WriteString("alice_info{version=\"1.0.0\"} 1\n")

	// Performance metrics
	if performanceMonitor != nil {
		analytics := performanceMonitor.GetAnalytics()

		metrics.WriteString("# HELP alice_requests_total Total number of requests processed\n")
		metrics.WriteString("# TYPE alice_requests_total counter\n")
		metrics.WriteString(fmt.Sprintf("alice_requests_total %d\n", analytics.TotalRequests))

		metrics.WriteString("# HELP alice_success_rate Success rate percentage\n")
		metrics.WriteString("# TYPE alice_success_rate gauge\n")
		metrics.WriteString(fmt.Sprintf("alice_success_rate %.2f\n", analytics.SuccessRate))

		metrics.WriteString("# HELP alice_api_latency_seconds Average API latency in seconds\n")
		metrics.WriteString("# TYPE alice_api_latency_seconds gauge\n")
		metrics.WriteString(fmt.Sprintf("alice_api_latency_seconds %.3f\n", float64(analytics.AvgAPILatency.Nanoseconds())/1e9))

		metrics.WriteString("# HELP alice_total_tokens Total tokens used\n")
		metrics.WriteString("# TYPE alice_total_tokens counter\n")
		metrics.WriteString(fmt.Sprintf("alice_total_tokens %d\n", analytics.TotalTokens))

		metrics.WriteString("# HELP alice_total_cost_usd Total cost in USD\n")
		metrics.WriteString("# TYPE alice_total_cost_usd counter\n")
		metrics.WriteString(fmt.Sprintf("alice_total_cost_usd %.6f\n", analytics.TotalCost))

		metrics.WriteString("# HELP alice_memory_usage_bytes Current memory usage in bytes\n")
		metrics.WriteString("# TYPE alice_memory_usage_bytes gauge\n")
		metrics.WriteString(fmt.Sprintf("alice_memory_usage_bytes %d\n", analytics.CurrentMemoryUsage))

		metrics.WriteString("# HELP alice_uptime_seconds Uptime in seconds\n")
		metrics.WriteString("# TYPE alice_uptime_seconds counter\n")
		metrics.WriteString(fmt.Sprintf("alice_uptime_seconds %d\n", analytics.UptimeSeconds))

		// Tool usage metrics
		for tool, count := range analytics.ToolUsageStats {
			metrics.WriteString(fmt.Sprintf("alice_tool_usage_total{tool=\"%s\"} %d\n", tool, count))
		}

		// Error metrics by type
		for errorType, count := range analytics.ErrorsByType {
			metrics.WriteString(fmt.Sprintf("alice_errors_total{type=\"%s\"} %d\n", errorType, count))
		}
	}

	// Security metrics
	if globalSecurityManager != nil {
		stats := globalSecurityManager.GetSecurityStats()

		if totalEvents, ok := stats["total_events"].(int); ok {
			metrics.WriteString("# HELP alice_security_events_total Total security events\n")
			metrics.WriteString("# TYPE alice_security_events_total counter\n")
			metrics.WriteString(fmt.Sprintf("alice_security_events_total %d\n", totalEvents))
		}

		if severities, ok := stats["severities"].(map[string]int); ok {
			for severity, count := range severities {
				metrics.WriteString(fmt.Sprintf("alice_security_events_by_severity{severity=\"%s\"} %d\n", severity, count))
			}
		}

		if eventTypes, ok := stats["event_types"].(map[string]int); ok {
			for eventType, count := range eventTypes {
				metrics.WriteString(fmt.Sprintf("alice_security_events_by_type{type=\"%s\"} %d\n", eventType, count))
			}
		}

		if activeVisitors, ok := stats["active_visitors"].(int); ok {
			metrics.WriteString("# HELP alice_active_visitors Current active visitors for rate limiting\n")
			metrics.WriteString("# TYPE alice_active_visitors gauge\n")
			metrics.WriteString(fmt.Sprintf("alice_active_visitors %d\n", activeVisitors))
		}
	}

	// Multi-agent metrics
	if globalAgentCoordinator != nil && globalAgentCoordinator.IsEnabled() {
		stats := globalAgentCoordinator.GetAgentStats()

		if totalTasks, ok := stats["total_tasks"].(int64); ok {
			metrics.WriteString("# HELP alice_multiagent_tasks_total Total multi-agent tasks\n")
			metrics.WriteString("# TYPE alice_multiagent_tasks_total counter\n")
			metrics.WriteString(fmt.Sprintf("alice_multiagent_tasks_total %d\n", totalTasks))
		}

		if activeTasks, ok := stats["active_tasks"].(int); ok {
			metrics.WriteString("# HELP alice_multiagent_active_tasks Current active tasks\n")
			metrics.WriteString("# TYPE alice_multiagent_active_tasks gauge\n")
			metrics.WriteString(fmt.Sprintf("alice_multiagent_active_tasks %d\n", activeTasks))
		}
	}

	return metrics.String()
}