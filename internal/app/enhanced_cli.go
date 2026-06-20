package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EnhancedCLIClient 增強的 CLI 客戶端，支援檔案輸入
type EnhancedCLIClient struct {
	*CLIClient
}

// NewEnhancedCLIClient 創建增強的 CLI 客戶端
func NewEnhancedCLIClient(model string) *EnhancedCLIClient {
	return &EnhancedCLIClient{
		CLIClient: NewClient(model),
	}
}

// CallWithFiles 調用 Claude Code CLI 並傳遞檔案
func (c *EnhancedCLIClient) CallWithFiles(ctx context.Context, message string, filePaths []string, projectDir, sessionID string) (*CLIResponse, error) {
	startTime := time.Now()

	// 構建基本參數
	args := []string{
		"-p",
		"--output-format", "json",
		"--model", c.Model,
		"--dangerously-skip-permissions",
		"--max-turns", c.maxTurnsStr(),
	}

	// 添加檔案參數
	for i, filePath := range filePaths {
		// 確保使用相對於專案目錄的路徑
		relPath, err := c.getRelativePath(filePath, projectDir)
		if err != nil {
			log.Printf("[enhanced-cli] warning: could not get relative path for %s: %v", filePath, err)
			relPath = filePath
		}

		// Claude CLI 的 --file 格式: file_id:relative_path
		fileSpec := fmt.Sprintf("img_%d:%s", i, relPath)
		args = append(args, "--file", fileSpec)

		log.Printf("[enhanced-cli] adding file: %s (relative: %s)", filePath, relPath)
	}

	// 添加 session ID 如果有的話
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	// 添加訊息
	args = append(args, message)

	// 執行命令
	log.Printf("[enhanced-cli] executing: claude %s", strings.Join(args, " "))

	output, err := runProcessOutput(ctx, ProcessOptions{
		Dir:     projectDir,
		Env:     cleanEnvForCLI(),
		Timeout: defaultAgentProcessTimeout,
	}, "claude", args...)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("agent aborted by user")
		}
		// CLI exited with error — try to parse stdout anyway
		if exitErr, ok := err.(*exec.ExitError); ok {
			if len(output) > 0 {
				var resp CLIResponse
				if parseErr := json.Unmarshal(output, &resp); parseErr == nil {
					log.Printf("[enhanced-cli] CLI exited with error but returned valid response (is_error=%v, turns=%d)", resp.IsError, resp.NumTurns)
					if !resp.IsError {
						resp.IsError = true
					}
					return &resp, fmt.Errorf("CLI exited with error: %s", resp.Result)
				}
			}
			log.Printf("[enhanced-cli] stderr: %s", string(exitErr.Stderr))
			return nil, fmt.Errorf("claude CLI error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("claude CLI exec: %w", err)
	}

	// 解析回應
	var resp CLIResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("parse CLI output: %w\nraw: %s", err, string(output))
	}

	// 記錄性能指標
	latency := time.Since(startTime)
	totalTokens := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens + resp.Usage.OutputTokens
	errorType := ""
	if resp.IsError {
		errorType = "cli_error"
	}

	// 使用 session ID 作為 chat ID 進行追蹤
	chatID := int64(0)
	if sessionID != "" {
		// 簡單的 session ID 雜湊
		for _, b := range []byte(sessionID) {
			chatID = chatID*31 + int64(b)
		}
	}

	RecordAPICallWithCache(latency, !resp.IsError, totalTokens, resp.TotalCostUSD, chatID, projectDir, errorType, ExtractModelShortName(c.Model),
		resp.Usage.InputTokens, resp.Usage.CacheReadInputTokens, resp.Usage.CacheCreationInputTokens, resp.Usage.OutputTokens)

	log.Printf("[enhanced-cli] completed in %v, tokens: %d, cost: $%.4f",
		latency, totalTokens, resp.TotalCostUSD)

	if resp.IsError {
		return &resp, fmt.Errorf("CLI returned error: %s", resp.Result)
	}

	return &resp, nil
}

