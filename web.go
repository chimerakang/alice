package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

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

// getBasicStats collects basic statistics from the Telegram bot
func (wi *WebInterface) getBasicStats() map[string]interface{} {
	wi.mu.RLock()
	defer wi.mu.RUnlock()

	// Get agent count safely using thread-safe method
	agentCount := 0
	if wi.bot != nil {
		agentCount = wi.bot.GetAgentCount()
	}

	return map[string]interface{}{
		"active_sessions":   agentCount,
		"tools_executed":    0, // Will implement in Phase 2
		"projects":         agentCount, // Each agent represents a project context
		"success_rate":     100.0,
		"uptime_seconds":   time.Now().Unix(),
		"timestamp":        time.Now(),
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