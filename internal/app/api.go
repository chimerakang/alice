package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
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

// Client 定義統一的客戶端接口（支持 CLI 和 API）
type Client interface {
	Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error)
	CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error)
	// CallPlan invokes CLI with --max-turns 1 for planning phase (no tool execution).
	CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error)
	GetModel() string
}

// CLIClient calls Claude Code CLI as a subprocess.
type CLIClient struct {
	Model    string
	MaxTurns int // max conversation turns per CLI invocation (default 50)
}

// cleanEnvForCLI 返回不含 Claude Code 嵌套檢測環境變數的環境變數列表。
// Claude Code 會設定 CLAUDECODE=1 等變數來防止嵌套啟動，必須清除。
func cleanEnvForCLI() []string {
	blocked := map[string]bool{
		"CLAUDECODE":                          true,
		"CLAUDE_CODE_ENTRYPOINT":              true,
		"CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING": true,
		"CLAUDE_AGENT_SDK_VERSION":            true,
	}
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if !blocked[key] {
			env = append(env, e)
		}
	}
	env = append(env, "ALICE_SKIP_HOOKS=1")
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
	Usage           struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
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

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectDir
	cmd.Env = cleanEnvForCLI()

	output, err := cmd.Output()
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
	totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
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

	RecordAPICall(latency, !resp.IsError, totalTokens, resp.TotalCostUSD, chatID, projectDir, errorType, ExtractModelShortName(model))

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

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectDir
	cmd.Env = cleanEnvForCLI()

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
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
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
	totalTokens := finalResp.Usage.InputTokens + finalResp.Usage.OutputTokens
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

	RecordAPICall(latency, !finalResp.IsError, totalTokens, finalResp.TotalCostUSD, chatID, projectDir, errorType, ExtractModelShortName(model))

	if finalResp.IsError {
		return finalResp, fmt.Errorf("CLI returned error: %s", finalResp.Result)
	}

	return finalResp, nil
}

// CallPlan invokes Claude Code CLI with --max-turns 1 for planning-only phase.
// No session resume — always starts a fresh session for the plan.
// No tool execution callbacks — planning phase should only think, not act.
func (c *CLIClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	startTime := time.Now()

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
		"--max-turns", "1",
	}

	args = append(args, message)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectDir
	cmd.Env = cleanEnvForCLI()

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
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
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
		}
	}

	if finalResp != nil {
		finalResp.ThinkingContent = strings.Join(thinkingBlocks, "\n\n---\n\n")
		finalResp.TextContent = strings.Join(textBlocks, "\n\n")
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
	totalTokens := finalResp.Usage.InputTokens + finalResp.Usage.OutputTokens
	errorType := ""
	if finalResp.IsError {
		errorType = "cli_plan_error"
	}

	RecordAPICall(latency, !finalResp.IsError, totalTokens, finalResp.TotalCostUSD, 0, projectDir, errorType, ExtractModelShortName(model))

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

	RecordAPICall(latency, true, totalTokens, totalCost, chatID, projectDir, "", ExtractModelShortName(model))

	return &CLIResponse{
		Type:            "text",
		Result:          textContent,
		TotalCostUSD:    totalCost,
		DurationMs:      int(latency.Milliseconds()),
		ThinkingContent: "",
		TextContent:     textContent,
		Usage: struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}{
			InputTokens:  apiResp.Usage.InputTokens,
			OutputTokens: apiResp.Usage.OutputTokens,
		},
	}, nil
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

// CallPlan 使用 Anthropic API 的計劃調用（APIClient 實現）
// 直接調用 Call，不使用 session resume
func (a *APIClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	resp, err := a.Call(ctx, message, projectDir, "", modelOverride)
	if err != nil {
		return nil, err
	}
	if onContent != nil && resp.TextContent != "" {
		onContent("text", resp.TextContent)
	}
	return resp, nil
}
