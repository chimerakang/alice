package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func (m *mockClient) CallPlan(ctx context.Context, message, projectDir, sessionID, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
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

func installFakeClaude(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCallReturnsParsedResponseWhenCLIExitsWithError(t *testing.T) {
	installFakeClaude(t, `cat <<'JSON'
{"type":"result","session_id":"session-1","is_error":false,"num_turns":51,"result":"completed before exit","usage":{"input_tokens":10,"output_tokens":5}}
JSON
echo "max turns exceeded" >&2
exit 1
`)

	client := &CLIClient{Model: "fake-model", MaxTurns: 50}
	resp, err := client.Call(context.Background(), "message", t.TempDir(), "", "")
	if err == nil {
		t.Fatal("Call() error = nil, want CLI exit error")
	}
	if resp == nil {
		t.Fatal("Call() response = nil, want parsed response")
	}
	if !resp.IsError {
		t.Fatal("Call() response IsError = false, want true after non-zero CLI exit")
	}
	if resp.Result != "completed before exit" {
		t.Fatalf("Call() result = %q, want parsed result", resp.Result)
	}
}

func TestCallStreamReturnsPartialResponseWhenCLIExitsWithError(t *testing.T) {
	installFakeClaude(t, `cat <<'JSON'
{"type":"assistant","message":{"content":[{"type":"text","text":"partial answer"},{"type":"tool_use","name":"Bash","input":{"cmd":"go test"}}]}}
{"type":"result","session_id":"session-1","is_error":false,"num_turns":51,"result":"completed before exit","usage":{"input_tokens":10,"output_tokens":5}}
JSON
echo "max turns exceeded" >&2
exit 1
`)

	client := &CLIClient{Model: "fake-model", MaxTurns: 50}
	var textChunks []string
	var toolName string
	resp, err := client.CallStream(context.Background(), "message", t.TempDir(), "", "",
		func(name string, input map[string]interface{}) {
			toolName = name
		},
		func(contentType, text string) {
			if contentType == "text" {
				textChunks = append(textChunks, text)
			}
		},
	)

	if err == nil {
		t.Fatal("CallStream() error = nil, want CLI exit error")
	}
	if resp == nil {
		t.Fatal("CallStream() response = nil, want parsed streaming response")
	}
	if !resp.IsError {
		t.Fatal("CallStream() response IsError = false, want true after non-zero CLI exit")
	}
	if resp.Result != "completed before exit" {
		t.Fatalf("CallStream() result = %q, want parsed result", resp.Result)
	}
	if resp.TextContent != "partial answer" {
		t.Fatalf("CallStream() TextContent = %q, want streamed text", resp.TextContent)
	}
	if len(textChunks) != 1 || textChunks[0] != "partial answer" {
		t.Fatalf("CallStream() text callbacks = %#v, want partial answer", textChunks)
	}
	if toolName != "Bash" {
		t.Fatalf("CallStream() tool callback = %q, want Bash", toolName)
	}
	if !strings.Contains(err.Error(), "likely exceeded --max-turns") {
		t.Fatalf("CallStream() error = %q, want max-turns hint", err)
	}
}

func TestEnhancedCallStreamWithFilesReturnsPartialResponseWhenCLIExitsWithError(t *testing.T) {
	installFakeClaude(t, `cat <<'JSON'
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"checking files"},{"type":"text","text":"image analysis summary"}]}}
{"type":"result","session_id":"session-1","is_error":false,"num_turns":51,"result":"completed before exit","usage":{"input_tokens":10,"output_tokens":5}}
JSON
echo "max turns exceeded" >&2
exit 1
`)

	client := NewEnhancedCLIClient("fake-model")
	client.MaxTurns = 50
	var contentTypes []string
	resp, err := client.CallStreamWithFiles(context.Background(), "message", nil, t.TempDir(), "",
		nil,
		func(contentType, text string) {
			contentTypes = append(contentTypes, contentType+":"+text)
		},
	)

	if err == nil {
		t.Fatal("CallStreamWithFiles() error = nil, want CLI exit error")
	}
	if resp == nil {
		t.Fatal("CallStreamWithFiles() response = nil, want parsed streaming response")
	}
	if !resp.IsError {
		t.Fatal("CallStreamWithFiles() response IsError = false, want true after non-zero CLI exit")
	}
	if resp.TextContent != "image analysis summary" {
		t.Fatalf("CallStreamWithFiles() TextContent = %q, want streamed text", resp.TextContent)
	}
	if resp.ThinkingContent != "checking files" {
		t.Fatalf("CallStreamWithFiles() ThinkingContent = %q, want streamed thinking", resp.ThinkingContent)
	}
	if got := strings.Join(contentTypes, ","); got != "thinking:checking files,text:image analysis summary" {
		t.Fatalf("CallStreamWithFiles() content callbacks = %q", got)
	}
	if !strings.Contains(err.Error(), "likely exceeded --max-turns") {
		t.Fatalf("CallStreamWithFiles() error = %q, want max-turns hint", err)
	}
}

func TestCallPlanUsesEmitPlanToolPayloadWhenCLIEmitsToolUse(t *testing.T) {
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	stdinPath := filepath.Join(t.TempDir(), "stdin.txt")
	script := fmt.Sprintf(`printf '%%s\n' "$@" > %q
cat > %q
cat <<'JSON'
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__planner_emit_plan__emit_plan","input":{"sub_tasks":[{"id":"s1","description":"Bundle context, edit, and verify for the e2e test","tool_hints":["Read","Edit","Bash"]}]}}]}}
{"type":"result","session_id":"planner-session-emit","is_error":false,"num_turns":1,"result":"completed via emit_plan","usage":{"input_tokens":12,"output_tokens":7,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}
JSON
`, argvPath, stdinPath)
	installFakeClaude(t, script)

	client := &CLIClient{Model: "fake-model", MaxTurns: 50}
	prompt := "補一個 e2e 測試"
	resp, err := client.CallPlan(context.Background(), prompt, t.TempDir(), "", "", nil)
	if err != nil {
		t.Fatalf("CallPlan() error = %v", err)
	}
	if resp.SessionID != "planner-session-emit" {
		t.Fatalf("CallPlan() session = %q, want planner-session-emit", resp.SessionID)
	}
	var gotPlan []map[string]any
	if err := json.Unmarshal([]byte(resp.TextContent), &gotPlan); err != nil {
		t.Fatalf("CallPlan() text is not JSON: %v", err)
	}
	if len(gotPlan) != 1 {
		t.Fatalf("CallPlan() plan length = %d, want 1", len(gotPlan))
	}
	if gotPlan[0]["id"] != "s1" || gotPlan[0]["description"] != "Bundle context, edit, and verify for the e2e test" {
		t.Fatalf("CallPlan() plan item = %#v, want target sub-task", gotPlan[0])
	}
	hints, ok := gotPlan[0]["tool_hints"].([]any)
	if !ok || len(hints) != 3 || hints[0] != "Read" || hints[1] != "Edit" || hints[2] != "Bash" {
		t.Fatalf("CallPlan() tool_hints = %#v, want [Read Edit Bash]", gotPlan[0]["tool_hints"])
	}

	argvBytes, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read argv capture: %v", err)
	}
	argv := string(argvBytes)
	if strings.Contains(argv, "--max-turns") {
		t.Fatalf("CallPlan argv unexpectedly contains --max-turns: %s", argv)
	}
	if strings.Contains(argv, prompt) {
		t.Fatalf("CallPlan argv contains prompt; prompt should be sent via stdin: %s", argv)
	}
	for _, want := range []string{"--strict-mcp-config", "--tools", "--json-schema"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("CallPlan argv missing %s: %s", want, argv)
		}
	}
	stdinBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	if got := string(stdinBytes); got != prompt {
		t.Fatalf("CallPlan stdin = %q, want prompt %q", got, prompt)
	}
}

