package app

import appengine "claude-tg-agent/internal/app/engine"

type BackendKind = appengine.BackendKind

const (
	BackendClaude = appengine.BackendClaude
	BackendCodex  = appengine.BackendCodex
)

type ModelPreference = appengine.ModelPreference

type contextMessage = appengine.ContextMessage

type ChatContext = appengine.ChatContext

func NewChatContext(chatID int64, threadID int, projectDir string) *ChatContext {
	return appengine.NewChatContext(chatID, threadID, projectDir)
}

func BackendKindForModel(model string) BackendKind {
	return appengine.BackendKindForModel(model)
}
