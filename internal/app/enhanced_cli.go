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
		"--max-turns", "25",
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

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "ALICE_SKIP_HOOKS=1")

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("agent aborted by user")
		}
		// CLI 可能 stderr 有錯誤訊息
		if exitErr, ok := err.(*exec.ExitError); ok {
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
	totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
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

	RecordAPICall(latency, !resp.IsError, totalTokens, resp.TotalCostUSD, chatID, errorType, ExtractModelShortName(c.Model))

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
		"--max-turns", "25",
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
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "ALICE_SKIP_HOOKS=1")

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

	// 處理流式輸出（簡化版，專注於最終結果）
	var finalResp *CLIResponse

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event struct {
			Type      string                 `json:"type"`
			SessionID string                 `json:"session_id"`
			IsError   bool                   `json:"is_error"`
			Result    string                 `json:"result"`
			Usage     struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal(line, &event); err != nil {
			continue // 忽略無法解析的行
		}

		// 只關注最終結果
		if event.Type == "result" {
			finalResp = &CLIResponse{
				Type:      event.Type,
				SessionID: event.SessionID,
				IsError:   event.IsError,
				Result:    event.Result,
				Usage:     event.Usage,
			}
			break
		}
	}

	if err := cmd.Wait(); err != nil && ctx.Err() != context.Canceled {
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