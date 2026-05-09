package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/security"
)

// AgentInfo represents agent information for the API
type AgentInfo struct {
	ChatID         int64                       `json:"chat_id"`
	ThreadID       int                         `json:"thread_id"`
	ProjectDir     string                      `json:"project_dir"`
	SessionID      string                      `json:"session_id"`
	IsActive       bool                        `json:"is_active"`
	IsProcessing   bool                        `json:"is_processing"`
	Execution      appengine.ExecutionSnapshot `json:"execution"`
	ExecutionState string                      `json:"execution_state"`
	LastActivity   time.Time                   `json:"last_activity"`
	CreatedAt      time.Time                   `json:"created_at"`
	ProjectCount   int                         `json:"project_count"`
	Stats          TokenStats                  `json:"stats"`
}

// DetailedStats represents enhanced statistics
type DetailedStats struct {
	ActiveSessions  int         `json:"active_sessions"`
	TotalSessions   int         `json:"total_sessions"`
	ToolsExecuted   int64       `json:"tools_executed"`
	TotalProjects   int         `json:"total_projects"`
	SuccessRate     float64     `json:"success_rate"`
	UptimeSeconds   int64       `json:"uptime_seconds"`
	Timestamp       time.Time   `json:"timestamp"`
	RecentAgents    []AgentInfo `json:"recent_agents"`
	TotalTokensUsed int64       `json:"total_tokens_used"`
	TotalCostUSD    float64     `json:"total_cost_usd"`
}

// WebInterface manages the HTTP server and web dashboard functionality
type WebInterface struct {
	bot       *TelegramBot
	staticDir string
	apiToken  string // Bearer token for control endpoints
	server    *http.Server
	mu        sync.RWMutex
}

// NewWebInterface creates a new web interface instance
func NewWebInterface(bot *TelegramBot, port, staticDir, apiToken string) *WebInterface {
	wi := &WebInterface{
		bot:       bot,
		apiToken:  apiToken,
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

	// Serve Vite-built static assets
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("web/assets/"))))
	mux.HandleFunc("/vite.svg", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/vite.svg")
	})

	// SPA fallback: serve index.html for all non-API routes (React Router handles client-side routing)
	mux.HandleFunc("/", wi.handleSPAFallback)

	// API endpoints
	mux.HandleFunc("/api/health", wi.handleHealth)
	mux.HandleFunc("/api/stats", wi.handleStats)
	mux.HandleFunc("/api/agents", wi.handleAgents)
	mux.HandleFunc("/api/agents/abort", wi.handleAgentAbort)
	mux.HandleFunc("/api/agents/reset", wi.handleAgentReset)
	mux.HandleFunc("/api/agents/project", wi.handleAgentSetProject)
	mux.HandleFunc("/api/agents/", wi.handleAgentDetail)
	mux.HandleFunc("/api/tools/recent", wi.handleRecentTools)
	mux.HandleFunc("/api/tools/executions", wi.handleToolExecutions)
	mux.HandleFunc("/api/decisions", wi.handleDecisions)
	mux.HandleFunc("/api/decisions/recent", wi.handleRecentDecisions)
	mux.HandleFunc("/api/decisions/range", wi.handleDecisionsRange)
	mux.HandleFunc("/api/decisions/search", wi.handleSearchDecisions)
	mux.HandleFunc("/api/decisions/export", wi.handleExportDecisions)
	mux.HandleFunc("/api/decisions/sources/stats", wi.handleSourceStats)
	mux.HandleFunc("/api/decisions/sources/performance", wi.handleSourcePerformance)
	mux.HandleFunc("/api/tasks", wi.handleUnifiedTasks)
	mux.HandleFunc("/api/hermes/active", wi.handleHermesActive)
	mux.HandleFunc("/api/hermes/resolve", wi.handleHermesResolve)
	mux.HandleFunc("/api/hermes/tasks", wi.handleHermesTasks)
	mux.HandleFunc("/api/hermes/snapshots", wi.handleHermesSnapshots)
	mux.HandleFunc("/api/runtime/events", wi.handleRuntimeEvents)
	mux.HandleFunc("/api/memory/preview", wi.handleMemoryPreview)
	mux.HandleFunc("/api/quality/decomposition", wi.handleQualityDecomposition)
	mux.HandleFunc("/api/quality/scores", wi.handleQualityScores)
	mux.HandleFunc("/api/quality/insights", wi.handleQualityInsights)
	mux.HandleFunc("/api/multiagent/status", wi.handleMultiAgentStatus)
	mux.HandleFunc("/api/multiagent/stats", wi.handleMultiAgentStats)
	mux.HandleFunc("/api/multiagent/agents", wi.handleMultiAgentAgents)

	// Performance Monitoring APIs
	mux.HandleFunc("/api/performance/analytics", wi.handlePerformanceAnalytics)
	mux.HandleFunc("/api/performance/metrics", wi.handlePerformanceMetrics)
	mux.HandleFunc("/api/performance/trends", wi.handlePerformanceTrends)
	mux.HandleFunc("/api/performance/recommendations", wi.handlePerformanceRecommendations)
	mux.HandleFunc("/api/performance/export", wi.handlePerformanceExport)

	// Model Routing APIs (Issue #72)
	mux.HandleFunc("/api/model-routing/status", wi.handleModelRoutingStatus)
	mux.HandleFunc("/api/model-routing/set", wi.handleModelRoutingSet)

	// Cost Tracking APIs (Issue #73)
	mux.HandleFunc("/api/costs/by-model", wi.handleCostsByModel)
	mux.HandleFunc("/api/costs/summary", wi.handleCostsSummary)

	// Cost Savings APIs (Issue #74)
	mux.HandleFunc("/api/costs/savings", wi.handleCostSavings)

	// Internationalization APIs (Issue #76)
	mux.HandleFunc("/api/language", wi.handleLanguage)

	// Security APIs
	mux.HandleFunc("/api/security/events", wi.handleSecurityEvents)
	mux.HandleFunc("/api/security/stats", wi.handleSecurityStats)
	mux.HandleFunc("/api/security/audit", wi.handleSecurityAudit)

	// Git Integration APIs
	mux.HandleFunc("/api/git/status", wi.handleGitStatus)
	mux.HandleFunc("/api/git/events", wi.handleGitEvents)
	mux.HandleFunc("/api/git/operations", wi.handleGitOperations)
	mux.HandleFunc("/api/git/diff", wi.handleGitDiff)

	// Storage & Persistence APIs
	mux.HandleFunc("/api/storage/health", wi.handleStorageHealth)
	mux.HandleFunc("/api/storage/stats", wi.handleStorageStats)
	mux.HandleFunc("/api/storage/cleanup", wi.handleStorageCleanup)

	// Checkpoint & Snapshot APIs
	mux.HandleFunc("/api/checkpoints", wi.handleCheckpoints)
	mux.HandleFunc("/api/checkpoints/create", wi.handleCreateCheckpoint)
	mux.HandleFunc("/api/checkpoints/restore", wi.handleRestoreCheckpoint)
	mux.HandleFunc("/api/checkpoints/stats", wi.handleCheckpointStats)

	// WebSocket Real-time Events
	mux.HandleFunc("/ws", wi.handleWebSocket)
	mux.HandleFunc("/api/websocket/stats", wi.handleWebSocketStats)

	// Claude Code Hooks Integration
	mux.HandleFunc("/api/hooks/claude-code", wi.handleClaudeCodeHook)
	mux.HandleFunc("/api/hooks/user-prompt-submit", wi.handleUserPromptSubmit)
	mux.HandleFunc("/api/hooks/codex-session-update", wi.handleCodexSessionUpdate)
	mux.HandleFunc("/api/hooks/prompt-classifications", wi.handlePromptClassifications)

	// Prometheus metrics endpoint
	mux.HandleFunc("/metrics", wi.handlePrometheusMetrics)

	// 應用安全中間件
	var handler http.Handler = mux

	if security.Global() != nil {
		// 依序應用中間件 (注意順序)
		handler = security.Global().SecurityHeadersMiddleware(handler)
		handler = security.Global().IPFilterMiddleware(handler)
		handler = security.Global().RateLimitMiddleware(handler)
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

// handleSPAFallback serves the React SPA for all non-API routes.
// API, WebSocket, metrics, and static asset routes are handled by their own handlers.
// Everything else gets index.html so React Router can handle client-side routing.
func (wi *WebInterface) handleSPAFallback(w http.ResponseWriter, r *http.Request) {
	// Let registered API/ws/metrics routes pass through (they have their own handlers)
	// This handler only fires for unmatched routes due to ServeMux longest-match rule

	indexPath := filepath.Join("web", "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}

	// If no index.html found, serve a simple status page
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
			"jobs":      globalJobTracker.Summary(),
			"storage":   runtimeStorageHealth(),
			"hermes":    runtimeHermesStatus(wi.bot),
		}

		json.NewEncoder(w).Encode(status)
	})(w, r)
}

