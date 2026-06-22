package hermes

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openTestDB opens an in-memory SQLite database for testing.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&mode=memory")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStore(t *testing.T) *SQLiteTaskStore {
	t.Helper()
	db := openTestDB(t)
	store, err := NewSQLiteTaskStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore: %v", err)
	}
	return store
}

func makeTask(id string, chatID int64) TaskState {
	return TaskState{
		ID:         id,
		ChatID:     chatID,
		ProjectDir: "/repo",
		Goal:       "refactor auth module",
		Plan: []SubTask{
			{ID: "s1", Description: "read auth.go", Status: SubTaskPending},
			{ID: "s2", Description: "write tests", Status: SubTaskPending},
		},
		Status: TaskStatusPlanning,
		TokenBudget: TokenBudget{
			MaxTotalTokens: 100_000,
			StartedAt:      time.Now(),
		},
	}
}

// ── CreateTask / GetTask ───────────────────────────────────────────────────────

func TestCreateAndGetTask(t *testing.T) {
	store := newTestStore(t)
	orig := makeTask("task-1", 42)
	orig.ThreadID = 7

	created, err := store.CreateTask(orig)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	got, err := store.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != "task-1" || got.ChatID != 42 || got.ThreadID != 7 {
		t.Errorf("unexpected task: %+v", got)
	}
	if got.Goal != orig.Goal {
		t.Errorf("goal mismatch: %q", got.Goal)
	}
	if got.ProjectDir != orig.ProjectDir {
		t.Errorf("project dir mismatch: %q", got.ProjectDir)
	}
	if len(got.Plan) != 2 {
		t.Errorf("plan length: got %d, want 2", len(got.Plan))
	}
}

func TestGetTask_NotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.GetTask("nonexistent")
	if err != ErrNoTask {
		t.Errorf("expected ErrNoTask, got %v", err)
	}
}

// ── Snapshots ────────────────────────────────────────────────────────────────

func TestSQLiteTaskStoreSnapshotSchema(t *testing.T) {
	store := newTestStore(t)

	rows, err := store.db.Query(`PRAGMA table_info(hermes_snapshots)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[name] = true
	}
	for _, name := range []string{
		"id", "task_id", "chat_id", "thread_id", "step", "state_json",
		"next_step", "source_node", "writes_json", "metadata_json",
		"parent_snapshot_id", "channel_versions_json", "created_at",
	} {
		if !columns[name] {
			t.Fatalf("hermes_snapshots missing column %q", name)
		}
	}

	var uniqueIndexes int
	if err := store.db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_hermes_snapshots_task_step'`).
		Scan(&uniqueIndexes); err != nil {
		t.Fatalf("count unique index: %v", err)
	}
	if uniqueIndexes != 1 {
		t.Fatalf("idx_hermes_snapshots_task_step count = %d, want 1", uniqueIndexes)
	}
}

func TestCreateAndReadSnapshotHistory(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-snapshot", 42))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	task.ThreadID = 7
	state := HermesStateFromTaskState(task)
	status := TaskStatusExecuting

	first, err := store.CreateSnapshot(Snapshot{
		TaskID:   task.ID,
		ChatID:   task.ChatID,
		ThreadID: task.ThreadID,
		Step:     1,
		State:    state,
		NextStep: RuntimeStepExecutor,
		Writes: StateUpdate{
			Status: &status,
		},
		Metadata: SnapshotMetadata{
			Source: "test",
			Reason: "plan_ready",
		},
		ChannelVersions: map[string]int64{"status": 1},
	})
	if err != nil {
		t.Fatalf("CreateSnapshot first: %v", err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() {
		t.Fatalf("snapshot identity not populated: %+v", first)
	}

	latestState := state
	latestState.Status = TaskStatusExecuting
	second, err := store.CreateSnapshot(Snapshot{
		TaskID:           task.ID,
		ChatID:           task.ChatID,
		ThreadID:         task.ThreadID,
		Step:             2,
		State:            latestState,
		NextStep:         RuntimeStepReviewer,
		SourceNode:       RuntimeStepExecutor,
		ParentSnapshotID: first.ID,
	})
	if err != nil {
		t.Fatalf("CreateSnapshot second: %v", err)
	}

	latest, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if latest.ID != second.ID || latest.Step != 2 || latest.NextStep != RuntimeStepReviewer {
		t.Fatalf("latest = %+v, want second snapshot", latest)
	}
	if latest.ChannelVersions == nil || len(latest.ChannelVersions) != 0 {
		t.Fatalf("nil channel versions should round-trip as empty map: %+v", latest.ChannelVersions)
	}

	byThread, err := store.GetLatestSnapshotForThread(42, 7)
	if err != nil {
		t.Fatalf("GetLatestSnapshotForThread: %v", err)
	}
	if byThread.ID != second.ID {
		t.Fatalf("latest by thread = %s, want %s", byThread.ID, second.ID)
	}

	history, err := store.ListSnapshotHistory(task.ID)
	if err != nil {
		t.Fatalf("ListSnapshotHistory: %v", err)
	}
	if len(history) != 2 || history[0].Step != 1 || history[1].Step != 2 {
		t.Fatalf("history steps = %+v, want [1 2]", history)
	}
	if history[0].Writes.Status == nil || *history[0].Writes.Status != TaskStatusExecuting {
		t.Fatalf("writes did not round-trip: %+v", history[0].Writes)
	}
	if history[0].Metadata.Source != "test" || history[0].ChannelVersions["status"] != 1 {
		t.Fatalf("metadata/channel versions did not round-trip: %+v %+v", history[0].Metadata, history[0].ChannelVersions)
	}
}

func TestCreateSnapshotRejectsDuplicateTaskStep(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-snapshot-dup", 42))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	snapshot := Snapshot{
		TaskID:   task.ID,
		ChatID:   task.ChatID,
		Step:     1,
		State:    HermesStateFromTaskState(task),
		NextStep: RuntimeStepExecutor,
	}
	if _, err := store.CreateSnapshot(snapshot); err != nil {
		t.Fatalf("CreateSnapshot first: %v", err)
	}
	if _, err := store.CreateSnapshot(snapshot); err == nil {
		t.Fatal("CreateSnapshot duplicate step error = nil, want unique constraint error")
	}
}

func TestSnapshotNotFound(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.GetLatestSnapshot("missing"); err != ErrNoTask {
		t.Fatalf("GetLatestSnapshot missing error = %v, want ErrNoTask", err)
	}
	if _, err := store.GetLatestSnapshotForThread(404, 0); err != ErrNoTask {
		t.Fatalf("GetLatestSnapshotForThread missing error = %v, want ErrNoTask", err)
	}
	if _, err := store.ListSnapshotHistory("missing"); err != ErrNoTask {
		t.Fatalf("ListSnapshotHistory missing error = %v, want ErrNoTask", err)
	}
}

func TestCommitRuntimeStepWritesLegacyAndSnapshot(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-runtime-commit", 42))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	status := TaskStatusExecuting
	currentIdx := 1
	plan := []SubTask{
		{ID: "s1", Description: "done", Status: SubTaskDone, Result: "ok", TokensUsed: 7, Attempts: 1},
	}
	accumulated := "ok"

	snapshot, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &status, CurrentIdx: &currentIdx, Plan: plan, Accumulated: &accumulated}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepExecutor,
		Metadata:   SnapshotMetadata{Source: "test", Reason: "subtask_done"},
	})
	if err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}
	if snapshot.Step != 1 || snapshot.NextStep != RuntimeStepExecutor || snapshot.Metadata.Reason != "subtask_done" {
		t.Fatalf("snapshot mismatch: %+v", snapshot)
	}

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusExecuting || got.CurrentIdx != 1 || got.Accumulated != "ok" {
		t.Fatalf("legacy task not updated atomically: %+v", got)
	}
	if len(got.Plan) != 1 || got.Plan[0].Status != SubTaskDone || got.Plan[0].TokensUsed != 7 {
		t.Fatalf("legacy plan not updated: %+v", got.Plan)
	}

	history, err := store.ListSnapshotHistory(task.ID)
	if err != nil {
		t.Fatalf("ListSnapshotHistory: %v", err)
	}
	if len(history) != 1 || history[0].State.Accumulated != "ok" {
		t.Fatalf("history mismatch: %+v", history)
	}
}

