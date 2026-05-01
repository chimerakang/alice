package app

import (
	"context"
	"testing"
	"time"
)

type stickyOverrideRecordingClient struct {
	models []string
}

func (c *stickyOverrideRecordingClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (c *stickyOverrideRecordingClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	c.models = append(c.models, modelOverride)
	resp := &CLIResponse{
		Result:       "ok",
		SessionID:    "session-" + modelOverride,
		TotalCostUSD: 0,
	}
	resp.Usage.InputTokens = 1
	resp.Usage.OutputTokens = 1
	return resp, nil
}

func (c *stickyOverrideRecordingClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	return nil, nil
}

func (c *stickyOverrideRecordingClient) GetModel() string {
	return "sonnet"
}

func TestExpiredStickySessionKeepsExplicitModelOverride(t *testing.T) {
	client := &stickyOverrideRecordingClient{}
	ctx := NewChatContext(1, 0, "/repo")
	ctx.SetSession(BackendClaude, "old-claude")
	ctx.LastActivity = time.Now().Add(-10 * time.Minute)

	agent := NewAgentWithContext(client, ctx)
	agent.SetRoutingConfig(ModelRoutingConfig{StickySession: true, SessionIdleTimeoutMin: 5})
	agent.lastUsedModel = "claude-sonnet-4-6"
	agent.SetModelOverride("gpt-5.5")

	if _, err := agent.Run("請確認 issue 292 到 294 是否處理完畢", nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(client.models) != 1 || client.models[0] != "gpt-5.5" {
		t.Fatalf("model calls = %v, want [gpt-5.5]", client.models)
	}
	if agent.LastUsedModel() != "gpt-5.5" {
		t.Fatalf("LastUsedModel = %q, want gpt-5.5", agent.LastUsedModel())
	}
}
