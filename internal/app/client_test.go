package app

import "context"

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