func TestCommitRuntimeStepSnapshotFailureRollsBackLegacy(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-runtime-rollback", 42))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	status := TaskStatusExecuting
	idxOne := 1
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		SnapshotID: "fixed-snapshot",
		Updates:    []StateUpdate{{Status: &status, CurrentIdx: &idxOne}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepPlanner,
	}); err != nil {
		t.Fatalf("CommitRuntimeStep first: %v", err)
	}

	idxTwo := 2
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		SnapshotID: "fixed-snapshot",
		Updates:    []StateUpdate{{CurrentIdx: &idxTwo}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepExecutor,
	}); err == nil {
		t.Fatal("CommitRuntimeStep duplicate snapshot id error = nil, want insert failure")
	}

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CurrentIdx != 1 {
		t.Fatalf("legacy current_idx advanced despite snapshot failure: got %d, want 1", got.CurrentIdx)
	}
	history, err := store.ListSnapshotHistory(task.ID)
	if err != nil {
		t.Fatalf("ListSnapshotHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
}

// ── GetActiveTaskForChat ───────────────────────────────────────────────────────

func TestGetActiveTaskForChat(t *testing.T) {
	store := newTestStore(t)
	task := makeTask("task-active", 99)
	if _, err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetActiveTaskForChat(99)
	if err != nil {
		t.Fatalf("GetActiveTaskForChat: %v", err)
	}
	if got.ID != "task-active" {
		t.Errorf("wrong task: %s", got.ID)
	}
}

func TestGetActiveTaskForChat_SkipsDoneTask(t *testing.T) {
	store := newTestStore(t)
	task := makeTask("task-done", 100)
	if _, err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkStatus("task-done", TaskStatusDone); err != nil {
		t.Fatal(err)
	}

	_, err := store.GetActiveTaskForChat(100)
	if err != ErrNoTask {
		t.Errorf("expected ErrNoTask for done task, got %v", err)
	}
}

// ── UpdateSubTask ──────────────────────────────────────────────────────────────

func TestUpdateSubTask(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-sub", 1)); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateSubTask("task-sub", 0, SubTaskDone, "read 200 lines", 500); err != nil {
		t.Fatalf("UpdateSubTask: %v", err)
	}

	got, _ := store.GetTask("task-sub")
	if got.Plan[0].Status != SubTaskDone {
		t.Errorf("status: got %s, want done", got.Plan[0].Status)
	}
	if got.Plan[0].Result != "read 200 lines" {
		t.Errorf("result: %q", got.Plan[0].Result)
	}
	if got.Plan[0].Attempts != 1 {
		t.Errorf("attempts: got %d, want 1", got.Plan[0].Attempts)
	}
	if got.Plan[0].TokensUsed != 500 {
		t.Errorf("tokens: got %d, want 500", got.Plan[0].TokensUsed)
	}
}

func TestUpdateSubTask_OutOfRange(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-oor", 1)); err != nil {
		t.Fatal(err)
	}
	err := store.UpdateSubTask("task-oor", 99, SubTaskDone, "", 0)
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

// ── AdvanceTask ────────────────────────────────────────────────────────────────

func TestAdvanceTask(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-adv", 2)); err != nil {
		t.Fatal(err)
	}

	if err := store.AdvanceTask("task-adv", 1, TaskStatusExecuting); err != nil {
		t.Fatalf("AdvanceTask: %v", err)
	}

	got, _ := store.GetTask("task-adv")
	if got.CurrentIdx != 1 {
		t.Errorf("CurrentIdx: got %d, want 1", got.CurrentIdx)
	}
	if got.Status != TaskStatusExecuting {
		t.Errorf("Status: got %s, want executing", got.Status)
	}
}

func TestSQLiteTaskStoreRejectsTerminalToActiveTransition(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-terminal", 2)); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.MarkStatus("task-terminal", TaskStatusDone); err != nil {
		t.Fatalf("MarkStatus(done): %v", err)
	}

	err := store.MarkStatus("task-terminal", TaskStatusExecuting)
	if err == nil || !strings.Contains(err.Error(), "invalid task status transition") {
		t.Fatalf("MarkStatus(done -> executing) error = %v", err)
	}

	got, err := store.GetTask("task-terminal")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusDone {
		t.Fatalf("status mutated after rejected transition: got %s, want %s", got.Status, TaskStatusDone)
	}
}

func TestMemoryTaskStoreRejectsTerminalToActiveTransition(t *testing.T) {
	store := NewMemoryTaskStore()
	if _, err := store.CreateTask(makeTask("task-memory-terminal", 2)); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.MarkStatus("task-memory-terminal", TaskStatusFailed); err != nil {
		t.Fatalf("MarkStatus(failed): %v", err)
	}

	err := store.AdvanceTask("task-memory-terminal", 1, TaskStatusExecuting)
	if err == nil || !strings.Contains(err.Error(), "invalid task status transition") {
		t.Fatalf("AdvanceTask(failed -> executing) error = %v", err)
	}

	got, err := store.GetTask("task-memory-terminal")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusFailed || got.CurrentIdx != 0 {
		t.Fatalf("task mutated after rejected transition: status=%s idx=%d", got.Status, got.CurrentIdx)
	}
}

// ── AppendArtifact ─────────────────────────────────────────────────────────────

func TestAppendArtifact(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-art", 3)); err != nil {
		t.Fatal(err)
	}

	art := Artifact{Path: "internal/auth/auth.go", Hash: "abc123", SubTaskID: "s1"}
	if err := store.AppendArtifact("task-art", art); err != nil {
		t.Fatalf("AppendArtifact: %v", err)
	}

	got, _ := store.GetTask("task-art")
	if len(got.Artifacts) != 1 {
		t.Fatalf("artifacts: got %d, want 1", len(got.Artifacts))
	}
	if got.Artifacts[0].Path != "internal/auth/auth.go" {
		t.Errorf("path: %q", got.Artifacts[0].Path)
	}
}