// CallStreamWithFiles 流式調用 Claude Code CLI 並傳遞檔案
func (c *EnhancedCLIClient) CallStreamWithFiles(ctx context.Context, message string, filePaths []string, projectDir, sessionID string,
	onToolUse func(string, map[string]interface{}), onContent func(string, string)) (*CLIResponse, error) {

	// 構建參數（與 CallWithFiles 類似，但使用 stream-json 格式）
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--model", c.Model,
		"--dangerously-skip-permissions",
		"--max-turns", c.maxTurnsStr(),
		"--include-partial-messages",
	}

	// 添加檔案參數
	for i, filePath := range filePaths {
		relPath, err := c.getRelativePath(filePath, projectDir)
		if err != nil {
			log.Printf("[enhanced-cli] warning: could not get relative path for %s: %v", filePath, err)
			relPath = filePath
		}

		fileSpec := fmt.Sprintf("img_%d:%s", i, relPath)
		args = append(args, "--file", fileSpec)
	}

	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	args = append(args, message)

	// 執行流式命令
	cmd, cancel := processCommand(ctx, ProcessOptions{
		Dir:     projectDir,
		Env:     append(os.Environ(), "ALICE_SKIP_HOOKS=1"),
		Timeout: defaultAgentProcessTimeout,
	}, "claude", args...)
	defer cancel()

	// 使用原 CLIClient.CallStream 的邏輯處理流式回應
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

	// Read newline-delimited stream-json with bufio.Reader.ReadBytes rather
	// than bufio.Scanner. The CLI can emit a single stream-json line larger
	// than Scanner's 4MB token cap (e.g. a big tool result or file read).
	// Scan() would then fail with bufio.ErrTooLong, the loop would stop
	// reading stdout, the OS pipe would fill, and the CLI would block forever
	// on write until the 15-min process timeout — exactly why Hermes sub-tasks
	// could freeze with progress stuck at "[N/M]". ReadBytes has no per-line
	// size limit, so oversized lines are read whole and drained.
	reader := bufio.NewReader(stdout)
	processLine := func(line []byte) {
		if len(line) == 0 {
			return
		}

		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Message *struct {
				Content []struct {
					Type     string                 `json:"type"`
					Name     string                 `json:"name"`
					Input    map[string]interface{} `json:"input"`
					Text     string                 `json:"text"`
					Thinking string                 `json:"thinking"`
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
			return // 忽略無法解析的行
		}

		switch event.Type {
		case "assistant":
			if event.Message == nil {
				return
			}
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

	for {
		line, readErr := reader.ReadBytes('\n')
		processLine(bytes.TrimRight(line, "\r\n"))
		if readErr != nil {
			break
		}
	}

	if finalResp != nil {
		finalResp.ThinkingContent = strings.Join(thinkingBlocks, "\n\n---\n\n")
		finalResp.TextContent = strings.Join(textBlocks, "\n\n")
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("agent aborted by user")
		}
		// CLI exited with error — but we may already have a result from streaming
		if finalResp != nil {
			log.Printf("[enhanced-cli] CLI exited with error but streaming captured result (is_error=%v, turns=%d, text_len=%d)", finalResp.IsError, finalResp.NumTurns, len(finalResp.TextContent))
			if !finalResp.IsError {
				finalResp.IsError = true
			}
			return finalResp, fmt.Errorf("CLI returned error: %s", formatCLIStreamError(finalResp, c.MaxTurns))
		}
		if stderrBuf.Len() > 0 {
			return nil, fmt.Errorf("claude CLI error: %s", stderrBuf.String())
		}
		return nil, fmt.Errorf("claude CLI wait: %w", err)
	}

	if finalResp == nil {
		return nil, fmt.Errorf("no final result received from claude CLI")
	}

	return finalResp, nil
}

// getRelativePath 獲取相對於專案目錄的路徑
func (c *EnhancedCLIClient) getRelativePath(filePath, projectDir string) (string, error) {
	// 如果檔案路徑已經是相對路徑，直接返回
	if !filepath.IsAbs(filePath) {
		return filePath, nil
	}

	// 轉換為相對路徑
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}

	relPath, err := filepath.Rel(absProjectDir, filePath)
	if err != nil {
		return "", err
	}

	// 確保使用 Unix 風格的路徑分隔符
	relPath = filepath.ToSlash(relPath)

	return relPath, nil
}

// fileExists 檢查檔案是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
