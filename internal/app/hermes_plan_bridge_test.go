package app

import (
	"context"
	"testing"
)

type planBridgeClient struct{}

func (c *planBridgeClient) Call(context.Context, string, string, string, string) (*CLIResponse, error) {
	return nil, nil
}

func (c *planBridgeClient) CallStream(context.Context, string, string, string, string, func(string, map[string]interface{}), func(string, string)) (*CLIResponse, error) {
	return nil, nil
}

func (c *planBridgeClient) CallPlan(_ context.Context, _, _, _, _ string, onContent func(string, string)) (*CLIResponse, error) {
	if onContent != nil {
		onContent("text", "I've emitted the plan as structured output: three sub-tasks.")
	}
	return &CLIResponse{
		TextContent: `[{"id":"s1","description":"Use the final salvaged JSON plan, not streamed prose.","tool_hints":["Read","Edit","Bash"]}]`,
		SessionID:   "planner-session",
	}, nil
}

func (c *planBridgeClient) GetModel() string { return "test-model" }

func TestMakePlanFnPrefersFinalTextContentOverStreamedProse(t *testing.T) {
	planFn := makePlanFn(&planBridgeClient{}, "claude-opus-4-8")
	res, err := planFn(context.Background(), "plan", "/repo", "")
	if err != nil {
		t.Fatalf("planFn: %v", err)
	}
	if res.Text == "I've emitted the plan as structured output: three sub-tasks." {
		t.Fatalf("planFn returned streamed prose instead of final TextContent")
	}
	if want := `[{"id":"s1","description":"Use the final salvaged JSON plan, not streamed prose.","tool_hints":["Read","Edit","Bash"]}]`; res.Text != want {
		t.Fatalf("planFn text = %q, want final TextContent %q", res.Text, want)
	}
}