// ── UpdateAccumulated ──────────────────────────────────────────────────────────

func TestUpdateAccumulated(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-acc", 4)); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateAccumulated("task-acc", "step 1 done"); err != nil {
		t.Fatalf("UpdateAccumulated: %v", err)
	}

	got, _ := store.GetTask("task-acc")
	if got.Accumulated != "step 1 done" {
		t.Errorf("accumulated: %q", got.Accumulated)
	}
}

// ── MarkInterrupted ────────────────────────────────────────────────────────────

func TestMarkInterrupted(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-int", 5)); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkInterrupted("task-int", 9999); err != nil {
		t.Fatalf("MarkInterrupted: %v", err)
	}

	got, _ := store.GetTask("task-int")
	if got.Status != TaskStatusInterrupted {
		t.Errorf("status: got %s, want interrupted", got.Status)
	}
	if got.InterruptedBy == nil || *got.InterruptedBy != 9999 {
		t.Errorf("interrupted_by: %v", got.InterruptedBy)
	}
}

// ── AddTokenUsage ──────────────────────────────────────────────────────────────

func TestAddTokenUsage(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-tok", 6)); err != nil {
		t.Fatal(err)
	}

	if err := store.AddTokenUsage("task-tok", 1500); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}
	if err := store.AddTokenUsage("task-tok", 2000); err != nil {
		t.Fatalf("AddTokenUsage second call: %v", err)
	}

	got, _ := store.GetTask("task-tok")
	if got.TokenBudget.UsedTokens != 3500 {
		t.Errorf("used tokens: got %d, want 3500", got.TokenBudget.UsedTokens)
	}
}

func TestAddPhaseUsage(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-phase", 6)); err != nil {
		t.Fatal(err)
	}

	if err := store.AddPhaseUsage("task-phase", "planner", "claude-opus", 100, 10, 0.12); err != nil {
		t.Fatalf("AddPhaseUsage planner: %v", err)
	}
	if err := store.AddPhaseUsage("task-phase", "planner", "claude-opus", 50, 5, 0.06); err != nil {
		t.Fatalf("AddPhaseUsage planner second: %v", err)
	}
	if err := store.AddPhaseUsage("task-phase", "executor", "claude-sonnet", 200, 20, 0.03); err != nil {
		t.Fatalf("AddPhaseUsage executor: %v", err)
	}

	got, err := store.GetTask("task-phase")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.PhaseUsages) != 2 {
		t.Fatalf("phase usages = %d, want 2: %#v", len(got.PhaseUsages), got.PhaseUsages)
	}
	planner := got.PhaseUsages[0]
	if planner.Phase != "planner" || planner.Model != "claude-opus" || planner.InputTokens != 150 || planner.OutputTokens != 15 || planner.CallCount != 2 {
		t.Fatalf("unexpected planner phase usage: %#v", planner)
	}
}

func TestAddUsageBreakdown(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-breakdown", 6)); err != nil {
		t.Fatal(err)
	}

	usage := TokenUsageBreakdown{
		UncachedInputTokens:      10,
		CacheReadInputTokens:     100,
		CacheCreationInputTokens: 20,
		OutputTokens:             3,
		CostUSD:                  0.01,
	}
	if err := store.AddModelUsageBreakdown("task-breakdown", "claude-sonnet", usage); err != nil {
		t.Fatalf("AddModelUsageBreakdown: %v", err)
	}
	if err := store.AddPhaseUsageBreakdown("task-breakdown", "executor", "claude-sonnet", usage); err != nil {
		t.Fatalf("AddPhaseUsageBreakdown: %v", err)
	}

	got, err := store.GetTask("task-breakdown")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	model := got.ModelUsages[0]
	if model.InputTokens != 130 || model.UncachedInputTokens != 10 || model.CacheReadInputTokens != 100 || model.CacheCreationInputTokens != 20 || model.OutputTokens != 3 {
		t.Fatalf("model usage breakdown = %#v", model)
	}
	phase := got.PhaseUsages[0]
	if phase.InputTokens != 130 || phase.UncachedInputTokens != 10 || phase.CacheReadInputTokens != 100 || phase.CacheCreationInputTokens != 20 || phase.OutputTokens != 3 {
		t.Fatalf("phase usage breakdown = %#v", phase)
	}
}

// ── ListTasksForChat ───────────────────────────────────────────────────────────

