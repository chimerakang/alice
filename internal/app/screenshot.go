package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/go-rod/rod"
)

// WebMetadata 網頁元信息
type WebMetadata struct {
	Title       string // 頁面標題
	Description string // 頁面描述
	ImageURL    string // og:image 或 twitter:image
}

// ScreenshotManager 管理網頁截圖功能
type ScreenshotManager struct {
	timeoutSec int
	maxRetries int
	tempDir    string
}

// NewScreenshotManager 建立截圖管理器
func NewScreenshotManager() *ScreenshotManager {
	return &ScreenshotManager{
		timeoutSec: 30,
		maxRetries: 2,
		tempDir:    "temp/screenshots",
	}
}

// CaptureScreenshot 截圖指定 URL
// 返回：(圖片檔案路徑, 錯誤)
func (sm *ScreenshotManager) CaptureScreenshot(ctx context.Context, urlStr string) (string, error) {
	// 驗證 URL
	if err := sm.validateURL(urlStr); err != nil {
		return "", fmt.Errorf("URL 驗證失敗: %w", err)
	}

	// 建立臨時目錄
	if err := os.MkdirAll(sm.tempDir, 0755); err != nil {
		return "", fmt.Errorf("無法建立臨時目錄: %w", err)
	}

	// 帶重試的截圖
	var lastErr error
	for attempt := 0; attempt <= sm.maxRetries; attempt++ {
		filePath, err := sm.captureWithRod(ctx, urlStr)
		if err == nil {
			return filePath, nil
		}
		lastErr = err

		// 如果不是最後一次嘗試，等待後重試
		if attempt < sm.maxRetries {
			time.Sleep(time.Second)
		}
	}

	return "", fmt.Errorf("截圖失敗（重試 %d 次）: %w", sm.maxRetries, lastErr)
}

// captureWithRod 使用 rod 進行單次截圖
func (sm *ScreenshotManager) captureWithRod(ctx context.Context, urlStr string) (string, error) {
	// 設置超時
	ctx, cancel := context.WithTimeout(ctx, time.Duration(sm.timeoutSec)*time.Second)
	defer cancel()

	// 啟動瀏覽器 (如果已啟動過會重複使用)
	browser := rod.New().MustConnect()
	defer browser.MustClose()

	// 開啟頁面
	page := browser.MustPage(urlStr)
	defer page.MustClose()

	// 等待頁面加載（先等 load 事件，再等 JS/網路穩定）
	page.MustWaitLoad()
	page.MustWaitIdle() // 等待頁面完全靜止（無 JS 執行、無網路請求）
	time.Sleep(500 * time.Millisecond) // 額外等待確保最終渲染完成

	// 獲取頁面截圖 (PNG 格式)
	buf := page.MustScreenshot()
	if len(buf) == 0 {
		return "", fmt.Errorf("截圖資料為空")
	}

	// 保存截圖
	filePath, err := sm.saveScreenshot(buf)
	if err != nil {
		return "", fmt.Errorf("保存截圖失敗: %w", err)
	}

	return filePath, nil
}

// validateURL 驗證 URL 格式
func (sm *ScreenshotManager) validateURL(urlStr string) error {
	if urlStr == "" {
		return fmt.Errorf("URL 不能為空")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("URL 解析失敗: %w", err)
	}

	// 檢查 scheme
	if u.Scheme == "" {
		return fmt.Errorf("URL 必須包含 scheme (http/https)，例如：https://example.com")
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("僅支援 http/https，不支援 %s", u.Scheme)
	}

	// 檢查主機名
	if u.Host == "" {
		return fmt.Errorf("URL 必須包含主機名")
	}

	return nil
}

// saveScreenshot 保存截圖到檔案
func (sm *ScreenshotManager) saveScreenshot(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("截圖資料為空")
	}

	// 生成檔名 (使用時間戳確保唯一性)
	filename := fmt.Sprintf("screenshot_%d.png", time.Now().UnixMilli())
	filePath := filepath.Join(sm.tempDir, filename)

	// 寫入檔案
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("寫入檔案失敗: %w", err)
	}

	return filePath, nil
}

// CaptureScreenshotWithMetadata 在單一 rod browser 中完成截圖和提取 metadata
// 返回：(圖片檔案路徑, metadata, 錯誤)
// 性能優化：避免為同一 URL 開啟兩個 rod browser 實例
func (sm *ScreenshotManager) CaptureScreenshotWithMetadata(ctx context.Context, urlStr string) (string, *WebMetadata, error) {
	// 驗證 URL
	if err := sm.validateURL(urlStr); err != nil {
		return "", nil, fmt.Errorf("URL 驗證失敗: %w", err)
	}

	// 建立臨時目錄
	if err := os.MkdirAll(sm.tempDir, 0755); err != nil {
		return "", nil, fmt.Errorf("無法建立臨時目錄: %w", err)
	}

	// 帶重試的操作
	var lastErr error
	for attempt := 0; attempt <= sm.maxRetries; attempt++ {
		filePath, metadata, err := sm.captureAndExtractWithRod(ctx, urlStr)
		if err == nil {
			return filePath, metadata, nil
		}
		lastErr = err

		// 如果不是最後一次嘗試，等待後重試
		if attempt < sm.maxRetries {
			time.Sleep(time.Second)
		}
	}

	return "", nil, fmt.Errorf("操作失敗（重試 %d 次）: %w", sm.maxRetries, lastErr)
}

