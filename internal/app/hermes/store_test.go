package hermes

import (
	"database/sql"
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
		Status:          TaskStatusPlanning,
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
	cfg := AccumulatedConfig{}
	updated, needsCompression := AppendResult("", "step 1 done", 1, cfg)
	if updated != "step 1 done" {
		t.Errorf("got %q", updated)
	}
	if needsCompression {
		t.Error("should not need compression yet")
	}
}

func TestAppendResult_TruncatesCheapPath(t *testing.T) {
	cfg := AccumulatedConfig{CheapMaxBytes: 20}
	long := "this is a fairly long first step result"
	updated, _ := AppendResult("", long, 1, cfg)
	if len(updated) > 20 {
		t.Errorf("expected truncation to 20 bytes, got %d", len(updated))
	}
}

func TestAppendResult_NeedsCompressionByCount(t *testing.T) {
	cfg := AccumulatedConfig{ExpensiveTriggerN: 3}
	_, needs := AppendResult("some text", "result", 3, cfg)
	if !needs {
		t.Error("expected needsCompression=true when completedCount >= trigger")
	}
}

func TestAppendResult_NeedsCompressionBySize(t *testing.T) {
	cfg := AccumulatedConfig{ExpensiveTriggerKB: 1} // 1 KB trigger
	big := string(make([]byte, 1100))               // 1100 bytes
	_, needs := AppendResult(big, "more", 1, cfg)
	if !needs {
		t.Error("expected needsCompression=true when accumulated > trigger")
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
