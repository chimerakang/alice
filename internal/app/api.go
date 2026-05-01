package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// truncStderr clips stderr captures so log lines stay readable. Used only by
// CallPlan diagnostics — it would never be called on hot paths.
func truncStderr(s string) string {
	const max = 500
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func formatCLIStreamError(resp *CLIResponse, maxTurns int) string {
	if resp == nil {
		return "unknown stream error"
	}
	parts := []string{}
	if detail := strings.TrimSpace(resp.Result); detail != "" {
		parts = append(parts, detail)
	} else {
		parts = append(parts, "stream ended with is_error=true but no result text")
	}
	if resp.NumTurns > 0 {
		if maxTurns > 0 {
			parts = append(parts, fmt.Sprintf("turns=%d max_turns=%d", resp.NumTurns, maxTurns))
			if resp.NumTurns >= maxTurns {
				parts = append(parts, "likely exceeded --max-turns")
			}
		} else {
			parts = append(parts, fmt.Sprintf("turns=%d", resp.NumTurns))
		}
	}
	if resp.Subtype != "" {
		parts = append(parts, "subtype="+resp.Subtype)
	}
	if partial := truncStderr(resp.TextContent); partial != "" {
		parts = append(parts, "partial_text="+partial)
	}
	return strings.Join(parts, "; ")
}

// Client 定義統一的客戶端接口（支持 CLI 和 API）
type Client interface {
	Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error)
	CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error)
	// CallPlan invokes CLI with planning safeguards. If sessionID is non-empty,
	// implementations that support native resume should continue that session.
	CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error)
	GetModel() string
}

// CLIClient calls Claude Code CLI as a subprocess.
type CLIClient struct {
	Model    string
	MaxTurns int // max conversation turns per CLI invocation (default 50)
}

// forcePromptCaching5m, when true, makes cleanEnvForCLI inject
// FORCE_PROMPT_CACHING_5M=1 into every claude-spawn env. Claude Code v2.1.108+
// honours this and forces 5m cache TTL (1.25x rate) instead of its default 1h
// (2x rate), which is the right default for short-lived Hermes workflows. See
// issue #149 + docs/arch/hermes-walking-agent.md.
var forcePromptCaching5m atomic.Bool

// SetForcePromptCaching5m toggles process-wide injection of
// FORCE_PROMPT_CACHING_5M=1 into the claude CLI env. Wired by the Hermes
// walking-agent path; safe to call multiple times.
func SetForcePromptCaching5m(v bool) {
	forcePromptCaching5m.Store(v)
}

// cleanEnvForCLI 返回不含 Claude Code 嵌套檢測環境變數的環境變數列表。
// Claude Code 會設定 CLAUDECODE=1 等變數來防止嵌套啟動，必須清除。
func cleanEnvForCLI() []string {
	blocked := map[string]bool{
		"CLAUDECODE":             true,
		"CLAUDE_CODE_ENTRYPOINT": true,
		"CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING": true,
		"CLAUDE_AGENT_SDK_VERSION":                  true,
	}
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if !blocked[key] {
			env = append(env, e)
		}
	}
	env = append(env, "ALICE_SKIP_HOOKS=1")
	if forcePromptCaching5m.Load() {
		env = append(env, "FORCE_PROMPT_CACHING_5M=1")
	}
	return env
}