// runtimeHermesStatus surfaces Hermes feature toggles + walking-agent state
// so dashboards / external probes can confirm whether walking is active and
// at what watermark. Returns minimal info when bot or config is nil; never
// includes credentials. Issue #149.
func runtimeHermesStatus(bot *TelegramBot) map[string]interface{} {
	out := map[string]interface{}{
		"enabled": false,
	}
	if bot == nil || bot.config == nil {
		return out
	}
	out["enabled"] = bot.config.Hermes.Enabled
	out["strict_mode_enabled"] = bot.config.Hermes.StrictModeEnabled
	out["auto_route_complex"] = bot.config.Hermes.AutoRouteComplex
	out["walking_agent_enabled"] = bot.config.Hermes.WalkingAgentEnabled
	maxCtx := bot.config.Hermes.WalkingAgentMaxContextTokens
	if maxCtx <= 0 {
		maxCtx = 120000
	}
	out["walking_agent_max_context_tokens"] = maxCtx
	if bot.config.Hermes.ReviewTimeoutSeconds > 0 {
		out["review_timeout_seconds"] = bot.config.Hermes.ReviewTimeoutSeconds
	} else {
		out["review_timeout_seconds"] = 120
	}
	return out
}

func runtimeStorageHealth() map[string]interface{} {
	status := map[string]interface{}{
		"enabled": globalStorage != nil,
	}
	if globalStorage == nil {
		return status
	}
	if err := globalStorage.Health(); err != nil {
		status["healthy"] = false
		status["error"] = err.Error()
		return status
	}
	status["healthy"] = true

	sqliteStorage, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		status["backend"] = "unknown"
		return status
	}
	status["backend"] = "sqlite"
	db, ok := sqliteStorage.GetDB().(*sql.DB)
	if !ok {
		status["backend_error"] = "sqlite db handle unavailable"
		return status
	}
	if count, err := countRows(db, `SELECT COUNT(*) FROM tasks WHERE status IN ('planning','executing','validating')`); err == nil {
		status["active_tasks"] = count
	} else {
		status["active_tasks_error"] = err.Error()
	}
	// After #169 slice 3d the legacy hermes_task_states.status column is no
	// longer authoritative. Active-task count derives from the latest
	// snapshot's denormalized state_status, falling back to the legacy
	// column for tasks created before the first CommitRuntimeStep.
	if count, err := countRows(db, `
		SELECT COUNT(*) FROM hermes_task_states AS task
		WHERE COALESCE((
			SELECT s.state_status FROM hermes_snapshots s
			WHERE s.task_id = task.id ORDER BY s.step DESC LIMIT 1
		), task.status) IN ('planning','executing','validating')`); err == nil {
		status["active_hermes_tasks"] = count
	} else {
		status["active_hermes_tasks_error"] = err.Error()
	}
	return status
}

func countRows(db *sql.DB, query string, args ...any) (int64, error) {
	var count int64
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
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
		execution := agent.ExecutionSnapshot()

		info := AgentInfo{
			ChatID:         key.chatID,
			ThreadID:       key.threadID,
			ProjectDir:     agent.ProjectDir(),
			SessionID:      agent.SessionID(),
			IsActive:       agent.IsActive(),
			IsProcessing:   agent.IsProcessing(),
			Execution:      execution,
			ExecutionState: string(execution.State),
			LastActivity:   agent.LastActivity(),
			CreatedAt:      agent.CreatedAt(),
			ProjectCount:   agent.ProjectCount(),
			Stats:          agent.Stats(),
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
	execution := agent.ExecutionSnapshot()

	return &AgentInfo{
		ChatID:         chatID,
		ThreadID:       threadID,
		ProjectDir:     agent.ProjectDir(),
		SessionID:      agent.SessionID(),
		IsActive:       agent.IsActive(),
		IsProcessing:   agent.IsProcessing(),
		Execution:      execution,
		ExecutionState: string(execution.State),
		LastActivity:   agent.LastActivity(),
		CreatedAt:      agent.CreatedAt(),
		ProjectCount:   agent.ProjectCount(),
		Stats:          agent.Stats(),
	}
}

// getBasicStats collects basic statistics from the Telegram bot
func (wi *WebInterface) getBasicStats() map[string]interface{} {
	stats := wi.getDetailedStats()

	// Convert DetailedStats to map[string]interface{}
	return map[string]interface{}{
		"active_sessions":   stats.ActiveSessions,
		"tools_executed":    stats.ToolsExecuted,
		"projects":          stats.TotalProjects,
		"success_rate":      stats.SuccessRate,
		"uptime_seconds":    stats.UptimeSeconds,
		"timestamp":         stats.Timestamp,
		"total_sessions":    stats.TotalSessions,
		"total_tokens_used": stats.TotalTokensUsed,
		"total_cost_usd":    stats.TotalCostUSD,
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
			execution := agent.ExecutionSnapshot()
			info := AgentInfo{
				ChatID:         key.chatID,
				ThreadID:       key.threadID,
				ProjectDir:     agent.ProjectDir(),
				SessionID:      agent.SessionID(),
				IsActive:       agent.IsActive(),
				IsProcessing:   agent.IsProcessing(),
				Execution:      execution,
				ExecutionState: string(execution.State),
				LastActivity:   agent.LastActivity(),
				CreatedAt:      agent.CreatedAt(),
				ProjectCount:   agent.ProjectCount(),
				Stats:          stats,
			}
			recentAgents = append(recentAgents, info)
		}
	}

	// Get tool execution count - prefer DB for accurate historical count
	toolExecutionCount := int64(globalToolLogger.GetExecutionCount())
	if globalStorage != nil {
		if dbCount, err := globalStorage.GetToolExecutionsCount(); err == nil && dbCount > toolExecutionCount {
			toolExecutionCount = dbCount
		}
	}

	return DetailedStats{
		ActiveSessions:  activeSessions,
		TotalSessions:   len(agents),
		ToolsExecuted:   toolExecutionCount,
		TotalProjects:   totalProjects,
		SuccessRate:     100.0, // Will calculate based on tool execution success/failure
		UptimeSeconds:   GetUptimeSeconds(),
		Timestamp:       time.Now(),
		RecentAgents:    recentAgents,
		TotalTokensUsed: totalTokensUsed,
		TotalCostUSD:    totalCostUSD,
	}
}

// parseTimeRange parses start_time and end_time query parameters, returning parsed times and ok flag
func parseTimeRange(r *http.Request) (start time.Time, end time.Time, ok bool) {
	startStr := r.URL.Query().Get("start_time")
	endStr := r.URL.Query().Get("end_time")
	if startStr == "" || endStr == "" {
		return time.Time{}, time.Time{}, false
	}
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		start, err = time.Parse("2006-01-02T15:04:05Z", startStr)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
	}
	end, err = time.Parse(time.RFC3339, endStr)
	if err != nil {
		end, err = time.Parse("2006-01-02T15:04:05Z", endStr)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
	}
	return start, end, true
}

