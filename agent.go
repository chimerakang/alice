package main

import (
	"fmt"
	"log"
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
func (a *Agent) Run(userMessage string, onUpdate func(string)) (string, error) {
	if onUpdate != nil {
		onUpdate("🔧 Claude Code 處理中 ...")
	}

	ps := a.current()
	log.Printf("[agent] calling CLI, session=%s, project=%s", ps.sessionID, a.projectDir)

	resp, err := a.client.Call(userMessage, a.projectDir, ps.sessionID)
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
