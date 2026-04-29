package app

import (
	"context"
	"testing"
	"time"
)

type modelRecordingClient struct {
	model    string
	calls    []string
	sessions []string
	messages []string
}

func (c *modelRecordingClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (c *modelRecordingClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	c.calls = append(c.calls, modelOverride)
	c.sessions = append(c.sessions, sessionID)
	c.messages = append(c.messages, message)
	resp := &CLIResponse{
		Result:       "ok",
		TextContent:  "ok",
		SessionID:    "session-" + modelOverride,
		TotalCostUSD: 0,
	}
	resp.Usage.InputTokens = 1
	resp.Usage.OutputTokens = 1
	return resp, nil
}

func (c *modelRecordingClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	return nil, nil
}

func (c *modelRecordingClient) GetModel() string {
	if c.model != "" {
		return c.model
	}
	return "sonnet"
}

// TestSelectModelHaikuRules tests that Haiku rules are correctly identified
func TestSelectModelHaikuRules(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message        string
		expectedModel  string
		expectedReason string
	}{
		{"Please translate this to English", "haiku", "static_rule"},
		{"Can you summarize this file?", "haiku", "static_rule"},
		{"Explain how this function works", "haiku", "static_rule"},
		{"Convert this to JSON format", "haiku", "static_rule"},
		{"Read the config.json file", "haiku", "static_rule"},
		{"What's the status of the build?", "haiku", "static_rule"},
		{"Polish this code snippet", "haiku", "static_rule"},
	}

	for _, tc := range testCases {
		model, reason := agent.selectModel(tc.message)
		if model != tc.expectedModel {
			t.Errorf("Message: %q\nExpected model: %s, got: %s", tc.message, tc.expectedModel, model)
		}
		if reason != tc.expectedReason {
			t.Errorf("Message: %q\nExpected reason: %s, got: %s", tc.message, tc.expectedReason, reason)
		}
	}
}

// TestSelectModelOpusRules tests that Opus rules are correctly identified
func TestSelectModelOpusRules(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message       string
		expectedModel string
	}{
		{"Refactor this entire module", "opus"},
		{"Design the architecture for this system", "opus"},
		{"Update multiple files across the codebase", "opus"},
		{"Debug this complex race condition", "opus"},
		{"Implement a new sorting algorithm", "opus"},
		{"Optimize the performance of this database query", "opus"},
	}

	for _, tc := range testCases {
		model, _ := agent.selectModel(tc.message)
		if model != tc.expectedModel {
			t.Errorf("Message: %q\nExpected model: %s, got: %s", tc.message, tc.expectedModel, model)
		}
	}
}

// TestSelectModelDefaultFallback tests that default Sonnet is returned for unmatched messages
func TestSelectModelDefaultFallback(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message string
	}{
		{"Hello, how are you?"},
		{"What's the weather like?"},
		{"Tell me a joke"},
		{"Random message"},
	}

	for _, tc := range testCases {
		model, reason := agent.selectModel(tc.message)
		if model != "sonnet" {
			t.Errorf("Message: %q\nExpected default model 'sonnet', got: %s", tc.message, model)
		}
		if reason != "default" {
			t.Errorf("Message: %q\nExpected default reason, got: %s", tc.message, reason)
		}
	}
}

// TestSelectModelCaseInsensitive tests that pattern matching is case-insensitive
func TestSelectModelCaseInsensitive(t *testing.T) {
	agent := &Agent{}

	testCases := []struct {
		message       string
		expectedModel string
	}{
		{"TRANSLATE this text", "haiku"},
		{"Translate THIS text", "haiku"},
		{"TrAnSlAtE this text", "haiku"},
		{"REFACTOR my code", "opus"},
		{"Refactor MY code", "opus"},
		{"ReFaCtOr my code", "opus"},
	}

	for _, tc := range testCases {
		model, _ := agent.selectModel(tc.message)
		if model != tc.expectedModel {
			t.Errorf("Message: %q\nExpected model: %s, got: %s", tc.message, tc.expectedModel, model)
		}
	}
}

// TestSelectModelPriorityOrder tests that higher priority (lower number) rules take precedence
func TestSelectModelPriorityOrder(t *testing.T) {
	agent := &Agent{}

	// Message containing both translation (Priority 1) and refactor (Priority 20)
	// Should choose translation (Haiku) due to lower priority number
	message := "Translate and refactor this code"
	model, _ := agent.selectModel(message)
	if model != "haiku" {
		t.Errorf("Message: %q\nExpected Haiku (priority 1), got: %s", message, model)
	}
}

func TestIsContinuationMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "explicit chinese continuation phrase",
			message: "但是 TV Timer RN 有沒有修？",
			want:    true,
		},
		{
			name:    "explicit english continuation phrase",
			message: "also update the failing tests",
			want:    true,
		},
		{
			name:    "pronoun reference chinese",
			message: "這個也一起修",
			want:    true,
		},
		{
			name:    "pronoun reference english",
			message: "can you fix that too?",
			want:    true,
		},
		{
			name:    "short chinese why question",
			message: "為什麼？",
			want:    true,
		},
		{
			name:    "short english how question",
			message: "how?",
			want:    true,
		},
		{
			name:    "issue scenario short follow up",
			message: "TV Timer RN 有沒有修？",
			want:    true,
		},
		{
			name:    "new long request",
			message: "請幫我在新的專案裡設計完整的資料庫 migration 與 API 實作",
			want:    false,
		},
		{
			name:    "blank message",
			message: "   ",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isContinuationMessage(tt.message); got != tt.want {
				t.Fatalf("isContinuationMessage(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}

func TestAgentRunKeepsContinuationModelWhenStickyDisabled(t *testing.T) {
	client := &modelRecordingClient{model: "sonnet"}
	agent := NewAgent(client, "/tmp/project", 1, 0)
	agent.SetRoutingConfig(ModelRoutingConfig{StickySession: false, SessionIdleTimeoutMin: 5})
	agent.SetModelOverride("opus")

	if _, err := agent.Run("Refactor this entire module", nil); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	agent.SetModelOverride("")

	if _, err := agent.Run("TV Timer RN 有沒有修？", nil); err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("CallStream calls = %d, want 2", len(client.calls))
	}
	if client.calls[1] != "opus" {
		t.Fatalf("second model = %q, want follow-up opus", client.calls[1])
	}
}

func TestAgentRunKeepsLastModelForActiveStickySession(t *testing.T) {
	client := &modelRecordingClient{model: "sonnet"}
	agent := NewAgent(client, "/tmp/project", 1, 0)
	agent.SetModelOverride("opus")

	if _, err := agent.Run("Refactor this entire module", nil); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	agent.SetModelOverride("")

	if _, err := agent.Run("status?", nil); err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("CallStream calls = %d, want 2", len(client.calls))
	}
	if client.calls[1] != "opus" {
		t.Fatalf("second model = %q, want sticky opus", client.calls[1])
	}
}

func TestAgentRunRetriesTriageAfterStickyIdleTimeout(t *testing.T) {
	client := &modelRecordingClient{model: "sonnet"}
	agent := NewAgent(client, "/tmp/project", 1, 0)
	agent.SetRoutingConfig(ModelRoutingConfig{StickySession: true, SessionIdleTimeoutMin: 5})
	agent.SetModelOverride("opus")

	if _, err := agent.Run("Refactor this entire module", nil); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	agent.SetModelOverride("")
	agent.current().ctx.LastActivity = time.Now().Add(-6 * time.Minute)

	if _, err := agent.Run("translate this", nil); err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("CallStream calls = %d, want 2", len(client.calls))
	}
	if client.calls[1] != "haiku" {
		t.Fatalf("second model = %q, want re-triaged haiku", client.calls[1])
	}
	if client.sessions[1] != "" {
		t.Fatalf("second session = %q, want fresh session after idle timeout", client.sessions[1])
	}
}

func TestAgentClearSessionDropsStickyModelAndRecentBridge(t *testing.T) {
	client := &modelRecordingClient{model: "sonnet"}
	agent := NewAgent(client, "/tmp/project", 1, 0)
	agent.SetModelOverride("opus")

	if _, err := agent.Run("Refactor this entire module", nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	agent.AddRecentMessage("之前的問題", "之前的答案")

	agent.ClearSession()

	if agent.LastUsedModel() != "" {
		t.Fatalf("LastUsedModel = %q, want empty", agent.LastUsedModel())
	}
	if got := len(agent.RecentMessagesSnapshot()); got != 0 {
		t.Fatalf("recent messages = %d, want 0", got)
	}
	if got := agent.SessionID(); got != "" {
		t.Fatalf("session = %q, want empty", got)
	}
}

func TestPrepareManualModelSwitchDropsBridgeAndTargetSession(t *testing.T) {
	chatCtx := NewChatContext(1, 0, "/tmp/project")
	chatCtx.SetSession(BackendClaude, "old-claude")
	chatCtx.SetSession(BackendCodex, "old-codex")
	chatCtx.AddRecentMessage("之前的問題", "之前的答案")
	agent := NewAgentWithContext(&modelRecordingClient{model: "sonnet"}, chatCtx)
	agent.lastUsedModel = "claude-opus-4-6"

	if !agent.PrepareManualModelSwitch("gpt-5.5") {
		t.Fatal("PrepareManualModelSwitch returned false, want true")
	}
	if got := chatCtx.Session(BackendCodex); got != "" {
		t.Fatalf("codex session = %q, want empty", got)
	}
	if got := chatCtx.Session(BackendClaude); got != "old-claude" {
		t.Fatalf("claude session = %q, want old-claude", got)
	}
	if agent.LastUsedModel() != "" {
		t.Fatalf("LastUsedModel = %q, want empty", agent.LastUsedModel())
	}
	if got := len(agent.RecentMessagesSnapshot()); got != 0 {
		t.Fatalf("recent messages = %d, want 0", got)
	}
}

func TestPrepareManualModelSwitchKeepsSameActiveModel(t *testing.T) {
	chatCtx := NewChatContext(1, 0, "/tmp/project")
	chatCtx.SetSession(BackendClaude, "active-claude")
	chatCtx.AddRecentMessage("之前的問題", "之前的答案")
	agent := NewAgentWithContext(&modelRecordingClient{model: "sonnet"}, chatCtx)
	agent.lastUsedModel = "claude-sonnet-4-6"

	if agent.PrepareManualModelSwitch("claude-sonnet-4-6") {
		t.Fatal("PrepareManualModelSwitch returned true for same active model")
	}
	if got := chatCtx.Session(BackendClaude); got != "active-claude" {
		t.Fatalf("claude session = %q, want active-claude", got)
	}
	if got := len(agent.RecentMessagesSnapshot()); got == 0 {
		t.Fatal("recent messages were cleared for same active model")
	}
}