func TestListTasksForChat(t *testing.T) {
	store := newTestStore(t)
	for _, id := range []string{"t1", "t2", "t3"} {
		task := makeTask(id, 77)
		if _, err := store.CreateTask(task); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := store.ListTasksForChat(77, 10)
	if err != nil {
		t.Fatalf("ListTasksForChat: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("got %d tasks, want 3", len(tasks))
	}
}

// ── TokenBudget.Exceeded ───────────────────────────────────────────────────────

func TestTokenBudget_Exceeded(t *testing.T) {
	b := TokenBudget{MaxTotalTokens: 1000, UsedTokens: 999}
	if b.Exceeded() {
		t.Error("999 < 1000 should not exceed")
	}
	b.UsedTokens = 1000
	if !b.Exceeded() {
		t.Error("1000 >= 1000 should exceed")
	}
}

func TestTokenBudget_WallclockExceeded(t *testing.T) {
	b := TokenBudget{
		MaxWallclockSeconds: 1,
		StartedAt:           time.Now().Add(-2 * time.Second),
	}
	if !b.Exceeded() {
		t.Error("elapsed > max should exceed")
	}
}

func TestTokenBudget_Unlimited(t *testing.T) {
	b := TokenBudget{UsedTokens: 999_999}
	if b.Exceeded() {
		t.Error("unlimited budget should never exceed")
	}
}

// ── AppendResult (accumulated) ─────────────────────────────────────────────────

func TestAppendResult_Basic(t *testing.T) {
	updated, needsCompression := AppendResult("", "step 1 done", 1)
	if updated != "step 1 done" {
		t.Errorf("got %q", updated)
	}
	if needsCompression {
		t.Error("should not need compression yet")
	}
}

func TestAppendResult_NeedsCompressionByCount(t *testing.T) {
	// Use the in-package constant directly so the assertion stays true if
	// expensivePathTriggerCount is later retuned.
	_, needs := AppendResult("some text", "result", expensivePathTriggerCount)
	if !needs {
		t.Error("expected needsCompression=true when completedCount >= trigger")
	}
}

// ── BuildExecutorPrompt ────────────────────────────────────────────────────────

func TestBuildExecutorPrompt_Basic(t *testing.T) {
	state := makeTask("t", 1)
	state.Status = TaskStatusExecuting
	state.Accumulated = "step 1 done"
	state.Artifacts = []Artifact{{Path: "main.go", Hash: "deadbeef"}}

	prompt := BuildExecutorPrompt(state, "")
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !contains(prompt, "refactor auth module") {
		t.Error("prompt should contain the goal")
	}
	if !contains(prompt, "step 1 done") {
		t.Error("prompt should contain accumulated")
	}
	if !contains(prompt, "main.go") {
		t.Error("prompt should list artifacts")
	}
}

func TestBuildExecutorPrompt_PriorSubTasksInjected(t *testing.T) {
	state := makeTask("t-prior", 1)
	state.Status = TaskStatusExecuting
	state.Plan = []SubTask{
		{ID: "s1", Description: "盤點現況", Status: SubTaskDone, Result: "**結論**：找到三個檔案"},
		{ID: "s2", Description: "修補 lint", Status: SubTaskFailed, Result: "buf lint 仍失敗"},
		{ID: "s3", Description: "撰寫摘要", Status: SubTaskPending},
	}
	state.CurrentIdx = 2

	prompt := BuildExecutorPrompt(state, "")
	if !contains(prompt, "前序子任務結果") {
		t.Error("expected prior sub-task block when CurrentIdx > 0")
	}
	if !contains(prompt, "盤點現況") || !contains(prompt, "找到三個檔案") {
		t.Error("expected prior done sub-task description + result to be injected")
	}
	if !contains(prompt, "修補 lint") || !contains(prompt, "buf lint 仍失敗") {
		t.Error("expected prior failed sub-task to be injected")
	}
	// Pending sub-task descriptions must not leak into the prior block (they
	// are noise — current sub-task already shows its own description).
	if contains(prompt, "前序子任務結果") {
		// Locate the "前序子任務結果" segment and make sure 撰寫摘要 (the pending
		// one we are about to do) doesn't appear *inside* it. The current
		// sub-task block at the bottom is still allowed to mention it.
		priorIdx := indexOf(prompt, "前序子任務結果")
		currentIdx := indexOf(prompt, "當前子任務")
		segment := prompt[priorIdx:currentIdx]
		if contains(segment, "撰寫摘要") {
			t.Error("pending sub-task should not be listed in the prior-subtasks block")
		}
	}
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestBuildExecutorPrompt_NilSubTask(t *testing.T) {
	state := makeTask("t", 1)
	state.Plan = nil
	if BuildExecutorPrompt(state, "") != "" {
		t.Error("expected empty prompt when no sub-task")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── ListTasksForChat: artifact loading must not deadlock on MaxOpenConns(1) ────
//
// Regression for #137: loading artifacts requires a second DB query. If rows
// from the outer task scan were still open, this would deadlock under
// SetMaxOpenConns(1). The fix: scan all task rows, close rows, then load
// artifacts per task. This test exercises that path explicitly.
func TestListTasksWithArtifacts_NoDeadlock(t *testing.T) {
	store := newTestStore(t) // MaxOpenConns(1) via openTestDB

	// Create two tasks, each with an artifact.
	for i, id := range []string{"dl-task-1", "dl-task-2"} {
		task := makeTask(id, 55)
		if _, err := store.CreateTask(task); err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
		art := Artifact{Path: "src/file.go", Hash: "deadbeef", SubTaskID: "s1"}
		if err := store.AppendArtifact(id, art); err != nil {
			t.Fatalf("AppendArtifact %d: %v", i, err)
		}
	}

	// ListTasksForChat must complete without deadlock; artifacts must be present.
	tasks, err := store.ListTasksForChat(55, 10)
	if err != nil {
		t.Fatalf("ListTasksForChat deadlocked or errored: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if len(task.Artifacts) != 1 {
			t.Errorf("task %s: expected 1 artifact, got %d", task.ID, len(task.Artifacts))
		}
	}
}

// ── UpdatePlannerSession ───────────────────────────────────────────────────────

func TestUpdatePlannerSession(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateTask(makeTask("task-ps", 7)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePlannerSession("task-ps", "sess-abc-123"); err != nil {
		t.Fatalf("UpdatePlannerSession: %v", err)
	}
	got, _ := store.GetTask("task-ps")
	if got.PlannerSessionID != "sess-abc-123" {
		t.Errorf("PlannerSessionID: %q", got.PlannerSessionID)
	}
}

// ── file-backed DB (smoke test) ────────────────────────────────────────────────

func TestFileBackedStore_RoundTrip(t *testing.T) {
	f, err := os.CreateTemp("", "hermes-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := sql.Open("sqlite", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store, err := NewSQLiteTaskStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore: %v", err)
	}

	task := makeTask("file-task", 88)
	if _, err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTask("file-task")
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != task.Goal {
		t.Errorf("goal mismatch after file round-trip")
	}
}

// ── ResetBudgetStartedAt ───────────────────────────────────────────────────────

// TestGetActiveTaskForChat_NoDeadlock verifies that GetActiveTaskForChat, which
// internally calls scanTask → loadArtifacts (a second query on the same conn),
// does not deadlock under SetMaxOpenConns(1).
func TestGetActiveTaskForChat_NoDeadlock(t *testing.T) {
	store := newTestStore(t)

	task := makeTask("active-dl", 99)
	if _, err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Artifact forces loadArtifacts to issue a real second query.
	art := Artifact{Path: "main.go", Hash: "aabbcc", SubTaskID: "s1"}
	if err := store.AppendArtifact("active-dl", art); err != nil {
		t.Fatalf("AppendArtifact: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := store.GetActiveTaskForChat(99)
		if err == ErrNoTask {
			err = nil
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetActiveTaskForChat error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetActiveTaskForChat deadlocked under SetMaxOpenConns(1)")
	}
}

// TestUpdateSubTask_NoDeadlock verifies the retry subtask update path —
// UpdateSubTask reads plan_json via QueryRow, patches it, writes back via Exec,
// then calls upsertUnifiedSubTask — completes without deadlock.
func TestUpdateSubTask_NoDeadlock(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.CreateTask(makeTask("retry-dl", 77)); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.UpdateSubTask("retry-dl", 0, SubTaskDone, "completed ok", 500)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("UpdateSubTask error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("UpdateSubTask deadlocked under SetMaxOpenConns(1)")
	}
}

// TestListThenUpdateSubTask_NoDeadlock simulates the dashboard+retry access
// pattern: list tasks for a chat (rows + artifacts), then immediately update a
// sub-task on the first result. Verifies connection is correctly released between
// the two calls under SetMaxOpenConns(1).
func TestListThenUpdateSubTask_NoDeadlock(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.CreateTask(makeTask("combo-dl", 66)); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	art := Artifact{Path: "pkg/foo.go", Hash: "cafebabe", SubTaskID: "s1"}
	if err := store.AppendArtifact("combo-dl", art); err != nil {
		t.Fatalf("AppendArtifact: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		tasks, err := store.ListTasksForChat(66, 5)
		if err != nil {
			done <- err
			return
		}
		if len(tasks) == 0 {
			done <- nil
			return
		}
		done <- store.UpdateSubTask(tasks[0].ID, 0, SubTaskDone, "ok", 100)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("list-then-update error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListTasksForChat + UpdateSubTask deadlocked under SetMaxOpenConns(1)")
	}
}

func TestResetBudgetStartedAt(t *testing.T) {
	store := newTestStore(t)
	task := makeTask("task-budget", 42)
	oldTime := time.Now().Add(-10 * time.Minute)
	task.TokenBudget.StartedAt = oldTime

	created, err := store.CreateTask(task)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	newTime := time.Now()
	if err := store.ResetBudgetStartedAt("task-budget", newTime); err != nil {
		t.Fatalf("ResetBudgetStartedAt: %v", err)
	}

	got, err := store.GetTask("task-budget")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if got.TokenBudget.StartedAt.Unix() != newTime.Unix() {
		t.Errorf("TokenBudget.StartedAt not updated: got %v, want %v", got.TokenBudget.StartedAt, newTime)
	}
	if got.TokenBudget.MaxTotalTokens != created.TokenBudget.MaxTotalTokens {
		t.Errorf("TokenBudget.MaxTotalTokens corrupted after reset")
	}
}

// TestMarkInterrupted_NoDeadlock verifies that MarkInterrupted, which issues a
// QueryRow (SELECT status) then two sequential Exec calls (UPDATE
// hermes_task_states + UPDATE tasks via updateUnifiedTaskStatus), completes
// without deadlocking under SetMaxOpenConns(1) and actually transitions the
// task to interrupted status.
func TestMarkInterrupted_NoDeadlock(t *testing.T) {
	store := newTestStore(t)
	task := makeTask("task-mark-int", 55)
	if _, err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.MarkInterrupted("task-mark-int", 1234)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("MarkInterrupted: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MarkInterrupted deadlocked under SetMaxOpenConns(1)")
	}

	got, err := store.GetTask("task-mark-int")
	if err != nil {
		t.Fatalf("GetTask after MarkInterrupted: %v", err)
	}
	if got.Status != TaskStatusInterrupted {
		t.Errorf("status = %q, want %q", got.Status, TaskStatusInterrupted)
	}
	if got.InterruptedBy == nil || *got.InterruptedBy != 1234 {
		t.Errorf("InterruptedBy = %v, want 1234", got.InterruptedBy)
	}
}

// TestMarkStatus_NoDeadlock verifies that MarkStatus, which issues a QueryRow
// (SELECT status) then two sequential Exec calls (UPDATE hermes_task_states +
// UPDATE tasks), completes without deadlocking under SetMaxOpenConns(1) and
// persists the new status correctly.
func TestMarkStatus_NoDeadlock(t *testing.T) {
	store := newTestStore(t)
	task := makeTask("task-mark-status", 56)
	if _, err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.MarkStatus("task-mark-status", TaskStatusExecuting)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("MarkStatus(planning->executing): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MarkStatus deadlocked under SetMaxOpenConns(1)")
	}

	got, err := store.GetTask("task-mark-status")
	if err != nil {
		t.Fatalf("GetTask after MarkStatus: %v", err)
	}
	if got.Status != TaskStatusExecuting {
		t.Errorf("status = %q, want %q", got.Status, TaskStatusExecuting)
	}
}

// TestMarkStatus_SequentialTransitions_NoDeadlock exercises the full
// planning → executing → done lifecycle with three sequential MarkStatus calls
// under SetMaxOpenConns(1) to confirm the read-then-write pattern never
// deadlocks across multiple transitions.
func TestMarkStatus_SequentialTransitions_NoDeadlock(t *testing.T) {
	store := newTestStore(t)
	task := makeTask("task-seq-trans", 57)
	if _, err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	transitions := []TaskStatus{TaskStatusExecuting, TaskStatusExecuting, TaskStatusDone}
	for _, next := range transitions {
		done := make(chan error, 1)
		go func(s TaskStatus) {
			done <- store.MarkStatus("task-seq-trans", s)
		}(next)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("MarkStatus(->%s): %v", next, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("MarkStatus(->%s) deadlocked under SetMaxOpenConns(1)", next)
		}
	}

	got, err := store.GetTask("task-seq-trans")
	if err != nil {
		t.Fatalf("GetTask after transitions: %v", err)
	}
	if got.Status != TaskStatusDone {
		t.Errorf("final status = %q, want %q", got.Status, TaskStatusDone)
	}
}

// ── ListStaleInterrupts (#169 slice 2) ─────────────────────────────────────

func TestListStaleInterrupts_SkipsTasksWithoutInterrupt(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-no-interrupt", 100))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	status := TaskStatusExecuting
	currentIdx := 0
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &status, CurrentIdx: &currentIdx}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepPlanner,
		Metadata:   SnapshotMetadata{Source: "test"},
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}

	stale, err := store.ListStaleInterrupts(time.Now())
	if err != nil {
		t.Fatalf("ListStaleInterrupts: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("want 0 stale, got %d: %+v", len(stale), stale)
	}
}

func TestListStaleInterrupts_FindsExpiredInterrupt(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-stale", 200))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	status := TaskStatusExecuting
	currentIdx := 1
	// Pretend the pause was created 25h ago, well past a 24h cutoff.
	createdAt := time.Now().Add(-25 * time.Hour)
	interrupt := &HermesInterrupt{
		ID:         "iv-old",
		SourceStep: RuntimeStepExecutor,
		ResumeStep: RuntimeStepExecutor,
		Reason:     "subtask_failure_pause",
		CreatedAt:  createdAt,
	}
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &status, CurrentIdx: &currentIdx, Interrupt: interrupt}},
		NextStep:   RuntimeStepApproval,
		SourceNode: RuntimeStepExecutor,
		Metadata:   SnapshotMetadata{Source: "test", Reason: "subtask_failure_pause"},
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}

	stale, err := store.ListStaleInterrupts(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("ListStaleInterrupts: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("want 1 stale, got %d: %+v", len(stale), stale)
	}
	got := stale[0]
	if got.TaskID != task.ID || got.ChatID != 200 {
		t.Errorf("ref mismatch: %+v", got)
	}
	if got.Interrupt.ID != "iv-old" || got.Interrupt.Reason != "subtask_failure_pause" {
		t.Errorf("interrupt mismatch: %+v", got.Interrupt)
	}
}

func TestListStaleInterrupts_SkipsRecentInterrupt(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-recent", 300))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	status := TaskStatusExecuting
	currentIdx := 0
	// Pause is 5min old — well within the cutoff.
	interrupt := &HermesInterrupt{
		ID:        "iv-recent",
		Reason:    "subtask_failure_pause",
		CreatedAt: time.Now().Add(-5 * time.Minute),
	}
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &status, CurrentIdx: &currentIdx, Interrupt: interrupt}},
		NextStep:   RuntimeStepApproval,
		SourceNode: RuntimeStepExecutor,
		Metadata:   SnapshotMetadata{Source: "test"},
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}

	stale, err := store.ListStaleInterrupts(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("ListStaleInterrupts: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("want 0 stale (recent interrupt), got %d: %+v", len(stale), stale)
	}
}

func TestListStaleInterrupts_SkipsTerminalTasks(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-done-with-interrupt", 400))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Even with a stale interrupt in the snapshot, terminal status should
	// exclude the task — failed/done are already accounted for elsewhere.
	executing := TaskStatusExecuting
	idx := 0
	stale := time.Now().Add(-25 * time.Hour)
	interrupt := &HermesInterrupt{ID: "iv", CreatedAt: stale}
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &executing, CurrentIdx: &idx, Interrupt: interrupt}},
		NextStep:   RuntimeStepApproval,
		SourceNode: RuntimeStepExecutor,
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}
	if err := store.MarkStatus(task.ID, TaskStatusDone); err != nil {
		t.Fatalf("MarkStatus: %v", err)
	}

	got, err := store.ListStaleInterrupts(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("ListStaleInterrupts: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("terminal task should be excluded: %+v", got)
	}
}

