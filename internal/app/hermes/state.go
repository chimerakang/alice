package hermes

import "time"

// TaskStatus represents the lifecycle stage of a Hermes task.
type TaskStatus string

const (
	TaskStatusPlanning    TaskStatus = "planning"
	TaskStatusExecuting   TaskStatus = "executing"
	TaskStatusValidating  TaskStatus = "validating"
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

// InterruptPolicy controls how incoming user messages are handled during task execution.
type InterruptPolicy string

const (
	// InterruptQueue holds new messages until the current sub-task completes.
	InterruptQueue InterruptPolicy = "queue"
	// InterruptAbort cancels the current sub-task and records the interrupting message.
	InterruptAbort InterruptPolicy = "interrupt"
	// InterruptInject treats the new message as feedback and appends it to Accumulated.
	InterruptInject InterruptPolicy = "inject"
)

// TaskState is the top-level record for a Hermes planning session.
type TaskState struct {
	ID               string         `json:"id"`                // UUID
	ChatID           int64          `json:"chat_id"`           // Telegram chat
	PlannerSessionID string         `json:"planner_session_id"` // Claude Code --resume ID
	Goal             string         `json:"goal"`
	Plan             []SubTask      `json:"plan"`
	CurrentIdx       int            `json:"current_idx"`
	Accumulated      string         `json:"accumulated"`  // rolling executor summary
	Artifacts        []Artifact     `json:"artifacts"`
	Status           TaskStatus     `json:"status"`
	InterruptedBy    *int64         `json:"interrupted_by,omitempty"` // Telegram message ID
	TokenBudget      TokenBudget    `json:"token_budget"`
	InterruptPolicy  InterruptPolicy `json:"interrupt_policy"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
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

// SubTask represents an atomic unit of work assigned to an Executor.
type SubTask struct {
	ID          string        `json:"id"`
	Description string        `json:"description"`
	ToolHints   []string      `json:"tool_hints,omitempty"` // suggested tools from Planner
	Status      SubTaskStatus `json:"status"`
	Result      string        `json:"result,omitempty"`   // short summary written by Executor
	Attempts    int           `json:"attempts"`
	TokensUsed  int           `json:"tokens_used"`
}

// Artifact records a file that was modified or created during task execution.
type Artifact struct {
	Path      string `json:"path"`
	Hash      string `json:"hash"`       // SHA-256 hex of file contents after write
	SubTaskID string `json:"sub_task_id"`
}

// TokenBudget enforces resource limits at the task level.
type TokenBudget struct {
	MaxTotalTokens      int       `json:"max_total_tokens"`       // 0 = unlimited
	MaxWallclockSeconds int       `json:"max_wallclock_seconds"`  // 0 = unlimited
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
