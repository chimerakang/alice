package task_test

import (
	"database/sql"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
	"claude-tg-agent/internal/app/task"

	_ "modernc.org/sqlite"
)

// openIntegrationDB opens a per-test in-memory SQLite DB with MaxOpenConns(1).
func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&mode=memory")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func newIntegrationService(t *testing.T) (*task.Service, *hermes.SQLiteTaskStore) {
	t.Helper()
	db := openIntegrationDB(t)
	store, err := hermes.NewSQLiteTaskStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore: %v", err)
	}
	return task.New(store), store
}

func makeIntegrationTask(id string, chatID int64) hermes.TaskState {
	return hermes.TaskState{
		ID:          id,
		ChatID:      chatID,
		ProjectDir:  "/repo",
		Goal:        "integration test goal",
		Plan:        []hermes.SubTask{{ID: "s1", Description: "step 1", Status: hermes.SubTaskPending}},
		Status:      hermes.TaskStatusPlanning,
		TokenBudget: hermes.TokenBudget{MaxTotalTokens: 50_000, StartedAt: time.Now()},
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []hermes.TaskStatus{
		hermes.TaskStatusDone,
		hermes.TaskStatusFailed,
		hermes.TaskStatusInterrupted,
	}
	for _, s := range terminal {
		if !task.IsTerminal(s) {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	nonTerminal := []hermes.TaskStatus{
		hermes.TaskStatusPlanning,
		hermes.TaskStatusExecuting,
	}
	for _, s := range nonTerminal {
		if task.IsTerminal(s) {
			t.Errorf("expected %q to be non-terminal", s)
		}
	}
}

func TestAllowedTransitions_planning(t *testing.T) {
	got := task.AllowedTransitions(hermes.TaskStatusPlanning)
	want := map[hermes.TaskStatus]bool{
		hermes.TaskStatusExecuting:   true,
		hermes.TaskStatusDone:        true,
		hermes.TaskStatusFailed:      true,
		hermes.TaskStatusInterrupted: true,
	}
	if len(got) != len(want) {
		t.Fatalf("planning: got %v, want keys of %v", got, want)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("planning: unexpected allowed transition to %q", s)
		}
	}
}

func TestAllowedTransitions_executing(t *testing.T) {
	got := task.AllowedTransitions(hermes.TaskStatusExecuting)
	want := map[hermes.TaskStatus]bool{
		hermes.TaskStatusDone:        true,
		hermes.TaskStatusFailed:      true,
		hermes.TaskStatusInterrupted: true,
	}
	if len(got) != len(want) {
		t.Fatalf("executing: got %v, want keys of %v", got, want)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("executing: unexpected allowed transition to %q", s)
		}
	}
}

func TestAllowedTransitions_terminal(t *testing.T) {
	for _, s := range []hermes.TaskStatus{
		hermes.TaskStatusDone,
		hermes.TaskStatusFailed,
		hermes.TaskStatusInterrupted,
	} {
		if got := task.AllowedTransitions(s); len(got) != 0 {
			t.Errorf("%q is terminal but AllowedTransitions returned %v", s, got)
		}
	}
}

// stubStore is a minimal hermes.TaskStateStore for Service unit tests.
type stubStore struct {
	task         hermes.TaskState
	markStatusFn func(id string, s hermes.TaskStatus) error
}

func (s *stubStore) CreateTask(task hermes.TaskState) (hermes.TaskState, error) {
	return task, nil
}
func (s *stubStore) GetTask(id string) (hermes.TaskState, error) { return s.task, nil }
func (s *stubStore) GetActiveTaskForChat(chatID int64) (hermes.TaskState, error) {
	return s.task, nil
}
func (s *stubStore) StorePlan(taskID string, plan []hermes.SubTask) error { return nil }
func (s *stubStore) UpdateSubTask(taskID string, idx int, status hermes.SubTaskStatus, result string, tokensUsed int) error {
	return nil
}
func (s *stubStore) MarkSubTaskStarted(taskID string, idx int) error { return nil }
func (s *stubStore) AdvanceTask(taskID string, nextIdx int, status hermes.TaskStatus) error {
	return nil
}
func (s *stubStore) AppendArtifact(taskID string, artifact hermes.Artifact) error { return nil }
func (s *stubStore) UpdateAccumulated(taskID string, accumulated string) error    { return nil }
func (s *stubStore) UpdatePlannerSession(taskID string, sessionID string) error   { return nil }
func (s *stubStore) MarkInterrupted(taskID string, messageID int64) error         { return nil }
func (s *stubStore) MarkStatus(id string, status hermes.TaskStatus) error {
	if s.markStatusFn != nil {
		return s.markStatusFn(id, status)
	}
	return nil
}
func (s *stubStore) ResetBudgetStartedAt(taskID string, t time.Time) error { return nil }
func (s *stubStore) AddTokenUsage(taskID string, delta int) error          { return nil }
func (s *stubStore) AddModelUsage(taskID, model string, in, out int, costUSD float64) error {
	return nil
}
func (s *stubStore) AddModelUsageBreakdown(taskID, model string, usage hermes.TokenUsageBreakdown) error {
	return nil
}
func (s *stubStore) AddPhaseUsage(taskID, phase, model string, in, out int, costUSD float64) error {
	return nil
}
func (s *stubStore) AddPhaseUsageBreakdown(taskID, phase, model string, usage hermes.TokenUsageBreakdown) error {
	return nil
}
func (s *stubStore) ListTasksForChat(chatID int64, limit int) ([]hermes.TaskState, error) {
	return []hermes.TaskState{s.task}, nil
}

func TestService_Transition_valid(t *testing.T) {
	called := false
	store := &stubStore{
		task: hermes.TaskState{ID: "t1", Status: hermes.TaskStatusPlanning},
		markStatusFn: func(id string, s hermes.TaskStatus) error {
			called = true
			return nil
		},
	}
	svc := task.New(store)
	if err := svc.Transition("t1", hermes.TaskStatusPlanning, hermes.TaskStatusExecuting); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected MarkStatus to be called")
	}
}