// computeTrendsFromMetrics calculates performance trends from a slice of metrics
func computeTrendsFromMetrics(metrics []PerformanceMetrics, hours int) map[string]interface{} {
	if len(metrics) == 0 {
		return map[string]interface{}{
			"period_hours": hours,
			"data_points":  0,
			"message":      "No data available for the specified period",
		}
	}

	totalLatency := time.Duration(0)
	totalTokens := 0
	totalCost := 0.0
	totalCacheRead := 0
	totalCacheWrite := 0
	successful := 0
	hourlyStats := make(map[int]map[string]interface{})

	for _, metric := range metrics {
		totalLatency += metric.APICallLatency
		totalTokens += metric.TokensUsed
		totalCost += metric.EstimatedCost
		totalCacheRead += metric.CacheReadTokens
		totalCacheWrite += metric.CacheWriteTokens
		if metric.APICallSuccess {
			successful++
		}

		hour := metric.Timestamp.Hour()
		if hourlyStats[hour] == nil {
			hourlyStats[hour] = map[string]interface{}{
				"requests":    0,
				"latency_sum": time.Duration(0),
				"tokens":      0,
				"cost":        0.0,
				"cache_read":  0,
				"cache_write": 0,
				"errors":      0,
			}
		}
		stats := hourlyStats[hour]
		stats["requests"] = stats["requests"].(int) + 1
		stats["latency_sum"] = stats["latency_sum"].(time.Duration) + metric.APICallLatency
		stats["tokens"] = stats["tokens"].(int) + metric.TokensUsed
		stats["cost"] = stats["cost"].(float64) + metric.EstimatedCost
		stats["cache_read"] = stats["cache_read"].(int) + metric.CacheReadTokens
		stats["cache_write"] = stats["cache_write"].(int) + metric.CacheWriteTokens
		if !metric.APICallSuccess || metric.ErrorType != "" {
			stats["errors"] = stats["errors"].(int) + 1
		}
	}

	cacheHitRate := 0.0
	if totalTokens > 0 {
		cacheHitRate = float64(totalCacheRead) / float64(totalTokens) * 100
	}
	return map[string]interface{}{
		"period_hours":       hours,
		"data_points":        len(metrics),
		"avg_latency_ms":     float64(totalLatency.Nanoseconds()) / float64(len(metrics)) / 1e6,
		"total_tokens":       totalTokens,
		"total_cost":         totalCost,
		"cache_read_tokens":  totalCacheRead,
		"cache_write_tokens": totalCacheWrite,
		"cache_hit_rate":     cacheHitRate,
		"cache_breakdown":    computeCacheBreakdown(metrics),
		"success_rate":       float64(successful) / float64(len(metrics)) * 100,
		"hourly_breakdown":   hourlyStats,
	}
}

type cacheBreakdownRow struct {
	Group                 string  `json:"group"`
	Calls                 int     `json:"calls"`
	Tokens                int     `json:"tokens"`
	InputTokens           int     `json:"input_tokens"`
	CacheReadTokens       int     `json:"cache_read_tokens"`
	CacheWriteTokens      int     `json:"cache_write_tokens"`
	OutputTokens          int     `json:"output_tokens"`
	Cost                  float64 `json:"cost"`
	CacheReadInputPercent float64 `json:"cache_read_input_percent"`
	CacheReadTotalPercent float64 `json:"cache_read_total_percent"`
}

func computeCacheBreakdown(metrics []PerformanceMetrics) map[string][]cacheBreakdownRow {
	return map[string][]cacheBreakdownRow{
		"by_provider": sortedCacheBreakdown(metrics, func(m PerformanceMetrics) string {
			return cacheProviderForModel(m.Model)
		}, 0),
		"by_model": sortedCacheBreakdown(metrics, func(m PerformanceMetrics) string {
			model := strings.TrimSpace(m.Model)
			if model == "" {
				return "unknown"
			}
			return model
		}, 0),
		"by_project": sortedCacheBreakdown(metrics, func(m PerformanceMetrics) string {
			project := strings.TrimSpace(m.ProjectPath)
			if project == "" {
				return "unknown"
			}
			return project
		}, 10),
	}
}

func sortedCacheBreakdown(metrics []PerformanceMetrics, groupFn func(PerformanceMetrics) string, limit int) []cacheBreakdownRow {
	grouped := make(map[string]*cacheBreakdownRow)
	for _, metric := range metrics {
		group := groupFn(metric)
		row := grouped[group]
		if row == nil {
			row = &cacheBreakdownRow{Group: group}
			grouped[group] = row
		}
		row.Calls++
		row.Tokens += metric.TokensUsed
		row.InputTokens += metric.InputTokens
		row.CacheReadTokens += metric.CacheReadTokens
		row.CacheWriteTokens += metric.CacheWriteTokens
		row.OutputTokens += metric.OutputTokens
		row.Cost += metric.EstimatedCost
	}

	rows := make([]cacheBreakdownRow, 0, len(grouped))
	for _, row := range grouped {
		inputDenom := row.InputTokens + row.CacheReadTokens + row.CacheWriteTokens
		if inputDenom > 0 {
			row.CacheReadInputPercent = float64(row.CacheReadTokens) * 100 / float64(inputDenom)
		}
		if row.Tokens > 0 {
			row.CacheReadTotalPercent = float64(row.CacheReadTokens) * 100 / float64(row.Tokens)
		}
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tokens == rows[j].Tokens {
			return rows[i].Group < rows[j].Group
		}
		return rows[i].Tokens > rows[j].Tokens
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func cacheProviderForModel(model string) string {
	short := ExtractModelShortName(model)
	switch short {
	case "haiku", "sonnet", "opus":
		return "claude"
	case "codex", "gpt-5.5", "gpt-5.4", "gpt-4o", "gpt-4.1", "gpt", "o3", "o4-mini":
		return "codex"
	default:
		if strings.HasPrefix(short, "gpt-") {
			return "codex"
		}
		return "unknown"
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

		var executions []ToolExecution
		var total int

		// If time range provided and storage available, query DB
		if startTime, endTime, ok := parseTimeRange(r); ok && globalStorage != nil {
			dbExecs, err := globalStorage.GetToolExecutionsByTimeRange(startTime, endTime, limit)
			if err == nil {
				executions = dbExecs
				total = len(executions)
			}
		}

		// Fallback to in-memory
		if executions == nil {
			executions = globalToolLogger.GetRecentExecutions(limit)
			total = len(executions)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"executions": executions,
			"total":      total,
			"timestamp":  time.Now(),
		})
	})(w, r)
}

// handleToolExecutions returns detailed tool execution statistics
func (wi *WebInterface) handleToolExecutions(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var executions []ToolExecution

		// If time range provided and storage available, query DB
		if startTime, endTime, ok := parseTimeRange(r); ok && globalStorage != nil {
			dbExecs, err := globalStorage.GetToolExecutionsByTimeRange(startTime, endTime, 0)
			if err == nil {
				executions = dbExecs
			}
		}

		// Fallback to in-memory
		if executions == nil {
			executions = globalToolLogger.GetRecentExecutions(0) // Get all
		}

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

		// Get recent 10 for display
		recentLimit := 10
		if len(executions) < recentLimit {
			recentLimit = len(executions)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_executions":  len(executions),
			"tool_counts":       toolCounts,
			"status_counts":     statusCounts,
			"success_rate":      successRate,
			"recent_executions": executions[:recentLimit],
			"timestamp":         time.Now(),
		})
	})(w, r)
}

