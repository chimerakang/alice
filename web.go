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
func (wi *WebInterface) CreateRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// Serve dashboard at root
	mux.HandleFunc("/", wi.handleDashboard)

	// API endpoints
	mux.HandleFunc("/api/health", wi.handleHealth)
	mux.HandleFunc("/api/stats", wi.handleStats)
	mux.HandleFunc("/api/agents", wi.handleAgents)
	mux.HandleFunc("/api/agents/", wi.handleAgentDetail)
	mux.HandleFunc("/api/tools/recent", wi.handleRecentTools)
	mux.HandleFunc("/api/tools/executions", wi.handleToolExecutions)
	mux.HandleFunc("/api/decisions", wi.handleDecisions)
	mux.HandleFunc("/api/decisions/recent", wi.handleRecentDecisions)
	mux.HandleFunc("/api/decisions/search", wi.handleSearchDecisions)
	mux.HandleFunc("/api/decisions/export", wi.handleExportDecisions)

	return mux
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