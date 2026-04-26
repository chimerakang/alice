package app

import (
	"context"
	"testing"
)

// mockClient is a minimal Client stub that records the sessionID it receives
// and returns a configurable response.
type mockClient struct {
	receivedSessionIDs []string
	responses          []*CLIResponse
	callIndex          int
}

func (m *mockClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	return nil, nil
}

func (m *mockClient) CallStream(ctx context.Context, message, projectDir, sessionID, modelOverride string, onToolUse func(string, map[string]interface{}), onContent func(string, string)) (*CLIResponse, error) {
	m.receivedSessionIDs = append(m.receivedSessionIDs, sessionID)
	resp := m.responses[m.callIndex]
	m.callIndex++
	if onContent != nil && resp.TextContent != "" {
		onContent("text", resp.TextContent)
	}
	return resp, nil
}

func (m *mockClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(string, string)) (*CLIResponse, error) {
	return nil, nil
}

func (m *mockClient) GetModel() string { return "test-model" }

// TestMakeExecFn_ThreadResume verifies that the session ID returned by the
// first sub-task call is forwarded as the session_id on subsequent calls
// (Issue #108 step 3: Codex thread resume across sub-tasks).
func TestMakeExecFn_ThreadResume(t *testing.T) {
	client := &mockClient{
		responses: []*CLIResponse{
			{SessionID: "sess-xyz", TextContent: "first result"},
			{SessionID: "sess-xyz", TextContent: "second result"},
			{SessionID: "sess-xyz", TextContent: "third result"},
		},
	}

	execFn := makeExecFn(client, "test-model")
	ctx := context.Background()

	// First call — no prior session; should pass empty string
	result1, sid1, _, _, _, err := execFn(ctx, "task 1", "/proj", "", nil)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if result1 != "first result" {
		t.Errorf("first result: want %q, got %q", "first result", result1)
	}
	if client.receivedSessionIDs[0] != "" {
		t.Errorf("first call should send empty session_id, got %q", client.receivedSessionIDs[0])
	}
	if sid1 != "sess-xyz" {
		t.Errorf("first call should return session_id=sess-xyz, got %q", sid1)
	}

	// Second call — should resume with session returned from first call
	_, sid2, _, _, _, err := execFn(ctx, "task 2", "/proj", sid1, nil)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if client.receivedSessionIDs[1] != "sess-xyz" {
		t.Errorf("second call should send session_id=sess-xyz, got %q", client.receivedSessionIDs[1])
	}
	if sid2 != "sess-xyz" {
		t.Errorf("second call should keep session_id=sess-xyz, got %q", sid2)
	}

	// Third call — session should still be forwarded
	_, sid3, _, _, _, err := execFn(ctx, "task 3", "/proj", sid2, nil)
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if client.receivedSessionIDs[2] != "sess-xyz" {
		t.Errorf("third call should forward session_id=sess-xyz, got %q", client.receivedSessionIDs[2])
	}
	if sid3 != "sess-xyz" {
		t.Errorf("third call should keep session_id=sess-xyz, got %q", sid3)
	}
}

// TestMakeExecFn_EmptySessionIDNotOverwritten verifies that a blank SessionID
// in a response does not reset the preserved session ID.
func TestMakeExecFn_EmptySessionIDNotOverwritten(t *testing.T) {
	client := &mockClient{
		responses: []*CLIResponse{
			{SessionID: "sess-abc", TextContent: "first"},
			{SessionID: "", TextContent: "second"}, // backend returned no session ID
			{SessionID: "", TextContent: "third"},
		},
	}

	execFn := makeExecFn(client, "test-model")
	ctx := context.Background()

	_, sid1, _, _, _, _ := execFn(ctx, "task 1", "/proj", "", nil)
	_, sid2, _, _, _, _ := execFn(ctx, "task 2", "/proj", sid1, nil)
	_, sid3, _, _, _, _ := execFn(ctx, "task 3", "/proj", sid2, nil)

	// All calls after the first should still carry the original session ID
	for i := 1; i < 3; i++ {
		if client.receivedSessionIDs[i] != "sess-abc" {
			t.Errorf("call %d should send sess-abc, got %q", i+1, client.receivedSessionIDs[i])
		}
	}
	if sid2 != "sess-abc" || sid3 != "sess-abc" {
		t.Errorf("returned session IDs should stay sess-abc, got %q and %q", sid2, sid3)
	}
}

// TestMakeExecFn_ReturnsBackendSessionID verifies that the bridge surfaces the
// backend-provided session ID back to the Coordinator, which is required after
// Codex resume fallback creates a fresh thread.
func TestMakeExecFn_ReturnsBackendSessionID(t *testing.T) {
	client := &mockClient{
		responses: []*CLIResponse{
			{SessionID: "thread-new", TextContent: "result"},
		},
	}

	execFn := makeExecFn(client, "test-model")
	result, sid, _, _, _, err := execFn(context.Background(), "task", "/proj", "thread-old", nil)
	if err != nil {
		t.Fatalf("execFn: %v", err)
	}
	if result != "result" {
		t.Errorf("returned result %q, want %q", result, "result")
	}
	if client.receivedSessionIDs[0] != "thread-old" {
		t.Errorf("bridge should send old session_id to backend, got %q", client.receivedSessionIDs[0])
	}
	if sid != "thread-new" {
		t.Errorf("bridge should return backend session_id thread-new, got %q", sid)
	}
}

// TestMakeExecFn_OnContentCallback verifies that text chunks are forwarded to
// the caller's onContent function.
func TestMakeExecFn_OnContentCallback(t *testing.T) {
	client := &mockClient{
		responses: []*CLIResponse{
			{SessionID: "s1", TextContent: "hello world"},
		},
	}

	execFn := makeExecFn(client, "test-model")
	var received string
	result, sid, _, _, _, err := execFn(context.Background(), "task", "/proj", "", func(chunk string) {
		received += chunk
	})
	if err != nil {
		t.Fatalf("execFn: %v", err)
	}
	if received != "hello world" {
		t.Errorf("onContent received %q, want %q", received, "hello world")
	}
	if result != "hello world" {
		t.Errorf("returned result %q, want %q", result, "hello world")
	}
	if sid != "s1" {
		t.Errorf("returned session_id %q, want %q", sid, "s1")
	}
}
