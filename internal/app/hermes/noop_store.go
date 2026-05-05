package hermes

import "time"

// NoopTaskStore implements TaskStateStore as a no-op.
// Used when the database is not available during development or testing.
type NoopTaskStore struct{}

func (n *NoopTaskStore) CreateTask(task TaskState) (TaskState, error) {
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
		task.UpdatedAt = time.Now()
	}
	return task, nil
}

func (n *NoopTaskStore) StorePlan(taskID string, plan []SubTask) error { return nil }
func (n *NoopTaskStore) GetTask(id string) (TaskState, error)          { return TaskState{}, ErrNoTask }
func (n *NoopTaskStore) GetActiveTaskForChat(chatID int64) (TaskState, error) {
	return TaskState{}, ErrNoTask
}
func (n *NoopTaskStore) UpdateSubTask(taskID string, idx int, status SubTaskStatus, result string, tokensUsed int) error {
	return nil
}
func (n *NoopTaskStore) MarkSubTaskStarted(taskID string, idx int) error                 { return nil }
func (n *NoopTaskStore) AdvanceTask(taskID string, nextIdx int, status TaskStatus) error { return nil }
func (n *NoopTaskStore) AppendArtifact(taskID string, artifact Artifact) error           { return nil }
func (n *NoopTaskStore) UpdateAccumulated(taskID string, accumulated string) error       { return nil }
func (n *NoopTaskStore) UpdatePlannerSession(taskID string, sessionID string) error      { return nil }
func (n *NoopTaskStore) MarkInterrupted(taskID string, messageID int64) error            { return nil }
func (n *NoopTaskStore) MarkStatus(taskID string, status TaskStatus) error               { return nil }
func (n *NoopTaskStore) AddTokenUsage(taskID string, delta int) error                    { return nil }
func (n *NoopTaskStore) AddModelUsage(taskID, model string, inputTokens, outputTokens int, costUSD float64) error {
	return nil
}
func (n *NoopTaskStore) AddModelUsageBreakdown(taskID, model string, usage TokenUsageBreakdown) error {
	return nil
}
func (n *NoopTaskStore) AddPhaseUsage(taskID, phase, model string, inputTokens, outputTokens int, costUSD float64) error {
	return nil
}
func (n *NoopTaskStore) AddPhaseUsageBreakdown(taskID, phase, model string, usage TokenUsageBreakdown) error {
	return nil
}
func (n *NoopTaskStore) ListTasksForChat(chatID int64, limit int) ([]TaskState, error) {
	return nil, nil
}
func (n *NoopTaskStore) ResetBudgetStartedAt(taskID string, t time.Time) error { return nil }
