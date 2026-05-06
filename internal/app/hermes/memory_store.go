package hermes

import (
	"fmt"
	"sync"
	"time"
)

// MemoryTaskStore is an in-process TaskStateStore used by tests and transient
// single-run engines that do not need SQLite persistence.
type MemoryTaskStore struct {
	mu        sync.Mutex
	tasks     map[string]TaskState
	snapshots map[string][]Snapshot
}

func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{
		tasks:     make(map[string]TaskState),
		snapshots: make(map[string][]Snapshot),
	}
}

func (s *MemoryTaskStore) CreateTask(task TaskState) (TaskState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task.ID == "" {
		return task, fmt.Errorf("task id is required")
	}
	now := time.Now()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	s.tasks[task.ID] = cloneTaskState(task)
	return task, nil
}

func (s *MemoryTaskStore) GetTask(id string) (TaskState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return TaskState{}, ErrNoTask
	}
	return cloneTaskState(task), nil
}

func (s *MemoryTaskStore) GetActiveTaskForChat(chatID int64) (TaskState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newest TaskState
	found := false
	for _, task := range s.tasks {
		if task.ChatID != chatID || task.IsTerminal() {
			continue
		}
		if !found || task.CreatedAt.After(newest.CreatedAt) {
			newest = task
			found = true
		}
	}
	if !found {
		return TaskState{}, ErrNoTask
	}
	return cloneTaskState(newest), nil
}

func (s *MemoryTaskStore) StorePlan(taskID string, plan []SubTask) error {
	return s.update(taskID, func(task *TaskState) {
		task.Plan = append([]SubTask(nil), plan...)
	})
}

func (s *MemoryTaskStore) UpdateSubTask(taskID string, idx int, status SubTaskStatus, result string, tokensUsed int) error {
	return s.updateErr(taskID, func(task *TaskState) error {
		if idx < 0 || idx >= len(task.Plan) {
			return fmt.Errorf("sub-task index %d out of range", idx)
		}
		task.Plan[idx].Status = status
		task.Plan[idx].Result = result
		task.Plan[idx].TokensUsed += tokensUsed
		task.Plan[idx].Attempts++
		return nil
	})
}

func (s *MemoryTaskStore) MarkSubTaskStarted(taskID string, idx int) error {
	return s.updateErr(taskID, func(task *TaskState) error {
		if idx < 0 || idx >= len(task.Plan) {
			return fmt.Errorf("sub-task index %d out of range", idx)
		}
		task.CurrentIdx = idx
		task.Plan[idx].Status = SubTaskInProgress
		return nil
	})
}

func (s *MemoryTaskStore) AdvanceTask(taskID string, nextIdx int, status TaskStatus) error {
	return s.updateErr(taskID, func(task *TaskState) error {
		if err := ValidateTaskStatusTransition(taskID, task.Status, status); err != nil {
			return err
		}
		task.CurrentIdx = nextIdx
		task.Status = status
		return nil
	})
}

func (s *MemoryTaskStore) AppendArtifact(taskID string, artifact Artifact) error {
	return s.update(taskID, func(task *TaskState) {
		task.Artifacts = append(task.Artifacts, artifact)
	})
}

func (s *MemoryTaskStore) UpdateAccumulated(taskID string, accumulated string) error {
	return s.update(taskID, func(task *TaskState) {
		task.Accumulated = accumulated
	})
}

func (s *MemoryTaskStore) UpdatePlannerSession(taskID string, sessionID string) error {
	return s.update(taskID, func(task *TaskState) {
		task.PlannerSessionID = sessionID
	})
}

func (s *MemoryTaskStore) MarkInterrupted(taskID string, messageID int64) error {
	return s.updateErr(taskID, func(task *TaskState) error {
		if err := ValidateTaskStatusTransition(taskID, task.Status, TaskStatusInterrupted); err != nil {
			return err
		}
		task.Status = TaskStatusInterrupted
		task.InterruptedBy = &messageID
		return nil
	})
}

func (s *MemoryTaskStore) MarkStatus(taskID string, status TaskStatus) error {
	return s.updateErr(taskID, func(task *TaskState) error {
		if err := ValidateTaskStatusTransition(taskID, task.Status, status); err != nil {
			return err
		}
		task.Status = status
		return nil
	})
}

func (s *MemoryTaskStore) ResetBudgetStartedAt(taskID string, t time.Time) error {
	return s.update(taskID, func(task *TaskState) {
		task.TokenBudget.StartedAt = t
	})
}

func (s *MemoryTaskStore) AddTokenUsage(taskID string, delta int) error {
	return s.update(taskID, func(task *TaskState) {
		task.TokenBudget.UsedTokens += delta
	})
}

func (s *MemoryTaskStore) AddModelUsage(taskID, model string, inputTokens, outputTokens int, costUSD float64) error {
	return s.update(taskID, func(task *TaskState) {
		task.AddUsage(model, inputTokens, outputTokens, costUSD)
	})
}

func (s *MemoryTaskStore) AddModelUsageBreakdown(taskID, model string, usage TokenUsageBreakdown) error {
	return s.update(taskID, func(task *TaskState) {
		task.AddUsageBreakdown(model, usage)
	})
}

