package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CLIClient calls Claude Code CLI as a subprocess.
type CLIClient struct {
	Model string
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
	return &CLIClient{Model: model}
}

// Call invokes the Claude Code CLI in print mode.
// If sessionID is non-empty, it resumes that session for conversation continuity.
func (c *CLIClient) Call(ctx context.Context, message, projectDir, sessionID string) (*CLIResponse, error) {
	startTime := time.Now()
	args := []string{
		"-p",
		"--output-format", "json",
		"--model", c.Model,
		"--dangerously-skip-permissions",
		"--max-turns", "25",
	}

	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	args = append(args, message)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectDir

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("agent aborted by user")
		}
		// CLI 可能 stderr 有錯誤訊息
		if exitErr, ok := err.(*exec.ExitError); ok {
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

	RecordAPICall(latency, !resp.IsError, totalTokens, resp.TotalCostUSD, chatID, errorType)

	if resp.IsError {
		return &resp, fmt.Errorf("CLI returned error: %s", resp.Result)
	}

	return &resp, nil
}

// CallStream invokes Claude Code CLI with stream-json output.
// onToolUse is called for each tool_use event during processing.
// onContent is called for thinking/text content blocks (contentType: "thinking" or "text").
func (c *CLIClient) CallStream(ctx context.Context, message, projectDir, sessionID string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	startTime := time.Now()
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--model", c.Model,
		"--dangerously-skip-permissions",
		"--max-turns", "25",
	}

	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	args = append(args, message)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectDir

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
		if _, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("claude CLI error: %s", stderrBuf.String())
		}
		return nil, fmt.Errorf("claude CLI exec: %w", err)
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

	RecordAPICall(latency, !finalResp.IsError, totalTokens, finalResp.TotalCostUSD, chatID, errorType)

	if finalResp.IsError {
		return finalResp, fmt.Errorf("CLI returned error: %s", finalResp.Result)
	}

	return finalResp, nil
}
