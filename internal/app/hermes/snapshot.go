package hermes

import "time"

// RuntimeStep is the explicit FSM step the durable Hermes runtime should run
// next. It intentionally stays smaller than LangGraph's trigger model.
type RuntimeStep string

const (
	RuntimeStepPlanner      RuntimeStep = "planner"
	RuntimeStepReplanSetup  RuntimeStep = "replan_setup" // pre-planner replan decision (#169 #4)
	RuntimeStepExecutor     RuntimeStep = "executor"
	RuntimeStepReviewer     RuntimeStep = "reviewer"
	RuntimeStepStrictReview RuntimeStep = "strict_review" // per-sub-task review (γ3c)
	RuntimeStepApproval     RuntimeStep = "approval"
	RuntimeStepTerminal     RuntimeStep = "terminal"
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
	// Replan carries the partial-vs-full replan decision the
	// ReplanSetupNode produced before re-entering the planner. It is
	// consumed (and cleared) by PlannerNode on the next attempt; see
	// #169 #4.
	Replan *ReplanContext `json:"replan,omitempty"`
}

// ReplanContext is the structured output of the replan-setup decision.
// Lives on HermesState while a retry attempt is queued; PlannerNode
// reads it instead of state.Goal / state.Accumulated when set, then
// clears it via StateUpdate.ClearReplan so it does not bleed into the
// next attempt.
type ReplanContext struct {
	// Goal is the augmented goal text the planner should consume. It
	// already folds in prior-review feedback (see buildReplanGoal /
	// buildPartialReplanGoal in the engine package).
	Goal string `json:"goal,omitempty"`
	// Accumulated is the seed accumulated text. Empty string means
	// "full reset"; non-empty means "partial retry — preserve this
	// transcript prefix".
	Accumulated string `json:"accumulated"`
	// PreservedSubTasks is the list of high-score sub-tasks to merge
	// in front of the planner's new tasks. Empty for full retries.
	PreservedSubTasks []SubTask `json:"preserved_subtasks,omitempty"`
	// AttemptIdx is the 1-based replan attempt index (1 = first
	// replan). Used by telemetry and the planner's id-collision
	// rename.
	AttemptIdx int `json:"attempt_idx,omitempty"`
	// Trigger is "partial" or "full" depending on the decision branch.
	// Telemetry / dashboards key on this; routing does not.
	Trigger string `json:"trigger,omitempty"`
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
	EndedAt    *time.Time    `json:"ended_at,omitempty"`
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
	// ModelUsages and PhaseUsages are merged by key (Model / (Phase, Model))
	// in the reducer to match the legacy AddModelUsageBreakdown semantics.
	// Each entry represents a delta to add; the reducer sums token fields
	// and bumps CallCount when the key already exists.
	ModelUsages []ModelUsage `json:"model_usages,omitempty"`
	PhaseUsages []PhaseUsage `json:"phase_usages,omitempty"`
	// TokenUsageDelta is added to TokenBudget.UsedTokens. Used by callers
	// that record a budget consumption without a full per-model breakdown.
	TokenUsageDelta int `json:"token_usage_delta,omitempty"`
	// BudgetStartedAt resets TokenBudget.StartedAt. Used by the budget
	// continue/resume path to start a fresh measurement window.
	BudgetStartedAt *time.Time `json:"budget_started_at,omitempty"`
	// PlannerSessionID and ExecutorSessionID record the backend resume IDs
	// captured by Plan/Execute calls; needed for cross-restart continuation.
	PlannerSessionID  *string            `json:"planner_session_id,omitempty"`
	ExecutorSessionID *string            `json:"executor_session_id,omitempty"`
	SubTaskResults    []SubTaskResult    `json:"subtask_results,omitempty"`
	Interrupt         *HermesInterrupt   `json:"interrupt,omitempty"`
	ClearInterrupt    bool               `json:"clear_interrupt,omitempty"`
	Errors            []HermesStateError `json:"errors,omitempty"`
	GithubIssueNumber *int               `json:"github_issue_number,omitempty"`
	// Replan / ClearReplan follow the Interrupt / ClearInterrupt
	// pattern: a non-nil Replan installs the replan context on the
	// state; ClearReplan=true (with Replan==nil) erases it. PlannerNode
	// sets ClearReplan once it has consumed the context.
	Replan      *ReplanContext `json:"replan,omitempty"`
	ClearReplan bool           `json:"clear_replan,omitempty"`
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

// RuntimeCommit is one durable execution boundary:
// current state + batched node writes -> reduced state + legacy view + snapshot.
type RuntimeCommit struct {
	TaskID           string
	Updates          []StateUpdate
	NextStep         RuntimeStep
	SourceNode       RuntimeStep
	Metadata         SnapshotMetadata
	SnapshotID       string
	ParentSnapshotID string
	ChannelVersions  map[string]int64
	CreatedAt        time.Time
}

// SnapshotStore defines durable checkpoint operations. It is separate from
// TaskStateStore so Phase 1 does not widen every existing task-store test stub.
type SnapshotStore interface {
	CreateSnapshot(snapshot Snapshot) (Snapshot, error)
	GetLatestSnapshot(taskID string) (Snapshot, error)
	GetLatestSnapshotForThread(chatID int64, threadID int) (Snapshot, error)
	ListSnapshotHistory(taskID string) ([]Snapshot, error)
}

// RuntimeStepStore commits reducer output and the legacy task view atomically.
type RuntimeStepStore interface {
	CommitRuntimeStep(commit RuntimeCommit) (Snapshot, error)
}
