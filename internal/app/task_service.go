package app

import (
	"fmt"
	"time"

	"github.com/chimerakang/alice-core/hermes"
)

// taskService replaces the former internal/app/task package. It wraps a
// hermes.TaskStateStore and enforces state-machine rules on
// every status mutation. Call newTaskService to construct one; use Transition instead of
// calling store.MarkStatus directly.
type taskService struct {
	store hermes.TaskStateStore
}

// newTaskService returns a taskService backed by store.
func newTaskService(store hermes.TaskStateStore) *taskService {
	return &taskService{store: store}
}

// Transition validates the from→to status change against the task state
// machine and, if valid, persists the new status via the underlying store.
// Returns an error if the transition is illegal or the store write fails.
func (svc *taskService) Transition(taskID string, from, to hermes.TaskStatus) error {
	if err := hermes.ValidateTaskStatusTransition(taskID, from, to); err != nil {
		return err
	}
	return svc.store.MarkStatus(taskID, to)
}

// GetTask fetches the current TaskState from the store.
func (svc *taskService) GetTask(id string) (hermes.TaskState, error) {
	return svc.store.GetTask(id)
}

// LatestSnapshot returns the latest committed snapshot for the task, or
// (Snapshot{}, false) when the underlying store does not implement
// SnapshotStore. Used by cold-restart resume paths (#169 β1) to inspect
// the durable Interrupt before reconstructing a Hermes coordinator.
func (svc *taskService) LatestSnapshot(taskID string) (hermes.Snapshot, bool) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return hermes.Snapshot{}, false
	}
	snap, err := store.GetLatestSnapshot(taskID)
	if err != nil {
		return hermes.Snapshot{}, false
	}
	return snap, true
}

// interruptResolutionStore is the storage capability needed to persist an
// operator's pause decision. Implemented by *hermes.SQLiteTaskStore.
type interruptResolutionStore interface {
	ApplyInterruptResolution(taskID string, decision hermes.InterruptResolution) error
}

// ApplyInterruptResolution persists the operator's pause decision durably
// (#169 β2). Returns ok=false when the underlying store does not support
// snapshot-aware resolution; callers should fall back to the legacy
// channel-based resume path.
func (svc *taskService) ApplyInterruptResolution(taskID string, decision hermes.InterruptResolution) (bool, error) {
	store, ok := svc.store.(interruptResolutionStore)
	if !ok {
		return false, nil
	}
	if err := store.ApplyInterruptResolution(taskID, decision); err != nil {
		return true, err
	}
	return true, nil
}

// GetActiveForChat returns the most recent non-terminal task for a chat.
// Returns hermes.ErrNoTask if no active task exists.
func (svc *taskService) GetActiveForChat(chatID int64) (hermes.TaskState, error) {
	return svc.store.GetActiveTaskForChat(chatID)
}

// ListForChat returns recent tasks for a chat (newest first, up to limit).
func (svc *taskService) ListForChat(chatID int64, limit int) ([]hermes.TaskState, error) {
	return svc.store.ListTasksForChat(chatID, limit)
}

// --- hermes.TaskStateStore implementation ---
// taskService implements hermes.TaskStateStore so it can be passed directly to the
// Hermes coordinator, eliminating the need for callers to reach through Store().

// CreateTask implements hermes.TaskStateStore.
func (svc *taskService) CreateTask(task hermes.TaskState) (hermes.TaskState, error) {
	return svc.store.CreateTask(task)
}

// GetActiveTaskForChat implements hermes.TaskStateStore.
func (svc *taskService) GetActiveTaskForChat(chatID int64) (hermes.TaskState, error) {
	return svc.store.GetActiveTaskForChat(chatID)
}

// StorePlan implements hermes.TaskStateStore.
func (svc *taskService) StorePlan(taskID string, plan []hermes.SubTask) error {
	return svc.store.StorePlan(taskID, plan)
}

// UpdateSubTask implements hermes.TaskStateStore.
func (svc *taskService) UpdateSubTask(taskID string, idx int, status hermes.SubTaskStatus, result string, tokensUsed int) error {
	return svc.store.UpdateSubTask(taskID, idx, status, result, tokensUsed)
}

