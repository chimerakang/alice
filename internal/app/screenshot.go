package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WebMetadata 網頁元信息
type WebMetadata struct {
	Title       string
	Description string
	ImageURL    string
}

type screenshotCommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ScreenshotManager 管理網頁截圖功能
type ScreenshotManager struct {
	timeoutSec         int
	maxRetries         int
	maxConcurrent      int
	tempDir            string
	waitForTimeout     time.Duration
	viewportSize       string
	commandRunner      screenshotCommandRunner
	metadataHTTPClient *http.Client
	captureLimiterMu   sync.Mutex
	captureLimiter     chan struct{}
}

const screenshotRetentionLimit = 25
const (
	defaultScreenshotTimeout    = 20 * time.Second
	defaultScreenshotWait       = 1 * time.Second
	defaultScreenshotViewport   = "1280,720"
	defaultScreenshotConcurrent = 2
	defaultMetadataTimeout      = 3 * time.Second
)

var errScreenshotTimeout = errors.New("screenshot timeout")

// NewScreenshotManager 建立截圖管理器
func NewScreenshotManager() *ScreenshotManager {
	sm := &ScreenshotManager{
		timeoutSec:     int(defaultScreenshotTimeout / time.Second),
		maxRetries:     1,
		maxConcurrent:  defaultScreenshotConcurrent,
		tempDir:        "temp/screenshots",
		waitForTimeout: defaultScreenshotWait,
		viewportSize:   defaultScreenshotViewport,
		metadataHTTPClient: &http.Client{
			Timeout: defaultMetadataTimeout,
		},
	}
	sm.commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return runProcessCombinedOutput(ctx, ProcessOptions{
			Timeout:     time.Duration(sm.timeoutSec+5) * time.Second,
			OutputLimit: 16 * 1024,
			LogPrefix:   "screenshot",
		}, name, args...)
	}
	return sm
}

// CaptureScreenshot 截圖指定 URL，返回圖片檔案路徑。
func (sm *ScreenshotManager) CaptureScreenshot(ctx context.Context, urlStr string) (string, error) {
	filePath, _, err := sm.captureWithRetry(ctx, urlStr, false)
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// CaptureScreenshotBytes 截圖指定 URL，返回圖片 bytes。
func (sm *ScreenshotManager) CaptureScreenshotBytes(ctx context.Context, urlStr string) ([]byte, *WebMetadata, error) {
	filePath, metadata, err := sm.captureWithRetry(ctx, urlStr, true)
	if err != nil {
		return nil, nil, err
	}

	data, readErr := os.ReadFile(filePath)
	removeErr := os.Remove(filePath)
	if readErr != nil {
		if removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[screenshot] cleanup failed after read error: %v", removeErr)
		}
		return nil, nil, fmt.Errorf("讀取截圖失敗: %w", readErr)
	}
	if removeErr != nil && !os.IsNotExist(removeErr) {
		log.Printf("[screenshot] cleanup failed after buffer capture: %v", removeErr)
	}
	return data, metadata, nil
}

// CaptureScreenshotWithMetadata 在單一 Playwright CLI 截圖流程中完成截圖和 metadata 提取。
// 返回：(圖片檔案路徑, metadata, 錯誤)
func (sm *ScreenshotManager) CaptureScreenshotWithMetadata(ctx context.Context, urlStr string) (string, *WebMetadata, error) {
	return sm.captureWithRetry(ctx, urlStr, true)
}

func (sm *ScreenshotManager) captureWithRetry(ctx context.Context, urlStr string, includeMetadata bool) (string, *WebMetadata, error) {
	if err := sm.validateURL(urlStr); err != nil {
		return "", nil, fmt.Errorf("URL 驗證失敗: %w", err)
	}
	release, err := sm.acquireCaptureSlot()
	if err != nil {
		return "", nil, err
	}
	defer release()
	if err := os.MkdirAll(sm.tempDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("無法建立臨時目錄: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= sm.maxRetries; attempt++ {
		filePath, err := sm.captureToFile(ctx, urlStr)
		if err != nil {
			lastErr = err
			if errors.Is(err, errScreenshotTimeout) {
				return "", nil, fmt.Errorf("截圖逾時")
			}
			if attempt < sm.maxRetries && ctx.Err() == nil {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			break
		}

		if !includeMetadata {
			if err := sm.CleanupOldScreenshots(screenshotRetentionLimit); err != nil {
				log.Printf("[screenshot] cleanup after capture failed: %v", err)
			}
			return filePath, nil, nil
		}

		metadata, metaErr := sm.fetchMetadata(ctx, urlStr)
		if metaErr != nil {
			log.Printf("[screenshot] metadata fetch failed for %s: %v", urlStr, metaErr)
		}
		if err := sm.CleanupOldScreenshots(screenshotRetentionLimit); err != nil {
			log.Printf("[screenshot] cleanup after capture failed: %v", err)
		}
		return filePath, metadata, nil
	}

	if lastErr != nil {
		log.Printf("[screenshot] capture failed after %d retries: %v", sm.maxRetries, lastErr)
	}
	return "", nil, fmt.Errorf("截圖失敗（已重試 %d 次）", sm.maxRetries)
}

func (sm *ScreenshotManager) captureToFile(ctx context.Context, urlStr string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(sm.timeoutSec)*time.Second)
	defer cancel()

	tmpFile, err := os.CreateTemp(sm.tempDir, "preview-*.png")
	if err != nil {
		return "", fmt.Errorf("建立臨時截圖檔失敗: %w", err)
	}
	filePath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("關閉臨時截圖檔失敗: %w", err)
	}

	args := sm.playwrightScreenshotArgs(urlStr, filePath)
	output, err := sm.commandRunner(ctx, "npx", args...)
	if err != nil {
		_ = os.Remove(filePath)
		if ctx.Err() != nil {
			log.Printf("[screenshot] playwright-cli timed out after %ds", sm.timeoutSec)
			return "", fmt.Errorf("playwright-cli 截圖逾時: %w", errScreenshotTimeout)
		}
		if summary := trimOutput(output, 800); summary != "" {
			log.Printf("[screenshot] playwright-cli failed: %v; output=%s", err, summary)
		} else {
			log.Printf("[screenshot] playwright-cli failed: %v", err)
		}
		return "", fmt.Errorf("playwright-cli 截圖失敗")
	}

	info, statErr := os.Stat(filePath)
	if statErr != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("截圖檔案不存在: %w", statErr)
	}
	if info.Size() == 0 {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("截圖資料為空")
	}

	return filePath, nil
}