// CLIResponse is the JSON output from `claude -p --output-format json`.
type CLIResponse struct {
	Type            string  `json:"type"`
	Subtype         string  `json:"subtype"`
	SessionID       string  `json:"session_id"`
	IsError         bool    `json:"is_error"`
	NumTurns        int     `json:"num_turns"`
	Result          string  `json:"result"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	DurationMs      int     `json:"duration_ms"`
	ThinkingContent string  `json:"thinking_content"` // accumulated thinking blocks
	TextContent     string  `json:"text_content"`     // accumulated text blocks
	// Usage mirrors Anthropic's message.usage. input_tokens carries only the
	// uncached portion; cache_read_input_tokens (0.1x rate) and
	// cache_creation_input_tokens (1.25x rate) account for prompt-cache
	// activity. See issue #148. For Codex backends cache_read_input_tokens
	// carries cached_input_tokens; cache_creation is unused.
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// TotalInputTokensWithCache returns the full Anthropic input volume:
// uncached input + cache reads + cache creations. Use this when you want
// to know how many tokens hit the model context, not just billable
// uncached tokens. See issue #148.
func (u CLIResponse) TotalInputTokensWithCache() int {
	return u.Usage.InputTokens + u.Usage.CacheReadInputTokens + u.Usage.CacheCreationInputTokens
}

func NewClient(model string) *CLIClient {
	return &CLIClient{Model: model, MaxTurns: 50}
}

// maxTurnsStr returns the max-turns value as a string for CLI args.
func (c *CLIClient) maxTurnsStr() string {
	if c.MaxTurns <= 0 {
		return "50"
	}
	return fmt.Sprintf("%d", c.MaxTurns)
}

// GetModel 返回客戶端的模型名稱（實現 Client 接口）
func (c *CLIClient) GetModel() string {
	return c.Model
}

// APIClient 使用 Anthropic API 直接調用（不經過 Claude Code CLI）
type APIClient struct {
	Model  string
	apiKey string
}

// NewAPIClient 創建使用 Anthropic API 的客戶端
func NewAPIClient(apiKey, model string) *APIClient {
	return &APIClient{
		Model:  model,
		apiKey: apiKey,
	}
}

// GetModel 返回客戶端的模型名稱（實現 Client 接口）
func (a *APIClient) GetModel() string {
	return a.Model
}

// Call invokes the Claude Code CLI in print mode.
// If sessionID is non-empty, it resumes that session for conversation continuity.
// modelOverride: if non-empty, use this model instead of c.Model (for dynamic routing)
func (c *CLIClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	startTime := time.Now()

	// Select model: use override if provided, otherwise use client's default
	model := c.Model
	if modelOverride != "" {
		model = modelOverride
	}

	args := []string{
		"-p",
		"--output-format", "json",
		"--model", model,
		"--dangerously-skip-permissions",
		"--max-turns", c.maxTurnsStr(),
	}

	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	args = append(args, message)

	output, err := runProcessOutput(ctx, ProcessOptions{
		Dir:     projectDir,
		Env:     cleanEnvForCLI(),
		Timeout: defaultAgentProcessTimeout,
	}, "claude", args...)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("agent aborted by user")
		}
		// CLI exited with error — try to parse stdout anyway (CLI may have sent a JSON response before exiting)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if len(output) > 0 {
				var resp CLIResponse
				if parseErr := json.Unmarshal(output, &resp); parseErr == nil {
					log.Printf("[cli] CLI exited with error but returned valid response (is_error=%v, turns=%d)", resp.IsError, resp.NumTurns)
					if !resp.IsError {
						resp.IsError = true
					}
					return &resp, fmt.Errorf("CLI exited with error: %s", resp.Result)
				}
			}
			return nil, fmt.Errorf("claude CLI error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("claude CLI exec: %w", err)
	}

	var resp CLIResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("parse CLI output: %w\nraw: %s", err, string(output))
	}

	// Record performance metrics
	latency := time.Since(startTime)
	totalTokens := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens + resp.Usage.OutputTokens
	errorType := ""
	if resp.IsError {
		errorType = "cli_error"
	}

	// Use session ID as chat ID for tracking (if available)
	chatID := int64(0)
	if sessionID != "" {
		// Simple hash of session ID for consistent chat ID
		for _, b := range []byte(sessionID) {
			chatID = chatID*31 + int64(b)
		}
	}

	RecordAPICallWithCache(latency, !resp.IsError, totalTokens, resp.TotalCostUSD, chatID, projectDir, errorType, ExtractModelShortName(model),
		resp.Usage.InputTokens, resp.Usage.CacheReadInputTokens, resp.Usage.CacheCreationInputTokens, resp.Usage.OutputTokens)

	if resp.IsError {
		return &resp, fmt.Errorf("CLI returned error: %s", resp.Result)
	}

	return &resp, nil
}

// CallStream invokes Claude Code CLI with stream-json output.
// onToolUse is called for each tool_use event during processing.
// onContent is called for thinking/text content blocks (contentType: "thinking" or "text").
// modelOverride: if non-empty, use this model instead of c.Model (for dynamic routing)
func (c *CLIClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	startTime := time.Now()

	// Select model: use override if provided, otherwise use client's default
	model := c.Model
	if modelOverride != "" {
		model = modelOverride
	}

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--model", model,
		"--dangerously-skip-permissions",
		"--max-turns", c.maxTurnsStr(),
	}

	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	args = append(args, message)

	cmd, cancel := processCommand(ctx, ProcessOptions{
		Dir:     projectDir,
		Env:     cleanEnvForCLI(),
		Timeout: defaultAgentProcessTimeout,
	}, "claude", args...)
	defer cancel()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude CLI start: %w", err)
	}

	var finalResp *CLIResponse
	var thinkingBlocks []string
	var textBlocks []string

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // up to 4MB per line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Message *struct {
				Content []struct {
					Type     string                 `json:"type"`
					Name     string                 `json:"name"`
					Input    map[string]interface{} `json:"input"`
					Text     string                 `json:"text"`     // for type="text"
					Thinking string                 `json:"thinking"` // for type="thinking"
				} `json:"content"`
			} `json:"message"`
			// result fields
			SessionID    string  `json:"session_id"`
			IsError      bool    `json:"is_error"`
			NumTurns     int     `json:"num_turns"`
			Result       string  `json:"result"`
			TotalCostUSD float64 `json:"total_cost_usd"`
			DurationMs   int     `json:"duration_ms"`
			Usage        struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		switch event.Type {
		case "assistant":
			if event.Message != nil {
				for _, c := range event.Message.Content {
					switch c.Type {
					case "tool_use":
						if onToolUse != nil {
							onToolUse(c.Name, c.Input)
						}
					case "thinking":
						if c.Thinking != "" {
							thinkingBlocks = append(thinkingBlocks, c.Thinking)
							if onContent != nil {
								onContent("thinking", c.Thinking)
							}
						}
					case "text":
						if c.Text != "" {
							textBlocks = append(textBlocks, c.Text)
							if onContent != nil {
								onContent("text", c.Text)
							}
						}
					}
				}
			}
		case "result":
			finalResp = &CLIResponse{
				Type:         event.Type,
				Subtype:      event.Subtype,
				SessionID:    event.SessionID,
				IsError:      event.IsError,
				NumTurns:     event.NumTurns,
				Result:       event.Result,
				TotalCostUSD: event.TotalCostUSD,
				DurationMs:   event.DurationMs,
			}
			finalResp.Usage.InputTokens = event.Usage.InputTokens
			finalResp.Usage.OutputTokens = event.Usage.OutputTokens
			finalResp.Usage.CacheReadInputTokens = event.Usage.CacheReadInputTokens
			finalResp.Usage.CacheCreationInputTokens = event.Usage.CacheCreationInputTokens
		}
	}

	// Merge accumulated thinking/text blocks into response
	if finalResp != nil {
		finalResp.ThinkingContent = strings.Join(thinkingBlocks, "\n\n---\n\n")
		finalResp.TextContent = strings.Join(textBlocks, "\n\n")
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("agent aborted by user")
		}
		// CLI exited with error — but we may already have streaming results
		if finalResp != nil {
			log.Printf("[cli] CLI exited with error but streaming captured result (is_error=%v, turns=%d, text_len=%d)", finalResp.IsError, finalResp.NumTurns, len(finalResp.TextContent))
			if !finalResp.IsError {
				finalResp.IsError = true
			}
			// Fall through to normal metrics recording and return below
		} else {
			if _, ok := err.(*exec.ExitError); ok {
				return nil, fmt.Errorf("claude CLI error: %s", stderrBuf.String())
			}
			return nil, fmt.Errorf("claude CLI exec: %w", err)
		}
	}

	if finalResp == nil {
		return nil, fmt.Errorf("no result event in stream output")
	}

	// Record performance metrics
	latency := time.Since(startTime)
	totalTokens := finalResp.Usage.InputTokens + finalResp.Usage.CacheReadInputTokens + finalResp.Usage.CacheCreationInputTokens + finalResp.Usage.OutputTokens
	errorType := ""
	if finalResp.IsError {
		errorType = "cli_stream_error"
	}

	// Use session ID as chat ID for tracking (if available)
	chatID := int64(0)
	if sessionID != "" {
		// Simple hash of session ID for consistent chat ID
		for _, b := range []byte(sessionID) {
			chatID = chatID*31 + int64(b)
		}
	}

	RecordAPICallWithCache(latency, !finalResp.IsError, totalTokens, finalResp.TotalCostUSD, chatID, projectDir, errorType, ExtractModelShortName(model),
		finalResp.Usage.InputTokens, finalResp.Usage.CacheReadInputTokens, finalResp.Usage.CacheCreationInputTokens, finalResp.Usage.OutputTokens)

	if finalResp.IsError {
		return finalResp, fmt.Errorf("CLI returned error: %s", formatCLIStreamError(finalResp, c.MaxTurns))
	}

	return finalResp, nil
}

// CallPlan invokes Claude Code CLI with --max-turns 3 for planning-only phase.
// If sessionID is non-empty, resumes that native Planner session.
// No tool execution callbacks — planning phase should only think, not act.
func (c *CLIClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	startTime := time.Now()

	model := c.Model
	if modelOverride != "" {
		model = modelOverride
	}

	mcpConfigPath, cleanupMCPConfig, err := writePlannerEmitPlanMCPConfig()
	if err != nil {
		return nil, fmt.Errorf("planner emit_plan mcp config: %w", err)
	}
	defer cleanupMCPConfig()

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--model", model,
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
		"--mcp-config", mcpConfigPath,
		"--tools", "",
	}

	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	args = append(args, message)

	// Diagnostic dump — when CallPlan fails with empty stderr, we want to be
	// able to replay the exact prompt offline. The file is overwritten each
	// call so /tmp doesn't fill up.
	log.Printf("[cli] CallPlan prompt: model=%s projectDir=%s len=%d", model, projectDir, len(message))
	if dumpErr := os.WriteFile("/tmp/alice_callplan_last.txt", []byte(message), 0644); dumpErr != nil {
		log.Printf("[cli] CallPlan prompt dump failed: %v", dumpErr)
	}

	cmd, cancel := processCommand(ctx, ProcessOptions{
		Dir:     projectDir,
		Env:     cleanEnvForCLI(),
		Timeout: defaultAgentProcessTimeout,
	}, "claude", args...)
	defer cancel()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude CLI start: %w", err)
	}

	var finalResp *CLIResponse
	var thinkingBlocks []string
	var textBlocks []string
	var emitPlanJSON string
	var sawEmitPlan bool
	var latestSessionID string

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Message *struct {
				Content []struct {
					Type     string `json:"type"`
					Name     string `json:"name"`
					Input    map[string]interface{} `json:"input"`
					Text     string `json:"text"`
					Thinking string `json:"thinking"`
				} `json:"content"`
			} `json:"message"`
			SessionID    string  `json:"session_id"`
			IsError      bool    `json:"is_error"`
			NumTurns     int     `json:"num_turns"`
			Result       string  `json:"result"`
			TotalCostUSD float64 `json:"total_cost_usd"`
			DurationMs   int     `json:"duration_ms"`
			Usage        struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.SessionID != "" {
			latestSessionID = event.SessionID
		}

		switch event.Type {
		case "assistant":
			if event.Message != nil {
				for _, c := range event.Message.Content {
					switch c.Type {
					case "thinking":
						if c.Thinking != "" {
							thinkingBlocks = append(thinkingBlocks, c.Thinking)
							if onContent != nil {
								onContent("thinking", c.Thinking)
							}
						}
					case "text":
						if c.Text != "" {
							textBlocks = append(textBlocks, c.Text)
							if onContent != nil {
								onContent("text", c.Text)
							}
						}
					case "tool_use":
						if isPlannerEmitPlanTool(c.Name) {
							if payload, ok := marshalPlannerEmitPlanPayload(c.Input); ok {
								emitPlanJSON = payload
								sawEmitPlan = true
							}
						}
					}
				}
			}
		case "result":
			finalResp = &CLIResponse{
				Type:         event.Type,
				Subtype:      event.Subtype,
				SessionID:    event.SessionID,
				IsError:      event.IsError,
				NumTurns:     event.NumTurns,
				Result:       event.Result,
				TotalCostUSD: event.TotalCostUSD,
				DurationMs:   event.DurationMs,
			}
			finalResp.Usage.InputTokens = event.Usage.InputTokens
			finalResp.Usage.OutputTokens = event.Usage.OutputTokens
			finalResp.Usage.CacheReadInputTokens = event.Usage.CacheReadInputTokens
			finalResp.Usage.CacheCreationInputTokens = event.Usage.CacheCreationInputTokens
			if finalResp.SessionID == "" && latestSessionID != "" {
				finalResp.SessionID = latestSessionID
			}
		}
	}

	if finalResp != nil {
		finalResp.ThinkingContent = strings.Join(thinkingBlocks, "\n\n---\n\n")
		if sawEmitPlan && emitPlanJSON != "" {
			finalResp.TextContent = emitPlanJSON
			if finalResp.Result == "" {
				finalResp.Result = "emit_plan"
			}
		} else {
			finalResp.TextContent = strings.Join(textBlocks, "\n\n")
		}
	}

	waitErr := cmd.Wait()
	if waitErr != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("agent aborted by user")
		}
		if finalResp != nil {
			log.Printf("[cli] CallPlan CLI exited with error but streaming captured result (is_error=%v, exit=%v, stderr=%q)", finalResp.IsError, waitErr, truncStderr(stderrBuf.String()))
			if !finalResp.IsError {
				finalResp.IsError = true
			}
		} else {
			if _, ok := waitErr.(*exec.ExitError); ok {
				return nil, fmt.Errorf("claude CLI error (exit=%v): %s", waitErr, stderrBuf.String())
			}
			return nil, fmt.Errorf("claude CLI exec: %w", waitErr)
		}
	}

	if finalResp == nil {
		return nil, fmt.Errorf("no result event in stream output (stderr=%q)", truncStderr(stderrBuf.String()))
	}

	// Record performance metrics
	latency := time.Since(startTime)
	totalTokens := finalResp.Usage.InputTokens + finalResp.Usage.CacheReadInputTokens + finalResp.Usage.CacheCreationInputTokens + finalResp.Usage.OutputTokens
	errorType := ""
	if finalResp.IsError {
		errorType = "cli_plan_error"
	}

	RecordAPICallWithCache(latency, !finalResp.IsError, totalTokens, finalResp.TotalCostUSD, 0, projectDir, errorType, ExtractModelShortName(model),
		finalResp.Usage.InputTokens, finalResp.Usage.CacheReadInputTokens, finalResp.Usage.CacheCreationInputTokens, finalResp.Usage.OutputTokens)

	if finalResp.IsError {
		// Result is sometimes empty when the CLI errors before producing
		// content; surface stderr and the wait-error to give the user a hint.
		detail := strings.TrimSpace(finalResp.Result)
		if detail == "" {
			detail = strings.TrimSpace(stderrBuf.String())
		}
		if detail == "" && waitErr != nil {
			detail = waitErr.Error()
		}
		if detail == "" {
			detail = "(empty CLI error — check claude CLI auth, network, or prompt size)"
		}
		return finalResp, fmt.Errorf("CLI returned error: %s", detail)
	}

	return finalResp, nil
}

const plannerEmitPlanServerName = "planner_emit_plan"
const plannerEmitPlanToolName = "emit_plan"

func isPlannerEmitPlanTool(name string) bool {
	name = strings.TrimSpace(name)
	return name == plannerEmitPlanToolName || strings.HasSuffix(name, "__"+plannerEmitPlanToolName)
}

func marshalPlannerEmitPlanPayload(input map[string]interface{}) (string, bool) {
	if len(input) == 0 {
		return "", false
	}
	payload, ok := input["sub_tasks"]
	if !ok {
		payload, ok = input["subTasks"]
	}
	if !ok {
		payload, ok = input["plan"]
	}
	if !ok {
		return "", false
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func writePlannerEmitPlanMCPConfig() (string, func(), error) {
	dir, err := os.MkdirTemp("", "alice-planner-emit-plan-*")
	if err != nil {
		return "", nil, err
	}

	scriptPath := filepath.Join(dir, "emit_plan_mcp.py")
	if err := os.WriteFile(scriptPath, []byte(plannerEmitPlanMCPScript), 0755); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}

	configPath := filepath.Join(dir, "mcp.json")
	config := map[string]any{
		"mcpServers": map[string]any{
			plannerEmitPlanServerName: map[string]any{
				"command": "python3",
				"args":    []string{"-u", scriptPath},
			},
		},
	}
	configBytes, err := json.Marshal(config)
	if err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return configPath, cleanup, nil
}

const plannerEmitPlanMCPScript = `#!/usr/bin/env python3
import json
import sys

TOOL_NAME = "emit_plan"
TOOL_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "sub_tasks": {
            "type": "array",
            "items": {
                "type": "object",
                "additionalProperties": True,
                "properties": {
                    "id": {"type": "string"},
                    "description": {"type": "string"},
                    "tool_hints": {
                        "type": "array",
                        "items": {"type": "string"},
                    },
                },
                "required": ["id", "description", "tool_hints"],
            },
        }
    },
    "required": ["sub_tasks"],
}

