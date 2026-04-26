package engine

import (
	"strings"
	"time"
)

// BackendKind identifies the native session namespace for a chat.
type BackendKind int

const (
	BackendClaude BackendKind = iota
	BackendCodex
)

// ModelPreference stores a chat's explicit model routing preference.
type ModelPreference string

// ContextMessage saves one turn for cross-engine context bridging.
type ContextMessage struct {
	Role    string // "user" or "assistant"
	Content string // truncated to 500 chars
}

// ChatContext is the shared conversation state for a Telegram chat/topic.
type ChatContext struct {
	ChatID       int64
	ThreadID     int
	ProjectDir   string
	RecentMsgs   []ContextMessage
	Sessions     map[BackendKind]string
	LastBackend  BackendKind
	Pref         ModelPreference
	CreatedAt    time.Time
	LastActivity time.Time
}

func NewChatContext(chatID int64, threadID int, projectDir string) *ChatContext {
	now := time.Now()
	return &ChatContext{
		ChatID:       chatID,
		ThreadID:     threadID,
		ProjectDir:   projectDir,
		Sessions:     make(map[BackendKind]string),
		CreatedAt:    now,
		LastActivity: now,
	}
}

func BackendKindForModel(model string) BackendKind {
	lower := strings.ToLower(model)
	if strings.Contains(lower, "gpt") || strings.Contains(lower, "codex") {
		return BackendCodex
	}
	return BackendClaude
}

func (c *ChatContext) Session(backend BackendKind) string {
	if c == nil || c.Sessions == nil {
		return ""
	}
	return c.Sessions[backend]
}

func (c *ChatContext) SetSession(backend BackendKind, sessionID string) {
	if c == nil || sessionID == "" {
		return
	}
	if c.Sessions == nil {
		c.Sessions = make(map[BackendKind]string)
	}
	c.Sessions[backend] = sessionID
	c.LastBackend = backend
	c.LastActivity = time.Now()
}

func (c *ChatContext) ClearSession(backend BackendKind) {
	if c == nil || c.Sessions == nil {
		return
	}
	delete(c.Sessions, backend)
}

func (c *ChatContext) AddRecentMessage(userMsg, assistantMsg string) {
	if c == nil {
		return
	}
	const maxLen = 500
	truncate := func(s string) string {
		runes := []rune(s)
		if len(runes) > maxLen {
			return string(runes[:maxLen]) + "..."
		}
		return s
	}
	c.RecentMsgs = append(c.RecentMsgs,
		ContextMessage{Role: "user", Content: truncate(userMsg)},
		ContextMessage{Role: "assistant", Content: truncate(assistantMsg)},
	)
	if len(c.RecentMsgs) > 10 {
		c.RecentMsgs = c.RecentMsgs[len(c.RecentMsgs)-10:]
	}
	c.LastActivity = time.Now()
}

func (c *ChatContext) RecentMessagesSnapshot() []ContextMessage {
	if c == nil {
		return nil
	}
	out := make([]ContextMessage, len(c.RecentMsgs))
	copy(out, c.RecentMsgs)
	return out
}