func TestService_Transition_invalid(t *testing.T) {
	store := &stubStore{task: hermes.TaskState{ID: "t1", Status: hermes.TaskStatusDone}}
	svc := task.New(store)
	if err := svc.Transition("t1", hermes.TaskStatusDone, hermes.TaskStatusExecuting); err == nil {
		t.Error("expected error for terminal → executing transition")
	}
}

// ── Integration tests: real SQLiteTaskStore under SetMaxOpenConns(1) ──────────

func TestService_Integration_Transition_PlanningToExecuting(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-int-1", 100)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := svc.Transition("t-int-1", hermes.TaskStatusPlanning, hermes.TaskStatusExecuting); err != nil {
		t.Fatalf("Transition planning→executing: %v", err)
	}

	got, err := svc.GetTask("t-int-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != hermes.TaskStatusExecuting {
		t.Errorf("status: got %q, want %q", got.Status, hermes.TaskStatusExecuting)
	}
}

func TestService_Integration_Transition_Invalid_Rejected(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-int-2", 100)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// done → executing is not a valid transition (terminal states are immutable)
	if err := svc.Transition("t-int-2", hermes.TaskStatusDone, hermes.TaskStatusExecuting); err == nil {
		t.Error("expected error for done→executing, got nil")
	}

	got, err := svc.GetTask("t-int-2")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != hermes.TaskStatusPlanning {
		t.Errorf("status should remain planning, got %q", got.Status)
	}
}