def send(obj):
    sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
    sys.stdout.flush()

def tool_response(arguments):
    sub_tasks = arguments.get("sub_tasks")
    if sub_tasks is None:
        sub_tasks = arguments.get("subTasks")
    if sub_tasks is None:
        sub_tasks = arguments.get("plan")
    if sub_tasks is None:
        return {
            "content": [{
                "type": "text",
                "text": "emit_plan tool received no sub_tasks array",
            }],
            "isError": True,
        }
    return {
        "content": [{
            "type": "text",
            "text": json.dumps(sub_tasks, ensure_ascii=False),
        }],
        "structuredContent": {
            "sub_tasks": sub_tasks,
        },
        "isError": False,
    }

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
    except json.JSONDecodeError:
        continue

    method = msg.get("method")
    req_id = msg.get("id")
    params = msg.get("params") or {}

    if method == "initialize":
        send({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "protocolVersion": params.get("protocolVersion", "2025-03-26"),
                "capabilities": {
                    "tools": {
                        "listChanged": False,
                    }
                },
                "serverInfo": {
                    "name": "alice-planner-emit-plan",
                    "version": "1.0.0",
                },
            },
        })
    elif method == "tools/list":
        send({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "tools": [{
                    "name": TOOL_NAME,
                    "description": "Emit the final Hermes sub-task array and stop planning.",
                    "inputSchema": TOOL_SCHEMA,
                }],
            },
        })
    elif method == "tools/call":
        if params.get("name") != TOOL_NAME:
            send({
                "jsonrpc": "2.0",
                "id": req_id,
                "error": {
                    "code": -32602,
                    "message": "Unknown tool",
                },
            })
            continue
        send({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": tool_response(params.get("arguments") or {}),
        })
    elif method == "notifications/initialized":
        continue
    else:
        if req_id is not None:
            send({
                "jsonrpc": "2.0",
                "id": req_id,
                "error": {
                    "code": -32601,
                    "message": "Method not found",
                },
            })