func (s *MemoryTaskStore) AddPhaseUsage(taskID, phase, model string, inputTokens, outputTokens int, costUSD float64) error {
	return s.update(taskID, func(task *TaskState) {
		task.AddPhaseUsage(phase, model, inputTokens, outputTokens, costUSD)
	})
}

func (s *MemoryTaskStore) AddPhaseUsageBreakdown(taskID, phase, model string, usage TokenUsageBreakdown) error {
	return s.update(taskID, func(task *TaskState) {
		task.AddPhaseUsageBreakdown(phase, model, usage)
	})
}

func (s *MemoryTaskStore) ListTasksForChat(chatID int64, limit int) ([]TaskState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []TaskState
	for _, task := range s.tasks {
		if task.ChatID == chatID {
			out = append(out, cloneTaskState(task))
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryTaskStore) CreateSnapshot(snapshot Snapshot) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.TaskID == "" {
		return snapshot, fmt.Errorf("snapshot task_id is required")
	}
	if snapshot.ID == "" {
		snapshot.ID = fmt.Sprintf("%s:%d", snapshot.TaskID, len(s.snapshots[snapshot.TaskID])+1)
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now()
	}
	if snapshot.ChannelVersions == nil {
		snapshot.ChannelVersions = map[string]int64{}
	}
	for _, existing := range s.snapshots[snapshot.TaskID] {
		if existing.Step == snapshot.Step || existing.ID == snapshot.ID {
			return snapshot, fmt.Errorf("snapshot duplicate id or step")
		}
	}
	s.snapshots[snapshot.TaskID] = append(s.snapshots[snapshot.TaskID], snapshot)
	return snapshot, nil
}

func (s *MemoryTaskStore) GetLatestSnapshot(taskID string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.snapshots[taskID]
	if len(history) == 0 {
		return Snapshot{}, ErrNoTask
	}
	return history[len(history)-1], nil
}

func (s *MemoryTaskStore) GetLatestSnapshotForThread(chatID int64, threadID int) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest Snapshot
	found := false
	for _, history := range s.snapshots {
		for _, snapshot := range history {
			if snapshot.ChatID != chatID || snapshot.ThreadID != threadID {
				continue
			}
			if !found || snapshot.CreatedAt.After(latest.CreatedAt) {
				latest = snapshot
				found = true
			}
		}
	}
	if !found {
		return Snapshot{}, ErrNoTask
	}
	return latest, nil
}

func (s *MemoryTaskStore) ListSnapshotHistory(taskID string) ([]Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.snapshots[taskID]
	if len(history) == 0 {
		return nil, ErrNoTask
	}
	return append([]Snapshot(nil), history...), nil
}

func (s *MemoryTaskStore) CommitRuntimeStep(commit RuntimeCommit) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[commit.TaskID]
	if !ok {
		return Snapshot{}, ErrNoTask
	}
	nextState, err := ApplyStateUpdates(HermesStateFromTaskState(task), commit.Updates)
	if err != nil {
		return Snapshot{}, err
	}
	if commit.CreatedAt.IsZero() {
		commit.CreatedAt = time.Now()
	}
	if commit.ChannelVersions == nil {
		commit.ChannelVersions = map[string]int64{}
	}
	history := s.snapshots[commit.TaskID]
	step := len(history) + 1
	parentID := commit.ParentSnapshotID
	if parentID == "" && len(history) > 0 {
		parentID = history[len(history)-1].ID
	}
	snapshotID := commit.SnapshotID
	if snapshotID == "" {
		snapshotID = fmt.Sprintf("%s:%d", commit.TaskID, step)
	}
	for _, existing := range history {
		if existing.ID == snapshotID || existing.Step == step {
			return Snapshot{}, fmt.Errorf("snapshot duplicate id or step")
		}
	}
	snapshot := Snapshot{
		ID:               snapshotID,
		TaskID:           commit.TaskID,
		ChatID:           nextState.ChatID,
		ThreadID:         nextState.ThreadID,
		Step:             step,
		State:            nextState,
		NextStep:         commit.NextStep,
		SourceNode:       commit.SourceNode,
		Writes:           collapseStateUpdates(commit.Updates),
		Metadata:         commit.Metadata,
		ParentSnapshotID: parentID,
		ChannelVersions:  commit.ChannelVersions,
		CreatedAt:        commit.CreatedAt,
	}
	task = hermesStateToTaskState(task, nextState)
	task.UpdatedAt = commit.CreatedAt
	s.tasks[commit.TaskID] = task
	s.snapshots[commit.TaskID] = append(history, snapshot)
	return snapshot, nil
}

func (s *MemoryTaskStore) update(taskID string, fn func(*TaskState)) error {
	return s.updateErr(taskID, func(task *TaskState) error {
		fn(task)
		return nil
	})
}

func (s *MemoryTaskStore) updateErr(taskID string, fn func(*TaskState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrNoTask
	}
	if err := fn(&task); err != nil {
		return err
	}
	task.UpdatedAt = time.Now()
	s.tasks[taskID] = task
	return nil
}

func cloneTaskState(task TaskState) TaskState {
	task.Plan = append([]SubTask(nil), task.Plan...)
	task.Artifacts = append([]Artifact(nil), task.Artifacts...)
	task.ModelUsages = append([]ModelUsage(nil), task.ModelUsages...)
	task.PhaseUsages = append([]PhaseUsage(nil), task.PhaseUsages...)
	return task
}
