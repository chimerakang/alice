package hermes

import (
	"fmt"
	"time"
)

// TaskStatus represents the lifecycle stage of a Hermes task.
type TaskStatus string

const (
	TaskStatusPlanning    TaskStatus = "planning"
	TaskStatusExecuting   TaskStatus = "executing"
	TaskStatusDone        TaskStatus = "done"
	TaskStatusFailed      TaskStatus = "failed"
	TaskStatusInterrupted TaskStatus = "interrupted"
)

// SubTaskStatus represents the execution state of a single atomic sub-task.
type SubTaskStatus string

const (
	SubTaskPending    SubTaskStatus = "pending"
	SubTaskInProgress SubTaskStatus = "in_progress"
	SubTaskDone       SubTaskStatus = "done"
	SubTaskSkipped    SubTaskStatus = "skipped"
	SubTaskFailed     SubTaskStatus = "failed"
)

// TaskState is the top-level record for a Hermes planning session.
//
// Interrupt handling is fixed: any message that arrives while a task is active
// is appended to Accumulated as user feedback (the former "inject" policy).
// The "queue" and "abort" alternatives were removed — they were never exposed
// in the UI and the runtime had to guess between them.
type TaskState struct {
	ID                string       `json:"id"`                            // UUID
	ChatID            int64        `json:"chat_id"`                       // Telegram chat
	ThreadID          int          `json:"thread_id,omitempty"`           // Telegram forum topic/thread
	// PlannerSessionID is captured from each CLI response for telemetry / dashboard
	// observation only. The Planner CallPlan path is intentionally session-less
	// on both Claude (api.go CLIClient.CallPlan) and Codex (codex_client.go
	// CodexClient.CallPlan) — neither passes --resume. Storing this value
	// across runs does not enable resume; it just lets the dashboard correlate
	// a TaskState row with a CLI session log.
	PlannerSessionID  string       `json:"planner_session_id"`
	ExecutorSessionID string       `json:"executor_session_id,omitempty"` // executor thread resume ID (transient, not persisted to DB)
	ProjectDir        string       `json:"project_dir,omitempty"`
	Goal              string       `json:"goal"`
	Plan              []SubTask    `json:"plan"`
	CurrentIdx        int          `json:"current_idx"`
	Accumulated       string       `json:"accumulated"` // rolling executor summary
	Artifacts         []Artifact   `json:"artifacts"`
	Status            TaskStatus   `json:"status"`
	InterruptedBy     *int64       `json:"interrupted_by,omitempty"` // Telegram message ID
	TokenBudget       TokenBudget  `json:"token_budget"`
	GithubIssueNumber int          `json:"github_issue_number,omitempty"` // 0 = no issue linked
	ModelUsages       []ModelUsage `json:"model_usages"`                  // per-model token tracking
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

// CurrentSubTask returns the active SubTask, or nil if the plan is exhausted.
func (t *TaskState) CurrentSubTask() *SubTask {
	if t.CurrentIdx < 0 || t.CurrentIdx >= len(t.Plan) {
		return nil
	}
	return &t.Plan[t.CurrentIdx]
}

// IsTerminal returns true when the task has reached a final state.
func (t *TaskState) IsTerminal() bool {
	switch t.Status {
	case TaskStatusDone, TaskStatusFailed, TaskStatusInterrupted:
		return true
	}
	return false
}

// ValidTaskStatusTransition reports whether a task may move from one lifecycle
// status to another. Terminal states are intentionally immutable so dashboard,
// retry, and startup recovery never observe a finished task becoming active
// again.
func ValidTaskStatusTransition(from, to TaskStatus) bool {
	if from == "" || from == to {
		return true
	}
	switch from {
	case TaskStatusPlanning:
		return to == TaskStatusExecuting || to == TaskStatusDone || to == TaskStatusFailed || to == TaskStatusInterrupted
	case TaskStatusExecuting:
		return to == TaskStatusExecuting || to == TaskStatusDone || to == TaskStatusFailed || to == TaskStatusInterrupted
	case TaskStatusDone, TaskStatusFailed, TaskStatusInterrupted:
		return false
	default:
		return false
	}
}

func ValidateTaskStatusTransition(taskID string, from, to TaskStatus) error {
	if ValidTaskStatusTransition(from, to) {
		return nil
	}
	return fmt.Errorf("invalid task status transition for %s: %s -> %s", taskID, from, to)
}

// AddUsage records token + cost usage for a given model, accumulating into
// ModelUsages slice. CostUSD is the per-call USD cost reported by the CLI
// (or estimated if Max-sub returned 0); 0 means "unknown for this call",
// not "free", and the summary should treat the model's CostUSD as authoritative
// only when CallCount > 0 *and* CostUSD > 0.
func (t *TaskState) AddUsage(model string, inputTokens, outputTokens int, costUSD float64) {
	// Find existing entry for this model.
	for i := range t.ModelUsages {
		if t.ModelUsages[i].Model == model {
			t.ModelUsages[i].InputTokens += inputTokens
			t.ModelUsages[i].OutputTokens += outputTokens
			t.ModelUsages[i].CostUSD += costUSD
			t.ModelUsages[i].CallCount++
			return
		}
	}
	// Create new entry if not found.
	t.ModelUsages = append(t.ModelUsages, ModelUsage{
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CostUSD:      costUSD,
		CallCount:    1,
	})
}

// SubTask represents an atomic unit of work assigned to an Executor.
type SubTask struct {
	ID          string        `json:"id"`
	Description string        `json:"description"`
	ToolHints   []string      `json:"tool_hints,omitempty"` // suggested tools from Planner
	Status      SubTaskStatus `json:"status"`
	Result      string        `json:"result,omitempty"` // short summary written by Executor
	Attempts    int           `json:"attempts"`
	TokensUsed  int           `json:"tokens_used"`
}

// Artifact records a file that was modified or created during task execution.
type Artifact struct {
	Path      string `json:"path"`
	Hash      string `json:"hash"` // SHA-256 hex of file contents after write
	SubTaskID string `json:"sub_task_id"`
}

// ModelUsage tracks token + cost consumption per model.
//
// CostUSD is summed from per-call costs reported by the CLI (Anthropic
// total_cost_usd / Codex local price calc) or, when those are 0,
// EstimateClaudeCost-derived fallbacks. Treat 0 as "unknown for the calls
// recorded so far" and prefer the live token×rate estimate path. See #148 1E.
type ModelUsage struct {
	Model        string  `json:"model"` // e.g., "claude-opus-4-7" or "claude-haiku-4-5-20251001"
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	CallCount    int     `json:"call_count"`
}

// TotalTokens returns the sum of input and output tokens.
func (m *ModelUsage) TotalTokens() int {
	return m.InputTokens + m.OutputTokens
}

// TokenBudget enforces resource limits at the task level.
type TokenBudget struct {
	MaxTotalTokens      int       `json:"max_total_tokens"`      // 0 = unlimited
	MaxWallclockSeconds int       `json:"max_wallclock_seconds"` // 0 = unlimited
	UsedTokens          int       `json:"used_tokens"`
	StartedAt           time.Time `json:"started_at"`
}

// Exceeded returns true if either the token or time budget has been consumed.
func (b *TokenBudget) Exceeded() bool {
	if b.MaxTotalTokens > 0 && b.UsedTokens >= b.MaxTotalTokens {
		return true
	}
	if b.MaxWallclockSeconds > 0 && !b.StartedAt.IsZero() {
		elapsed := time.Since(b.StartedAt).Seconds()
		if int(elapsed) >= b.MaxWallclockSeconds {
			return true
		}
	}
	return false
}

// BudgetStatus returns a human-readable summary of current budget usage.
func (b *TokenBudget) BudgetStatus() string {
	if b.MaxTotalTokens == 0 && b.MaxWallclockSeconds == 0 {
		return "unlimited"
	}
	elapsed := ""
	if !b.StartedAt.IsZero() {
		elapsed = time.Since(b.StartedAt).Round(time.Second).String()
	}
	return "tokens=" + itoa(b.UsedTokens) + "/" + itoa(b.MaxTotalTokens) +
		" elapsed=" + elapsed + "/" + itoa(b.MaxWallclockSeconds) + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