`

// Call 使用 Anthropic API 直接調用（APIClient 實現）
func (a *APIClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	startTime := time.Now()

	// Select model: use override if provided, otherwise use default
	model := a.Model
	if modelOverride != "" {
		model = modelOverride
	}

	// 構建 API 請求
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": message,
			},
		},
	}

	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(reqBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// 提取文本內容
	textContent := ""
	if len(apiResp.Content) > 0 && apiResp.Content[0].Type == "text" {
		textContent = apiResp.Content[0].Text
	}

	// Record performance metrics
	latency := time.Since(startTime)
	// APIClient path uses raw Anthropic SDK; cache fields aren't parsed here yet
	// (separate work). Total reflects only uncached + output for now.
	totalTokens := apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens

	// 計算成本（$3 input, $15 output per million tokens）
	inputCost := float64(apiResp.Usage.InputTokens) * 3 / 1_000_000
	outputCost := float64(apiResp.Usage.OutputTokens) * 15 / 1_000_000
	totalCost := inputCost + outputCost

	// Use session ID as chat ID for tracking
	chatID := int64(0)
	if sessionID != "" {
		// Simple hash of session ID for consistent chat ID
		for _, b := range []byte(sessionID) {
			chatID = chatID*31 + int64(b)
		}
	}

	RecordAPICallWithCache(latency, true, totalTokens, totalCost, chatID, projectDir, "", ExtractModelShortName(model),
		apiResp.Usage.InputTokens, 0, 0, apiResp.Usage.OutputTokens)

	cliResp := &CLIResponse{
		Type:            "text",
		Result:          textContent,
		TotalCostUSD:    totalCost,
		DurationMs:      int(latency.Milliseconds()),
		ThinkingContent: "",
		TextContent:     textContent,
	}
	cliResp.Usage.InputTokens = apiResp.Usage.InputTokens
	cliResp.Usage.OutputTokens = apiResp.Usage.OutputTokens
	return cliResp, nil
}

// CallStream 使用 Anthropic API 的流式調用（APIClient 實現）
// 注意：為了簡化，當前實現與 Call 相同（非真正流式）
func (a *APIClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	// 對於 API 版本，我們使用相同的 Call 實現
	// 但在回調中通知 onContent
	resp, err := a.Call(ctx, message, projectDir, sessionID, modelOverride)
	if err != nil {
		return nil, err
	}

	// 觸發回調以保持兼容性
	if onContent != nil && resp.TextContent != "" {
		onContent("text", resp.TextContent)
	}

	return resp, nil
}

// CallPlan 使用 Anthropic API 的計劃調用（APIClient 實現）。
// APIClient does not persist native sessions; sessionID is accepted for Client
// interface compatibility and metrics attribution.
func (a *APIClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	resp, err := a.Call(ctx, message, projectDir, sessionID, modelOverride)
	if err != nil {
		return nil, err
	}
	if onContent != nil && resp.TextContent != "" {
		onContent("text", resp.TextContent)
	}
	return resp, nil
}
