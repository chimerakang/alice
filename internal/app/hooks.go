package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ClaudeCodeHookPayload represents the structured payload sent by the hook script
type ClaudeCodeHookPayload struct {
	SessionID       string         `json:"session_id"`
	Event           string         `json:"event"`  // "session_complete", "session_active"
	Source          string         `json:"source"` // "terminal", "vscode", "unknown"
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

// UserPromptSubmitPayload is the subset of Claude Code's UserPromptSubmit hook
// payload that Alice records for the #104 VS Code interception experiment.
// Fields follow the Claude Code hook schema; unknown fields are ignored.
type UserPromptSubmitPayload struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"`
	CWD            string `json:"cwd"`
	Prompt         string `json:"prompt"`
	TranscriptPath string `json:"transcript_path"`
	Source         string `json:"source"` // optional, script may inject vscode/terminal
}

// vscodeBlockMode resolves the current Phase 2 behavior for complex prompts.
// Controlled by env var ALICE_VSCODE_BLOCK_MODE:
//
//	off    (default) — Phase 1 observe only, no blocking.
//	dryrun           — log "would block" decisions without sending block to Claude Code.
//	on               — return {"decision":"block"} for complex classifications.
//
// Dry-run is a low-risk way to watch real blocking decisions accumulate in the
// log before committing to hard block mode.
func vscodeBlockMode() string {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv("ALICE_VSCODE_BLOCK_MODE")))
	switch mode {
	case "on", "dryrun":
		return mode
	default:
		return "off"
	}
}

// handleUserPromptSubmit receives UserPromptSubmit hook events. Phase 1 of
// issue #104 classifies the prompt via the Complexity Gate heuristic and
// persists the decision. Phase 2 (gated by ALICE_VSCODE_BLOCK_MODE) may
// return {"decision":"block"} for complex prompts, redirecting the user to
// Alice's Hermes orchestration via Telegram.
func (wi *WebInterface) handleUserPromptSubmit(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var payload UserPromptSubmitPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		if payload.SessionID == "" {
			http.Error(w, `{"error":"session_id is required"}`, http.StatusBadRequest)
			return
		}

		source := payload.Source
		if source == "" {
			source = "unknown"
		}

		result := ClassifyComplexity(payload.Prompt)
		snippet := payload.Prompt
		if len([]rune(snippet)) > 200 {
			runes := []rune(snippet)
			snippet = string(runes[:200])
		}

		if globalStorage != nil {
			if sqliteStorage, ok := globalStorage.(*SQLiteStorage); ok {
				rec := PromptClassificationRecord{
					SessionID:      payload.SessionID,
					Timestamp:      time.Now(),
					Source:         source,
					CWD:            payload.CWD,
					PromptSnippet:  snippet,
					PromptLength:   len([]rune(payload.Prompt)),
					Classification: string(result.Complexity),
					MatchedRule:    result.MatchedRule,
				}
				if err := sqliteStorage.RecordPromptClassification(rec); err != nil {
					log.Printf("[hooks] prompt classification record failed: %v", err)
				}
			}
		}

		mode := vscodeBlockMode()
		shouldBlock := result.Complexity == ComplexityComplex && mode == "on"
		wouldBlock := result.Complexity == ComplexityComplex && mode == "dryrun"

		log.Printf("[hooks] UserPromptSubmit session=%s source=%s classification=%s rule=%s len=%d mode=%s block=%t dryrun=%t",
			payload.SessionID, source, result.Complexity, result.MatchedRule, len([]rune(payload.Prompt)),
			mode, shouldBlock, wouldBlock)

		if shouldBlock {
			reason := "Alice Hermes: 判定為 complex 任務。請改用 Telegram 執行 /hermes 以啟動 Planner-Executor 協作。"
			if custom := os.Getenv("ALICE_VSCODE_BLOCK_REASON"); custom != "" {
				reason = custom
			}
			// Phase 2: broadcast block event via WebSocket so the dashboard and any
			// registered listeners (e.g. future TG notifier) can surface the intercept.
			if globalWebSocketHub != nil {
				globalWebSocketHub.BroadcastEvent("hook_prompt_blocked", map[string]interface{}{
					"session_id":     payload.SessionID,
					"source":         source,
					"classification": string(result.Complexity),
					"matched_rule":   result.MatchedRule,
					"reason":         reason,
					"timestamp":      time.Now(),
				})
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"decision": "block",
				"reason":   reason,
			})
			return
		}

		// Observe mode / dry-run: return empty object so Claude Code proceeds unchanged.
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})(w, r)
}

func (wi *WebInterface) handleCodexSessionUpdate(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var payload CodexSessionUpdatePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.SessionID) == "" && strings.TrimSpace(payload.ThreadID) == "" && strings.TrimSpace(payload.SessionPath) == "" {
			http.Error(w, `{"error":"session_id, thread_id, or session_path is required"}`, http.StatusBadRequest)
			return
		}
		if payload.SessionID == "" {
			payload.SessionID = payload.ThreadID
		}
		if payload.SessionID == "" {
			payload.SessionID = strings.TrimSuffix(filepath.Base(payload.SessionPath), filepath.Ext(payload.SessionPath))
		}

		recordCodexSessionUpdate(r.Context(), payload)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"session": payload.SessionID,
			"event":   payload.Event,
		})
	})(w, r)
}

// handlePromptClassifications serves GET /api/hooks/prompt-classifications.
// Returns recent PromptClassificationRecord rows (newest first) so the dashboard
// can visualise classifier accuracy for Issue #104 Phase 1 evaluation.
func (wi *WebInterface) handlePromptClassifications(w http.ResponseWriter, r *http.Request) {
	wi.handleWithRecovery(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		if globalStorage == nil {
			json.NewEncoder(w).Encode([]PromptClassificationRecord{})
			return
		}
		sqliteStorage, ok := globalStorage.(*SQLiteStorage)
		if !ok {
			http.Error(w, `{"error":"storage unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		recs, err := sqliteStorage.ListPromptClassifications(limit)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		if recs == nil {
			recs = []PromptClassificationRecord{}
		}
		json.NewEncoder(w).Encode(recs)
	})(w, r)
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