func TestCommitRuntimeStep_ClearInterruptRemovesFromSnapshot(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-clear-interrupt", 500))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	status := TaskStatusExecuting
	idx := 0
	interrupt := &HermesInterrupt{ID: "iv", Reason: "subtask_failure_pause", CreatedAt: time.Now()}
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &status, CurrentIdx: &idx, Interrupt: interrupt}},
		NextStep:   RuntimeStepApproval,
		SourceNode: RuntimeStepExecutor,
	}); err != nil {
		t.Fatalf("commit pause: %v", err)
	}
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{ClearInterrupt: true}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepApproval,
	}); err != nil {
		t.Fatalf("commit clear: %v", err)
	}
	latest, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if latest.State.Interrupt != nil {
		t.Errorf("interrupt should be cleared, got %+v", latest.State.Interrupt)
	}
}

// ── Slice 3c: snapshot-priority read paths ─────────────────────────────────

func TestGetTask_PrefersLatestSnapshotState(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-overlay", 7))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// First snapshot: Plan + status executing + accumulated.
	executing := TaskStatusExecuting
	currentIdx := 1
	plan := []SubTask{
		{ID: "s1", Description: "first", Status: SubTaskDone, Result: "ok-1", TokensUsed: 10},
		{ID: "s2", Description: "second", Status: SubTaskInProgress},
	}
	accumulated := "first done"
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Plan: plan, CurrentIdx: &currentIdx, Status: &executing, Accumulated: &accumulated}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepExecutor,
	}); err != nil {
		t.Fatalf("CommitRuntimeStep #1: %v", err)
	}

	// Read back via GetTask — should match snapshot, not the initial CreateTask state.
	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusExecuting {
		t.Errorf("status = %q, want executing", got.Status)
	}
	if got.CurrentIdx != 1 {
		t.Errorf("CurrentIdx = %d, want 1", got.CurrentIdx)
	}
	if got.Accumulated != "first done" {
		t.Errorf("Accumulated = %q, want %q", got.Accumulated, "first done")
	}
	if len(got.Plan) != 2 || got.Plan[0].Status != SubTaskDone || got.Plan[1].Status != SubTaskInProgress {
		t.Errorf("Plan mismatch: %+v", got.Plan)
	}

	// CreatedAt must come from legacy row (not zero, not from snapshot).
	if got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be preserved from legacy row, got zero")
	}
}

