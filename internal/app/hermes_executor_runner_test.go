package app

import (
	"context"
	"errors"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

type sessionTrackingClient struct {
	calls            []sessionCall
	respSession      string
	respModelInvoked string
}

type sessionCall struct {
	sessionID string
	model     string
}

func (c *sessionTrackingClient) Call(context.Context, string, string, string, string) (*CLIResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *sessionTrackingClient) CallStream(_ context.Context, _ string, _, sessionID, model string, _ func(string, map[string]interface{}), _ func(string, string)) (*CLIResponse, error) {
	c.calls = append(c.calls, sessionCall{sessionID: sessionID, model: model})
	c.respModelInvoked = model
	resp := &CLIResponse{
		Result:      "ok",
		TextContent: "ok",
		SessionID:   c.respSession,
	}
	resp.Usage.InputTokens = 5
	resp.Usage.OutputTokens = 5
	return resp, nil
}

func (c *sessionTrackingClient) CallPlan(context.Context, string, string, string, func(string, string)) (*CLIResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *sessionTrackingClient) GetModel() string { return "claude-sonnet-4-5" }

// Issue #149: when WalkingEnabled the runner must NOT clear the model session
// between consecutive Run calls (so the next call can `--resume` and reuse
// prompt-cache + transcript). Legacy mode (walking off) keeps clearing as
// before, preventing the gladsheim #108 regression.
func TestHermesExecutorRunner_WalkingEnabled_KeepsSessionBetweenRuns(t *testing.T) {
	chatCtx := NewChatContext(123, 0, "/tmp/test-walking")
	chatCtx.SetSession(BackendClaude, "primed-session")
	client := &sessionTrackingClient{respSession: "primed-session"}
	agent := NewAgentWithContext(client, chatCtx)
	runner := newHermesExecutorRunner(agent, "claude-sonnet-4-5", "")
	runner.SetWalkingEnabled(true)

	subTask := hermes.SubTask{ID: "s1", Description: "list files"}
	runner.BindSubTask(subTask)
	if _, err := runner.Run("dummy prompt 1", nil); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	subTask2 := hermes.SubTask{ID: "s2", Description: "summarise"}
	runner.BindSubTask(subTask2)
	if _, err := runner.Run("dummy prompt 2", nil); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(client.calls) != 2 {
		t.Fatalf("expected 2 CallStream calls, got %d", len(client.calls))
	}
	if client.calls[1].sessionID != "primed-session" {
		t.Fatalf("walking mode dropped the session between sub-tasks: call2 sessionID=%q", client.calls[1].sessionID)
	}
}

// Walking mode must still force a fresh session when the model boundary is
// crossed (light → heavy or vice versa), since cache key includes the model
// fingerprint. The runner clears the prior model's session and the next call
// goes out without --resume.
func TestHermesExecutorRunner_WalkingEnabled_ClearsOnModelChange(t *testing.T) {
	chatCtx := NewChatContext(123, 0, "/tmp/test-walking")
	chatCtx.SetSession(BackendClaude, "primed-session-light")
	client := &sessionTrackingClient{respSession: "primed-session-light"}
	agent := NewAgentWithContext(client, chatCtx)
	runner := newHermesExecutorRunner(agent, "claude-haiku-4-5", "claude-sonnet-4-5")
	runner.SetWalkingEnabled(true)

	light := hermes.SubTask{ID: "s1", Description: "Read README"}
	heavy := hermes.SubTask{ID: "s2", Description: "Edit auth.go and refactor", ToolHints: []string{"Edit"}}

	runner.BindSubTask(light)
	if _, err := runner.Run("dummy prompt 1", nil); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// Simulate model-switch: bind heavy sub-task next.
	runner.BindSubTask(heavy)
	// Second call's CLI client returns a *new* session id (heavy-session) because
	// the runner cleared the prior light session.
	client.respSession = "heavy-session"
	if _, err := runner.Run("dummy prompt 2", nil); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(client.calls) != 2 {
		t.Fatalf("expected 2 CallStream calls, got %d", len(client.calls))
	}
	// The second call should NOT carry the light session id — runner cleared it
	// because the model boundary was crossed.
	if client.calls[1].sessionID == "primed-session-light" {
		t.Fatalf("walking should clear stale model session on model change, got sessionID=%q",
			client.calls[1].sessionID)
	}
}

// When walking is disabled the runner keeps the legacy "clear per sub-task"
// behaviour. This protects against an accidental regression of the gladsheim
// #108 prompt-bloat fix.
func TestHermesExecutorRunner_LegacyMode_ClearsBetweenRuns(t *testing.T) {
	chatCtx := NewChatContext(123, 0, "/tmp/test-walking")
	chatCtx.SetSession(BackendClaude, "primed-session")
	client := &sessionTrackingClient{respSession: "session-after-run"}
	agent := NewAgentWithContext(client, chatCtx)
	runner := newHermesExecutorRunner(agent, "claude-sonnet-4-5", "")
	// SetWalkingEnabled(false) — the default — so we exercise legacy path.

	subTask := hermes.SubTask{ID: "s1", Description: "list files"}
	runner.BindSubTask(subTask)
	if _, err := runner.Run("dummy prompt 1", nil); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	runner.BindSubTask(hermes.SubTask{ID: "s2", Description: "summarise"})
	if _, err := runner.Run("dummy prompt 2", nil); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(client.calls) != 2 {
		t.Fatalf("expected 2 CallStream calls, got %d", len(client.calls))
	}
	if client.calls[1].sessionID == "primed-session" {
		t.Fatalf("legacy mode must clear session between sub-tasks, got sessionID=%q on call2",
			client.calls[1].sessionID)
	}
}
