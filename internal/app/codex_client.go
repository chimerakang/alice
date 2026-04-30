package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// codexPricing holds per-token USD pricing for known Codex / OpenAI models.
// Keys are lowercased model identifiers; matched longest-key-first so that
// "gpt-5.5-pro" wins over "gpt-5.5".
var codexPricing = map[string]struct{ Input, Output float64 }{
	// GPT-5 series
	"gpt-5.5-pro":   {Input: 30.0 / 1e6, Output: 180.0 / 1e6},
	"gpt-5.5":       {Input: 5.0 / 1e6, Output: 30.0 / 1e6},
	"gpt-5.4-mini":  {Input: 0.75 / 1e6, Output: 4.5 / 1e6},
	"gpt-5.4":       {Input: 2.5 / 1e6, Output: 15.0 / 1e6},
	"gpt-5.3-codex": {Input: 1.75 / 1e6, Output: 14.0 / 1e6},
	// Reasoning series
	"o3":      {Input: 10.0 / 1e6, Output: 40.0 / 1e6},
	"o4-mini": {Input: 1.1 / 1e6, Output: 4.4 / 1e6},
	// GPT-4 series
	"gpt-4.1-mini": {Input: 0.4 / 1e6, Output: 1.6 / 1e6},
	"gpt-4.1":      {Input: 2.0 / 1e6, Output: 8.0 / 1e6},
	"gpt-4o-mini":  {Input: 0.15 / 1e6, Output: 0.6 / 1e6},
	"gpt-4o":       {Input: 2.5 / 1e6, Output: 10.0 / 1e6},
}

// codexPricingKeysByLength is codexPricing keys sorted longest-first,
// so substring matching prefers more specific names (e.g. gpt-5.5-pro before gpt-5.5).
var codexPricingKeysByLength = sortedKeysByLengthDesc(codexPricing)

func sortedKeysByLengthDesc(m map[string]struct{ Input, Output float64 }) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return keys
}

// cleanEnvForCodex strips Codex nested-execution detection variables.
// If openaiKey is non-empty, it is injected as OPENAI_API_KEY (overriding any
// pre-existing value in the parent env) so the configured Whisper key can be
// reused for codex without setting it globally on the host.
func cleanEnvForCodex(openaiKey string) []string {
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		// Drop any CODEX_NESTED_* guard vars that prevent nested launch.
		if strings.HasPrefix(key, "CODEX_NESTED_") {
			continue
		}
		// Drop pre-existing OPENAI_API_KEY when caller supplies one — we want the
		// supplied key to be authoritative for this subprocess.
		if openaiKey != "" && key == "OPENAI_API_KEY" {
			continue
		}
		env = append(env, e)
	}
	if openaiKey != "" {
		env = append(env, "OPENAI_API_KEY="+openaiKey)
	}
	env = append(env, "ALICE_SKIP_HOOKS=1")
	return env
}

// calcCodexCost estimates USD cost from token counts using the pricing table.
// Iterates longest-key-first so specific names (gpt-5.5-pro) match before generic prefixes (gpt-5.5).
// Falls back to o4-mini pricing for unknown models and logs once so unknown models surface in alice.log.
func calcCodexCost(model string, inputTokens, outputTokens int) float64 {
	normalized := strings.ToLower(model)
	for _, name := range codexPricingKeysByLength {
		if strings.Contains(normalized, name) {
			p := codexPricing[name]
			return float64(inputTokens)*p.Input + float64(outputTokens)*p.Output
		}
	}
	log.Printf("[codex] unknown model %q — falling back to o4-mini pricing; cost will be approximate", model)
	p := codexPricing["o4-mini"]
	return float64(inputTokens)*p.Input + float64(outputTokens)*p.Output
}

// CodexClient wraps the Codex CLI as an agent backend, implementing Client.
type CodexClient struct {
	Model     string
	OpenAIKey string // injected into subprocess env as OPENAI_API_KEY when non-empty
}

var ErrSessionUnavailable = errors.New("codex session unavailable")

// NewCodexClient creates a CodexClient with the given model name.
// openaiKey is optional: if empty, the codex subprocess inherits OPENAI_API_KEY from the parent env.
func NewCodexClient(model, openaiKey string) *CodexClient {
	return &CodexClient{Model: model, OpenAIKey: openaiKey}
}

// GetModel implements Client.
func (c *CodexClient) GetModel() string {
	return c.Model
}