func TestGetTask_FallsBackToLegacyWhenNoSnapshot(t *testing.T) {
	// A task that was just CreateTask'd has no snapshot yet. GetTask must
	// still return the legacy row instead of erroring out.
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-no-snap", 9))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusPlanning {
		t.Errorf("status = %q, want %q (initial CreateTask state)", got.Status, TaskStatusPlanning)
	}
	if got.ChatID != 9 {
		t.Errorf("ChatID = %d, want 9", got.ChatID)
	}
}

func TestGetActiveTaskForChat_UsesSnapshotStatus(t *testing.T) {
	// If snapshot says the task moved to done, GetActiveTaskForChat must
	// exclude it — even if some hypothetical test bypassed the legacy
	// status update, the read path must respect the snapshot's status.
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("task-active", 21))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	executing := TaskStatusExecuting
	currentIdx := 0
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &executing, CurrentIdx: &currentIdx}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepPlanner,
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}

	got, err := store.GetActiveTaskForChat(21)
	if err != nil {
		t.Fatalf("GetActiveTaskForChat: %v", err)
	}
	if got.ID != task.ID || got.Status != TaskStatusExecuting {
		t.Errorf("active task mismatch: id=%q status=%q", got.ID, got.Status)
	}
}

func TestListTasksForChat_AppliesSnapshotOverlay(t *testing.T) {
	store := newTestStore(t)
	chatID := int64(50)
	// Create three tasks, advance each to a different state via CommitRuntimeStep.
	for i := 1; i <= 3; i++ {
		base := makeTask(fmt.Sprintf("task-list-%d", i), chatID)
		if _, err := store.CreateTask(base); err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
	}
	executing := TaskStatusExecuting
	idx := 1
	for i := 1; i <= 3; i++ {
		acc := fmt.Sprintf("progress-%d", i)
		if _, err := store.CommitRuntimeStep(RuntimeCommit{
			TaskID:     fmt.Sprintf("task-list-%d", i),
			Updates:    []StateUpdate{{Status: &executing, CurrentIdx: &idx, Accumulated: &acc}},
			NextStep:   RuntimeStepExecutor,
			SourceNode: RuntimeStepExecutor,
		}); err != nil {
			t.Fatalf("CommitRuntimeStep %d: %v", i, err)
		}
	}

	tasks, err := store.ListTasksForChat(chatID, 10)
	if err != nil {
		t.Fatalf("ListTasksForChat: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(tasks))
	}
	for _, tk := range tasks {
		if tk.Status != TaskStatusExecuting {
			t.Errorf("task %s status = %q, want executing (from snapshot)", tk.ID, tk.Status)
		}
		if !strings.HasPrefix(tk.Accumulated, "progress-") {
			t.Errorf("task %s accumulated = %q, want progress-*", tk.ID, tk.Accumulated)
		}
	}
}

// ── Slice 3d: snapshot is authoritative for active-task filter ─────────────

func TestGetActiveTaskForChat_FiltersOnSnapshotStatus(t *testing.T) {
	// Even if some path were to set legacy.status='executing' on a task
	// whose latest snapshot says 'done', the filter must respect the
	// snapshot. Verifies state_status column is the source of truth.
	store := newTestStore(t)
	chatID := int64(80)
	task, err := store.CreateTask(makeTask("task-3d-active", chatID))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// First commit: executing
	executing := TaskStatusExecuting
	idx := 0
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &executing, CurrentIdx: &idx}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepPlanner,
	}); err != nil {
		t.Fatalf("commit executing: %v", err)
	}
	if got, err := store.GetActiveTaskForChat(chatID); err != nil || got.ID != task.ID {
		t.Fatalf("active task should be present after executing commit: id=%q err=%v", got.ID, err)
	}

	// Second commit: done (terminal). Filter must exclude it.
	done := TaskStatusDone
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []StateUpdate{{Status: &done}},
		NextStep:   RuntimeStepTerminal,
		SourceNode: RuntimeStepReviewer,
	}); err != nil {
		t.Fatalf("commit done: %v", err)
	}
	got, err := store.GetActiveTaskForChat(chatID)
	if err != ErrNoTask {
		t.Errorf("after terminal snapshot expected ErrNoTask, got id=%q err=%v", got.ID, err)
	}
}