// UpdateSubTaskSnapshot updates a sub-task through CommitRuntimeStep when the
// backing store supports runtime snapshots. It preserves the legacy
// UpdateSubTask merge semantics while keeping snapshot-priority read paths in
// sync. Stores without RuntimeStepStore support fall back to UpdateSubTask.
func (svc *taskService) UpdateSubTaskSnapshot(taskID string, idx int, status hermes.SubTaskStatus, result string, tokensUsed int, reason string) error {
	runtime, ok := svc.store.(hermes.RuntimeStepStore)
	if !ok {
		return svc.store.UpdateSubTask(taskID, idx, status, result, tokensUsed)
	}
	current, err := svc.store.GetTask(taskID)
	if err != nil {
		return err
	}
	if idx < 0 || idx >= len(current.Plan) {
		return fmt.Errorf("sub-task index %d out of range", idx)
	}
	plan := append([]hermes.SubTask(nil), current.Plan...)
	plan[idx].Status = status
	plan[idx].Result = result
	plan[idx].TokensUsed += tokensUsed
	plan[idx].Attempts++
	currentIdx := current.CurrentIdx
	if reason == "" {
		reason = "subtask_update"
	}
	_, err = runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID: taskID,
		Updates: []hermes.StateUpdate{
			{Plan: plan, CurrentIdx: &currentIdx},
			hermes.StateUpdateForSubTaskResult(plan[idx], idx),
		},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata: hermes.SnapshotMetadata{
			Source: "task_service",
			Reason: reason,
		},
	})
	return err
}

// MarkSubTaskStarted implements hermes.TaskStateStore.
func (svc *taskService) MarkSubTaskStarted(taskID string, idx int) error {
	return svc.store.MarkSubTaskStarted(taskID, idx)
}

// AdvanceTask implements hermes.TaskStateStore.
func (svc *taskService) AdvanceTask(taskID string, nextIdx int, status hermes.TaskStatus) error {
	return svc.store.AdvanceTask(taskID, nextIdx, status)
}

// AppendArtifact implements hermes.TaskStateStore.
func (svc *taskService) AppendArtifact(taskID string, artifact hermes.Artifact) error {
	return svc.store.AppendArtifact(taskID, artifact)
}

// UpdateAccumulated implements hermes.TaskStateStore.
func (svc *taskService) UpdateAccumulated(taskID string, accumulated string) error {
	return svc.store.UpdateAccumulated(taskID, accumulated)
}

// UpdatePlannerSession implements hermes.TaskStateStore.
func (svc *taskService) UpdatePlannerSession(taskID string, sessionID string) error {
	return svc.store.UpdatePlannerSession(taskID, sessionID)
}

// MarkInterrupted implements hermes.TaskStateStore.
func (svc *taskService) MarkInterrupted(taskID string, messageID int64) error {
	return svc.store.MarkInterrupted(taskID, messageID)
}

// MarkStatus implements hermes.TaskStateStore. The service layer fetches the
// current status first and validates the transition before delegating to the
// store, ensuring the state machine is enforced even if the backing store is
// replaced with one that has no built-in validation.
func (svc *taskService) MarkStatus(taskID string, status hermes.TaskStatus) error {
	current, err := svc.store.GetTask(taskID)
	if err != nil {
		return err
	}
	if err := hermes.ValidateTaskStatusTransition(taskID, current.Status, status); err != nil {
		return err
	}
	return svc.store.MarkStatus(taskID, status)
}

// ResetBudgetStartedAt implements hermes.TaskStateStore.
func (svc *taskService) ResetBudgetStartedAt(taskID string, t time.Time) error {
	return svc.store.ResetBudgetStartedAt(taskID, t)
}

// AddTokenUsage implements hermes.TaskStateStore.
func (svc *taskService) AddTokenUsage(taskID string, delta int) error {
	return svc.store.AddTokenUsage(taskID, delta)
}