func TestService_Integration_FullLifecycle(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-int-3", 200)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	steps := []struct {
		from hermes.TaskStatus
		to   hermes.TaskStatus
	}{
		{hermes.TaskStatusPlanning, hermes.TaskStatusExecuting},
		{hermes.TaskStatusExecuting, hermes.TaskStatusDone},
	}
	for _, s := range steps {
		if err := svc.Transition("t-int-3", s.from, s.to); err != nil {
			t.Fatalf("Transition %s→%s: %v", s.from, s.to, err)
		}
	}

	got, err := svc.GetTask("t-int-3")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != hermes.TaskStatusDone {
		t.Errorf("final status: got %q, want done", got.Status)
	}
	if !task.IsTerminal(got.Status) {
		t.Error("done should be terminal")
	}
}

func TestService_UpdateSubTaskSnapshotWritesRuntimeSnapshot(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-snap-subtask", 100)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	executing := hermes.TaskStatusExecuting
	currentIdx := 0
	if _, err := store.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     ts.ID,
		Updates:    []hermes.StateUpdate{{Status: &executing, CurrentIdx: &currentIdx}},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepPlanner,
		Metadata:   hermes.SnapshotMetadata{Source: "test", Reason: "plan_ready"},
	}); err != nil {
		t.Fatalf("initial CommitRuntimeStep: %v", err)
	}

	if err := svc.UpdateSubTaskSnapshot(ts.ID, 0, hermes.SubTaskDone, "retry fixed result", 7, "retry_writeback"); err != nil {
		t.Fatalf("UpdateSubTaskSnapshot: %v", err)
	}

	got, err := svc.GetTask(ts.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Plan[0].Status != hermes.SubTaskDone || got.Plan[0].Result != "retry fixed result" || got.Plan[0].TokensUsed != 7 || got.Plan[0].Attempts != 1 {
		t.Fatalf("snapshot-backed subtask mismatch: %#v", got.Plan[0])
	}
	latest, err := store.GetLatestSnapshot(ts.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if latest.Metadata.Reason != "retry_writeback" {
		t.Fatalf("latest snapshot reason = %q, want retry_writeback", latest.Metadata.Reason)
	}
	if latest.Writes.Plan == nil || latest.Writes.Plan[0].Result != "retry fixed result" {
		t.Fatalf("latest snapshot writes missing retry result: %#v", latest.Writes.Plan)
	}
}

func TestService_DelegatesRuntimeStepStore(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-service-runtime", 100)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	runtime, ok := any(svc).(hermes.RuntimeStepStore)
	if !ok {
		t.Fatal("task.Service should implement hermes.RuntimeStepStore")
	}
	executing := hermes.TaskStatusExecuting
	if _, err := runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     ts.ID,
		Updates:    []hermes.StateUpdate{{Status: &executing}},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepPlanner,
		Metadata:   hermes.SnapshotMetadata{Source: "test", Reason: "service_runtime_delegate"},
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}
	latest, err := store.GetLatestSnapshot(ts.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if latest.Metadata.Reason != "service_runtime_delegate" {
		t.Fatalf("latest snapshot reason = %q", latest.Metadata.Reason)
	}
}

func TestService_Integration_GetActiveForChat(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-int-4", 300)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	active, err := svc.GetActiveForChat(300)
	if err != nil {
		t.Fatalf("GetActiveForChat: %v", err)
	}
	if active.ID != "t-int-4" {
		t.Errorf("active task ID: got %q, want t-int-4", active.ID)
	}
}

func TestService_Integration_ListForChat(t *testing.T) {
	svc, store := newIntegrationService(t)
	for _, id := range []string{"t-int-5a", "t-int-5b"} {
		ts := makeIntegrationTask(id, 400)
		if _, err := store.CreateTask(ts); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}

	tasks, err := svc.ListForChat(400, 10)
	if err != nil {
		t.Fatalf("ListForChat: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("ListForChat: got %d tasks, want 2", len(tasks))
	}
}

func TestService_Integration_TerminalRejectsTransition(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-int-6", 500)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Move to failed (terminal)
	if err := svc.Transition("t-int-6", hermes.TaskStatusPlanning, hermes.TaskStatusFailed); err != nil {
		t.Fatalf("Transition planning→failed: %v", err)
	}

	// Any further transition must be rejected
	if err := svc.Transition("t-int-6", hermes.TaskStatusFailed, hermes.TaskStatusExecuting); err == nil {
		t.Error("expected error transitioning out of terminal state failed")
	}
}

// TestService_Integration_ConcurrentTransition verifies that concurrent
// Transition calls on the same taskID do not corrupt state. SQLite's
// SetMaxOpenConns(1) serialises writes, so exactly one goroutine should
// succeed in moving planning→executing while the rest are either rejected
// by the state-machine guard or receive a store error. The final persisted
// status must be a valid post-planning state.
func TestService_Integration_ConcurrentTransition(t *testing.T) {
	svc, store := newIntegrationService(t)
	ts := makeIntegrationTask("t-conc-1", 600)
	if _, err := store.CreateTask(ts); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	const workers = 8
	errs := make([]error, workers)
	done := make(chan struct{})
	// All goroutines attempt the same planning→executing transition concurrently.
	for i := range workers {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			errs[idx] = svc.Transition("t-conc-1", hermes.TaskStatusPlanning, hermes.TaskStatusExecuting)
		}(i)
	}
	for range workers {
		<-done
	}

	successCount := 0
	for _, err := range errs {
		if err == nil {
			successCount++
		}
	}
	// Under SQLite single-connection serialisation at least one call must succeed.
	if successCount == 0 {
		t.Error("expected at least one goroutine to succeed, got zero")
	}

	// Final persisted status must be a valid reachable state from planning.
	got, err := svc.GetTask("t-conc-1")
	if err != nil {
		t.Fatalf("GetTask after concurrent transitions: %v", err)
	}
	allowed := map[hermes.TaskStatus]bool{
		hermes.TaskStatusExecuting: true,
		// planning→done/failed/interrupted are also legal but none of our
		// concurrent callers requested those.
	}
	// Under SQLite serialisation, at least one planning→executing succeeded,
	// so the final persisted status must be executing — not planning.
	if got.Status != hermes.TaskStatusExecuting {
		t.Errorf("expected final status %q, got %q", hermes.TaskStatusExecuting, got.Status)
	}
	_ = allowed
}

// ── LatestSnapshot accessor (#169 β1) ──────────────────────────────────────

func TestService_LatestSnapshot_ReturnsInterruptOnPause(t *testing.T) {
	svc, store := newIntegrationService(t)
	created, err := svc.CreateTask(makeIntegrationTask("task-snap-pause", 200))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	executing := hermes.TaskStatusExecuting
	idx := 0
	interrupt := &hermes.HermesInterrupt{
		ID:         "iv-test",
		Reason:     "subtask_failure_pause",
		SourceStep: hermes.RuntimeStepExecutor,
		ResumeStep: hermes.RuntimeStepExecutor,
		CreatedAt:  time.Now(),
	}
	if _, err := store.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     created.ID,
		Updates:    []hermes.StateUpdate{{Status: &executing, CurrentIdx: &idx, Interrupt: interrupt}},
		NextStep:   hermes.RuntimeStepApproval,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata:   hermes.SnapshotMetadata{Source: "test", Reason: "subtask_failure_pause"},
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}

	snap, ok := svc.LatestSnapshot(created.ID)
	if !ok {
		t.Fatal("LatestSnapshot returned ok=false; expected snapshot store available")
	}
	if snap.State.Interrupt == nil || snap.State.Interrupt.ID != "iv-test" {
		t.Errorf("interrupt mismatch: %+v", snap.State.Interrupt)
	}
}

func TestService_LatestSnapshot_NoSnapshotForFreshTask(t *testing.T) {
	svc, _ := newIntegrationService(t)
	created, err := svc.CreateTask(makeIntegrationTask("task-fresh", 201))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_, ok := svc.LatestSnapshot(created.ID)
	if ok {
		t.Errorf("LatestSnapshot returned ok=true for fresh task without commits")
	}
}
