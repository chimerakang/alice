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
2. Decide whether to decompose into sub-tasks OR return a single "execute directly" sub-task.
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

COMPLEXITY GATE (Preferred Behavior):
- For SIMPLE goals (1-3 sequential operations: e.g. "commit & push & tag", "add file & run test"):
  → Return a SINGLE sub-task: {"id":"s1", "description": "Execute the goal directly", "tool_hints": [...]}
- For COMPLEX goals (architecture changes, multi-module refactors, feature implementation):
  → Decompose into 3-7 logically independent sub-tasks.

GRANULARITY RULES:
- Each sub-task must be > 1 minute of work (not: "stage file A", "stage file B").
- Group related operations (all file edits for one feature, all tests for one module).
- Avoid sequential dependencies within a sub-task unless they're inseparable.
- NEVER decompose a single command into multiple steps (e.g. "git commit" is 1 task, not 5).

IMPLEMENTATION IMPERATIVE (critical):
- If the goal references implementation work — keywords like 修, 修正, 修復, 實作,
  實現, 完成, refactor, fix, implement, build, create, redesign — the plan MUST
  include at least one sub-task that uses Edit, Write, file_patch, or Bash to
  modify the code. Verification-only plans (Read + git log + report) are a
  failure mode — they look successful but accomplish nothing the user asked for.
- Verification-only plans (no Edit/Write/file_patch) are PERMITTED only when the
  goal explicitly says "verify", "review", "check status", "audit", "report",
  "查詢", "檢視", "確認進度", "報告狀態", or "分析" without an implementation verb.
- For implementation goals, the typical shape is: read context → modify code →
  verify (build/test) → commit. The middle step is non-negotiable.
- If the goal is "[GitHub #N] ..." with an unchecked checklist, treat the
  unchecked items as the implementation list. Do not silently downgrade them
  to "verify that item X was done".

ISSUE INFORMATION SOURCE (critical):
- For ANY question about a GitHub issue — its body, status, comments, checklist
  state — the authoritative source is the GitHub API via the gh CLI:
    gh issue view N --json title,body,state,labels,comments
    gh issue list --state open --json number,title,labels
    gh issue view N --comments
- DO NOT plan a sub-task that reads docs/MASTER_TASKS.md for issue state.
  MASTER_TASKS.md is a periodically-synced snapshot produced by /task-sync;
  it lags GitHub by minutes to days and frequently shows stale checklists,
  closed issues marked open, etc. Reading it for issue lookup is a bug.
- MASTER_TASKS.md is acceptable ONLY when the goal explicitly asks about
  cross-task planning / phase organisation / portfolio-level reporting that
  the local doc captures more concisely than running multiple gh queries.
- When the goal already starts with "[GitHub #N] <title>\n\n..." the issue
  body has already been fetched and inlined — no gh issue view call needed,
  just plan against the body that is already in the Goal.

Limits:
- Maximum 15 sub-tasks per plan.
- Prefer underdcomposition (fewer, larger tasks) over overcomposition (many, tiny tasks).