// AddModelUsage implements hermes.TaskStateStore.
func (svc *taskService) AddModelUsage(taskID, model string, inputTokens, outputTokens int, costUSD float64) error {
	return svc.store.AddModelUsage(taskID, model, inputTokens, outputTokens, costUSD)
}

// AddModelUsageBreakdown implements hermes.TaskStateStore.
func (svc *taskService) AddModelUsageBreakdown(taskID, model string, usage hermes.TokenUsageBreakdown) error {
	return svc.store.AddModelUsageBreakdown(taskID, model, usage)
}

// AddPhaseUsage implements hermes.TaskStateStore.
func (svc *taskService) AddPhaseUsage(taskID, phase, model string, inputTokens, outputTokens int, costUSD float64) error {
	return svc.store.AddPhaseUsage(taskID, phase, model, inputTokens, outputTokens, costUSD)
}

// AddPhaseUsageBreakdown implements hermes.TaskStateStore.
func (svc *taskService) AddPhaseUsageBreakdown(taskID, phase, model string, usage hermes.TokenUsageBreakdown) error {
	return svc.store.AddPhaseUsageBreakdown(taskID, phase, model, usage)
}

// ListTasksForChat implements hermes.TaskStateStore.
func (svc *taskService) ListTasksForChat(chatID int64, limit int) ([]hermes.TaskState, error) {
	return svc.store.ListTasksForChat(chatID, limit)
}

// CommitRuntimeStep implements hermes.RuntimeStepStore when the wrapped store
// supports runtime commits. This keeps callers that pass *taskService as their task
// store on the durable snapshot path.
func (svc *taskService) CommitRuntimeStep(commit hermes.RuntimeCommit) (hermes.Snapshot, error) {
	runtime, ok := svc.store.(hermes.RuntimeStepStore)
	if !ok {
		if commit.CreatedAt.IsZero() {
			commit.CreatedAt = time.Now()
		}
		state, err := hermes.ApplyStateUpdates(hermes.HermesState{TaskID: commit.TaskID}, commit.Updates)
		if err != nil {
			return hermes.Snapshot{}, err
		}
		return hermes.Snapshot{
			TaskID:     commit.TaskID,
			State:      state,
			NextStep:   commit.NextStep,
			SourceNode: commit.SourceNode,
			Metadata:   commit.Metadata,
			CreatedAt:  commit.CreatedAt,
		}, nil
	}
	return runtime.CommitRuntimeStep(commit)
}

// CreateSnapshot implements hermes.SnapshotStore by forwarding to the
// wrapped store when it supports the snapshot interface. Required for
// the graph runtime's seedPlannerSnapshot path; non-snapshot stores
// fall through to a no-op-style synthetic so callers can keep treating
// taskService as if every backend supports it.
func (svc *taskService) CreateSnapshot(snapshot hermes.Snapshot) (hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return snapshot, fmt.Errorf("CreateSnapshot: underlying store does not implement SnapshotStore")
	}
	return store.CreateSnapshot(snapshot)
}

// GetLatestSnapshot implements hermes.SnapshotStore.
func (svc *taskService) GetLatestSnapshot(taskID string) (hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return hermes.Snapshot{}, fmt.Errorf("GetLatestSnapshot: underlying store does not implement SnapshotStore")
	}
	return store.GetLatestSnapshot(taskID)
}

// GetLatestSnapshotForThread implements hermes.SnapshotStore.
func (svc *taskService) GetLatestSnapshotForThread(chatID int64, threadID int) (hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return hermes.Snapshot{}, fmt.Errorf("GetLatestSnapshotForThread: underlying store does not implement SnapshotStore")
	}
	return store.GetLatestSnapshotForThread(chatID, threadID)
}

// ListSnapshotHistory implements hermes.SnapshotStore.
func (svc *taskService) ListSnapshotHistory(taskID string) ([]hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return nil, fmt.Errorf("ListSnapshotHistory: underlying store does not implement SnapshotStore")
	}
	return store.ListSnapshotHistory(taskID)
}

