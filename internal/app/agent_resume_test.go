package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type resumeBridgeClient struct {
	calls []resumeBridgeCall
}

type resumeBridgeCall struct {
	message   string
	sessionID string
	model     string
}

func (c *resumeBridgeClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *resumeBridgeClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	c.calls = append(c.calls, resumeBridgeCall{
		message:   message,
		sessionID: sessionID,
		model:     modelOverride,
	})
	if len(c.calls) == 1 {
		return nil, ErrSessionUnavailable
	}
	resp := &CLIResponse{
		Result:       "issue created from previous findings",
		TextContent:  "issue created from previous findings",
		SessionID:    "new-codex-thread",
		TotalCostUSD: 0.01,
	}
	resp.Usage.InputTokens = 123
	resp.Usage.OutputTokens = 45
	return resp, nil
}

func (c *resumeBridgeClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *resumeBridgeClient) GetModel() string {
	return "gpt-5.5"
}

func TestAgentRunInjectsBridgeWhenCodexResumeUnavailable(t *testing.T) {
	chatCtx := NewChatContext(123, 0, "/tmp/project")
	chatCtx.SetSession(BackendCodex, "stale-codex-thread")
	chatCtx.AddRecentMessage(
		"請分析 workforce-saas 的權限模型",
		"目前只有租戶管理員，沒有平台超級管理員；請整理成 issue 時要包含檔案參照。",
	)
	client := &resumeBridgeClient{}
	agent := NewAgentWithContext(client, chatCtx)
	agent.SetModelOverride("gpt-5.5")

	got, err := agent.Run("請將這些問題整理成 issue", nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got != "issue created from previous findings" {
		t.Fatalf("Run result = %q", got)
	}
	if len(client.calls) != 2 {
		t.Fatalf("CallStream calls = %d, want 2", len(client.calls))
	}
	if client.calls[0].sessionID != "stale-codex-thread" {
		t.Fatalf("first session = %q, want stale-codex-thread", client.calls[0].sessionID)
	}
	if client.calls[1].sessionID != "" {
		t.Fatalf("retry session = %q, want empty fresh session", client.calls[1].sessionID)
	}
	retryPrompt := client.calls[1].message
	for _, want := range []string{
		"[Context from previous conversation",
		"請分析 workforce-saas 的權限模型",
		"目前只有租戶管理員",
		"請將這些問題整理成 issue",
	} {
		if !strings.Contains(retryPrompt, want) {
			t.Fatalf("retry prompt missing %q:\n%s", want, retryPrompt)
		}
	}
	if gotSession := chatCtx.Session(BackendCodex); gotSession != "new-codex-thread" {
		t.Fatalf("codex session = %q, want new-codex-thread", gotSession)
	}
}

func TestAgentRunSkipsRecentBridgeForExplicitIssueOnResumeFallback(t *testing.T) {
	chatCtx := NewChatContext(123, 0, "/tmp/project")
	chatCtx.SetSession(BackendCodex, "stale-codex-thread")
	chatCtx.AddRecentMessage(
		"剛剛在處理 #99",
		"#99 的結論不應污染新的 explicit issue request。",
	)
	client := &resumeBridgeClient{}
	agent := NewAgentWithContext(client, chatCtx)
	agent.SetModelOverride("gpt-5.5")

	if _, err := agent.Run("請繼續處理 #143", nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("CallStream calls = %d, want 2", len(client.calls))
	}
	retryPrompt := client.calls[1].message
	if strings.Contains(retryPrompt, "#99 的結論") {
		t.Fatalf("retry prompt leaked unrelated recent bridge:\n%s", retryPrompt)
	}
	if strings.Contains(retryPrompt, "[Context from previous conversation") {
		t.Fatalf("retry prompt included generic recent bridge for explicit issue:\n%s", retryPrompt)
	}
	if !strings.Contains(retryPrompt, "請繼續處理 #143") {
		t.Fatalf("retry prompt missing current request:\n%s", retryPrompt)
	}
}