// TestCallPlanPrefersStructuredOutputOverProse covers the #178 fix: Opus 4.8
// often narrates a prose plan summary in `result` instead of emitting a tool
// call. The --json-schema structured_output must still yield the validated
// sub_tasks, and it takes priority over any prose text.
func TestCallPlanPrefersStructuredOutputOverProse(t *testing.T) {
	script := `cat > /dev/null
cat <<'JSON'
{"type":"assistant","message":{"content":[{"type":"text","text":"Plan emitted successfully with all 2 sub-tasks."}]}}
{"type":"result","session_id":"planner-structured","is_error":false,"num_turns":1,"result":"Plan emitted successfully with all 2 sub-tasks.","structured_output":{"sub_tasks":[{"id":"s1","description":"App-scope the entitlement schema and backfill","tool_hints":["Read","Edit","Bash"]},{"id":"s2","description":"Update admin handlers to require app context","tool_hints":["Edit","Bash"]}]},"usage":{"input_tokens":20,"output_tokens":9,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}
JSON
`
	installFakeClaude(t, script)

	client := &CLIClient{Model: "fake-model", MaxTurns: 50}
	resp, err := client.CallPlan(context.Background(), "app-scope entitlements", t.TempDir(), "", "", nil)
	if err != nil {
		t.Fatalf("CallPlan() error = %v", err)
	}
	if resp.SessionID != "planner-structured" {
		t.Fatalf("CallPlan() session = %q, want planner-structured", resp.SessionID)
	}
	var gotPlan []map[string]any
	if err := json.Unmarshal([]byte(resp.TextContent), &gotPlan); err != nil {
		t.Fatalf("CallPlan() text is not a JSON sub_tasks array (got prose instead?): %v\ntext=%q", err, resp.TextContent)
	}
	if len(gotPlan) != 2 {
		t.Fatalf("CallPlan() plan length = %d, want 2", len(gotPlan))
	}
	if gotPlan[0]["id"] != "s1" || gotPlan[1]["id"] != "s2" {
		t.Fatalf("CallPlan() plan ids = %v/%v, want s1/s2", gotPlan[0]["id"], gotPlan[1]["id"])
	}
}

