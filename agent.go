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

type Agent struct {
	client     *CLIClient
	projectDir string
	sessionID  string // CLI session ID，用於 --resume 延續對話
	stats      TokenStats
}

func NewAgent(client *CLIClient, projectDir string) *Agent {
	return &Agent{
		client:     client,
		projectDir: projectDir,
	}
}

// Run sends a message to Claude Code CLI and returns the response text.
func (a *Agent) Run(userMessage string, onUpdate func(string)) (string, error) {
	if onUpdate != nil {
		onUpdate("🔧 Claude Code 處理中 ...")
	}

	log.Printf("[agent] calling CLI, session=%s, project=%s", a.sessionID, a.projectDir)

	resp, err := a.client.Call(userMessage, a.projectDir, a.sessionID)
	if err != nil {
		return "", fmt.Errorf("CLI call failed: %w", err)
	}

	// 保存 session ID 以便下次 --resume
	a.sessionID = resp.SessionID

	// 更新統計
	a.stats.APICallCount++
	a.stats.TotalInputTokens += int64(resp.Usage.InputTokens)
	a.stats.TotalOutputTokens += int64(resp.Usage.OutputTokens)
	a.stats.TotalCostUSD += resp.TotalCostUSD

	log.Printf("[agent] done: turns=%d tokens_in=%d tokens_out=%d cost=$%.4f session=%s",
		resp.NumTurns, resp.Usage.InputTokens, resp.Usage.OutputTokens,
		resp.TotalCostUSD, resp.SessionID)

	return resp.Result, nil
}

// Reset clears session and stats (new conversation)
func (a *Agent) Reset() {
	a.sessionID = ""
	a.stats = TokenStats{}
}

// SetProject changes the working directory and resets the session
func (a *Agent) SetProject(dir string) {
	a.projectDir = dir
	a.Reset()
}

// Stats returns the current token usage statistics
func (a *Agent) Stats() TokenStats {
	return a.stats
}

// SessionID returns the current CLI session ID
func (a *Agent) SessionID() string {
	return a.sessionID
}
