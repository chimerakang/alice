package hermes

import "time"

// RuntimeStep is the explicit FSM step the durable Hermes runtime should run
// next. It intentionally stays smaller than LangGraph's trigger model.
type RuntimeStep string

const (
	RuntimeStepPlanner  RuntimeStep = "planner"
	RuntimeStepExecutor RuntimeStep = "executor"
	RuntimeStepReviewer RuntimeStep = "reviewer"
	RuntimeStepApproval RuntimeStep = "approval"
	RuntimeStepTerminal RuntimeStep = "terminal"
)

// HermesState is the canonical durable runtime state stored in snapshots.
// It mirrors the current TaskState surface while leaving room for reducer-only
// fields introduced in later phases.
type HermesState struct {
	TaskID            string             `json:"task_id"`
	ChatID            int64              `json:"chat_id"`
	ThreadID          int                `json:"thread_id,omitempty"`
	PlannerSessionID  string             `json:"planner_session_id,omitempty"`
	ExecutorSessionID string             `json:"executor_session_id,omitempty"`
	ProjectDir        string             `json:"project_dir,omitempty"`
	Goal              string             `json:"goal,omitempty"`
	Status            TaskStatus         `json:"status,omitempty"`
	CurrentIdx        int                `json:"current_idx"`
	Plan              []SubTask          `json:"plan,omitempty"`
	Accumulated       string             `json:"accumulated,omitempty"`
	Artifacts         []Artifact         `json:"artifacts,omitempty"`
	TokenBudget       TokenBudget        `json:"token_budget,omitempty"`
	GithubIssueNumber int                `json:"github_issue_number,omitempty"`
	ModelUsages       []ModelUsage       `json:"model_usages,omitempty"`
	PhaseUsages       []PhaseUsage       `json:"phase_usages,omitempty"`
	SubTaskResults    []SubTaskResult    `json:"subtask_results,omitempty"`
	Interrupt         *HermesInterrupt   `json:"interrupt,omitempty"`
	Errors            []HermesStateError `json:"errors,omitempty"`
}

// HermesStateFromTaskState converts the existing persisted task shape into the
// new snapshot state shape.
func HermesStateFromTaskState(task TaskState) HermesState {
	state := HermesState{
		TaskID:            task.ID,
		ChatID:            task.ChatID,
		ThreadID:          task.ThreadID,
		PlannerSessionID:  task.PlannerSessionID,
		ExecutorSessionID: task.ExecutorSessionID,
		ProjectDir:        task.ProjectDir,
		Goal:              task.Goal,
		Status:            task.Status,
		CurrentIdx:        task.CurrentIdx,
		Plan:              append([]SubTask(nil), task.Plan...),
		Accumulated:       task.Accumulated,
		Artifacts:         append([]Artifact(nil), task.Artifacts...),
		TokenBudget:       task.TokenBudget,
		GithubIssueNumber: task.GithubIssueNumber,
		ModelUsages:       append([]ModelUsage(nil), task.ModelUsages...),
		PhaseUsages:       append([]PhaseUsage(nil), task.PhaseUsages...),
	}
	if task.InterruptedBy != nil {
		state.Interrupt = &HermesInterrupt{
			MessageID: *task.InterruptedBy,
			Reason:    "legacy_interrupted_by",
		}
	}
	return state
}

// SubTaskResult is the reducer-friendly result record emitted by executor-like
// nodes. Later phases define the merge rule by sub-task id.
type SubTaskResult struct {
	SubTaskID  string        `json:"subtask_id,omitempty"`
	Index      int           `json:"index"`
	Status     SubTaskStatus `json:"status,omitempty"`
	Result     string        `json:"result,omitempty"`
	TokensUsed int           `json:"tokens_used,omitempty"`
	Attempts   int           `json:"attempts,omitempty"`
}

// HermesInterrupt captures a durable human-in-the-loop pause. Telegram callback
// wiring is intentionally deferred to #166.
type HermesInterrupt struct {
	ID         string      `json:"id,omitempty"`
	MessageID  int64       `json:"message_id,omitempty"`
	SourceStep RuntimeStep `json:"source_step,omitempty"`
	ResumeStep RuntimeStep `json:"resume_step,omitempty"`
	Payload    any         `json:"payload,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	CreatedAt  time.Time   `json:"created_at,omitempty"`
	ExpiresAt  *time.Time  `json:"expires_at,omitempty"`
}

// HermesStateError records runtime errors without immediately deciding recovery
// semantics. Reducer behavior is introduced in #163.
type HermesStateError struct {
	Step      RuntimeStep `json:"step,omitempty"`
	Message   string      `json:"message"`
	Retryable bool        `json:"retryable,omitempty"`
	CreatedAt time.Time   `json:"created_at,omitempty"`
}

// StateUpdate is a partial update returned by a runtime node. Pointer fields
// distinguish "not updated" from zero values; reducers are implemented in #163.
type StateUpdate struct {
	Status            *TaskStatus        `json:"status,omitempty"`
	CurrentIdx        *int               `json:"current_idx,omitempty"`
	Plan              []SubTask          `json:"plan,omitempty"`
	Accumulated       *string            `json:"accumulated,omitempty"`
	AccumulatedDelta  string             `json:"accumulated_delta,omitempty"`
	Artifacts         []Artifact         `json:"artifacts,omitempty"`
	ModelUsages       []ModelUsage       `json:"model_usages,omitempty"`
	PhaseUsages       []PhaseUsage       `json:"phase_usages,omitempty"`
	SubTaskResults    []SubTaskResult    `json:"subtask_results,omitempty"`
	Interrupt         *HermesInterrupt   `json:"interrupt,omitempty"`
	ClearInterrupt    bool               `json:"clear_interrupt,omitempty"`
	Errors            []HermesStateError `json:"errors,omitempty"`
	GithubIssueNumber *int               `json:"github_issue_number,omitempty"`
}

// SnapshotMetadata captures debug and audit context for a committed snapshot.
type SnapshotMetadata struct {
	Source     string         `json:"source,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Attempt    int            `json:"attempt,omitempty"`
	Model      string         `json:"model,omitempty"`
	StepStatus string         `json:"step_status,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// Snapshot is the persisted checkpoint unit for the durable Hermes runtime.
type Snapshot struct {
	ID               string           `json:"id"`
	TaskID           string           `json:"task_id"`
	ChatID           int64            `json:"chat_id"`
	ThreadID         int              `json:"thread_id,omitempty"`
	Step             int              `json:"step"`
	State            HermesState      `json:"state"`
	NextStep         RuntimeStep      `json:"next_step"`
	SourceNode       RuntimeStep      `json:"source_node,omitempty"`
	Writes           StateUpdate      `json:"writes,omitempty"`
	Metadata         SnapshotMetadata `json:"metadata,omitempty"`
	ParentSnapshotID string           `json:"parent_snapshot_id,omitempty"`
	ChannelVersions  map[string]int64 `json:"channel_versions,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
}

// SnapshotStore defines durable checkpoint operations. It is separate from
// TaskStateStore so Phase 1 does not widen every existing task-store test stub.
type SnapshotStore interface {
	CreateSnapshot(snapshot Snapshot) (Snapshot, error)
	GetLatestSnapshot(taskID string) (Snapshot, error)
	GetLatestSnapshotForThread(chatID int64, threadID int) (Snapshot, error)
	ListSnapshotHistory(taskID string) ([]Snapshot, error)
}
