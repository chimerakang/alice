package engine

import (
	"fmt"
	"strings"
	"time"
)

// BackendKind identifies the native session namespace for a chat.
type BackendKind int

const (
	BackendClaude BackendKind = iota
	BackendCodex
)

// BackendAuto lets routing helpers pick the opposite backend dynamically.
const BackendAuto BackendKind = -1

// ModelPreference stores a chat's explicit model routing preference.
type ModelPreference string

// ContextMessage saves one turn for cross-engine context bridging.
type ContextMessage struct {
	Role    string // "user" or "assistant"
	Content string // truncated with head+tail preservation
}

// ChatState is the user-facing lifecycle state for one chat/topic.
// It is intentionally small: detailed task, session, and execution state live
// in their own agents instead of being folded into ChatContext.
type ChatState string

const (
	ChatStateIdle          ChatState = "idle"
	ChatStateReceiving     ChatState = "receiving"
	ChatStateRouting       ChatState = "routing"
	ChatStateDispatching   ChatState = "dispatching"
	ChatStateBusy          ChatState = "busy"
	ChatStateAwaitingInput ChatState = "awaiting_input"
	ChatStateInterrupting  ChatState = "interrupting"
)

// ChatContext is the shared conversation state for a Telegram chat/topic.
type ChatContext struct {
	ChatID       int64
	ThreadID     int
	ProjectDir   string
	RecentMsgs   []ContextMessage
	Sessions     map[BackendKind]string
	LastBackend  BackendKind
	Pref         ModelPreference
	CurrentState ChatState
	StateSince   time.Time
	StateReason  string
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
		CurrentState: ChatStateIdle,
		StateSince:   now,
		CreatedAt:    now,
		LastActivity: now,
	}
}

// TransitionState validates and records a Chat FSM transition.
func (c *ChatContext) TransitionState(to ChatState, reason string) error {
	if c == nil {
		return nil
	}
	from := c.CurrentState
	if from == "" {
		from = ChatStateIdle
	}
	if !ValidChatStateTransition(from, to) {
		return fmt.Errorf("invalid chat state transition: %s -> %s", from, to)
	}
	if c.CurrentState != to {
		c.CurrentState = to
		c.StateSince = time.Now()
	}
	c.StateReason = reason
	return nil
}

func (c *ChatContext) StateSnapshot() StateSnapshot {
	if c == nil {
		return StateSnapshot{Agent: "chat", State: string(ChatStateIdle), Terminal: true, Reason: "nil_context"}
	}
	state := c.CurrentState
	if state == "" {
		state = ChatStateIdle
	}
	since := c.StateSince
	if since.IsZero() {
		since = c.CreatedAt
	}
	return StateSnapshot{
		Agent:  "chat",
		State:  string(state),
		Since:  since,
		Reason: c.StateReason,
	}
}

// State implements the RuntimeAgent state surface for ChatContext.
func (c *ChatContext) State() StateSnapshot {
	return c.StateSnapshot()
}

// Name implements the RuntimeAgent identity surface for ChatContext.
func (c *ChatContext) Name() string {
	return "chat"
}

func ValidChatStateTransition(from, to ChatState) bool {
	if to == "" {
		return false
	}
	if from == "" || from == to {
		return true
	}
	switch from {
	case ChatStateIdle:
		return to == ChatStateReceiving || to == ChatStateRouting || to == ChatStateDispatching || to == ChatStateBusy || to == ChatStateAwaitingInput
	case ChatStateReceiving:
		return to == ChatStateRouting || to == ChatStateIdle
	case ChatStateRouting:
		return to == ChatStateDispatching || to == ChatStateAwaitingInput || to == ChatStateIdle
	case ChatStateDispatching:
		return to == ChatStateBusy || to == ChatStateAwaitingInput || to == ChatStateIdle
	case ChatStateBusy:
		return to == ChatStateInterrupting || to == ChatStateAwaitingInput || to == ChatStateIdle
	case ChatStateAwaitingInput:
		return to == ChatStateReceiving || to == ChatStateRouting || to == ChatStateDispatching || to == ChatStateBusy || to == ChatStateIdle
	case ChatStateInterrupting:
		return to == ChatStateBusy || to == ChatStateIdle
	default:
		return false
	}
}

func AssistantAwaitsInput(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	choiceSignals := []string{
		"(a)", "(b)", "a)", "b)", "a.", "b.",
		"選項", "选项", "方案", "option",
		"第一個", "第一个", "第二個", "第二个",
	}
	hasChoiceSignal := false
	for _, signal := range choiceSignals {
		if strings.Contains(lower, signal) {
			hasChoiceSignal = true
			break
		}
	}
	if hasChoiceSignal {
		waitingPhrases := []string{
			"要繼續做哪個", "要继续做哪个", "想先做哪個", "想先做哪个",
			"你想", "你要", "是否要", "要我", "要不要",
			"which one", "which option", "what would you like",
		}
		for _, phrase := range waitingPhrases {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}
	questionTails := []string{
		"要我繼續嗎？", "要我继续吗？", "要繼續嗎？", "要继续吗？",
		"要我現在做嗎？", "要我现在做吗？",
		"should i continue?", "want me to continue?",
	}
	for _, tail := range questionTails {
		if strings.HasSuffix(lower, tail) {
			return true
		}
	}
	return false
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
	truncate := func(s string) string {
		runes := []rune(s)
		const maxLen = 900
		if len(runes) <= maxLen {
			return s
		}
		const edgeLen = 430
		return string(runes[:edgeLen]) + "\n...[middle omitted for continuity]...\n" + string(runes[len(runes)-edgeLen:])
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