// handleDecisions returns comprehensive decision statistics
func (wi *WebInterface) handleDecisions(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Load from database first for persistence across restarts
		var decisions []DecisionLog
		if globalStorage != nil {
			if dbDecisions, err := globalStorage.GetDecisionLogs(1000, 0); err == nil {
				decisions = dbDecisions
			}
		}
		if len(decisions) == 0 {
			decisions = globalDecisionLogger.GetRecentDecisions(0)
		}

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

		// Recent 5 decisions for preview
		recentCount := 5
		if len(decisions) < recentCount {
			recentCount = len(decisions)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_decisions":      len(decisions),
			"task_type_counts":     taskTypeCounts,
			"outcome_stats":        outcomeStats,
			"success_rate":         successRate,
			"avg_duration_ms":      avgDuration,
			"recent_decisions":     decisions[:recentCount],
			"transparency_enabled": globalDecisionLogger.IsEnabled(),
			"timestamp":            time.Now(),
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

		// Prefer database over in-memory logger so data persists across restarts
		var decisions interface{}
		total := 0
		if globalStorage != nil {
			if dbDecisions, err := globalStorage.GetDecisionLogs(limit, 0); err == nil {
				decisions = dbDecisions
				total = len(dbDecisions)
			}
		}
		if decisions == nil {
			mem := globalDecisionLogger.GetRecentDecisions(limit)
			decisions = mem
			total = len(mem)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decisions": decisions,
			"total":     total,
			"limit":     limit,
			"timestamp": time.Now(),
		})
	})(w, r)
}

func (wi *WebInterface) handleRuntimeEvents(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Storage is unavailable",
			})
			return
		}

		query := r.URL.Query()
		limit := parsePositiveIntParam(query.Get("limit"), 50)
		offset := parsePositiveIntParam(query.Get("offset"), 0)
		eventType := strings.TrimSpace(query.Get("type"))

		var (
			events []RuntimeEventRecord
			err    error
		)
		if eventType != "" {
			events, err = globalStorage.GetRuntimeEventsByType(eventType, limit)
		} else {
			events, err = globalStorage.GetRuntimeEvents(limit, offset)
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"events":    events,
			"total":     len(events),
			"limit":     limit,
			"offset":    offset,
			"type":      eventType,
			"timestamp": time.Now(),
		})
	})(w, r)
}

// handleUnifiedTasks returns task-centric execution graphs from the transitional
// #114 schema: tasks -> sub_tasks -> tool_events/artifacts/reviews.
func (wi *WebInterface) handleUnifiedTasks(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Storage is unavailable",
			})
			return
		}

		query, err := parseUnifiedTaskQuery(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		tasks, err := globalStorage.ListUnifiedTaskGraphs(query)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Database query failed: %v", err),
			})
			return
		}
		total, err := globalStorage.CountUnifiedTasks(query)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Database count failed: %v", err),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks":     tasks,
			"total":     total,
			"limit":     query.Limit,
			"offset":    query.Offset,
			"timestamp": time.Now(),
		})
	})(w, r)
}

type memoryPreviewSection struct {
	Source   string `json:"source"`
	Scope    string `json:"scope"`
	Priority int    `json:"priority"`
	Size     int    `json:"size"`
	Preview  string `json:"preview"`
}

func (wi *WebInterface) handleMemoryPreview(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		req, err := parseMemoryPreviewRequest(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		var taskSource hermesMemoryTaskSource
		if wi != nil && wi.bot != nil && wi.bot.taskSvc != nil {
			taskSource = wi.bot.taskSvc
		}
		if wi != nil && wi.bot != nil {
			ctx := wi.bot.getChatContext(chatKey{chatID: req.ChatID, threadID: req.ThreadID}, req.ProjectDir)
			req.RecentMessages = ctx.RecentMessagesSnapshot()
			if req.ProjectDir == "" {
				req.ProjectDir = ctx.ProjectDir
			}
		}

		resolver := NewMemoryResolverWithAllSources(taskSource, globalGeneralMemorySource(), defaultStaticHintSourceForProject(req.ProjectDir))
		bundle, err := resolver.Resolve(r.Context(), req)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		sections := make([]memoryPreviewSection, 0, len(bundle.Sections))
		for _, section := range bundle.Sections {
			text := strings.TrimSpace(section.Text)
			sections = append(sections, memoryPreviewSection{
				Source:   section.Source,
				Scope:    section.Scope,
				Priority: section.Priority,
				Size:     len([]rune(text)),
				Preview:  clampMemoryText(text, 500),
			})
		}
		rendered := bundle.Render()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sections":         sections,
			"section_count":    len(sections),
			"rendered_size":    len([]rune(rendered)),
			"rendered_preview": clampMemoryText(rendered, 1000),
			"timestamp":        time.Now(),
		})
	})(w, r)
}

func parseMemoryPreviewRequest(r *http.Request) (MemoryRequest, error) {
	q := r.URL.Query()
	chatIDRaw := strings.TrimSpace(q.Get("chat_id"))
	if chatIDRaw == "" {
		return MemoryRequest{}, fmt.Errorf("chat_id is required")
	}
	chatID, err := strconv.ParseInt(chatIDRaw, 10, 64)
	if err != nil || chatID == 0 {
		return MemoryRequest{}, fmt.Errorf("chat_id must be a non-zero integer")
	}

	req := MemoryRequest{
		ChatID:      chatID,
		ProjectDir:  strings.TrimSpace(q.Get("project_dir")),
		UserMessage: strings.TrimSpace(q.Get("message")),
		Mode:        strings.TrimSpace(q.Get("mode")),
	}
	if req.Mode == "" {
		req.Mode = "preview"
	}
	if raw := strings.TrimSpace(q.Get("thread_id")); raw != "" {
		threadID, err := strconv.Atoi(raw)
		if err != nil {
			return MemoryRequest{}, fmt.Errorf("thread_id must be an integer")
		}
		req.ThreadID = threadID
	}
	if raw := strings.TrimSpace(q.Get("issue")); raw != "" {
		issueNumber, err := strconv.Atoi(raw)
		if err != nil || issueNumber <= 0 {
			return MemoryRequest{}, fmt.Errorf("issue must be a positive integer")
		}
		req.IssueNumber = issueNumber
	}
	if raw := strings.TrimSpace(q.Get("budget")); raw != "" {
		budget, err := strconv.Atoi(raw)
		if err != nil || budget <= 0 {
			return MemoryRequest{}, fmt.Errorf("budget must be a positive integer")
		}
		req.BudgetChars = budget
	}
	return req, nil
}

func (wi *WebInterface) handleQualityDecomposition(w http.ResponseWriter, r *http.Request) {
	wi.handleQualityStats(w, r, func(window QualityWindow) (interface{}, error) {
		return globalStorage.GetQualityDecompositionStats(window)
	})
}

func (wi *WebInterface) handleQualityScores(w http.ResponseWriter, r *http.Request) {
	wi.handleQualityStats(w, r, func(window QualityWindow) (interface{}, error) {
		return globalStorage.GetQualityScoreStats(window)
	})
}

func (wi *WebInterface) handleQualityInsights(w http.ResponseWriter, r *http.Request) {
	wi.handleQualityStats(w, r, func(window QualityWindow) (interface{}, error) {
		insights, err := globalStorage.GetQualityInsights(window)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"window_start": window.Start,
			"window_end":   window.End,
			"insights":     insights,
		}, nil
	})
}

func (wi *WebInterface) handleQualityStats(w http.ResponseWriter, r *http.Request, load func(QualityWindow) (interface{}, error)) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if globalStorage == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Storage is unavailable"})
			return
		}
		window, err := ResolveQualityWindow(r.URL.Query().Get("window"), time.Now().UTC())
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		payload, err := load(window)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Quality query failed: %v", err),
			})
			return
		}
		json.NewEncoder(w).Encode(payload)
	})(w, r)
}

