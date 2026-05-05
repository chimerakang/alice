package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/google/uuid"
)

// EditRecord 記錄單次修改
type EditRecord struct {
	Prompt    string    `json:"prompt"`
	Timestamp time.Time `json:"timestamp"`
}

// Prototype 代表一個 UI 原型
type Prototype struct {
	ID             string       `json:"id"`
	ChatID         int64        `json:"chat_id"`
	OriginalPrompt string       `json:"original_prompt"`
	CurrentHTML    string       `json:"current_html"`
	EditHistory    []EditRecord `json:"edit_history"`
	Version        int          `json:"version"`
	MessageID      int64        `json:"message_id"` // TG 截圖訊息 ID
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// PrototypeManager 管理 UI 原型的生命週期
type PrototypeManager struct {
	client    Client
	outputDir string
	storage   Storage
}

// NewPrototypeManager 建立 PrototypeManager
func NewPrototypeManager(client Client, outputDir string, storage Storage) (*PrototypeManager, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("prototype outputDir: %w", err)
	}
	return &PrototypeManager{
		client:    client,
		outputDir: outputDir,
		storage:   storage,
	}, nil
}

const generatePromptTmpl = `你是一個 UI 原型設計師。根據以下描述生成完整的單頁 HTML 原型：

描述：%s

要求：
- 使用 Tailwind CSS CDN（<script src="https://cdn.tailwindcss.com"></script>）
- 響應式設計（mobile-first）
- 所有文字使用繁體中文
- 圖片使用 placeholder（https://placehold.co/寬x高）
- 輸出完整 index.html，包含所有 CSS/JS inline
- body 寬度固定 1280px 用於截圖（max-w-[1280px] mx-auto）
- 只輸出 HTML 程式碼，不要加任何說明文字`

const editPromptTmpl = `以下是現有的 HTML 原型。請根據使用者的修改需求調整 HTML。
只修改需要改的部分，保持其他部分不變。
輸出修改後的完整 HTML，不要加任何說明文字。

修改需求：%s

現有 HTML：
%s`

var htmlBlockRe = regexp.MustCompile("(?s)```html\n?(.*?)```")
var htmlTagRe = regexp.MustCompile("(?s)<!DOCTYPE[^>]*>.*</html>")

// extractHTML 從 Claude 回應中萃取 HTML 內容
func extractHTML(text string) string {
	// 先找 ```html ... ``` 區塊
	if m := htmlBlockRe.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	// 直接找 <!DOCTYPE...></html>
	if m := htmlTagRe.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	return strings.TrimSpace(text)
}

// Generate 根據描述生成新的 UI 原型
func (pm *PrototypeManager) Generate(ctx context.Context, chatID int64, prompt string) (*Prototype, error) {
	cliPrompt := fmt.Sprintf(generatePromptTmpl, prompt)
	resp, err := pm.client.Call(ctx, cliPrompt, pm.outputDir, "", "")
	if err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}

	html := extractHTML(resp.Result)
	if html == "" {
		html = extractHTML(resp.TextContent)
	}
	if html == "" {
		return nil, fmt.Errorf("generate: no HTML found in response")
	}

	now := time.Now()
	p := &Prototype{
		ID:             uuid.New().String(),
		ChatID:         chatID,
		OriginalPrompt: prompt,
		CurrentHTML:    html,
		EditHistory:    []EditRecord{},
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := pm.storage.CreatePrototype(*p); err != nil {
		return nil, fmt.Errorf("generate: save: %w", err)
	}
	return p, nil
}

// Edit 根據修改描述更新現有原型
func (pm *PrototypeManager) Edit(ctx context.Context, id string, editPrompt string) (*Prototype, error) {
	p, err := pm.storage.GetPrototype(id)
	if err != nil {
		return nil, fmt.Errorf("edit: get: %w", err)
	}

	cliPrompt := fmt.Sprintf(editPromptTmpl, editPrompt, p.CurrentHTML)
	resp, err := pm.client.Call(ctx, cliPrompt, pm.outputDir, "", "")
	if err != nil {
		return nil, fmt.Errorf("edit: %w", err)
	}

	html := extractHTML(resp.Result)
	if html == "" {
		html = extractHTML(resp.TextContent)
	}
	if html == "" {
		return nil, fmt.Errorf("edit: no HTML found in response")
	}

	p.EditHistory = append(p.EditHistory, EditRecord{Prompt: editPrompt, Timestamp: time.Now()})
	p.CurrentHTML = html
	p.Version++
	p.UpdatedAt = time.Now()

	if err := pm.storage.UpdatePrototype(p); err != nil {
		return nil, fmt.Errorf("edit: save: %w", err)
	}
	return &p, nil
}

// Screenshot 對 HTML 進行伺服器端截圖，回傳 PNG bytes
func (pm *PrototypeManager) Screenshot(ctx context.Context, html string) ([]byte, error) {
	// 寫入暫存檔
	tmp, err := os.CreateTemp("", "prototype-*.html")
	if err != nil {
		return nil, fmt.Errorf("screenshot: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(html); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("screenshot: write: %w", err)
	}
	tmp.Close()

	fileURL := "file://" + tmpPath

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(1280, 900),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	chromCtx, cancelChrom := chromedp.NewContext(allocCtx)
	defer cancelChrom()

	var pngBuf []byte
	if err := chromedp.Run(chromCtx,
		chromedp.Navigate(fileURL),
		chromedp.FullScreenshot(&pngBuf, 90),
	); err != nil {
		return nil, fmt.Errorf("screenshot: chromedp: %w", err)
	}
	return pngBuf, nil
}

// List 列出特定 chat 的所有原型
func (pm *PrototypeManager) List(ctx context.Context, chatID int64) ([]Prototype, error) {
	return pm.storage.ListPrototypesByChat(chatID)
}

// Export 將原型 HTML 寫入檔案，返回路徑
func (pm *PrototypeManager) Export(ctx context.Context, id string) (string, error) {
	p, err := pm.storage.GetPrototype(id)
	if err != nil {
		return "", fmt.Errorf("export: %w", err)
	}
	outPath := filepath.Join(pm.outputDir, fmt.Sprintf("prototype-%s-v%d.html", shortPrototypeID(id), p.Version))
	if err := os.WriteFile(outPath, []byte(p.CurrentHTML), 0644); err != nil {
		return "", fmt.Errorf("export: write: %w", err)
	}
	return outPath, nil
}

func shortPrototypeID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