// codexEvent is the union struct for JSONL events emitted by `codex exec --json`.
type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"` // thread.started
	Message  string `json:"message"`   // error event payload (raw, may be JSON-encoded)
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"` // turn.failed
	Item *struct {
		ID               string `json:"id"`
		Type             string `json:"type"`
		Text             string `json:"text"`              // agent_message
		Command          string `json:"command"`           // command_execution
		AggregatedOutput string `json:"aggregated_output"` // command_execution
		ExitCode         *int   `json:"exit_code"`
		Status           string `json:"status"`
	} `json:"item"` // item.started / item.completed
	Usage *struct {
		InputTokens           int `json:"input_tokens"`
		CachedInputTokens     int `json:"cached_input_tokens"`
		OutputTokens          int `json:"output_tokens"`
		ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	} `json:"usage"` // turn.completed
}

// runCodexStream executes `codex exec [resume]` and processes the JSONL event stream.
// sessionID non-empty triggers `codex exec resume <sessionID>`.
func (c *CodexClient) runCodexStream(
	ctx context.Context,
	prompt, projectDir, sessionID, model string,
	onToolUse func(toolName string, toolInput map[string]interface{}),
	onContent func(contentType, text string),
) (*CLIResponse, error) {
	startTime := time.Now()
	projectDir = codexWorkingDir(projectDir)

	args := buildCodexExecArgs(sessionID, model, projectDir, prompt)

	cmd, cancel := processCommand(ctx, ProcessOptions{
		Dir:     projectDir,
		Env:     cleanEnvForCodex(c.OpenAIKey),
		Timeout: defaultAgentProcessTimeout,
	}, "codex", args...)
	defer cancel()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex exec start: %w", err)
	}

	var (
		threadID     string
		textBlocks   []string
		inputTokens  int
		outputTokens int
		numTurns     int
		streamErrMsg string // captured from error / turn.failed events
	)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev codexEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "thread.started":
			threadID = ev.ThreadID

		case "error":
			if ev.Message != "" {
				streamErrMsg = ev.Message
				log.Printf("[codex] stream error event: %s", ev.Message)
			}

		case "turn.failed":
			if ev.Error != nil && ev.Error.Message != "" {
				streamErrMsg = ev.Error.Message
				log.Printf("[codex] turn.failed: %s", ev.Error.Message)
			}

		case "item.completed":
			if ev.Item == nil {
				continue
			}
			switch ev.Item.Type {
			case "agent_message":
				if ev.Item.Text != "" {
					textBlocks = append(textBlocks, ev.Item.Text)
					if onContent != nil {
						onContent("text", ev.Item.Text)
					}
				}
			case "command_execution":
				// Codex only has shell execution; map it to a synthetic tool_use event.
				if onToolUse != nil {
					toolInput := map[string]interface{}{
						"command": ev.Item.Command,
						"output":  ev.Item.AggregatedOutput,
					}
					if ev.Item.ExitCode != nil {
						toolInput["exit_code"] = *ev.Item.ExitCode
					}
					onToolUse("command_execution", toolInput)
				}
			}

		case "turn.completed":
			numTurns++
			if ev.Usage != nil {
				inputTokens += ev.Usage.InputTokens
				outputTokens += ev.Usage.OutputTokens
			}
		}
	}

	waitErr := cmd.Wait()
	isError := false
	errorType := ""
	var fatalErr error
	// A stream-level error/turn.failed is fatal even if the codex process exited 0.
	if streamErrMsg != "" {
		isError = true
		errorType = "codex_stream_error"
		fatalErr = fmt.Errorf("codex stream error: %s", streamErrMsg)
	}
	if waitErr != nil {
		if ctx.Err() == context.Canceled {
			// Caller-initiated cancellation: do not record metrics, propagate.
			return nil, fmt.Errorf("agent aborted by user")
		}
		if _, ok := waitErr.(*exec.ExitError); ok {
			stderrStr := stderrBuf.String()
			if strings.Contains(stderrStr, "failed to record rollout items") {
				log.Printf("[codex] ignored known stderr noise: %s", strings.TrimSpace(stderrStr))
			} else {
				isError = true
				errorType = "codex_exit_error"
				log.Printf("[codex] exec failed: session=%q stderr=%q", sessionID, strings.TrimSpace(stderrStr))
				fatalErr = fmt.Errorf("codex exec error: %s", stderrStr)
			}
		} else {
			isError = true
			errorType = "codex_exec_error"
			fatalErr = fmt.Errorf("codex exec: %w", waitErr)
		}
	}

	result := strings.Join(textBlocks, "\n\n")
	cost := calcCodexCost(model, inputTokens, outputTokens)
	latency := time.Since(startTime)

	// Hash session/thread ID to a stable chat ID for metrics (same approach as CLIClient).
	chatID := int64(0)
	for _, b := range []byte(threadID) {
		chatID = chatID*31 + int64(b)
	}

	totalTokens := inputTokens + outputTokens
	RecordAPICall(latency, !isError, totalTokens, cost, chatID, projectDir, errorType, ExtractModelShortName(model))

	if fatalErr != nil {
		return nil, fatalErr
	}

	resp := &CLIResponse{
		Type:         "result",
		Subtype:      "success",
		SessionID:    threadID,
		IsError:      false,
		NumTurns:     numTurns,
		Result:       result,
		TotalCostUSD: cost,
		DurationMs:   int(latency.Milliseconds()),
		TextContent:  result,
	}
	resp.Usage.InputTokens = inputTokens
	resp.Usage.OutputTokens = outputTokens

	return resp, nil
}