func (sm *ScreenshotManager) playwrightScreenshotArgs(urlStr, filePath string) []string {
	timeoutMS := strconv.Itoa(sm.timeoutSec * 1000)

	return []string{
		"--yes",
		"playwright",
		"screenshot",
		"--browser=chromium",
		"--full-page",
		"--ignore-https-errors",
		"--viewport-size", sm.viewportSize,
		"--wait-for-timeout", strconv.Itoa(int(sm.waitForTimeout / time.Millisecond)),
		"--timeout", timeoutMS,
		urlStr,
		filePath,
	}
}

func (sm *ScreenshotManager) validateURL(urlStr string) error {
	if strings.TrimSpace(urlStr) == "" {
		return fmt.Errorf("URL 不能為空")
	}
	return validatePreviewURL(urlStr)
}

func (sm *ScreenshotManager) fetchMetadata(ctx context.Context, urlStr string) (*WebMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultMetadataTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("建立請求失敗: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Alice Bot)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := sm.metadataHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("讀取網頁失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml+xml") {
		return nil, fmt.Errorf("非 HTML 內容: %s", ct)
	}

	limited := io.LimitReader(resp.Body, 2*1024*1024)
	htmlBytes, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("讀取 HTML 失敗: %w", err)
	}
	if len(htmlBytes) == 0 {
		return &WebMetadata{}, nil
	}

	return sm.extractMetadataFromHTML(string(htmlBytes)), nil
}

func (sm *ScreenshotManager) extractMetadataFromHTML(html string) *WebMetadata {
	metadata := &WebMetadata{}

	titleRe := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	if matches := titleRe.FindStringSubmatch(html); len(matches) > 1 {
		metadata.Title = strings.TrimSpace(matches[1])
	}

	descRe := regexp.MustCompile(`(?is)<meta\s+property=["']og:description["']\s+content=["']([^"']+)["']`)
	if matches := descRe.FindStringSubmatch(html); len(matches) > 1 {
		metadata.Description = strings.TrimSpace(matches[1])
	} else {
		descRe := regexp.MustCompile(`(?is)<meta\s+name=["']description["']\s+content=["']([^"']+)["']`)
		if matches := descRe.FindStringSubmatch(html); len(matches) > 1 {
			metadata.Description = strings.TrimSpace(matches[1])
		}
	}

	imgRe := regexp.MustCompile(`(?is)<meta\s+property=["']og:image["']\s+content=["']([^"']+)["']`)
	if matches := imgRe.FindStringSubmatch(html); len(matches) > 1 {
		metadata.ImageURL = strings.TrimSpace(matches[1])
	} else {
		imgRe := regexp.MustCompile(`(?is)<meta\s+property=["']twitter:image["']\s+content=["']([^"']+)["']`)
		if matches := imgRe.FindStringSubmatch(html); len(matches) > 1 {
			metadata.ImageURL = strings.TrimSpace(matches[1])
		}
	}

	return metadata
}

func (sm *ScreenshotManager) acquireCaptureSlot() (func(), error) {
	if sm.maxConcurrent <= 0 {
		sm.maxConcurrent = defaultScreenshotConcurrent
	}

	sm.captureLimiterMu.Lock()
	if sm.captureLimiter == nil || cap(sm.captureLimiter) != sm.maxConcurrent {
		sm.captureLimiter = make(chan struct{}, sm.maxConcurrent)
	}
	limiter := sm.captureLimiter
	sm.captureLimiterMu.Unlock()

	select {
	case limiter <- struct{}{}:
		return func() { <-limiter }, nil
	default:
		return nil, fmt.Errorf("截圖服務忙碌，請稍後再試")
	}
}

// ExtractMetadata 從網頁提取元信息（標題、描述、og:image）
// 廢棄警告：建議使用 CaptureScreenshotWithMetadata() 以提高性能
func (sm *ScreenshotManager) ExtractMetadata(ctx context.Context, urlStr string) *WebMetadata {
	metadata, err := sm.fetchMetadata(ctx, urlStr)
	if err != nil {
		log.Printf("[screenshot] ExtractMetadata failed: %v", err)
		return &WebMetadata{}
	}
	return metadata
}

// CleanupOldScreenshots 清理舊的截圖檔案，保留最近的 N 張。
func (sm *ScreenshotManager) CleanupOldScreenshots(maxScreenshots int) error {
	entries, err := os.ReadDir(sm.tempDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if len(entries) <= maxScreenshots {
		return nil
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var files []fileInfo
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".png" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, fileInfo{
			path:    filepath.Join(sm.tempDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].modTime.Before(files[i].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}

	filesToDelete := len(files) - maxScreenshots
	for i := 0; i < filesToDelete; i++ {
		if err := os.Remove(files[i].path); err != nil && !os.IsNotExist(err) {
			log.Printf("[screenshot] cleanup failed for %s: %v", files[i].path, err)
		}
	}

	return nil
}

func trimOutput(output []byte, limit int) string {
	if len(output) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(output))
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