func parseUnifiedTaskQuery(r *http.Request) (UnifiedTaskQuery, error) {
	query := UnifiedTaskQuery{
		Limit:      parsePositiveIntQuery(r, "limit", 100),
		Offset:     parseNonNegativeIntQuery(r, "offset", 0),
		ID:         r.URL.Query().Get("task_id"),
		ProjectDir: r.URL.Query().Get("project_dir"),
		Status:     r.URL.Query().Get("status"),
	}
	if hasReview := r.URL.Query().Get("has_review"); hasReview != "" {
		switch strings.ToLower(strings.TrimSpace(hasReview)) {
		case "1", "true", "yes", "y":
			value := true
			query.HasReview = &value
		case "0", "false", "no", "n":
			value := false
			query.HasReview = &value
		default:
			return query, fmt.Errorf("invalid has_review value: %q", hasReview)
		}
	}
	if start := r.URL.Query().Get("start_time"); start != "" {
		parsed, err := parseAPITime(start)
		if err != nil {
			return query, fmt.Errorf("invalid start_time format: %w", err)
		}
		query.StartTime = &parsed
	}
	if end := r.URL.Query().Get("end_time"); end != "" {
		parsed, err := parseAPITime(end)
		if err != nil {
			return query, fmt.Errorf("invalid end_time format: %w", err)
		}
		query.EndTime = &parsed
	}
	return query, nil
}

func parsePositiveIntQuery(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseNonNegativeIntQuery(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func parseAPITime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02T15:04:05Z", value)
}

func parsePositiveIntParam(value string, fallback int) int {
	if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}

// handleDecisionsRange returns decision logs within a specified time range
// Supports time-based filtering from frontend date picker components
func (wi *WebInterface) handleDecisionsRange(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Storage is unavailable",
			})
			return
		}

		// Parse time range parameters (from frontend date picker)
		startTimeStr := r.URL.Query().Get("start_time")
		endTimeStr := r.URL.Query().Get("end_time")

		if startTimeStr == "" || endTimeStr == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "start_time and end_time parameters are required",
			})
			return
		}

		// Parse timestamps (support both RFC3339 and ISO8601 formats)
		startTime, err := time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			// Try ISO8601 format
			startTime, err = time.Parse("2006-01-02T15:04:05Z", startTimeStr)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": fmt.Sprintf("Invalid start_time format: %v", err),
				})
				return
			}
		}

		endTime, err := time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			// Try ISO8601 format
			endTime, err = time.Parse("2006-01-02T15:04:05Z", endTimeStr)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": fmt.Sprintf("Invalid end_time format: %v", err),
				})
				return
			}
		}

		// Parse pagination parameters
		limitStr := r.URL.Query().Get("limit")
		limit := 2000 // default limit
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		offsetStr := r.URL.Query().Get("offset")
		offset := 0
		if offsetStr != "" {
			if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
				offset = parsedOffset
			}
		}

		// Parse optional source filter
		source := r.URL.Query().Get("source")

		// Query database for decisions within time range
		decisions, err := globalStorage.GetDecisionLogsByTimeRangeWithOffset(startTime, endTime, limit, offset, source)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Database query failed: %v", err),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"decisions":  decisions,
			"total":      len(decisions),
			"limit":      limit,
			"offset":     offset,
			"start_time": startTime,
			"end_time":   endTime,
			"source":     source, // "" means all sources
			"timestamp":  time.Now(),
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
			"decisions": decisions,
			"total":     len(decisions),
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
			"enabled":       globalAgentCoordinator.IsEnabled(),
			"total_agents":  len(agents),
			"active_agents": activeAgents,
			"total_tasks":   totalTasks,
			"agent_details": agents,
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

		var analytics PerformanceAnalytics

		// If time range provided and storage available, query DB
		if startTime, endTime, ok := parseTimeRange(r); ok && globalStorage != nil {
			hours := int(endTime.Sub(startTime).Hours())
			if hours < 1 {
				hours = 1
			}
			dbAnalytics, err := globalStorage.GetPerformanceAnalytics(hours)
			if err == nil && dbAnalytics.TotalRequests > 0 {
				analytics = dbAnalytics
			}
		}

		// Fallback to in-memory
		if analytics.TotalRequests == 0 {
			analytics = performanceMonitor.GetAnalytics()
		}

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

		var metrics []PerformanceMetrics

		// If time range provided and storage available, query DB
		if startTime, endTime, ok := parseTimeRange(r); ok && globalStorage != nil {
			dbMetrics, err := globalStorage.GetPerformanceMetricsByTimeRange(startTime, endTime, limit)
			if err == nil {
				metrics = dbMetrics
			}
		}

		// Fallback to in-memory
		if metrics == nil {
			metrics = performanceMonitor.GetRecentMetrics(limit)
		}

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

		var trends map[string]interface{}

		// If time range provided and storage available, query DB and compute trends
		if startTime, endTime, ok := parseTimeRange(r); ok && globalStorage != nil {
			dbMetrics, err := globalStorage.GetPerformanceMetricsByTimeRange(startTime, endTime, 10000)
			if err == nil && len(dbMetrics) > 0 {
				trends = computeTrendsFromMetrics(dbMetrics, hours)
			}
		}

		// Fallback to in-memory
		if trends == nil {
			trends = performanceMonitor.GetPerformanceTrends(hours)
		}

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

// === Cost Tracking API Handlers (Issue #73) ===

// handleCostsByModel returns cost breakdown by model
func (wi *WebInterface) handleCostsByModel(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Storage is unavailable",
			})
			return
		}

		// Parse query parameters (hours to look back)
		hoursStr := r.URL.Query().Get("hours")
		hours := 168 // default 1 week
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}

		// Query performance metrics for the time range
		startTime := time.Now().Add(-time.Duration(hours) * time.Hour)
		endTime := time.Now()
		metrics, err := globalStorage.GetPerformanceMetricsByTimeRange(startTime, endTime, 10000)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Query failed: %v", err),
			})
			return
		}

		// Aggregate by model
		costsByModel := make(map[string]struct {
			Calls       int     `json:"calls"`
			Tokens      int     `json:"tokens"`
			Cost        float64 `json:"cost"`
			InputTokens int     `json:"input_tokens"`
		})

		for _, metric := range metrics {
			model := metric.Model
			if model == "" {
				model = "unknown"
			}
			stats := costsByModel[model]
			stats.Calls++
			stats.Tokens += metric.TokensUsed
			stats.Cost += metric.EstimatedCost
			costsByModel[model] = stats
		}

		// Return response
		response := map[string]interface{}{
			"period_hours": hours,
			"start_time":   startTime.Format(time.RFC3339),
			"end_time":     endTime.Format(time.RFC3339),
			"by_model":     costsByModel,
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleCostsSummary returns overall cost summary
func (wi *WebInterface) handleCostsSummary(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Storage is unavailable",
			})
			return
		}

		// Parse query parameters (hours to look back)
		hoursStr := r.URL.Query().Get("hours")
		hours := 168 // default 1 week
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}

		// Query performance metrics for the time range
		startTime := time.Now().Add(-time.Duration(hours) * time.Hour)
		endTime := time.Now()
		metrics, err := globalStorage.GetPerformanceMetricsByTimeRange(startTime, endTime, 10000)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Query failed: %v", err),
			})
			return
		}

		// Calculate totals and by-model breakdown
		totalCost := 0.0
		totalCalls := 0
		totalTokens := 0
		costsByModel := make(map[string]struct {
			Calls int     `json:"calls"`
			Cost  float64 `json:"cost"`
		})

		for _, metric := range metrics {
			totalCost += metric.EstimatedCost
			totalCalls += 1
			totalTokens += metric.TokensUsed

			model := metric.Model
			if model == "" {
				model = "unknown"
			}
			stats := costsByModel[model]
			stats.Calls++
			stats.Cost += metric.EstimatedCost
			costsByModel[model] = stats
		}

		// Calculate average cost per call
		avgCostPerCall := 0.0
		if totalCalls > 0 {
			avgCostPerCall = totalCost / float64(totalCalls)
		}

		// Return response
		response := map[string]interface{}{
			"period_hours":      hours,
			"start_time":        startTime.Format(time.RFC3339),
			"end_time":          endTime.Format(time.RFC3339),
			"total_cost":        totalCost,
			"total_calls":       totalCalls,
			"total_tokens":      totalTokens,
			"avg_cost_per_call": avgCostPerCall,
			"by_model":          costsByModel,
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleCostSavings returns cost savings report (Issue #74)
func (wi *WebInterface) handleCostSavings(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if globalStorage == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Storage is unavailable",
			})
			return
		}

		// Parse query parameters (hours to look back)
		hoursStr := r.URL.Query().Get("hours")
		hours := 168 // default 1 week
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}

		// Get cost savings report (support projectPath parameter for filtering)
		projectPath := r.URL.Query().Get("projectPath")
		var report CostSavingsReport
		var err error
		if projectPath != "" {
			report, err = globalStorage.GetCostSavingsByProject(projectPath, hours)
		} else {
			report, err = globalStorage.GetCostSavings(hours)
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("Query failed: %v", err),
			})
			return
		}

		// Return response
		w.Header().Set("Cache-Control", "public, max-age=60") // Cache for 1 minute
		json.NewEncoder(w).Encode(report)
	})(w, r)
}