// buildCodexExecArgs assembles the argv for `codex exec` / `codex exec resume`.
// `codex exec resume` (codex-cli >=0.125) rejects -C/--cd, so we omit it on the
// resume path; the working directory is set via the process Dir instead.
// Fresh `codex exec` keeps -C for log readability. See issue #145.
func buildCodexExecArgs(sessionID, model, projectDir, prompt string) []string {
	if sessionID != "" {
		return []string{
			"exec", "resume", sessionID,
			"--json",
			"--dangerously-bypass-approvals-and-sandbox",
			"-m", model,
			prompt,
		}
	}
	return []string{
		"exec",
		"--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"-m", model,
		"-C", projectDir,
		prompt,
	}
}

func codexWorkingDir(projectDir string) string {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return "."
	}
	return projectDir
}

// codexPlanGuard is prepended to CallPlan prompts so codex (which has no --max-turns)
// behaves as a read-only invocation: no shell commands, no file writes.
// Output format is NOT specified here — callers (planner, reviewer) provide their own format instructions.
const codexPlanGuard = `[READ-ONLY MODE — DO NOT EXECUTE]
You are in a read-only invocation. Do NOT run any shell commands or modify any files.
Tool calls are forbidden in this turn. Follow the output format instructions below exactly.

`

// Call implements Client — non-streaming single invocation.
func (c *CodexClient) Call(ctx context.Context, message, projectDir, sessionID, modelOverride string) (*CLIResponse, error) {
	model := c.Model
	if modelOverride != "" {
		model = modelOverride
	}
	resp, err := c.runCodexStream(ctx, message, projectDir, sessionID, model, nil, nil)
	if err != nil && sessionID != "" && strings.Contains(err.Error(), "codex exec error") {
		return resp, fmt.Errorf("%w: %s: %v", ErrSessionUnavailable, sessionID, err)
	}
	return resp, err
}

// CallStream implements Client — streaming with tool/content callbacks.
func (c *CodexClient) CallStream(
	ctx context.Context,
	message, projectDir, sessionID, modelOverride string,
	onToolUse func(toolName string, toolInput map[string]interface{}),
	onContent func(contentType, text string),
) (*CLIResponse, error) {
	model := c.Model
	if modelOverride != "" {
		model = modelOverride
	}
	resp, err := c.runCodexStream(ctx, message, projectDir, sessionID, model, onToolUse, onContent)
	if err != nil && sessionID != "" && strings.Contains(err.Error(), "codex exec error") {
		return resp, fmt.Errorf("%w: %s: %v", ErrSessionUnavailable, sessionID, err)
	}
	return resp, err
}

// CallPlan implements Client — single-turn planning, no session resume, no tool callbacks.
// Codex has no --max-turns flag, so we prepend a strong "no tool execution" guard to the prompt.
// The runCodexStream call passes a no-op onToolUse so any tool events that slip through are silently dropped.
func (c *CodexClient) CallPlan(ctx context.Context, message, projectDir, modelOverride string, onContent func(contentType, text string)) (*CLIResponse, error) {
	model := c.Model
	if modelOverride != "" {
		model = modelOverride
	}
	guarded := codexPlanGuard + message
	noopTool := func(string, map[string]interface{}) {}
	return c.runCodexStream(ctx, guarded, projectDir, "", model, noopTool, onContent)
}
