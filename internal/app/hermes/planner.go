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
3. Call the emit_plan tool with an object that contains a sub_tasks array. No prose before or after.

Each sub-task object must have:
  - "id": unique string (e.g. "s1", "s2")
  - "description": one sentence describing what to do
  - "tool_hints": array of Claude Code tool names the Executor will likely need

Example tool input:
{"sub_tasks":[
  {"id":"s1","description":"Read internal/auth/auth.go to understand current structure","tool_hints":["Read"]},
  {"id":"s2","description":"Write unit tests for auth.go in internal/auth/auth_test.go","tool_hints":["Edit","Bash"]},
  {"id":"s3","description":"Run go test ./internal/auth/... and confirm all tests pass","tool_hints":["Bash"]}
]}

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
- SINGLE-ACTION RULE: if the goal is one natural action ("add one test",
  "fix one function", "add one field", "rename one symbol", "verify one
  already-implemented change"), return ONE sub-task whose description bundles
  read context -> modify/verify -> run the relevant check. Do NOT split that
  into separate Read / Edit / Test sub-tasks; the Executor handles those steps
  inside one call.

  ANTI-PATTERN (over-decomposition — DO NOT DO THIS):
    Goal: "Add an e2e test verifying result.prize_pct"
    BAD plan (4 sub-tasks):
      s1: Read result_test.go to understand test patterns
      s2: Read payout.go to find ApplyPayoutTemplate signature
      s3: Add the e2e test in result_test.go
      s4: Run go test to verify
    Why bad: each sub-task fires a separate Executor + Reviewer cold-start.
    The Executor inside s3 will naturally Read s1+s2's files anyway.
    GOOD plan (1 sub-task):
      s1: Add e2e test in internal/biz/result_test.go that creates
          tournament + applies payout template + settles + asserts
          result.prize_pct matches template tier; read result_test.go
          and payout.go for context first, then run go test ./internal/biz/...
          to verify green.
    Result: one Executor call, one Reviewer call, ~70% fewer tokens.

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
- Checked checklist items are already completed. Never create sub-tasks for
  checked items unless the goal explicitly asks to redo them.

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

Output ONLY the emit_plan tool call. No explanation, no preamble.`

// jsonBlockRe extracts the first ```json ... ``` block from Planner output.
var jsonBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\[.*?\\])\\s*```")

// CallPlanResult is the value returned by a CallPlanFunc invocation.
// CostUSD is the per-call USD cost reported by the CLI (or estimated for
// Max-sub by upstream); 0 means the caller couldn't price this call.
// See #148 1E.
type CallPlanResult struct {
	Text         string
	SessionID    string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// CallPlanFunc is the calling convention the Planner uses to invoke the CLI.
// sessionID is the previous Planner native session to resume, when available.
type CallPlanFunc func(ctx context.Context, message, projectDir, sessionID string) (CallPlanResult, error)

type PlannerRecoveryRequest struct {
	Mode        string
	Attempt     int
	MaxAttempts int
	Reason      string
}

type PlannerRecoveryDecision struct {
	Retry       bool
	Reason      string
	NextAttempt int
}

type PlannerRecoveryFunc func(PlannerRecoveryRequest) PlannerRecoveryDecision

// PlannerSession manages the long-lived Planner CLI session.
type PlannerSession struct {
	callFn     CallPlanFunc
	sessionID  string // Claude Code --resume ID; empty until first call
	maxRetries int
	extraRules string // prepended to plannerSystemPrompt when non-empty
	recoveryFn PlannerRecoveryFunc
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

// SetRecoveryDecider lets the owning runtime decide planner retry policy without
// making the hermes package import the higher-level engine package.
func (p *PlannerSession) SetRecoveryDecider(fn PlannerRecoveryFunc) { p.recoveryFn = fn }

func (p *PlannerSession) shouldRetry(mode string, attempt int, reason string) PlannerRecoveryDecision {
	req := PlannerRecoveryRequest{
		Mode:        mode,
		Attempt:     attempt,
		MaxAttempts: p.maxRetries,
		Reason:      reason,
	}
	if p.recoveryFn != nil {
		return p.recoveryFn(req)
	}
	if attempt < p.maxRetries {
		return PlannerRecoveryDecision{Retry: true, Reason: reason, NextAttempt: attempt + 1}
	}
	return PlannerRecoveryDecision{Retry: false, Reason: "max_planner_retries_reached"}
}

// Plan sends the goal to the Planner and returns parsed SubTasks plus the
// accumulated input / output token counts and USD cost across all attempts
// (the caller needs the split so per-model ModelUsage can be recorded).
// Retries up to maxRetries times on JSON parse failure, re-injecting the error
// as feedback each time.
// Enforces Complexity Gate: single-operation goals return 1 sub-task.
func (p *PlannerSession) Plan(ctx context.Context, goal, projectDir string) ([]SubTask, int, int, float64, error) {
	systemSection := plannerSystemPrompt
	if p.extraRules != "" {
		systemSection = p.extraRules + "\n\n" + plannerSystemPrompt
	}
	prompt := systemSection + "\n\nGoal: " + goal
	var lastText string
	var totalIn, totalOut int
	var totalCost float64

	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		res, err := p.callFn(ctx, prompt, projectDir, p.sessionID)
		if err != nil {
			return nil, totalIn, totalOut, totalCost, fmt.Errorf("planner attempt %d: %w", attempt, err)
		}
		if res.SessionID != "" {
			p.sessionID = res.SessionID
		}
		lastText = res.Text
		totalIn += res.InputTokens
		totalOut += res.OutputTokens
		totalCost += res.CostUSD

		tasks, parseErr := parsePlannerJSON(res.Text)
		if parseErr == nil && len(tasks) > 0 {
			// Enforce Complexity Gate: if Planner returned 1 task with "Execute directly" pattern,
			// allow it without further validation. Otherwise validate granularity.
			if len(tasks) == 1 && isDirectExecutionTask(tasks[0]) {
				// Complexity Gate: direct execution mode (simple goal)
				return tasks, totalIn, totalOut, totalCost, nil
			}
			// Multi-task plan: validate granularity and quality before executor tokens are spent.
			if err := validatePlanQuality(goal, tasks); err != nil {
				// Quality violation — inject feedback and retry
				if p.shouldRetry("planner_retry", attempt, "plan_quality_rejected").Retry {
					prompt = fmt.Sprintf(
						"Plan quality gate rejected attempt %d:\n%s\n\nFix the plan before execution. Group information gathering into deliverable sub-tasks, include code-changing work for implementation goals, and include concrete validation for changed code. Call emit_plan again with the corrected sub_tasks array.",
						attempt, err.Error(),
					)
					continue
				}
				return nil, totalIn, totalOut, totalCost, fmt.Errorf("plan quality validation failed after retries: %w", err)
			}
			return tasks, totalIn, totalOut, totalCost, nil
		}

		// Empty plan means "Planner found nothing to do" — distinct from a
		// malformed schema failure. Give a targeted retry message so the
		// Planner either confirms completion via a single verification
		// sub-task or surfaces remaining work.
		if parseErr == nil && len(tasks) == 0 {
			if p.shouldRetry("planner_retry", attempt, "empty_plan").Retry {
				prompt = "Your previous output described no sub-tasks. Every sub-task object MUST include all three required fields: `id` (string, e.g. \"verify-1\"), `description` (string), and `tool_hints` (array of strings). " +
					"If the goal is already satisfied by the current state of the project, call emit_plan with a single verification sub-task inside sub_tasks: \"Verify the goal is already satisfied. Inspect the relevant files, run the relevant build/test/grep commands, and report concrete evidence (file paths, line numbers, command output).\" with tool_hints [\"Read\",\"Bash\"]. " +
					"If there is real work remaining, call emit_plan again with those sub-tasks now. Do not send an empty sub_tasks array again."
				continue
			}
			return nil, totalIn, totalOut, totalCost, &ErrPlannerEmptyPlan{Goal: goal}
		}

		if !p.shouldRetry("planner_retry", attempt, "json_parse_failed").Retry {
			return nil, totalIn, totalOut, totalCost, &ErrPlannerJSONFailed{RawOutput: lastText}
		}
		parseErrMsg := "(unknown parse error)"
		if parseErr != nil {
			parseErrMsg = parseErr.Error()
		}
		prompt = fmt.Sprintf(
			"Error: emit_plan input parse failed on attempt %d: %s\n"+
				"Your output was:\n%s\n\n"+
				"Fix the schema. Every sub-task object MUST include all three required fields:\n"+
				"  - `id` (string, e.g. \"s1\")\n"+
				"  - `description` (string)\n"+
				"  - `tool_hints` (array of strings, e.g. [\"Read\", \"Bash\"])\n"+
				"Call emit_plan again with the corrected sub_tasks array. No prose.",
			attempt, parseErrMsg, res.Text,
		)
	}

	return nil, totalIn, totalOut, totalCost, &ErrPlannerJSONFailed{RawOutput: lastText}
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

func validatePlanQuality(goal string, tasks []SubTask) error {
	if err := validateGranularityForPlan(tasks); err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	if err := validateImplementationPlan(goal, tasks); err != nil {
		return err
	}
	if err := validateDeliverableTraceability(goal, tasks); err != nil {
		return err
	}
	return nil
}

func validateImplementationPlan(goal string, tasks []SubTask) error {
	if !strings.Contains(goal, "[GitHub #") && !strings.Contains(goal, "Hermes quality feedback") {
		return nil
	}
	if !goalRequiresImplementation(goal) || goalIsVerificationOnly(goal) {
		return nil
	}
	if !planHasMutation(tasks) {
		return fmt.Errorf("missing implementation step: goal asks for implementation/fix work, but the plan has no Edit/Write/file_patch or modifying Bash sub-task")
	}
	if !planHasValidation(tasks) {
		return fmt.Errorf("missing validation: implementation plans must include a concrete build/test/lint/verify command or validation step")
	}
	return nil
}

func validateDeliverableTraceability(goal string, tasks []SubTask) error {
	if !strings.Contains(goal, "[GitHub #") || len(tasks) <= 1 {
		return nil
	}
	readOnlyCount := 0
	standaloneVerifyCount := 0
	for _, task := range tasks {
		if isStandaloneReadOnlyTask(task) {
			readOnlyCount++
		}
		if isStandaloneValidationTask(task) {
			standaloneVerifyCount++
		}
	}
	if readOnlyCount > 1 && readOnlyCount >= len(tasks)/2 {
		return fmt.Errorf("incomplete traceability: too many standalone read/inspect sub-tasks (%d/%d); merge discovery into the deliverable sub-task that satisfies an acceptance criterion", readOnlyCount, len(tasks))
	}
	if standaloneVerifyCount > 1 {
		return fmt.Errorf("over-split validation: %d standalone validation sub-tasks; fold validation into the related deliverable sub-task unless this is a final full-suite verification", standaloneVerifyCount)
	}
	return nil
}

// Compress asks the Planner to condense the accumulated execution log and
// returns the compressed text with input / output token counts and USD cost.
func (p *PlannerSession) Compress(ctx context.Context, req CompressRequest, projectDir string) (string, int, int, float64, error) {
	res, err := p.callFn(ctx, CompressPrompt(req), projectDir, p.sessionID)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("compress: %w", err)
	}
	if res.SessionID != "" {
		p.sessionID = res.SessionID
	}
	return strings.TrimSpace(res.Text), res.InputTokens, res.OutputTokens, res.CostUSD, nil
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
	if looksLikeSplitSingleAction(tasks) {
		return fmt.Errorf("granularity violation: plan splits one natural action into separate read/modify/verify sub-tasks; bundle them into one sub-task")
	}

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

func goalRequiresImplementation(goal string) bool {
	goal = strings.ToLower(goal)
	return containsAny(goal,
		"修", "修正", "修復", "實作", "實現", "完成", "新增", "加入", "建立", "重構", "遷移",
		"fix", "implement", "build", "create", "add ", "update", "refactor", "migrate", "wire ",
	)
}

func goalIsVerificationOnly(goal string) bool {
	goal = strings.ToLower(goal)
	verifyWords := []string{
		"verify", "review", "check status", "audit", "report", "inspect", "確認", "檢查", "檢視", "驗證", "報告", "分析",
	}
	hasVerify := false
	for _, word := range verifyWords {
		if strings.Contains(goal, word) {
			hasVerify = true
			break
		}
	}
	if !hasVerify {
		return false
	}
	return !containsAny(goal, "修正", "修復", "實作", "實現", "新增", "加入", "建立", "fix", "implement", "create", "add ")
}

func planHasMutation(tasks []SubTask) bool {
	for _, task := range tasks {
		if taskHasMutation(task) {
			return true
		}
	}
	return false
}

func taskHasMutation(task SubTask) bool {
	for _, hint := range task.ToolHints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		switch hint {
		case "edit", "write", "file_patch":
			return true
		case "bash":
			desc := strings.ToLower(task.Description)
			if containsAny(desc, "commit", "push", "tag ", "apply", "generate", "make proto", "migration", "migrate", "新增", "修改", "修正", "產生", "生成") {
				return true
			}
		}
	}
	desc := strings.ToLower(task.Description)
	mutationDesc := strings.ReplaceAll(desc, "implementation", "")
	return containsAny(mutationDesc, "modify", "edit", "write", "add ", "create", "implement ", "fix ", "update", "refactor", "新增", "補", "修改", "修正", "實作")
}

func planHasValidation(tasks []SubTask) bool {
	for _, task := range tasks {
		if taskHasValidation(task) {
			return true
		}
	}
	return false
}

func taskHasValidation(task SubTask) bool {
	desc := strings.ToLower(task.Description)
	if containsAny(desc, "test", "go test", "npm test", "build", "lint", "typecheck", "verify", "validate", "confirm", "run ", "測試", "驗證", "確認", "編譯", "建置") {
		return true
	}
	for _, hint := range task.ToolHints {
		if strings.EqualFold(strings.TrimSpace(hint), "Bash") {
			return true
		}
	}
	return false
}

func isStandaloneReadOnlyTask(task SubTask) bool {
	desc := strings.ToLower(task.Description)
	if taskHasMutation(task) {
		return false
	}
	readHints := 0
	for _, hint := range task.ToolHints {
		switch strings.ToLower(strings.TrimSpace(hint)) {
		case "read", "grep", "glob", "webfetch":
			readHints++
		case "bash":
			if containsAny(desc, "grep", "rg ", "find ", "git status", "git diff", "git log", "ls ", "cat ") {
				readHints++
			}
		}
	}
	return readHints > 0 && containsAny(desc, "read", "inspect", "understand", "review", "check", "grep", "search", "map ", "確認", "檢查", "檢視", "讀取")
}

func isStandaloneValidationTask(task SubTask) bool {
	if taskHasMutation(task) {
		return false
	}
	desc := strings.ToLower(task.Description)
	return containsAny(desc, "run ", "test", "build", "lint", "typecheck", "verify", "validate", "confirm", "測試", "驗證", "確認", "建置", "編譯")
}

func looksLikeSplitSingleAction(tasks []SubTask) bool {
	if len(tasks) < 2 || len(tasks) > 4 {
		return false
	}
	var readish, changeish, verifyish int
	for _, t := range tasks {
		desc := strings.ToLower(t.Description)
		switch {
		case containsAny(desc, "read ", "inspect", "understand", "review existing", "look at", "查看", "檢視", "讀取"):
			readish++
		case containsAny(desc, "add ", "write ", "modify", "edit", "update", "implement", "fix ", "create", "新增", "補", "修改", "修正", "實作"):
			changeish++
		case containsAny(desc, "test", "verify", "run ", "build", "confirm", "validate", "驗證", "測試", "確認"):
			verifyish++
		}
	}
	if changeish == 0 {
		return false
	}
	return verifyish > 0 && readish+changeish+verifyish == len(tasks)
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
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

// ErrPlannerEmptyPlan is returned when the Planner repeatedly produces a
// syntactically valid but empty plan, suggesting the goal is already
// satisfied or the Planner cannot decompose it. Distinct from a JSON
// parse failure.
type ErrPlannerEmptyPlan struct {
	Goal string
}

func (e *ErrPlannerEmptyPlan) Error() string {
	preview := e.Goal
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return fmt.Sprintf("planner returned empty plan after retries — goal may already be satisfied or undecomposable: %s", preview)
}
