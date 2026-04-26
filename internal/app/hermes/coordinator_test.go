package hermes

import (
	"context"
	"testing"
	"time"
)

func TestCoordinatorExecutorSessionSeededFromConfig(t *testing.T) {
	var receivedSessionIDs []string
	callFn := func(ctx context.Context, prompt, projectDir, sessionID string, onContent func(string)) (string, string, int, int, string, error) {
		receivedSessionIDs = append(receivedSessionIDs, sessionID)
		return "result", "thread-seeded", 5, 3, "", nil
	}

	coord := NewCoordinator(
		CoordinatorConfig{
			ProjectDir:           "/proj",
			ChatID:               42,
			ExecutorModel:        "test-model",
			MaxRetriesPerSubtask: 1,
			ExecutorSessionID:    "seed-thread-xyz",
		},
		func(context.Context, string, string) (string, string, int, int, error) {
			return "[]", "planner-thread", 0, 0, nil
		},
		callFn,
		nil,
		&NoopTaskStore{},
		&NoopProgressReporter{},
		nil,
	)

	tasks := []SubTask{{ID: "s1", Description: "seeded sub-task"}}
	state := TaskState{
		ID:   "task-seeded",
		Goal: "test seeding executor session from config",
		Plan: tasks,
	}

	// Simulate Start() seeding — set executorSessionID from config
	coord.mu.Lock()
	coord.executorSessionID = coord.cfg.ExecutorSessionID
	coord.mu.Unlock()

	oc := coord.runOneSubTask(context.Background(), state.ID, 0, 1, tasks, state)
	if oc.execErr != nil {
		t.Fatalf("sub-task failed: %v", oc.execErr)
	}
	if got := receivedSessionIDs[0]; got != "seed-thread-xyz" {
		t.Fatalf("first sub-task should receive seeded session %q, got %q", "seed-thread-xyz", got)
	}
}

func TestCoordinatorExecutorSessionInFinalState(t *testing.T) {
	callFn := func(ctx context.Context, prompt, projectDir, sessionID string, onContent func(string)) (string, string, int, int, string, error) {
		return "✅ done", "final-thread-id", 10, 5, "", nil
	}
	planFn := func(ctx context.Context, goal, projectDir string) (string, string, int, int, error) {
		return `[{"id":"s1","description":"simple task","tool_hints":[],"status":"pending"}]`, "planner-sid", 0, 0, nil
	}

	store := newTestStore(t)

	var capturedState TaskState
	coord := NewCoordinator(
		CoordinatorConfig{
			ProjectDir:           "/proj",
			ChatID:               1,
			ExecutorModel:        "test-model",
			MaxRetriesPerSubtask: 1,
			Budget:               TokenBudget{MaxTotalTokens: 100000},
			OnDone: func(_ context.Context, state TaskState) {
				capturedState = state
			},
		},
		planFn,
		callFn,
		nil,
		store,
		&NoopProgressReporter{},
		nil,
	)

	taskID, err := coord.Start(context.Background(), "verify executor session in OnDone — complex goal with implementation steps")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = taskID

	// Wait up to 5s for the task to finish
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !coord.IsRunning() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if coord.IsRunning() {
		t.Fatal("coordinator still running after 5s")
	}
	if capturedState.ExecutorSessionID != "final-thread-id" {
		t.Fatalf("OnDone state.ExecutorSessionID = %q, want %q", capturedState.ExecutorSessionID, "final-thread-id")
	}
}

func TestCoordinatorExecutorSessionCarriesBetweenSubTasks(t *testing.T) {
	var receivedSessionIDs []string
	callFn := func(ctx context.Context, prompt, projectDir, sessionID string, onContent func(string)) (string, string, int, int, string, error) {
		receivedSessionIDs = append(receivedSessionIDs, sessionID)
		switch len(receivedSessionIDs) {
		case 1:
			return "first result", "thread-a", 11, 7, "", nil
		case 2:
			return "second result", "thread-b", 13, 5, "", nil
		default:
			t.Fatalf("unexpected executor call %d", len(receivedSessionIDs))
			return "", "", 0, 0, "", nil
		}
	}

	coord := NewCoordinator(
		CoordinatorConfig{
			ProjectDir:           "/proj",
			ChatID:               42,
			ExecutorModel:        "test-model",
			MaxRetriesPerSubtask: 1,
		},
		func(context.Context, string, string) (string, string, int, int, error) {
			return "[]", "planner-thread", 0, 0, nil
		},
		callFn,
		nil,
		&NoopTaskStore{},
		&NoopProgressReporter{},
		nil,
	)

	tasks := []SubTask{
		{ID: "s1", Description: "第一個子任務"},
		{ID: "s2", Description: "第二個子任務"},
	}
	state := TaskState{
		ID:   "task-1",
		Goal: "驗證 executor session 傳遞",
		Plan: tasks,
	}

	first := coord.runOneSubTask(context.Background(), state.ID, 0, len(tasks), tasks, state)
	if first.execErr != nil {
		t.Fatalf("first sub-task failed: %v", first.execErr)
	}
	if got := receivedSessionIDs[0]; got != "" {
		t.Fatalf("first sub-task should start with empty sessionID, got %q", got)
	}
	if got := coord.executorSessionID; got != "thread-a" {
		t.Fatalf("executor session should update to thread-a, got %q", got)
	}

	second := coord.runOneSubTask(context.Background(), state.ID, 1, len(tasks), tasks, state)
	if second.execErr != nil {
		t.Fatalf("second sub-task failed: %v", second.execErr)
	}
	if got := receivedSessionIDs[1]; got != "thread-a" {
		t.Fatalf("second sub-task should reuse thread-a, got %q", got)
	}
	if got := coord.executorSessionID; got != "thread-b" {
		t.Fatalf("executor session should advance to thread-b, got %q", got)
	}
}
