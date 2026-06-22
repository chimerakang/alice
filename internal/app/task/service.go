// Package task provides a unified, state-machine-aware task management layer
// that bridges the Hermes legacy task store and the unified `tasks` mirror
// table. It is the single source of truth for valid task state transitions
// and is intended to be consumed by /retry, the Hermes coordinator, and
// dashboard read paths (wired in a subsequent refactor).
//
// # Task state machine
//
// The following transitions are permitted (terminal states have no outbound
// edges; all others are invalid and rejected by Transition):
//
//	(new) ──> planning
//	planning ──> executing
//	planning ──> done | failed | interrupted     (plan-phase early exit)
//	executing ──> done | failed | interrupted
//	done       ──> (terminal)
//	failed     ──> (terminal)
//	interrupted ──> (terminal)
//
// # Sub-task state machine
//
//	pending ──> in_progress ──> done | failed | skipped
//
// # Hermes-legacy ↔ unified-task status mapping
//
// The hermes.TaskStatus string values are identical to the `status` column
// in the unified `tasks` table. No transformation is required; both sides
// use: "planning", "executing", "done", "failed", "interrupted".
// Similarly, hermes.SubTaskStatus maps directly to the `sub_tasks.status`
// column: "pending", "in_progress", "done", "failed", "skipped".
//
// # SQLite-backed durable job queue evaluation (issue #137)
//
// Decision: NOT adopted at this time. Rationale below.
//
// Alice currently has four "background job" surfaces that could benefit from a
// durable queue: Hermes sub-task execution, /retry, cron (robfig/cron +
// SQLite-persisted ScheduledTask), and task-sync.  A SQLite-backed durable
// queue (e.g. a `job_queue` table polled by a worker goroutine) would add
// at-least-once delivery guarantees and survive process restarts.
//
// Why we skip it for now:
//  1. Single-connection constraint. Alice uses SetMaxOpenConns(1) on its
//     SQLite pool to prevent concurrent-write corruption.  A polling worker
//     loop would add a constant-rate reader that competes with the existing
//     write workload; under high throughput this increases lock-wait latency
//     rather than reducing it.
//  2. Bot-restart recovery is already solved by the startup zombie-sweep
//     (af3e8c2), which marks dangling executing/planning tasks as failed.
//     Full at-least-once re-enqueue requires idempotent handlers; none of
//     the four surfaces is currently idempotent, so adopting the queue would
//     need handler rewrites as a prerequisite.
//  3. Cron and task-sync are already durable: cron tasks survive restarts
//     because CronScheduler reloads from the `scheduled_tasks` SQLite table
//     on startup; task-sync is triggered on demand and carries no state.
//  4. The Hermes/retry surfaces are the only ones that genuinely need
//     at-least-once semantics.  They are short-lived (minutes) and already
//     have explicit failure paths; a durable queue adds complexity without
//     meaningfully improving MTTR for the failure modes seen in production.
//
// Re-evaluate if any of the following becomes true:
//   - Bot process restarts more than once per active Hermes session.
//   - We move to WAL mode + multiple connections (removes constraint 1).
//   - A job surfaces that is genuinely non-idempotent and long-running (>30 min).
package task