Output ONLY the JSON block. No explanation, no preamble.`

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

// SetSessionID seeds the Planner's CLI --resume session ID.
func (p *PlannerSession) SetSessionID(sessionID string) { p.sessionID = sessionID }

// Plan sends the goal to the Planner and returns parsed SubTasks plus the
// accumulated input / output token counts across all attempts (the caller
// needs the split so per-model ModelUsage can be recorded).
// Retries up to maxRetries times on JSON parse failure, re-injecting the error
// as feedback each time.
// Enforces Complexity Gate: single-operation goals return 1 sub-task.
func (p *PlannerSession) Plan(ctx context.Context, goal, projectDir string) ([]SubTask, int, int, error) {
	systemSection := plannerSystemPrompt
	if p.extraRules != "" {
		systemSection = p.extraRules + "\n\n" + plannerSystemPrompt
	}
	prompt := systemSection + "\n\nGoal: " + goal
	var lastText string
	var totalIn, totalOut int

	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		text, sid, inT, outT, err := p.callFn(ctx, prompt, projectDir)
		if err != nil {
			return nil, totalIn, totalOut, fmt.Errorf("planner attempt %d: %w", attempt, err)
		}
		if sid != "" {
			p.sessionID = sid
		}
		lastText = text
		totalIn += inT
		totalOut += outT

		tasks, parseErr := parsePlannerJSON(text)
		if parseErr == nil && len(tasks) > 0 {
			// Enforce Complexity Gate: if Planner returned 1 task with "Execute directly" pattern,
			// allow it without further validation. Otherwise validate granularity.
			if len(tasks) == 1 && isDirectExecutionTask(tasks[0]) {
				// Complexity Gate: direct execution mode (simple goal)
				return tasks, totalIn, totalOut, nil
			}
			// Multi-task plan: validate granularity
			if err := validateGranularityForPlan(tasks); err != nil {
				// Granularity violation — inject feedback and retry
				if attempt < p.maxRetries {
					prompt = fmt.Sprintf(
						"Decomposition violated granularity rules on attempt %d:\n%s\n\nFix by grouping related operations. Output ONLY the corrected JSON array.",
						attempt, err.Error(),
					)
					continue
				}
				return nil, totalIn, totalOut, fmt.Errorf("granularity validation failed after retries: %w", err)
			}
			return tasks, totalIn, totalOut, nil
		}

		if attempt < p.maxRetries {
			prompt = fmt.Sprintf(
				"Error: JSON parse failed on attempt %d. Your output was:\n%s\n\nOutput ONLY the JSON array in a ```json``` block.",
				attempt, text,
			)
		}
	}

	return nil, totalIn, totalOut, &ErrPlannerJSONFailed{RawOutput: lastText}
}

// isDirectExecutionTask checks if the single sub-task is marked as "execute directly"
// (pattern: "Execute the goal directly" or similar).
func isDirectExecutionTask(task SubTask) bool {
	desc := strings.ToLower(task.Description)
	return strings.Contains(desc, "execute") && strings.Contains(desc, "directly")
}

// validateGranularityForPlan applies granularity rules to multi-task plans.
// (Wraps unmarshalSubTasks logic for reuse).
func validateGranularityForPlan(tasks []SubTask) error {
	if len(tasks) <= 1 {
		return nil // No validation needed for single tasks
	}
	return validateGranularity(tasks)
}

// Compress asks the Planner to condense the accumulated execution log and
// returns the compressed text with input / output token counts separated.
func (p *PlannerSession) Compress(ctx context.Context, req CompressRequest, projectDir string) (string, int, int, error) {
	text, sid, inT, outT, err := p.callFn(ctx, CompressPrompt(req), projectDir)
	if err != nil {
		return "", 0, 0, fmt.Errorf("compress: %w", err)
	}
	if sid != "" {
		p.sessionID = sid
	}
	return strings.TrimSpace(text), inT, outT, nil
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

	// Granularity gate: reject overly fine-grained decompositions
	if len(tasks) > 1 {
		if err := validateGranularity(tasks); err != nil {
			return nil, err
		}
	}

	return tasks, nil
}

// validateGranularity ensures sub-tasks are not oversplit (e.g. per-file operations).
// Rejects: many short tasks, repetitive patterns (stage file A, stage file B, etc).
func validateGranularity(tasks []SubTask) error {
	// Pattern: many (>8) tasks with high repetition ("stage", "add", "update" repeated)
	if len(tasks) > 8 {
		descWords := make(map[string]int)
		for _, t := range tasks {
			// Extract first verb from description
			parts := strings.Fields(t.Description)
			if len(parts) > 0 {
				verb := strings.ToLower(parts[0])
				descWords[verb]++
			}
		}
		// If one verb accounts for >50% of tasks, likely over-decomposed
		for verb, count := range descWords {
			if count > len(tasks)/2 {
				return fmt.Errorf(
					"granularity violation: %d/%d tasks start with %q (likely per-item decomposition, not per-feature)",
					count, len(tasks), verb,
				)
			}
		}
	}
	return nil
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
