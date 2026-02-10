package main

import (
	"fmt"
	"log"
	"path/filepath"
)

// TokenStats tracks cumulative token usage for an agent session.
type TokenStats struct {
	TotalInputTokens  int64   // 累計 input tokens
	TotalOutputTokens int64   // 累計 output tokens
	TotalCostUSD      float64 // 累計費用（Max 訂閱下為 0）
	APICallCount      int     // CLI 呼叫次數
}

// projectState 保存單一專案的對話狀態
type projectState struct {
	sessionID string
	stats     TokenStats
}

type Agent struct {
	client     *CLIClient
	projectDir string
	projects   map[string]*projectState // projectDir → state
}

func NewAgent(client *CLIClient, projectDir string) *Agent {
	return &Agent{
		client:     client,
		projectDir: projectDir,
		projects:   make(map[string]*projectState),
	}
}

// current 取得目前專案的狀態，不存在則建立
func (a *Agent) current() *projectState {
	if ps, ok := a.projects[a.projectDir]; ok {
		return ps
	}
	ps := &projectState{}
	a.projects[a.projectDir] = ps
	return ps
}

// Run sends a message to Claude Code CLI and returns the response text.
// onUpdate(msg, silent): silent=false for initial status, silent=true for tool updates.
func (a *Agent) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	if onUpdate != nil {
		onUpdate("🔧 Claude Code 處理中 ...", false)
	}

	ps := a.current()
	log.Printf("[agent] calling CLI (stream), session=%s, project=%s", ps.sessionID, a.projectDir)

	resp, err := a.client.CallStream(userMessage, a.projectDir, ps.sessionID, func(toolName string, toolInput map[string]interface{}) {
		if onUpdate == nil {
			return
		}
		msg := formatToolUpdate(toolName, toolInput)
		if msg != "" {
			onUpdate(msg, true)
		}
	})
	if err != nil {
		return "", fmt.Errorf("CLI call failed: %w", err)
	}

	// 保存 session ID 以便下次 --resume
	ps.sessionID = resp.SessionID

	// 更新統計
	ps.stats.APICallCount++
	ps.stats.TotalInputTokens += int64(resp.Usage.InputTokens)
	ps.stats.TotalOutputTokens += int64(resp.Usage.OutputTokens)
	ps.stats.TotalCostUSD += resp.TotalCostUSD

	log.Printf("[agent] done: turns=%d tokens_in=%d tokens_out=%d cost=$%.4f session=%s",
		resp.NumTurns, resp.Usage.InputTokens, resp.Usage.OutputTokens,
		resp.TotalCostUSD, resp.SessionID)

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
