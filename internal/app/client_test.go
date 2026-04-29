package app

import (
	"context"
	"strings"
	"testing"
)

type mockClient struct{}

func (m *mockClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return &CLIResponse{Result: "ok", TextContent: "ok", SessionID: sessionID}, nil
}

func (m *mockClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(toolName string, toolInput map[string]interface{}), onContent func(contentType, text string)) (*CLIResponse, error) {
	return &CLIResponse{Result: "ok", TextContent: "ok", SessionID: sessionID}, nil
}

func (m *mockClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	if onContent != nil {
		onContent("text", "ok")
	}
	return &CLIResponse{Result: "ok", TextContent: "ok", SessionID: "plan-session"}, nil
}

func (m *mockClient) GetModel() string {
	return "test-model"
}

func TestFormatCLIStreamErrorIncludesMaxTurnsAndPartialOutput(t *testing.T) {
	resp := &CLIResponse{
		IsError:     true,
		NumTurns:    51,
		TextContent: "已經登入並開始 E2E curl 驗證，但尚未完成最後彙整",
	}

	got := formatCLIStreamError(resp, 50)
	for _, want := range []string{
		"stream ended with is_error=true but no result text",
		"turns=51 max_turns=50",
		"likely exceeded --max-turns",
		"partial_text=已經登入並開始 E2E curl 驗證",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatCLIStreamError() = %q, want substring %q", got, want)
		}
	}
}
