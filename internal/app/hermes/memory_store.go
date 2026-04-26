package hermes

import (
	"fmt"
	"sync"
	"time"
)

// MemoryTaskStore is an in-process TaskStateStore used by tests and transient
// single-run engines that do not need SQLite persistence.
type MemoryTaskStore struct {
	mu    sync.Mutex
	tasks map[string]TaskState
}

func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{tasks: make(map[string]TaskState)}
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

func (s *MemoryTaskStore) AdvanceTask(taskID string, nextIdx int, status TaskStatus) error {
	return s.update(taskID, func(task *TaskState) {
		task.CurrentIdx = nextIdx
		task.Status = status
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
	return s.update(taskID, func(task *TaskState) {
		task.Status = TaskStatusInterrupted
		task.InterruptedBy = &messageID
	})
}

func (s *MemoryTaskStore) MarkStatus(taskID string, status TaskStatus) error {
	return s.update(taskID, func(task *TaskState) {
		task.Status = status
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

func (s *MemoryTaskStore) AddModelUsage(taskID, model string, inputTokens, outputTokens int) error {
	return s.update(taskID, func(task *TaskState) {
		task.AddUsage(model, inputTokens, outputTokens)
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
	return task
}