// === Security API Handlers ===

// handleSecurityEvents returns recent security events with optional time range filtering
func (wi *WebInterface) handleSecurityEvents(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if security.Global() == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Security management is disabled",
			})
			return
		}

		// Parse query parameters
		query := r.URL.Query()
		limitStr := query.Get("limit")
		limit := 50 // default limit
		if limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				limit = parsedLimit
			}
		}

		severity := query.Get("severity")
		startTimeStr := query.Get("start_time")
		endTimeStr := query.Get("end_time")

		var events []security.SecurityEvent
		var err error

		// Check if time range is provided
		if startTimeStr != "" && endTimeStr != "" && globalStorage != nil {
			// Parse time parameters
			startTime, err1 := time.Parse(time.RFC3339, startTimeStr)
			endTime, err2 := time.Parse(time.RFC3339, endTimeStr)

			if err1 == nil && err2 == nil {
				// Query by time range
				events, err = globalStorage.GetSecurityEventsByTimeRange(startTime, endTime, limit)
				if err != nil {
					log.Printf("Failed to get events by time range: %v", err)
					events = []security.SecurityEvent{}
				}
			} else {
				// Fall back to manager if time parsing fails
				events = security.Global().GetSecurityEvents(limit, severity)
			}
		} else if globalStorage != nil {
			// Query all events from database (no time filter)
			events, err = globalStorage.GetSecurityEvents(limit, 0)
			if err != nil {
				log.Printf("Failed to get events from database: %v", err)
				events = security.Global().GetSecurityEvents(limit, severity)
			}
		} else {
			// Fallback to manager
			events = security.Global().GetSecurityEvents(limit, severity)
		}

		// Filter by severity if specified
		if severity != "" && events != nil {
			var filtered []security.SecurityEvent
			for _, e := range events {
				if e.Severity == severity {
					filtered = append(filtered, e)
				}
			}
			events = filtered
		}

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

		if security.Global() == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Security management is disabled",
			})
			return
		}

		// Parse time range parameters
		query := r.URL.Query()
		startTimeStr := query.Get("start_time")
		endTimeStr := query.Get("end_time")

		totalEvents := 0
		blockedAttempts := 0
		piiDetections := 0
		threatLevel := "low"

		// Check if time range is provided and storage is available
		if startTimeStr != "" && endTimeStr != "" && globalStorage != nil {
			var startTime, endTime time.Time
			var err error

			// Parse time parameters if provided
			if startTimeStr != "" {
				startTime, err = time.Parse(time.RFC3339, startTimeStr)
				if err != nil {
					log.Printf("Invalid start_time format: %v", err)
					startTime = time.Now().Add(-24 * time.Hour)
				}
			} else {
				startTime = time.Now().Add(-24 * time.Hour)
			}

			if endTimeStr != "" {
				endTime, err = time.Parse(time.RFC3339, endTimeStr)
				if err != nil {
					log.Printf("Invalid end_time format: %v", err)
					endTime = time.Now()
				}
			} else {
				endTime = time.Now()
			}

			// Query by time range
			events, err := globalStorage.GetSecurityEventsByTimeRange(startTime, endTime, 10000)
			if err != nil {
				log.Printf("Failed to get events by time range: %v", err)
			} else {
				totalEvents = len(events)

				// Count event types from results
				eventTypes := make(map[string]int)
				severities := make(map[string]int)

				for _, event := range events {
					eventTypes[event.EventType]++
					severities[event.Severity]++

					// Count blocked attempts and PII detections
					if event.EventType == "rate_limit_exceeded" || event.EventType == "blocked_ip_access" || event.EventType == "unauthorized_ip_access" {
						blockedAttempts++
					}
					if len(event.EventType) >= 3 && event.EventType[:3] == "pii" {
						piiDetections++
					}
				}

				// Determine threat level
				if severities["critical"] > 0 {
					threatLevel = "critical"
				} else if severities["high"] > 5 {
					threatLevel = "high"
				} else if severities["medium"] > 0 {
					threatLevel = "medium"
				}

				stats := map[string]interface{}{
					"enabled":          true,
					"total_events":     totalEvents,
					"blocked_attempts": blockedAttempts,
					"pii_detections":   piiDetections,
					"threat_level":     threatLevel,
					"event_types":      eventTypes,
					"severities":       severities,
					"timestamp":        time.Now(),
				}

				json.NewEncoder(w).Encode(stats)
				return
			}
		}

		// Fallback to manager stats (no time filter)
		stats := security.Global().GetSecurityStats()
		stats["enabled"] = true
		stats["timestamp"] = time.Now()

		json.NewEncoder(w).Encode(stats)
	})(w, r)
}

// handleSecurityAudit provides detailed security audit information
func (wi *WebInterface) handleSecurityAudit(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if security.Global() == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Security management is disabled",
			})
			return
		}

		// Get events by severity
		critical := security.Global().GetSecurityEvents(10, "critical")
		high := security.Global().GetSecurityEvents(20, "high")
		medium := security.Global().GetSecurityEvents(30, "medium")
		low := security.Global().GetSecurityEvents(10, "low")

		stats := security.Global().GetSecurityStats()

		response := map[string]interface{}{
			"enabled": true,
			"summary": stats,
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
	if security.Global() != nil {
		stats := security.Global().GetSecurityStats()

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

// ==================== Checkpoint API Handlers ====================

// handleCheckpoints lists checkpoints for a project
func (wi *WebInterface) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			wi.handleListCheckpoints(w, r)
		case http.MethodDelete:
			wi.handleDeleteCheckpoint(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})(w, r)
}

