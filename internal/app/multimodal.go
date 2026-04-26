package app

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

// MessageContent 統一的訊息內容結構
type MessageContent struct {
	Text      string   `json:"text"`                // 文字內容
	Images    []string `json:"images,omitempty"`    // 圖片檔案路徑列表
	Caption   string   `json:"caption,omitempty"`   // 圖片說明文字
	MessageID string   `json:"message_id,omitempty"` // 訊息 ID
}

// MultimodalAgent 支援多模態內容的代理介面
type MultimodalAgent interface {
	ProcessMessage(ctx context.Context, content MessageContent, onUpdate func(string, bool)) (string, error)
	SupportsImages() bool
	ProjectDir() string
}

// EnhancedAgent 增強的代理，支援多模態訊息處理
type EnhancedAgent struct {
	*Agent // 嵌入原有的 Agent
}

// NewEnhancedAgent 創建增強代理
func NewEnhancedAgent(agent *Agent) *EnhancedAgent {
	return &EnhancedAgent{Agent: agent}
}

// SupportsImages 檢查是否支援圖片處理
func (ea *EnhancedAgent) SupportsImages() bool {
	return true // Claude Code CLI 支援 --file 參數
}

// ProcessMessage 統一處理文字和圖片訊息
func (ea *EnhancedAgent) ProcessMessage(ctx context.Context, content MessageContent, onUpdate func(string, bool)) (string, error) {
	if len(content.Images) == 0 {
		// 純文字訊息，使用原有邏輯
		return ea.Agent.Run(content.Text, onUpdate)
	}

	// 包含圖片的訊息，使用增強的處理邏輯
	return ea.processMultimodalMessage(ctx, content, onUpdate)
}

// processMultimodalMessage 處理多模態訊息（文字+圖片）
func (ea *EnhancedAgent) processMultimodalMessage(ctx context.Context, content MessageContent, onUpdate func(string, bool)) (string, error) {
	if onUpdate != nil {
		imageCount := len(content.Images)
		if imageCount == 1 {
			onUpdate("🖼️ 準備分析圖片內容...", false)
		} else {
			onUpdate(fmt.Sprintf("🖼️ 準備分析 %d 張圖片...", imageCount), false)
		}
	}

	// 構建提示文字
	var prompt strings.Builder

	if content.Text != "" {
		prompt.WriteString(content.Text)
		prompt.WriteString("\n\n")
	}

	// 添加圖片描述
	for i, imagePath := range content.Images {
		if len(content.Images) == 1 {
			prompt.WriteString(fmt.Sprintf("請分析這張圖片: %s", filepath.Base(imagePath)))
		} else {
			prompt.WriteString(fmt.Sprintf("請分析第 %d 張圖片: %s", i+1, filepath.Base(imagePath)))
		}
		if i < len(content.Images)-1 {
			prompt.WriteString("\n")
		}
	}

	// 添加說明文字
	if content.Caption != "" {
		prompt.WriteString("\n\n用戶說明: ")
		prompt.WriteString(content.Caption)
	}

	// 使用增強的 CLI 客戶端處理
	return ea.runWithFiles(ctx, prompt.String(), content.Images, onUpdate)
}

// runWithFiles 使用 Claude Code CLI 的 --file 參數處理檔案
func (ea *EnhancedAgent) runWithFiles(ctx context.Context, message string, imagePaths []string, onUpdate func(string, bool)) (string, error) {
	// runWithFiles requires EnhancedCLIClient for --file flag support.
	// Other backends (CodexClient, APIClient) cannot pass image attachments —
	// the images will be silently dropped, so we surface a notice to the user
	// before falling back to a text-only Run.
	cliClient, ok := ea.Agent.client.(*CLIClient)
	if !ok {
		log.Printf("[enhanced-agent] backend %T does not support file attachments; %d image(s) will be ignored", ea.Agent.client, len(imagePaths))
		if onUpdate != nil && len(imagePaths) > 0 {
			onUpdate("⚠️ 目前 AI backend 不支援圖片附件，僅以文字訊息處理。", false)
		}
		return ea.Agent.Run(message, onUpdate)
	}

	// 使用專門的增強 CLI 客戶端
	enhancedClient := &EnhancedCLIClient{
		CLIClient: cliClient,
	}

	ps := ea.Agent.current()
	log.Printf("[enhanced-agent] calling CLI with %d files, session=%s, project=%s",
		len(imagePaths), ps.sessionID, ea.Agent.projectDir)

	if onUpdate != nil {
		onUpdate("🤖 Claude 正在分析圖片內容...", false)
	}

	// 嘗試使用流式 API（如果支援的話）
	if len(imagePaths) == 1 {
		// 單張圖片，使用非流式 API 確保穩定性
		resp, err := enhancedClient.CallWithFiles(ctx, message, imagePaths, ea.Agent.projectDir, ps.sessionID)
		if err != nil {
			return "", fmt.Errorf("enhanced CLI call error: %w", err)
		}

		if resp.IsError {
			return "", fmt.Errorf("Claude analysis error: %s", resp.Result)
		}

		return resp.Result, nil
	} else {
		// 多張圖片，使用流式 API 提供進度回饋
		resp, err := enhancedClient.CallStreamWithFiles(ctx, message, imagePaths, ea.Agent.projectDir, ps.sessionID,
			func(toolName string, toolInput map[string]interface{}) {
				// 工具使用回調
				if onUpdate != nil {
					msg := fmt.Sprintf("🔧 使用工具: %s", toolName)
					onUpdate(msg, true)
				}
			},
			func(contentType, text string) {
				// 內容回調
				if onUpdate != nil && text != "" {
					switch contentType {
					case "thinking":
						if len(text) > 100 {
							text = text[:100] + "..."
						}
						onUpdate("🧠 "+text, true)
					case "text":
						if len(text) > 200 {
							text = text[:200] + "..."
						}
						onUpdate("💬 "+text, true)
					}
				}
			})

		if err != nil {
			return "", fmt.Errorf("enhanced CLI stream error: %w", err)
		}

		if resp.IsError {
			return "", fmt.Errorf("Claude analysis error: %s", resp.Result)
		}

		return resp.Result, nil
	}
}

// CreateMessageContent 創建訊息內容的輔助函數
func CreateMessageContent(text string) MessageContent {
	return MessageContent{
		Text: text,
	}
}

// CreateImageMessageContent 創建包含圖片的訊息內容
func CreateImageMessageContent(text string, imagePaths []string, caption string) MessageContent {
	return MessageContent{
		Text:    text,
		Images:  imagePaths,
		Caption: caption,
	}
}

// ValidateMessageContent 驗證訊息內容
func ValidateMessageContent(content MessageContent) error {
	if content.Text == "" && len(content.Images) == 0 {
		return fmt.Errorf("message must contain either text or images")
	}

	// 檢查圖片檔案是否存在
	for _, imagePath := range content.Images {
		if !fileExists(imagePath) {
			return fmt.Errorf("image file not found: %s", imagePath)
		}
	}

	return nil
}