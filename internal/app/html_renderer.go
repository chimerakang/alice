package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

// HTMLRenderer 使用 Chromedp 進行 HTML 渲染
type HTMLRenderer struct {
	config *Config
}

// NewHTMLRenderer 創建新的 HTML 渲染器
func NewHTMLRenderer(config *Config) *HTMLRenderer {
	return &HTMLRenderer{
		config: config,
	}
}

// RenderHTMLToPNG 將 HTML 渲染為 PNG 圖片
// 支持 CSS/JavaScript 動態渲染
// viewport: 視口尺寸 (width x height)，例如 "1200x800"
func (r *HTMLRenderer) RenderHTMLToPNG(ctx context.Context, html string, width, height int) ([]byte, error) {
	// 驗證配置
	if !r.config.Rendering.EnableHTMLScreenshots {
		return nil, fmt.Errorf("HTML rendering is disabled in configuration")
	}

	// 設置超時
	ctxTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 創建瀏覽器上下文
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctxTimeout, opts...)
	defer allocCancel()

	// 創建新的瀏覽器實例
	chromeCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	var buf []byte

	// 構建 data URL
	dataURL := fmt.Sprintf("data:text/html;charset=utf-8,%s", html)

	// 執行渲染任務
	err := chromedp.Run(chromeCtx,
		// 設置視口大小
		chromedp.EmulateViewport(int64(width), int64(height)),

		// 導航到 HTML
		chromedp.Navigate(dataURL),

		// 等待頁面完全加載
		chromedp.WaitVisible("body"),

		// 截圖
		chromedp.CaptureScreenshot(&buf),
	)

	if err != nil {
		log.Printf("[html_renderer] Failed to render HTML: %v", err)
		return nil, fmt.Errorf("failed to render HTML: %w", err)
	}

	log.Printf("[html_renderer] Successfully rendered HTML to PNG (%d bytes, %dx%d)", len(buf), width, height)
	return buf, nil
}


// RenderHTMLWithOptions 高級渲染選項
type RenderOptions struct {
	Width       int
	Height      int
	Format      string // "png" or "pdf"
	WaitTime    time.Duration
	WaitSelector string
}

// RenderHTMLAdvanced 帶選項的高級渲染
func (r *HTMLRenderer) RenderHTMLAdvanced(ctx context.Context, html string, opts RenderOptions) ([]byte, error) {
	if !r.config.Rendering.EnableHTMLScreenshots {
		return nil, fmt.Errorf("HTML rendering is disabled in configuration")
	}

	if opts.Width == 0 {
		opts.Width = 1200
	}
	if opts.Height == 0 {
		opts.Height = 800
	}
	if opts.Format == "" {
		opts.Format = "png"
	}
	if opts.WaitTime == 0 {
		opts.WaitTime = 2 * time.Second
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	allocCtx, allocCancel := chromedp.NewExecAllocator(
		ctxTimeout,
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("no-sandbox", true),
		)...,
	)
	defer allocCancel()

	chromeCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	dataURL := fmt.Sprintf("data:text/html;charset=utf-8,%s", html)
	var buf []byte

	tasks := []chromedp.Action{
		chromedp.EmulateViewport(int64(opts.Width), int64(opts.Height)),
		chromedp.Navigate(dataURL),
	}

	// 等待特定選擇器或固定時間
	if opts.WaitSelector != "" {
		tasks = append(tasks, chromedp.WaitVisible(opts.WaitSelector))
	} else {
		// 添加固定延遲以確保 JavaScript 執行完成
		tasks = append(tasks, chromedp.Sleep(opts.WaitTime))
	}

	// PNG 截圖（暫不支持 PDF）
	tasks = append(tasks, chromedp.CaptureScreenshot(&buf))

	err := chromedp.Run(chromeCtx, tasks...)
	if err != nil {
		log.Printf("[html_renderer] Failed to render with advanced options: %v", err)
		return nil, fmt.Errorf("failed to render: %w", err)
	}

	log.Printf("[html_renderer] Successfully rendered HTML to %s (%d bytes, %dx%d)", opts.Format, len(buf), opts.Width, opts.Height)
	return buf, nil
}

// RenderHTMLFromFile 從 HTML 文件渲染
func (r *HTMLRenderer) RenderHTMLFromFile(ctx context.Context, filePath string, width, height int) ([]byte, error) {
	// 讀取檔案內容
	content, err := readFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read HTML file: %w", err)
	}

	return r.RenderHTMLToPNG(ctx, string(content), width, height)
}

// readFile 讀取檔案（輔助函數）
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
