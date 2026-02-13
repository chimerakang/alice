package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// ClaudeCodeHookPayload represents the structured payload sent by the hook script
type ClaudeCodeHookPayload struct {
	SessionID       string         `json:"session_id"`
	Event           string         `json:"event"`            // "session_complete", "session_active"
	Source          string         `json:"source"`           // "terminal", "vscode", "unknown"
	TranscriptPath  string         `json:"transcript_path"`
	ProjectDir      string         `json:"project_dir"`
	UserPrompt      string         `json:"user_prompt,omitempty"`
	AgentResponse   string         `json:"agent_response,omitempty"`
	ThinkingContent string         `json:"thinking_content,omitempty"`
	ToolCalls       []HookToolCall `json:"tool_calls,omitempty"`
	DurationMs      int            `json:"duration_ms,omitempty"`
	TokensInput     int64          `json:"tokens_input,omitempty"`
	TokensOutput    int64          `json:"tokens_output,omitempty"`
	FilesChanged    []string       `json:"files_changed,omitempty"`
	Success         bool           `json:"success"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	GitCommitHash   string         `json:"git_commit_hash,omitempty"`
	GitBranch       string         `json:"git_branch,omitempty"`
	Timestamp       string         `json:"timestamp,omitempty"`
}

// HookToolCall represents a tool call from the Claude Code transcript
type HookToolCall struct {
	ToolName   string                 `json:"tool_name"`
	Input      map[string]interface{} `json:"input,omitempty"`
	Status     string                 `json:"status"`
	DurationMs int64                  `json:"duration_ms,omitempty"`
}

// handleClaudeCodeHook receives hook events from the Claude Code hook script
func (wi *WebInterface) handleClaudeCodeHook(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var payload ClaudeCodeHookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		if payload.SessionID == "" {
			http.Error(w, `{"error":"session_id is required"}`, http.StatusBadRequest)
			return
		}

		switch payload.Event {
		case "session_complete":
			wi.handleHookSessionComplete(w, payload)
		case "session_active":
			wi.handleHookSessionActive(w, payload)
		default:
			http.Error(w, fmt.Sprintf(`{"error":"unknown event type: %s"}`, payload.Event), http.StatusBadRequest)
		}
	})(w, r)
}

// handleHookSessionComplete processes a completed Claude Code session
func (wi *WebInterface) handleHookSessionComplete(w http.ResponseWriter, payload ClaudeCodeHookPayload) {
	// Upsert: if session_id already exists, delete old record and re-insert with latest data.
	// Claude Code resumes sessions with the same ID, so Stop hooks may fire multiple times
	// for the same session with updated transcript data.
	if globalStorage != nil {
		if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
			count, err := sqliteStorage.GetDecisionLogCountBySessionID(payload.SessionID)
			if err == nil && count > 0 {
				log.Printf("[hooks] Updating existing session %s (%d old records)", payload.SessionID, count)
				if delErr := sqliteStorage.DeleteDecisionLogsBySessionID(payload.SessionID); delErr != nil {
					log.Printf("[hooks] Warning: failed to delete old records for session %s: %v", payload.SessionID, delErr)
				}
			}
		}
	}

	source := payload.Source
	if source == "" {
		source = "unknown"
	}

	timestamp := time.Now()
	if payload.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339, payload.Timestamp); err == nil {
			timestamp = parsed.Local() // Normalize to local timezone for consistent SQLite sorting
		}
	}

	// Convert HookToolCalls to ToolExecution
	toolCalls := make([]ToolExecution, len(payload.ToolCalls))
	for i, tc := range payload.ToolCalls {
		toolCalls[i] = ToolExecution{
			Timestamp: timestamp,
			ToolName:  tc.ToolName,
			Input:     tc.Input,
			Status:    tc.Status,
			Duration:  time.Duration(tc.DurationMs) * time.Millisecond,
		}
	}

	taskType := inferTaskType(payload.UserPrompt, toolCalls)
	filesChanged := extractFilesChanged(toolCalls)
	if len(filesChanged) == 0 && len(payload.FilesChanged) > 0 {
		filesChanged = payload.FilesChanged
	}

	decision := DecisionLog{
		Timestamp:       timestamp,
		SessionID:       payload.SessionID,
		ProjectPath:     payload.ProjectDir,
		ChatID:          0, // Not from Telegram
		ThreadID:        0,
		UserPrompt:      payload.UserPrompt,
		AgentResponse:   payload.AgentResponse,
		ThinkingContent: payload.ThinkingContent,
		ToolCalls:       toolCalls,
		Context: map[string]interface{}{
			"source":          source,
			"transcript_path": payload.TranscriptPath,
			"hook_event":      "session_complete",
		},
		Outcome: ExecutionOutcome{
			Success:      payload.Success,
			ErrorMessage: payload.ErrorMessage,
			TaskType:     taskType,
			FilesChanged: filesChanged,
			Summary:      generateSummary(payload.UserPrompt, payload.AgentResponse, taskType),
		},
		DurationMs: payload.DurationMs,
		TokensUsed: TokenStats{
			TotalInputTokens:  payload.TokensInput,
			TotalOutputTokens: payload.TokensOutput,
		},
		GitCommitHash: payload.GitCommitHash,
		GitBranch:     payload.GitBranch,
		Source:        source,
	}

	// Log through standard decision logger (handles DB + memory + WebSocket broadcast)
	globalDecisionLogger.LogDecision(decision)

	log.Printf("[hooks] Session %s from %s logged (%d tools, %dms)",
		payload.SessionID, source, len(toolCalls), payload.DurationMs)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"session": payload.SessionID,
		"source":  source,
	})
}

// handleHookSessionActive handles a lightweight "session active" ping
func (wi *WebInterface) handleHookSessionActive(w http.ResponseWriter, payload ClaudeCodeHookPayload) {
	if globalWebSocketHub != nil {
		globalWebSocketHub.BroadcastEvent("hook_session_active", map[string]interface{}{
			"session_id":  payload.SessionID,
			"source":      payload.Source,
			"project_dir": payload.ProjectDir,
			"user_prompt": payload.UserPrompt,
			"timestamp":   time.Now(),
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"session": payload.SessionID,
	})
}