func TestCallPlanSalvagesProseWithLiteralJSONFallback(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	argvPath := filepath.Join(dir, "argv2.txt")
	script := fmt.Sprintf(`count=0
if [ -f %[1]q ]; then count=$(cat %[1]q); fi
count=$((count + 1))
printf '%%s' "$count" > %[1]q
cat > /dev/null
if [ "$count" = "1" ]; then
cat <<'JSON'
{"type":"assistant","message":{"content":[{"type":"text","text":"I've emitted the plan as structured output: 2 sub-tasks covering schema and admin UI."}]}}
{"type":"result","session_id":"planner-prose","is_error":false,"num_turns":1,"result":"I've emitted the plan as structured output: 2 sub-tasks covering schema and admin UI.","usage":{"input_tokens":20,"output_tokens":9,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}
JSON
else
printf '%%s\n' "$@" > %[2]q
cat <<'JSON'
{"type":"assistant","message":{"content":[{"type":"text","text":"[{\"id\":\"s1\",\"description\":\"Add the app-scoped entitlement schema and migration, then run backend migration tests.\",\"tool_hints\":[\"Read\",\"Edit\",\"Bash\"]},{\"id\":\"s2\",\"description\":\"Update the admin user UI to display app-scoped entitlement sources, then run the focused frontend tests.\",\"tool_hints\":[\"Read\",\"Edit\",\"Bash\"]}]"}]}}
{"type":"result","session_id":"planner-fallback","is_error":false,"num_turns":1,"result":"fallback json emitted","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}
JSON
fi
`, countPath, argvPath)
	installFakeClaude(t, script)

	client := &CLIClient{Model: "fake-model", MaxTurns: 50}
	resp, err := client.CallPlan(context.Background(), "plan issue #115", t.TempDir(), "", "", nil)
	if err != nil {
		t.Fatalf("CallPlan() error = %v", err)
	}
	var gotPlan []map[string]any
	if err := json.Unmarshal([]byte(resp.TextContent), &gotPlan); err != nil {
		t.Fatalf("CallPlan() salvaged text is not JSON: %v\ntext=%q", err, resp.TextContent)
	}
	if len(gotPlan) != 2 || gotPlan[0]["id"] != "s1" || gotPlan[1]["id"] != "s2" {
		t.Fatalf("CallPlan() plan = %#v, want salvaged fallback plan", gotPlan)
	}
	argvBytes, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read fallback argv: %v", err)
	}
	if strings.Contains(string(argvBytes), "--json-schema") {
		t.Fatalf("fallback argv should not use --json-schema: %s", string(argvBytes))
	}
}