func TestGetActiveTaskForChat_FallsBackToLegacyStatusWhenNoSnapshot(t *testing.T) {
	// CreateTask writes only the legacy row (no snapshot yet). Filter
	// must still find such a fresh task as active via COALESCE fallback.
	store := newTestStore(t)
	chatID := int64(81)
	task, err := store.CreateTask(makeTask("task-3d-fresh", chatID))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	got, err := store.GetActiveTaskForChat(chatID)
	if err != nil {
		t.Fatalf("fresh task should be active via legacy fallback, got err=%v", err)
	}
	if got.ID != task.ID {
		t.Errorf("active task id = %q, want %q", got.ID, task.ID)
	}
}

func TestSweepZombieTasks_MarksNonTerminalSnapshotsAsFailed(t *testing.T) {
	store := newTestStore(t)

	// Three tasks: one executing, one already done, one with no snapshot.
	executingTask, _ := store.CreateTask(makeTask("zombie-exec", 90))
	doneTask, _ := store.CreateTask(makeTask("zombie-done", 91))
	freshTask, _ := store.CreateTask(makeTask("zombie-fresh", 92))

	executing := TaskStatusExecuting
	idx := 0
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     executingTask.ID,
		Updates:    []StateUpdate{{Status: &executing, CurrentIdx: &idx}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepPlanner,
	}); err != nil {
		t.Fatalf("commit executing: %v", err)
	}
	done := TaskStatusDone
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     doneTask.ID,
		Updates:    []StateUpdate{{Status: &done}},
		NextStep:   RuntimeStepTerminal,
		SourceNode: RuntimeStepExecutor,
	}); err != nil {
		t.Fatalf("commit done: %v", err)
	}

	swept, err := store.SweepZombieTasks("test_sweep")
	if err != nil {
		t.Fatalf("SweepZombieTasks: %v", err)
	}
	// executingTask + freshTask (planning) should be swept; doneTask must not.
	if swept != 2 {
		t.Errorf("swept = %d, want 2", swept)
	}

	// Verify snapshots now have terminal state for the two swept tasks.
	for _, id := range []string{executingTask.ID, freshTask.ID} {
		snap, err := store.GetLatestSnapshot(id)
		if err != nil {
			t.Errorf("GetLatestSnapshot(%s): %v", id, err)
			continue
		}
		if snap.State.Status != TaskStatusFailed {
			t.Errorf("task %s: status = %q, want failed", id, snap.State.Status)
		}
		if snap.Metadata.Source != "orphan_recovery" || snap.Metadata.Reason != "test_sweep" {
			t.Errorf("task %s metadata = %+v, want orphan_recovery/test_sweep", id, snap.Metadata)
		}
	}
	// The done task's latest snapshot must remain done (not swept).
	doneSnap, err := store.GetLatestSnapshot(doneTask.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot(done): %v", err)
	}
	if doneSnap.State.Status != TaskStatusDone {
		t.Errorf("done task latest status = %q, want done", doneSnap.State.Status)
	}
}

func TestMarkTaskFailedDurable_WritesSnapshotWithReason(t *testing.T) {
	store := newTestStore(t)
	task, err := store.CreateTask(makeTask("mark-failed", 100))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.MarkTaskFailedDurable(task.ID, "stale_interrupt_orphan"); err != nil {
		t.Fatalf("MarkTaskFailedDurable: %v", err)
	}
	snap, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if snap.State.Status != TaskStatusFailed {
		t.Errorf("snapshot status = %q, want failed", snap.State.Status)
	}
	if snap.Metadata.Source != "orphan_recovery" || snap.Metadata.Reason != "stale_interrupt_orphan" {
		t.Errorf("snapshot metadata = %+v", snap.Metadata)
	}
}

// ── ApplyInterruptResolution (#169 β2) ─────────────────────────────────────

// commitFailurePauseSnapshot writes a Hermes failure pause snapshot
// onto a freshly-created task, returning the sub-task index that was
// captured in Interrupt.Payload (used by skip resolution).
func commitFailurePauseSnapshot(t *testing.T, store *SQLiteTaskStore, taskID string, idx int) {
	t.Helper()
	executing := TaskStatusExecuting
	currentIdx := idx
	plan := []SubTask{
		{ID: "s1", Description: "first", Status: SubTaskDone},
		{ID: "s2", Description: "second", Status: SubTaskInProgress, Result: "deadline exceeded"},
	}
	interrupt := &HermesInterrupt{
		ID:         "iv-pause",
		Reason:     "subtask_failure_pause",
		SourceStep: RuntimeStepExecutor,
		ResumeStep: RuntimeStepExecutor,
		CreatedAt:  time.Now(),
		Payload: map[string]any{
			"sub_task_idx":  idx,
			"sub_task_id":   plan[idx].ID,
			"sub_task_desc": plan[idx].Description,
			"total":         len(plan),
			"error_text":    "deadline exceeded",
			"failure_kind":  FailureEnv.Label(),
		},
	}
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     taskID,
		Updates:    []StateUpdate{{Status: &executing, CurrentIdx: &currentIdx, Plan: plan, Interrupt: interrupt}},
		NextStep:   RuntimeStepApproval,
		SourceNode: RuntimeStepExecutor,
		Metadata:   SnapshotMetadata{Source: "test", Reason: "subtask_failure_pause"},
	}); err != nil {
		t.Fatalf("commit pause snapshot: %v", err)
	}
}

func TestApplyInterruptResolution_RetryClearsInterrupt(t *testing.T) {
	store := newTestStore(t)
	task, _ := store.CreateTask(makeTask("res-retry", 300))
	commitFailurePauseSnapshot(t, store, task.ID, 1)

	if err := store.ApplyInterruptResolution(task.ID, InterruptResolutionRetry); err != nil {
		t.Fatalf("ApplyInterruptResolution: %v", err)
	}

	snap, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if snap.State.Interrupt != nil {
		t.Errorf("Interrupt should be cleared, got %+v", snap.State.Interrupt)
	}
	// retry must NOT mutate the plan — sub-task stays in_progress for replay
	if snap.State.Plan[1].Status != SubTaskInProgress {
		t.Errorf("plan[1] status = %q, want %q (retry should not change plan)", snap.State.Plan[1].Status, SubTaskInProgress)
	}
	if snap.Metadata.Reason != "user_retry_after_pause" {
		t.Errorf("metadata.reason = %q, want user_retry_after_pause", snap.Metadata.Reason)
	}
}

