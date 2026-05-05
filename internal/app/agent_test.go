package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
)

type modelRecordingClient struct {
	model    string
	response string
	err      error
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
	if c.err != nil {
		return &CLIResponse{TextContent: "partial", SessionID: "session-" + modelOverride}, c.err
	}
	result := c.response
	if result == "" {
		result = "ok"
	}
	resp := &CLIResponse{
		Result:       result,
		TextContent:  result,
		SessionID:    "session-" + modelOverride,
		TotalCostUSD: 0,
	}
	resp.Usage.InputTokens = 1
	resp.Usage.OutputTokens = 1
	return resp, nil
}

func (c *modelRecordingClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &CLIResponse{Result: "plan", TextContent: "plan", SessionID: "plan-session"}, nil
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
			name:    "option label follow up",
			message: "我先問一下，處理完b的話，node graph會是正常的情況嗎？",
			want:    true,
		},
		{
			name:    "ordinal option follow up",
			message: "第二個做完後會正常嗎？",
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

func TestAgentRunInjectsRecentBridgeForOptionLabelFollowUp(t *testing.T) {
	client := &modelRecordingClient{model: "sonnet"}
	chatCtx := NewChatContext(1, 0, "/tmp/project")
	chatCtx.SetSession(BackendClaude, "active-sonnet")
	chatCtx.AddRecentMessage(
		"GraphFull 現況分析",
		strings.Repeat("前置說明。", 220)+"\n接下來合理的後續：(a) 對 GraphFull 加 service-level 測試；(b) 點選節點後在 cytoscape 上 highlight 鄰居、或切回 center mode 並帶入 nodeId。要繼續做哪個？",
	)
	agent := NewAgentWithContext(client, chatCtx)
	agent.lastUsedModel = "sonnet"
	agent.SetRoutingConfig(ModelRoutingConfig{StickySession: true, SessionIdleTimeoutMin: 5})

	if _, err := agent.Run("我先問一下，處理完b的話，node graph會是正常的情況嗎？", nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(client.messages) != 1 {
		t.Fatalf("CallStream messages = %d, want 1", len(client.messages))
	}
	prompt := client.messages[0]
	for _, want := range []string{
		"[Context from previous conversation",
		"(b) 點選節點後在 cytoscape 上 highlight 鄰居",
		"center mode",
		"nodeId",
		"處理完b",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestAgentRunSetsAwaitingInputForChoiceResponse(t *testing.T) {
	client := &modelRecordingClient{
		model:    "sonnet",
		response: "接下來合理的後續：(a) 補 service-level 測試；(b) highlight 鄰居並帶入 nodeId。你想先做哪個？",
	}
	chatCtx := NewChatContext(1, 0, "/tmp/project")
	agent := NewAgentWithContext(client, chatCtx)

	if _, err := agent.Run("分析 node graph", nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := chatCtx.StateSnapshot().State; got != string(appengine.ChatStateAwaitingInput) {
		t.Fatalf("chat state = %q, want %q", got, appengine.ChatStateAwaitingInput)
	}
}

func TestAgentRunUpdatesExecutionLifecycle(t *testing.T) {
	client := &modelRecordingClient{model: "sonnet"}
	agent := NewAgent(client, "/tmp/project", 1, 0)

	if _, err := agent.Run("hello", nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if agent.IsProcessing() {
		t.Fatal("IsProcessing after successful Run = true, want false")
	}
	snapshot := agent.executionLifecycle().Snapshot()
	if snapshot.State != appengine.ExecutionStateSucceeded {
		t.Fatalf("execution state = %q, want %q", snapshot.State, appengine.ExecutionStateSucceeded)
	}
	if !snapshot.Terminal {
		t.Fatal("execution terminal = false, want true")
	}
}

func TestAgentRunMarksExecutionFailedOnStreamError(t *testing.T) {
	client := &modelRecordingClient{model: "sonnet", err: errors.New("boom")}
	agent := NewAgent(client, "/tmp/project", 1, 0)

	if _, err := agent.Run("hello", nil); err == nil {
		t.Fatal("Run returned nil error, want failure")
	}
	if agent.IsProcessing() {
		t.Fatal("IsProcessing after failed Run = true, want false")
	}
	snapshot := agent.executionLifecycle().Snapshot()
	if snapshot.State != appengine.ExecutionStateFailed {
		t.Fatalf("execution state = %q, want %q", snapshot.State, appengine.ExecutionStateFailed)
	}
	if !snapshot.Terminal {
		t.Fatal("execution terminal = false, want true")
	}
}

func TestAgentRunWithPlanUpdatesExecutionLifecycle(t *testing.T) {
	agent := NewAgent(&mockClient{}, "/tmp/project", 1, 0)
	agent.SetPlanMode(true, "planner", "executor")

	if _, err := agent.RunWithPlan("build a plan", nil); err != nil {
		t.Fatalf("RunWithPlan returned error: %v", err)
	}
	if agent.IsProcessing() {
		t.Fatal("IsProcessing after successful RunWithPlan = true, want false")
	}
	snapshot := agent.executionLifecycle().Snapshot()
	if snapshot.State != appengine.ExecutionStateSucceeded {
		t.Fatalf("execution state = %q, want %q", snapshot.State, appengine.ExecutionStateSucceeded)
	}
	if !snapshot.Terminal {
		t.Fatal("execution terminal = false, want true")
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

func TestDecideDirectModel(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Agent)
		message    string
		wantModel  string
		wantReason string
	}{
		{
			name: "user override",
			setup: func(agent *Agent) {
				agent.SetModelOverride("opus")
			},
			message:    "translate this",
			wantModel:  "opus",
			wantReason: "user_command",
		},
		{
			name: "sticky session",
			setup: func(agent *Agent) {
				agent.lastUsedModel = "opus"
				agent.current().ctx.SetSession(BackendClaude, "claude-session")
			},
			message:    "translate this",
			wantModel:  "opus",
			wantReason: "sticky_session",
		},
		{
			name: "follow up inherits current session when sticky disabled",
			setup: func(agent *Agent) {
				agent.SetRoutingConfig(ModelRoutingConfig{StickySession: false, SessionIdleTimeoutMin: 5})
				agent.lastUsedModel = "opus"
				agent.current().ctx.SetSession(BackendClaude, "claude-session")
			},
			message:    "這個也一起修",
			wantModel:  "opus",
			wantReason: "follow_up",
		},
		{
			name:       "static rule",
			message:    "translate this",
			wantModel:  "haiku",
			wantReason: "static_rule",
		},
		{
			name:       "default rule",
			message:    "hello there",
			wantModel:  "sonnet",
			wantReason: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewAgent(&modelRecordingClient{model: "sonnet"}, "/tmp/project", 1, 0)
			if tt.setup != nil {
				tt.setup(agent)
			}
			got := agent.decideDirectModel(agent.current(), tt.message)
			if got.Model != tt.wantModel || got.RoutingReason != tt.wantReason {
				t.Fatalf("decision = (%q, %q), want (%q, %q)", got.Model, got.RoutingReason, tt.wantModel, tt.wantReason)
			}
		})
	}
}

func TestDecideDirectSession(t *testing.T) {
	chatCtx := NewChatContext(1, 0, "/tmp/project")
	chatCtx.SetSession(BackendClaude, "claude-session")
	chatCtx.SetSession(BackendCodex, "codex-session")
	chatCtx.LastBackend = BackendClaude
	agent := NewAgentWithContext(&modelRecordingClient{model: "sonnet"}, chatCtx)

	claudeDecision := agent.decideDirectSession(agent.current(), "claude-sonnet-4-6")
	if claudeDecision.Backend != BackendClaude {
		t.Fatalf("claude backend = %v, want %v", claudeDecision.Backend, BackendClaude)
	}
	if claudeDecision.PreviousBackend != BackendClaude {
		t.Fatalf("previous backend = %v, want %v", claudeDecision.PreviousBackend, BackendClaude)
	}
	if claudeDecision.SessionID != "claude-session" || !claudeDecision.HasNativeSession {
		t.Fatalf("claude session decision = %+v", claudeDecision)
	}

	codexDecision := agent.decideDirectSession(agent.current(), "gpt-5.5")
	if codexDecision.Backend != BackendCodex {
		t.Fatalf("codex backend = %v, want %v", codexDecision.Backend, BackendCodex)
	}
	if codexDecision.SessionID != "codex-session" || !codexDecision.HasNativeSession {
		t.Fatalf("codex session decision = %+v", codexDecision)
	}

	chatCtx.ClearSession(BackendClaude)
	freshDecision := agent.decideDirectSession(agent.current(), "claude-opus-4-6")
	if freshDecision.SessionID != "" || freshDecision.HasNativeSession {
		t.Fatalf("fresh session decision = %+v", freshDecision)
	}
}

func TestDecideDirectRunCombinesModelAndSession(t *testing.T) {
	chatCtx := NewChatContext(1, 0, "/tmp/project")
	chatCtx.SetSession(BackendCodex, "codex-session")
	chatCtx.LastBackend = BackendClaude
	agent := NewAgentWithContext(&modelRecordingClient{model: "sonnet"}, chatCtx)
	agent.SetModelOverride("gpt-5.5")

	decision := agent.decideDirectRun(agent.current(), "translate this")
	if decision.Model != "gpt-5.5" || decision.RoutingReason != "user_command" {
		t.Fatalf("model decision = (%q, %q), want (gpt-5.5, user_command)", decision.Model, decision.RoutingReason)
	}
	if decision.Backend != BackendCodex || decision.PreviousBackend != BackendClaude {
		t.Fatalf("backend decision = (%v, prev %v), want (%v, prev %v)", decision.Backend, decision.PreviousBackend, BackendCodex, BackendClaude)
	}
	if decision.SessionID != "codex-session" || !decision.HasNativeSession {
		t.Fatalf("session decision = %+v", decision)
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

func TestResolveAgentMemoryBridgeModelSwitchUsesRecentMessagesOnly(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "CLAUDE.md"), []byte("stale static guidance must not enter model switch bridge"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	chatCtx := NewChatContext(1, 0, projectDir)
	chatCtx.AddRecentMessage("先處理 Hermes token 浪費", "目前重點是 issue state snapshot 與 quality gate")
	agent := NewAgentWithContext(&modelRecordingClient{model: "sonnet"}, chatCtx)

	bridge := agent.resolveAgentMemoryBridge(context.Background(), agent.current(), "好，繼續", "direct_model_switch")

	if !strings.Contains(bridge, "issue state snapshot") {
		t.Fatalf("bridge missing recent messages:\n%s", bridge)
	}
	if strings.Contains(bridge, "Static project context") || strings.Contains(bridge, "stale static guidance") {
		t.Fatalf("model switch bridge leaked static project hints:\n%s", bridge)
	}
	if strings.Contains(bridge, "Persisted general task context") {
		t.Fatalf("model switch bridge leaked general memory:\n%s", bridge)
	}
}