// handleListCheckpoints lists checkpoints for a project
func (wi *WebInterface) handleListCheckpoints(w http.ResponseWriter, r *http.Request) {
	// Get query parameters
	projectDir := r.URL.Query().Get("project_dir")
	if projectDir == "" {
		http.Error(w, "project_dir parameter is required", http.StatusBadRequest)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20 // Default limit
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	// Parse time range parameters
	startTimeStr := r.URL.Query().Get("start_time")
	endTimeStr := r.URL.Query().Get("end_time")

	var startTime, endTime time.Time
	var err error

	// Parse time parameters if provided
	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			http.Error(w, "Invalid start_time format. Use RFC3339.", http.StatusBadRequest)
			return
		}
	}
	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			http.Error(w, "Invalid end_time format. Use RFC3339.", http.StatusBadRequest)
			return
		}
	}

	// Check if checkpoint manager is available
	if globalCheckpointManager == nil {
		http.Error(w, "Checkpoint manager not available", http.StatusServiceUnavailable)
		return
	}

	// Get checkpoints with time range support
	var checkpoints []Checkpoint
	if startTimeStr != "" && endTimeStr != "" {
		checkpoints, err = globalCheckpointManager.ListCheckpointsByTimeRange(projectDir, startTime, endTime, limit)
	} else {
		checkpoints, err = globalCheckpointManager.ListCheckpoints(projectDir, limit)
	}

	if err != nil {
		log.Printf("Error listing checkpoints: %v", err)
		http.Error(w, "Failed to list checkpoints", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"checkpoints": checkpoints,
		"total_count": len(checkpoints),
		"project_dir": projectDir,
		"limit":       limit,
		"timestamp":   time.Now(),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding checkpoints response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleDeleteCheckpoint deletes a specific checkpoint
func (wi *WebInterface) handleDeleteCheckpoint(w http.ResponseWriter, r *http.Request) {
	checkpointID := r.URL.Query().Get("id")
	if checkpointID == "" {
		http.Error(w, "checkpoint id parameter is required", http.StatusBadRequest)
		return
	}

	// Check if checkpoint manager is available
	if globalCheckpointManager == nil {
		http.Error(w, "Checkpoint manager not available", http.StatusServiceUnavailable)
		return
	}

	// Delete checkpoint
	if err := globalCheckpointManager.DeleteCheckpoint(checkpointID); err != nil {
		log.Printf("Error deleting checkpoint %s: %v", checkpointID, err)
		http.Error(w, "Failed to delete checkpoint", http.StatusInternalServerError)
		return
	}

	// Broadcast WebSocket event for real-time monitoring
	if globalWebSocketHub != nil {
		deleteEvent := map[string]interface{}{
			"checkpoint_id": checkpointID,
			"timestamp":     time.Now(),
			"action":        "deleted",
		}
		globalWebSocketHub.BroadcastEvent("checkpoint_deleted", deleteEvent)
	}

	response := map[string]interface{}{
		"success":       true,
		"checkpoint_id": checkpointID,
		"timestamp":     time.Now(),
	}

	json.NewEncoder(w).Encode(response)
}

// handleCreateCheckpoint creates a new checkpoint
func (wi *WebInterface) handleCreateCheckpoint(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req struct {
			ProjectDir    string `json:"project_dir"`
			Description   string `json:"description"`
			SessionID     string `json:"session_id,omitempty"`
			ChatID        int64  `json:"chat_id,omitempty"`
			DecisionLogID string `json:"decision_log_id,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.ProjectDir == "" {
			http.Error(w, "project_dir is required", http.StatusBadRequest)
			return
		}

		if req.Description == "" {
			req.Description = "Manual checkpoint created via API"
		}

		// Check if checkpoint manager is available
		if globalCheckpointManager == nil {
			http.Error(w, "Checkpoint manager not available", http.StatusServiceUnavailable)
			return
		}

		// Create checkpoint
		checkpoint, err := globalCheckpointManager.CreateCheckpoint(
			req.ProjectDir,
			req.Description,
			TriggerManual,
			req.SessionID,
			req.ChatID,
			"",                // No dangerous operation for manual checkpoints
			req.DecisionLogID, // Use decision_log_id from request
		)
		if err != nil {
			log.Printf("Error creating checkpoint: %v", err)
			http.Error(w, fmt.Sprintf("Failed to create checkpoint: %v", err), http.StatusInternalServerError)
			return
		}

		// Broadcast WebSocket event for real-time monitoring
		if globalWebSocketHub != nil {
			checkpointEvent := map[string]interface{}{
				"checkpoint_id": checkpoint.ID,
				"project_dir":   checkpoint.ProjectDir,
				"description":   checkpoint.Description,
				"trigger_type":  checkpoint.TriggerType,
				"session_id":    checkpoint.SessionID,
				"chat_id":       checkpoint.ChatID,
				"timestamp":     checkpoint.Timestamp,
				"size":          checkpoint.Size,
			}
			globalWebSocketHub.BroadcastEvent("checkpoint_created", checkpointEvent)
		}

		response := map[string]interface{}{
			"success":    true,
			"checkpoint": checkpoint,
			"timestamp":  time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleRestoreCheckpoint restores a project to a checkpoint
func (wi *WebInterface) handleRestoreCheckpoint(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req struct {
			CheckpointID string `json:"checkpoint_id"`
			ProjectDir   string `json:"project_dir"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.CheckpointID == "" {
			http.Error(w, "checkpoint_id is required", http.StatusBadRequest)
			return
		}

		if req.ProjectDir == "" {
			http.Error(w, "project_dir is required", http.StatusBadRequest)
			return
		}

		// Check if checkpoint manager is available
		if globalCheckpointManager == nil {
			http.Error(w, "Checkpoint manager not available", http.StatusServiceUnavailable)
			return
		}

		// Restore checkpoint
		if err := globalCheckpointManager.RestoreCheckpoint(req.CheckpointID, req.ProjectDir); err != nil {
			log.Printf("Error restoring checkpoint %s: %v", req.CheckpointID, err)
			http.Error(w, fmt.Sprintf("Failed to restore checkpoint: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"success":       true,
			"checkpoint_id": req.CheckpointID,
			"project_dir":   req.ProjectDir,
			"timestamp":     time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleCheckpointStats returns checkpoint statistics
func (wi *WebInterface) handleCheckpointStats(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get query parameters
		projectDir := r.URL.Query().Get("project_dir")
		if projectDir == "" {
			http.Error(w, "project_dir parameter is required", http.StatusBadRequest)
			return
		}

		// Check if checkpoint manager is available
		if globalCheckpointManager == nil {
			http.Error(w, "Checkpoint manager not available", http.StatusServiceUnavailable)
			return
		}

		// Get checkpoint statistics
		stats, err := globalCheckpointManager.GetCheckpointStats(projectDir)
		if err != nil {
			log.Printf("Error getting checkpoint stats: %v", err)
			http.Error(w, "Failed to get checkpoint statistics", http.StatusInternalServerError)
			return
		}

		// Add manager status
		stats["manager_enabled"] = globalCheckpointManager.IsEnabled()
		stats["project_dir"] = projectDir
		stats["timestamp"] = time.Now()

		json.NewEncoder(w).Encode(stats)
	})(w, r)
}

// handleAgentReset resets an agent's conversation history
func (wi *WebInterface) handleAgentReset(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req struct {
			ChatID   int64 `json:"chat_id"`
			ThreadID int   `json:"thread_id,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.ChatID == 0 {
			http.Error(w, "chat_id is required", http.StatusBadRequest)
			return
		}

		// Find the agent
		key := chatKey{chatID: req.ChatID, threadID: req.ThreadID}
		wi.bot.agentsMu.RLock()
		agent, exists := wi.bot.agents[key]
		wi.bot.agentsMu.RUnlock()

		if !exists {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}

		// Get stats before reset
		stats := agent.Stats()

		// Reset the agent
		agent.Reset()

		// Broadcast WebSocket event for real-time monitoring
		if globalWebSocketHub != nil {
			resetEvent := map[string]interface{}{
				"chat_id":                req.ChatID,
				"thread_id":              req.ThreadID,
				"previous_api_calls":     stats.APICallCount,
				"previous_input_tokens":  stats.TotalInputTokens,
				"previous_output_tokens": stats.TotalOutputTokens,
				"timestamp":              time.Now(),
			}
			globalWebSocketHub.BroadcastEvent("agent_reset", resetEvent)
		}

		response := map[string]interface{}{
			"success":   true,
			"chat_id":   req.ChatID,
			"thread_id": req.ThreadID,
			"message":   "Agent conversation history reset successfully",
			"previous_stats": map[string]interface{}{
				"api_calls":     stats.APICallCount,
				"input_tokens":  stats.TotalInputTokens,
				"output_tokens": stats.TotalOutputTokens,
			},
			"timestamp": time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleAgentSetProject switches an agent's project directory
func (wi *WebInterface) handleAgentSetProject(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req struct {
			ChatID     int64  `json:"chat_id"`
			ThreadID   int    `json:"thread_id,omitempty"`
			ProjectDir string `json:"project_dir"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.ChatID == 0 {
			http.Error(w, "chat_id is required", http.StatusBadRequest)
			return
		}

		if req.ProjectDir == "" {
			http.Error(w, "project_dir is required", http.StatusBadRequest)
			return
		}

		// Validate project directory exists
		if _, err := os.Stat(req.ProjectDir); os.IsNotExist(err) {
			http.Error(w, "Project directory does not exist", http.StatusBadRequest)
			return
		}

		// Find the agent
		key := chatKey{chatID: req.ChatID, threadID: req.ThreadID}
		wi.bot.agentsMu.RLock()
		agent, exists := wi.bot.agents[key]
		wi.bot.agentsMu.RUnlock()

		if !exists {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}

		// Get previous project directory
		previousProjectDir := agent.ProjectDir()

		// Set new project directory
		agent.SetProject(req.ProjectDir)

		// Broadcast WebSocket event for real-time monitoring
		if globalWebSocketHub != nil {
			projectChangeEvent := map[string]interface{}{
				"chat_id":              req.ChatID,
				"thread_id":            req.ThreadID,
				"previous_project_dir": previousProjectDir,
				"new_project_dir":      req.ProjectDir,
			}

			globalWebSocketHub.BroadcastEvent("agent_project_changed", projectChangeEvent)
		}

		response := map[string]interface{}{
			"success":              true,
			"chat_id":              req.ChatID,
			"thread_id":            req.ThreadID,
			"previous_project_dir": previousProjectDir,
			"new_project_dir":      req.ProjectDir,
			"message":              fmt.Sprintf("Agent project directory changed from %s to %s", previousProjectDir, req.ProjectDir),
			"timestamp":            time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleModelRoutingStatus returns the current model routing configuration and preferences
func (wi *WebInterface) handleModelRoutingStatus(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get all agents and their model preferences
		agents := wi.bot.GetAgentsSafely()
		preferences := make(map[string]interface{})

		for key := range agents {
			chatKey := fmt.Sprintf("chat_%d_thread_%d", key.chatID, key.threadID)
			pref := wi.bot.getUserModelPreference(key)

			// Determine display mode
			var mode string
			var modelName string
			switch pref {
			case "fast":
				mode = "fast"
				modelName = wi.bot.config.ModelRouting.FastModel
			case "smart":
				mode = "smart"
				modelName = wi.bot.config.ModelRouting.SmartModel
			case "deep":
				mode = "deep"
				modelName = wi.bot.config.ModelRouting.DeepModel
			case "gpt-fast":
				mode = "gpt-fast"
				modelName = wi.bot.config.ModelRouting.CodexFastModel
			case "gpt-smart":
				mode = "gpt-smart"
				modelName = wi.bot.config.ModelRouting.CodexSmartModel
			case "gpt-deep":
				mode = "gpt-deep"
				modelName = wi.bot.config.ModelRouting.CodexDeepModel
			case "plan":
				mode = "plan"
				modelName = fmt.Sprintf("%s → %s", wi.bot.config.ModelRouting.PlanModel, wi.bot.config.ModelRouting.ExecuteModel)
			case "":
				mode = "auto"
				modelName = wi.bot.config.Model
			default:
				mode = "custom"
				modelName = pref
			}

			preferences[chatKey] = map[string]interface{}{
				"mode":  mode,
				"model": modelName,
			}
		}

		status := map[string]interface{}{
			"enabled":                   wi.bot.config.ModelRouting.EnableDynamicRouting,
			"fast_model":                wi.bot.config.ModelRouting.FastModel,
			"smart_model":               wi.bot.config.ModelRouting.SmartModel,
			"deep_model":                wi.bot.config.ModelRouting.DeepModel,
			"default_model":             wi.bot.config.Model,
			"use_gpt4o_mini_for_triage": wi.bot.config.ModelRouting.UseGPT4oMini,
			"total_chats":               len(agents),
			"preferences":               preferences,
			"timestamp":                 time.Now(),
		}

		json.NewEncoder(w).Encode(status)
	})(w, r)
}

// handleModelRoutingSet sets the model routing preference for a specific chat
func (wi *WebInterface) handleModelRoutingSet(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		// Check authentication token if configured
		if wi.apiToken != "" && r.Header.Get("Authorization") != "Bearer "+wi.apiToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		var req struct {
			ChatID   int64  `json:"chat_id"`
			ThreadID int    `json:"thread_id"`
			Mode     string `json:"mode"` // "fast", "smart", "deep", "gpt-fast", "gpt-smart", "gpt-deep", "plan", "auto", or a model name
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
			return
		}

		req.Mode = strings.TrimSpace(req.Mode)
		if req.Mode == "" {
			http.Error(w, "mode is required", http.StatusBadRequest)
			return
		}

		// Check if dynamic routing is enabled
		if !wi.bot.config.ModelRouting.EnableDynamicRouting {
			http.Error(w, "Dynamic model routing is not enabled", http.StatusBadRequest)
			return
		}

		key := chatKey{chatID: req.ChatID, threadID: req.ThreadID}

		// Determine what to set
		var modeValue string
		var modelName string
		switch req.Mode {
		case "fast":
			modeValue = "fast"
			modelName = wi.bot.config.ModelRouting.FastModel
		case "smart":
			modeValue = "smart"
			modelName = wi.bot.config.ModelRouting.SmartModel
		case "deep":
			modeValue = "deep"
			modelName = wi.bot.config.ModelRouting.DeepModel
		case "gpt-fast":
			modeValue = "gpt-fast"
			modelName = wi.bot.config.ModelRouting.CodexFastModel
		case "gpt-smart":
			modeValue = "gpt-smart"
			modelName = wi.bot.config.ModelRouting.CodexSmartModel
		case "gpt-deep":
			modeValue = "gpt-deep"
			modelName = wi.bot.config.ModelRouting.CodexDeepModel
		case "plan":
			modeValue = "plan"
			modelName = fmt.Sprintf("%s → %s", wi.bot.config.ModelRouting.PlanModel, wi.bot.config.ModelRouting.ExecuteModel)
		case "auto":
			modeValue = "" // Empty string means auto mode
			modelName = wi.bot.config.Model
		default:
			modeValue = req.Mode
			modelName = req.Mode
		}

		wi.bot.setUserModelPreference(key, modeValue)

		response := map[string]interface{}{
			"success":   true,
			"chat_id":   req.ChatID,
			"thread_id": req.ThreadID,
			"mode":      req.Mode,
			"model":     modelName,
			"message":   fmt.Sprintf("Model routing mode set to %s (%s)", req.Mode, modelName),
			"timestamp": time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}

// handleLanguage handles language preference updates (Issue #76)
func (wi *WebInterface) handleLanguage(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req struct {
			Language string `json:"language"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate language code
		if req.Language != "en" && req.Language != "zh-TW" {
			http.Error(w, "Unsupported language", http.StatusBadRequest)
			return
		}

		// Get chat ID from query parameter or use default (0 for anonymous users)
		var chatID int64 = 0
		if chatIDStr := r.URL.Query().Get("chat_id"); chatIDStr != "" {
			var err error
			if chatID, err = strconv.ParseInt(chatIDStr, 10, 64); err != nil {
				// If chat_id is not valid, use 0 (frontend-only storage)
				chatID = 0
			}
		}

		// If we have a valid chat_id, persist it in the bot's language manager
		if chatID != 0 {
			wi.bot.setChatlanguage(chatID, req.Language)
		}

		response := map[string]interface{}{
			"success":   true,
			"language":  req.Language,
			"chat_id":   chatID,
			"message":   "Language preference updated",
			"timestamp": time.Now(),
		}

		json.NewEncoder(w).Encode(response)
	})(w, r)
}