func TestApplyInterruptResolution_BudgetRetryResetsBudgetStartedAtAndPreservesResumeStep(t *testing.T) {
	store := newTestStore(t)
	task, _ := store.CreateTask(makeTask("res-budget", 310))
	// Commit a budget-exceeded snapshot mid-reviewer (uncommon but
	// tests the ResumeStep preservation explicitly).
	executing := TaskStatusExecuting
	startedAt := time.Now().Add(-2 * time.Hour)
	plan := []SubTask{
		{ID: "s1", Description: "first", Status: SubTaskDone},
	}
	interrupt := &HermesInterrupt{
		ID:         "iv-budget",
		Reason:     "budget_exceeded",
		SourceStep: RuntimeStepReviewer,
		ResumeStep: RuntimeStepReviewer,
		CreatedAt:  time.Now(),
		Payload: map[string]any{
			"used_tokens":      99000,
			"max_total_tokens": 50000,
		},
	}
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID: task.ID,
		Updates: []StateUpdate{{
			Status:          &executing,
			Plan:            plan,
			Interrupt:       interrupt,
			BudgetStartedAt: &startedAt,
		}},
		NextStep:   RuntimeStepReviewer,
		SourceNode: RuntimeStepReviewer,
		Metadata:   SnapshotMetadata{Source: "test", Reason: "budget_exceeded"},
	}); err != nil {
		t.Fatalf("commit budget snapshot: %v", err)
	}

	if err := store.ApplyInterruptResolution(task.ID, InterruptResolutionRetry); err != nil {
		t.Fatalf("ApplyInterruptResolution: %v", err)
	}
	snap, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if snap.State.Interrupt != nil {
		t.Errorf("Interrupt should be cleared, got %+v", snap.State.Interrupt)
	}
	if snap.NextStep != RuntimeStepReviewer {
		t.Errorf("NextStep = %q, want reviewer (preserved from ResumeStep)", snap.NextStep)
	}
	if snap.Metadata.Reason != "user_continue_after_budget" {
		t.Errorf("metadata.reason = %q, want user_continue_after_budget", snap.Metadata.Reason)
	}
	if !snap.State.TokenBudget.StartedAt.After(startedAt) {
		t.Errorf("BudgetStartedAt = %v, want reset to a time after %v", snap.State.TokenBudget.StartedAt, startedAt)
	}
}

func TestApplyInterruptResolution_SkipMarksSubTaskAndAdvances(t *testing.T) {
	store := newTestStore(t)
	task, _ := store.CreateTask(makeTask("res-skip", 301))
	commitFailurePauseSnapshot(t, store, task.ID, 1)

	if err := store.ApplyInterruptResolution(task.ID, InterruptResolutionSkip); err != nil {
		t.Fatalf("ApplyInterruptResolution: %v", err)
	}

	snap, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if snap.State.Interrupt != nil {
		t.Errorf("Interrupt should be cleared, got %+v", snap.State.Interrupt)
	}
	if snap.State.Plan[1].Status != SubTaskSkipped {
		t.Errorf("plan[1] status = %q, want %q", snap.State.Plan[1].Status, SubTaskSkipped)
	}
	if snap.State.CurrentIdx != 2 {
		t.Errorf("CurrentIdx = %d, want 2 (advanced past skipped)", snap.State.CurrentIdx)
	}
}

func TestApplyInterruptResolution_AbortMarksTaskFailed(t *testing.T) {
	store := newTestStore(t)
	task, _ := store.CreateTask(makeTask("res-abort", 302))
	commitFailurePauseSnapshot(t, store, task.ID, 1)

	if err := store.ApplyInterruptResolution(task.ID, InterruptResolutionAbort); err != nil {
		t.Fatalf("ApplyInterruptResolution: %v", err)
	}

	snap, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if snap.State.Status != TaskStatusFailed {
		t.Errorf("status = %q, want failed", snap.State.Status)
	}
	if snap.Metadata.Reason != "user_abort_after_pause" {
		t.Errorf("metadata.reason = %q, want user_abort_after_pause", snap.Metadata.Reason)
	}
}

func TestApplyInterruptResolution_RejectsTaskWithoutInterrupt(t *testing.T) {
	store := newTestStore(t)
	task, _ := store.CreateTask(makeTask("res-no-interrupt", 303))
	// No commit, so no snapshot at all — should also error.
	err := store.ApplyInterruptResolution(task.ID, InterruptResolutionRetry)
	if err == nil {
		t.Error("expected error when no snapshot/interrupt exists")
	}
}

func TestApplyInterruptResolution_RejectsUnknownDecision(t *testing.T) {
	store := newTestStore(t)
	task, _ := store.CreateTask(makeTask("res-bad-decision", 304))
	commitFailurePauseSnapshot(t, store, task.ID, 1)
	err := store.ApplyInterruptResolution(task.ID, InterruptResolution("bogus"))
	if err == nil {
		t.Error("expected error for unknown decision")
	}
}

func TestSweepZombieTasks_SkipsPausedTasksWithActiveInterrupt(t *testing.T) {
	// Slice 3d hotfix: tasks with a pending HermesInterrupt are
	// legitimately paused waiting for the operator's click — they must
	// NOT be marked failed by the zombie sweep, otherwise every alice
	// restart would kill paused tasks before β1/β2 cold-restart resume
	// can pick them up.
	store := newTestStore(t)
	pausedTask, _ := store.CreateTask(makeTask("zombie-paused", 110))
	commitFailurePauseSnapshot(t, store, pausedTask.ID, 1)

	zombieTask, _ := store.CreateTask(makeTask("zombie-real", 111))
	executing := TaskStatusExecuting
	idx := 0
	if _, err := store.CommitRuntimeStep(RuntimeCommit{
		TaskID:     zombieTask.ID,
		Updates:    []StateUpdate{{Status: &executing, CurrentIdx: &idx}},
		NextStep:   RuntimeStepExecutor,
		SourceNode: RuntimeStepPlanner,
	}); err != nil {
		t.Fatalf("commit zombie: %v", err)
	}

	swept, err := store.SweepZombieTasks("test_paused_protection")
	if err != nil {
		t.Fatalf("SweepZombieTasks: %v", err)
	}
	if swept != 1 {
		t.Errorf("swept = %d, want 1 (paused task should be excluded)", swept)
	}

	// Paused task must still be paused (Interrupt intact).
	pausedSnap, err := store.GetLatestSnapshot(pausedTask.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot paused: %v", err)
	}
	if pausedSnap.State.Status == TaskStatusFailed {
		t.Errorf("paused task got marked failed by zombie sweep — bug returned")
	}
	if pausedSnap.State.Interrupt == nil {
		t.Errorf("paused task interrupt was cleared — should still be pending")
	}

	// Real zombie was correctly swept.
	zombieSnap, _ := store.GetLatestSnapshot(zombieTask.ID)
	if zombieSnap.State.Status != TaskStatusFailed {
		t.Errorf("zombie task status = %q, want failed", zombieSnap.State.Status)
	}
}