// captureAndExtractWithRod 單一 rod browser 實例完成截圖和 metadata 提取
func (sm *ScreenshotManager) captureAndExtractWithRod(ctx context.Context, urlStr string) (string, *WebMetadata, error) {
	// 設置超時
	ctx, cancel := context.WithTimeout(ctx, time.Duration(sm.timeoutSec)*time.Second)
	defer cancel()

	// 啟動瀏覽器
	browser := rod.New().MustConnect()
	defer browser.MustClose()

	// 開啟頁面
	page := browser.MustPage(urlStr)
	defer page.MustClose()

	// 等待頁面加載（先等 load 事件，再等 JS/網路穩定）
	page.MustWaitLoad()
	page.MustWaitIdle() // 等待頁面完全靜止
	time.Sleep(500 * time.Millisecond) // 額外等待確保最終渲染完成

	// 獲取 HTML 並提取 metadata
	html := page.MustHTML()
	metadata := sm.extractMetadataFromHTML(html)

	// 獲取頁面截圖
	buf := page.MustScreenshot()
	if len(buf) == 0 {
		return "", nil, fmt.Errorf("截圖資料為空")
	}

	// 保存截圖
	filePath, err := sm.saveScreenshot(buf)
	if err != nil {
		return "", nil, fmt.Errorf("保存截圖失敗: %w", err)
	}

	return filePath, metadata, nil
}

// extractMetadataFromHTML 從 HTML 字串提取元信息
func (sm *ScreenshotManager) extractMetadataFromHTML(html string) *WebMetadata {
	metadata := &WebMetadata{
		Title:       "",
		Description: "",
		ImageURL:    "",
	}

	// 提取 title
	titleRe := regexp.MustCompile(`<title[^>]*>([^<]+)</title>`)
	if matches := titleRe.FindStringSubmatch(html); len(matches) > 1 {
		metadata.Title = matches[1]
	}

	// 提取 og:description
	descRe := regexp.MustCompile(`<meta\s+property=["']og:description["']\s+content=["']([^"']+)["']`)
	if matches := descRe.FindStringSubmatch(html); len(matches) > 1 {
		metadata.Description = matches[1]
	} else {
		// 降級到 meta description
		descRe := regexp.MustCompile(`<meta\s+name=["']description["']\s+content=["']([^"']+)["']`)
		if matches := descRe.FindStringSubmatch(html); len(matches) > 1 {
			metadata.Description = matches[1]
		}
	}

	// 提取 og:image
	imgRe := regexp.MustCompile(`<meta\s+property=["']og:image["']\s+content=["']([^"']+)["']`)
	if matches := imgRe.FindStringSubmatch(html); len(matches) > 1 {
		metadata.ImageURL = matches[1]
	} else {
		// 降級到 twitter:image
		imgRe := regexp.MustCompile(`<meta\s+property=["']twitter:image["']\s+content=["']([^"']+)["']`)
		if matches := imgRe.FindStringSubmatch(html); len(matches) > 1 {
			metadata.ImageURL = matches[1]
		}
	}

	return metadata
}

// ExtractMetadata 從網頁提取元信息（標題、描述、og:image）
// 廢棄警告：建議使用 CaptureScreenshotWithMetadata() 以提高性能
func (sm *ScreenshotManager) ExtractMetadata(ctx context.Context, urlStr string) *WebMetadata {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	browser := rod.New().MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(urlStr)
	defer page.MustClose()

	// 等待頁面加載
	page.MustWaitLoad()

	// 獲取 HTML
	html := page.MustHTML()

	// 使用共享的 metadata 提取邏輯
	return sm.extractMetadataFromHTML(html)
}

// CleanupOldScreenshots 清理舊的截圖檔案，保留最近的 N 張
func (sm *ScreenshotManager) CleanupOldScreenshots(maxScreenshots int) error {
	entries, err := os.ReadDir(sm.tempDir)
	if err != nil {
		// 目錄不存在時返回 nil（不是錯誤）
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// 如果檔案數量未超過限制，無需清理
	if len(entries) <= maxScreenshots {
		return nil
	}

	// 按修改時間排序並刪除最舊的檔案
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var files []fileInfo
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".png" {
			info, _ := entry.Info()
			files = append(files, fileInfo{
				path:    filepath.Join(sm.tempDir, entry.Name()),
				modTime: info.ModTime(),
			})
		}
	}

	// 按時間排序（舊到新）
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].modTime.Before(files[i].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}

	// 刪除超過限制的最舊檔案
	filesToDelete := len(files) - maxScreenshots
	for i := 0; i < filesToDelete; i++ {
		if err := os.Remove(files[i].path); err != nil {
			// 忽略刪除錯誤，繼續處理其他檔案
		}
	}

	return nil
}
