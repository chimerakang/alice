package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// plannerSystemPrompt is the role-defining prefix prepended to every Planner call.
const plannerSystemPrompt = `You are the Planner in a Hermes Brain-Executor system.

Your job:
1. Receive a goal from the user.
2. Break it into atomic sub-tasks that a separate Executor can carry out one at a time.
3. Output ONLY a JSON array inside a fenced code block. No prose before or after.

Each sub-task object must have:
  - "id": unique string (e.g. "s1", "s2")
  - "description": one sentence describing what to do
  - "tool_hints": array of Claude Code tool names the Executor will likely need

Example output:
` + "```json" + `
[
  {"id":"s1","description":"Read internal/auth/auth.go to understand current structure","tool_hints":["Read"]},
  {"id":"s2","description":"Write unit tests for auth.go in internal/auth/auth_test.go","tool_hints":["Edit","Bash"]},
  {"id":"s3","description":"Run go test ./internal/auth/... and confirm all tests pass","tool_hints":["Bash"]}
]
` + "```" + `

Rules:
- Each sub-task must be independently executable.
- Keep descriptions concrete and tool-actionable.
- Maximum 15 sub-tasks per plan.
- Output ONLY the JSON block. No explanation, no preamble.`

// jsonBlockRe extracts the first ```json ... ``` block from Planner output.
var jsonBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\[.*?\\])\\s*```")

// CallPlanFunc is the calling convention the Planner uses to invoke the CLI.
// Returns: text output, session ID (for --resume), input tokens, output tokens, error.
type CallPlanFunc func(ctx context.Context, message, projectDir string) (text, sessionID string, inTokens, outTokens int, err error)

// PlannerSession manages the long-lived Planner CLI session.
type PlannerSession struct {
	callFn     CallPlanFunc
	sessionID  string // Claude Code --resume ID; empty until first call
	maxRetries int
	extraRules string // prepended to plannerSystemPrompt when non-empty
}

// NewPlannerSession creates a Planner session that calls the CLI via callFn.
// extraRules is prepended to the built-in plannerSystemPrompt on every Plan() call.
func NewPlannerSession(callFn CallPlanFunc, maxRetries int, extraRules string) *PlannerSession {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &PlannerSession{callFn: callFn, maxRetries: maxRetries, extraRules: extraRules}
}

// SessionID returns the current --resume session ID (empty before first call).
func (p *PlannerSession) SessionID() string { return p.sessionID }

// Plan sends the goal to the Planner and returns parsed SubTasks.
// Retries up to maxRetries times on JSON parse failure, re-injecting the error
// as feedback each time.
func (p *PlannerSession) Plan(ctx context.Context, goal, projectDir string) ([]SubTask, int, error) {
	systemSection := plannerSystemPrompt
	if p.extraRules != "" {
		systemSection = p.extraRules + "\n\n" + plannerSystemPrompt
	}
	prompt := systemSection + "\n\nGoal: " + goal
	var lastText string
	var totalTokens int

	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		text, sid, inT, outT, err := p.callFn(ctx, prompt, projectDir)
		if err != nil {
			return nil, totalTokens, fmt.Errorf("planner attempt %d: %w", attempt, err)
		}
		if sid != "" {
			p.sessionID = sid
		}
		lastText = text
		totalTokens += inT + outT

		tasks, parseErr := parsePlannerJSON(text)
		if parseErr == nil && len(tasks) > 0 {
			return tasks, totalTokens, nil
		}

		if attempt < p.maxRetries {
			prompt = fmt.Sprintf(
				"Error: JSON parse failed on attempt %d. Your output was:\n%s\n\nOutput ONLY the JSON array in a ```json``` block.",
				attempt, text,
			)
		}
	}

	return nil, totalTokens, &ErrPlannerJSONFailed{RawOutput: lastText}
}

// Compress asks the Planner to condense the accumulated execution log.
func (p *PlannerSession) Compress(ctx context.Context, req CompressRequest, projectDir string) (string, int, error) {
	text, sid, inT, outT, err := p.callFn(ctx, CompressPrompt(req), projectDir)
	if err != nil {
		return "", 0, fmt.Errorf("compress: %w", err)
	}
	if sid != "" {
		p.sessionID = sid
	}
	return strings.TrimSpace(text), inT + outT, nil
}

// ── JSON parsing ──────────────────────────────────────────────────────────────

func parsePlannerJSON(text string) ([]SubTask, error) {
	// Prefer the fenced block
	if m := jsonBlockRe.FindStringSubmatch(text); len(m) > 1 {
		return unmarshalSubTasks(m[1])
	}
	// Fallback: first '[' … last ']'
	if start := strings.IndexByte(text, '['); start >= 0 {
		if end := strings.LastIndexByte(text, ']'); end > start {
			return unmarshalSubTasks(text[start : end+1])
		}
	}
	return nil, fmt.Errorf("no JSON array found in planner output")
}

func unmarshalSubTasks(raw string) ([]SubTask, error) {
	var tasks []SubTask
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &tasks); err != nil {
		return nil, fmt.Errorf("JSON unmarshal: %w", err)
	}
	for i, t := range tasks {
		if t.ID == "" {
			return nil, fmt.Errorf("sub-task %d missing id", i)
		}
		if t.Description == "" {
			return nil, fmt.Errorf("sub-task %d missing description", i)
		}
		if tasks[i].Status == "" {
			tasks[i].Status = SubTaskPending
		}
	}
	return tasks, nil
}

// ── Error types ───────────────────────────────────────────────────────────────

// ErrPlannerJSONFailed is returned when the Planner cannot produce valid JSON
// after all retries.
type ErrPlannerJSONFailed struct {
	RawOutput string
}

func (e *ErrPlannerJSONFailed) Error() string {
	preview := e.RawOutput
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return fmt.Sprintf("planner JSON parse failed after retries; last output: %s", preview)
}