import (
	"fmt"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

// Service wraps a hermes.TaskStateStore and enforces state-machine rules on
// every status mutation. Call New to construct one; use Transition instead of
// calling store.MarkStatus directly.
type Service struct {
	store hermes.TaskStateStore
}

// New returns a Service backed by store.
func New(store hermes.TaskStateStore) *Service {
	return &Service{store: store}
}

// Transition validates the from→to status change against the task state
// machine and, if valid, persists the new status via the underlying store.
// Returns an error if the transition is illegal or the store write fails.
func (svc *Service) Transition(taskID string, from, to hermes.TaskStatus) error {
	if err := hermes.ValidateTaskStatusTransition(taskID, from, to); err != nil {
		return err
	}
	return svc.store.MarkStatus(taskID, to)
}

// GetTask fetches the current TaskState from the store.
func (svc *Service) GetTask(id string) (hermes.TaskState, error) {
	return svc.store.GetTask(id)
}

// LatestSnapshot returns the latest committed snapshot for the task, or
// (Snapshot{}, false) when the underlying store does not implement
// SnapshotStore. Used by cold-restart resume paths (#169 β1) to inspect
// the durable Interrupt before reconstructing a Hermes coordinator.
func (svc *Service) LatestSnapshot(taskID string) (hermes.Snapshot, bool) {
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
func (svc *Service) ApplyInterruptResolution(taskID string, decision hermes.InterruptResolution) (bool, error) {
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
func (svc *Service) GetActiveForChat(chatID int64) (hermes.TaskState, error) {
	return svc.store.GetActiveTaskForChat(chatID)
}

// ListForChat returns recent tasks for a chat (newest first, up to limit).
func (svc *Service) ListForChat(chatID int64, limit int) ([]hermes.TaskState, error) {
	return svc.store.ListTasksForChat(chatID, limit)
}

// --- hermes.TaskStateStore implementation ---
// Service implements hermes.TaskStateStore so it can be passed directly to the
// Hermes coordinator, eliminating the need for callers to reach through Store().

// CreateTask implements hermes.TaskStateStore.
func (svc *Service) CreateTask(task hermes.TaskState) (hermes.TaskState, error) {
	return svc.store.CreateTask(task)
}

// GetActiveTaskForChat implements hermes.TaskStateStore.
func (svc *Service) GetActiveTaskForChat(chatID int64) (hermes.TaskState, error) {
	return svc.store.GetActiveTaskForChat(chatID)
}

// StorePlan implements hermes.TaskStateStore.
func (svc *Service) StorePlan(taskID string, plan []hermes.SubTask) error {
	return svc.store.StorePlan(taskID, plan)
}

// UpdateSubTask implements hermes.TaskStateStore.
func (svc *Service) UpdateSubTask(taskID string, idx int, status hermes.SubTaskStatus, result string, tokensUsed int) error {
	return svc.store.UpdateSubTask(taskID, idx, status, result, tokensUsed)
}

// UpdateSubTaskSnapshot updates a sub-task through CommitRuntimeStep when the
// backing store supports runtime snapshots. It preserves the legacy
// UpdateSubTask merge semantics while keeping snapshot-priority read paths in
// sync. Stores without RuntimeStepStore support fall back to UpdateSubTask.
func (svc *Service) UpdateSubTaskSnapshot(taskID string, idx int, status hermes.SubTaskStatus, result string, tokensUsed int, reason string) error {
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
func (svc *Service) MarkSubTaskStarted(taskID string, idx int) error {
	return svc.store.MarkSubTaskStarted(taskID, idx)
}

// AdvanceTask implements hermes.TaskStateStore.
func (svc *Service) AdvanceTask(taskID string, nextIdx int, status hermes.TaskStatus) error {
	return svc.store.AdvanceTask(taskID, nextIdx, status)
}

// AppendArtifact implements hermes.TaskStateStore.
func (svc *Service) AppendArtifact(taskID string, artifact hermes.Artifact) error {
	return svc.store.AppendArtifact(taskID, artifact)
}

// UpdateAccumulated implements hermes.TaskStateStore.
func (svc *Service) UpdateAccumulated(taskID string, accumulated string) error {
	return svc.store.UpdateAccumulated(taskID, accumulated)
}

// UpdatePlannerSession implements hermes.TaskStateStore.
func (svc *Service) UpdatePlannerSession(taskID string, sessionID string) error {
	return svc.store.UpdatePlannerSession(taskID, sessionID)
}

// MarkInterrupted implements hermes.TaskStateStore.
func (svc *Service) MarkInterrupted(taskID string, messageID int64) error {
	return svc.store.MarkInterrupted(taskID, messageID)
}

// MarkStatus implements hermes.TaskStateStore. The service layer fetches the
// current status first and validates the transition before delegating to the
// store, ensuring the state machine is enforced even if the backing store is
// replaced with one that has no built-in validation.
func (svc *Service) MarkStatus(taskID string, status hermes.TaskStatus) error {
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
func (svc *Service) ResetBudgetStartedAt(taskID string, t time.Time) error {
	return svc.store.ResetBudgetStartedAt(taskID, t)
}

// AddTokenUsage implements hermes.TaskStateStore.
func (svc *Service) AddTokenUsage(taskID string, delta int) error {
	return svc.store.AddTokenUsage(taskID, delta)
}

// AddModelUsage implements hermes.TaskStateStore.
func (svc *Service) AddModelUsage(taskID, model string, inputTokens, outputTokens int, costUSD float64) error {
	return svc.store.AddModelUsage(taskID, model, inputTokens, outputTokens, costUSD)
}

// AddModelUsageBreakdown implements hermes.TaskStateStore.
func (svc *Service) AddModelUsageBreakdown(taskID, model string, usage hermes.TokenUsageBreakdown) error {
	return svc.store.AddModelUsageBreakdown(taskID, model, usage)
}

// AddPhaseUsage implements hermes.TaskStateStore.
func (svc *Service) AddPhaseUsage(taskID, phase, model string, inputTokens, outputTokens int, costUSD float64) error {
	return svc.store.AddPhaseUsage(taskID, phase, model, inputTokens, outputTokens, costUSD)
}

// AddPhaseUsageBreakdown implements hermes.TaskStateStore.
func (svc *Service) AddPhaseUsageBreakdown(taskID, phase, model string, usage hermes.TokenUsageBreakdown) error {
	return svc.store.AddPhaseUsageBreakdown(taskID, phase, model, usage)
}

// ListTasksForChat implements hermes.TaskStateStore.
func (svc *Service) ListTasksForChat(chatID int64, limit int) ([]hermes.TaskState, error) {
	return svc.store.ListTasksForChat(chatID, limit)
}

// CommitRuntimeStep implements hermes.RuntimeStepStore when the wrapped store
// supports runtime commits. This keeps callers that pass *Service as their task
// store on the durable snapshot path.
func (svc *Service) CommitRuntimeStep(commit hermes.RuntimeCommit) (hermes.Snapshot, error) {
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
// Service as if every backend supports it.
func (svc *Service) CreateSnapshot(snapshot hermes.Snapshot) (hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return snapshot, fmt.Errorf("CreateSnapshot: underlying store does not implement SnapshotStore")
	}
	return store.CreateSnapshot(snapshot)
}

// GetLatestSnapshot implements hermes.SnapshotStore.
func (svc *Service) GetLatestSnapshot(taskID string) (hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return hermes.Snapshot{}, fmt.Errorf("GetLatestSnapshot: underlying store does not implement SnapshotStore")
	}
	return store.GetLatestSnapshot(taskID)
}

// GetLatestSnapshotForThread implements hermes.SnapshotStore.
func (svc *Service) GetLatestSnapshotForThread(chatID int64, threadID int) (hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return hermes.Snapshot{}, fmt.Errorf("GetLatestSnapshotForThread: underlying store does not implement SnapshotStore")
	}
	return store.GetLatestSnapshotForThread(chatID, threadID)
}

// ListSnapshotHistory implements hermes.SnapshotStore.
func (svc *Service) ListSnapshotHistory(taskID string) ([]hermes.Snapshot, error) {
	store, ok := svc.store.(hermes.SnapshotStore)
	if !ok {
		return nil, fmt.Errorf("ListSnapshotHistory: underlying store does not implement SnapshotStore")
	}
	return store.ListSnapshotHistory(taskID)
}

// IsTerminal reports whether s is a terminal task status (done, failed, or
// interrupted). A terminal task must not accept further Transition calls.
func IsTerminal(s hermes.TaskStatus) bool {
	switch s {
	case hermes.TaskStatusDone, hermes.TaskStatusFailed, hermes.TaskStatusInterrupted:
		return true
	default:
		return false
	}
}

// AllowedTransitions returns all statuses reachable from from according to
// the task state machine. Returns nil for terminal states.
func AllowedTransitions(from hermes.TaskStatus) []hermes.TaskStatus {
	candidates := []hermes.TaskStatus{
		hermes.TaskStatusPlanning,
		hermes.TaskStatusExecuting,
		hermes.TaskStatusDone,
		hermes.TaskStatusFailed,
		hermes.TaskStatusInterrupted,
	}
	var allowed []hermes.TaskStatus
	for _, to := range candidates {
		if to != from && hermes.ValidTaskStatusTransition(from, to) {
			allowed = append(allowed, to)
		}
	}
	return allowed
}
