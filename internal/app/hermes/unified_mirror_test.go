package hermes

import "testing"

func TestSQLiteTaskStoreMirrorsPlanToUnifiedTables(t *testing.T) {
	store := newTestStore(t)
	var broadcasts []string
	store.SetUnifiedTaskUpdateHook(func(taskID string) {
		broadcasts = append(broadcasts, taskID)
	})

	task, err := store.CreateTask(makeTask("task-unified", 42))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	plan := []SubTask{
		{ID: "s1", Description: "first step", Status: SubTaskPending},
		{ID: "s2", Description: "second step", Status: SubTaskPending},
	}
	if err := store.StorePlan(task.ID, plan); err != nil {
		t.Fatalf("StorePlan: %v", err)
	}
	if err := store.UpdateSubTask(task.ID, 0, SubTaskDone, "first done", 17); err != nil {
		t.Fatalf("UpdateSubTask: %v", err)
	}
	if err := store.AppendArtifact(task.ID, Artifact{SubTaskID: "s1", Path: "foo.go", Hash: "abc"}); err != nil {
		t.Fatalf("AppendArtifact: %v", err)
	}
	if err := store.AddModelUsage(task.ID, "claude-sonnet", 10, 7); err != nil {
		t.Fatalf("AddModelUsage: %v", err)
	}
	if err := store.MarkStatus(task.ID, TaskStatusDone); err != nil {
		t.Fatalf("MarkStatus: %v", err)
	}

	var taskStatus, projectDir string
	var totalIn, totalOut int
	if err := store.db.QueryRow(`
		SELECT status, project_dir, total_input_tokens, total_output_tokens
		FROM tasks WHERE id = ?`, task.ID).Scan(&taskStatus, &projectDir, &totalIn, &totalOut); err != nil {
		t.Fatalf("query unified task: %v", err)
	}
	if taskStatus != string(TaskStatusDone) || projectDir != "/repo" || totalIn != 10 || totalOut != 7 {
		t.Fatalf("unexpected unified task status/project/tokens: status=%q project=%q in=%d out=%d", taskStatus, projectDir, totalIn, totalOut)
	}

	var subStatus, result string
	var tokens int
	if err := store.db.QueryRow(`
		SELECT status, result_text, input_tokens
		FROM sub_tasks WHERE id = ?`, UnifiedSubTaskID(task.ID, 0, "s1")).Scan(&subStatus, &result, &tokens); err != nil {
		t.Fatalf("query unified subtask: %v", err)
	}
	if subStatus != string(SubTaskDone) || result != "first done" || tokens != 17 {
		t.Fatalf("unexpected unified subtask: status=%q result=%q tokens=%d", subStatus, result, tokens)
	}

	var artifactPath, artifactHash string
	if err := store.db.QueryRow(`
		SELECT path, hash FROM artifacts WHERE sub_task_id = ?`, UnifiedSubTaskID(task.ID, 0, "s1")).
		Scan(&artifactPath, &artifactHash); err != nil {
		t.Fatalf("query unified artifact: %v", err)
	}
	if artifactPath != "foo.go" || artifactHash != "abc" {
		t.Fatalf("unexpected artifact: path=%q hash=%q", artifactPath, artifactHash)
	}
	if len(broadcasts) == 0 {
		t.Fatal("expected unified task broadcasts")
	}
	if broadcasts[len(broadcasts)-1] != task.ID {
		t.Fatalf("last broadcast task ID: got %q, want %q", broadcasts[len(broadcasts)-1], task.ID)
	}
}
