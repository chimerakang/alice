package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

func TestHandleHermesSnapshotsIncludesTaskUsageDetails(t *testing.T) {
	prev := globalStorage
	globalStorage = newTestSQLiteStorage(t)
	t.Cleanup(func() { globalStorage = prev })

	taskStoreAny := buildHermesTaskStore()
	taskStore, ok := taskStoreAny.(*hermes.SQLiteTaskStore)
	if !ok {
		t.Fatalf("buildHermesTaskStore returned %T, want *hermes.SQLiteTaskStore", taskStoreAny)
	}

	now := time.Now().UTC().Truncate(time.Second)
	task, err := taskStore.CreateTask(hermes.TaskState{
		ID:         "task-hermes-snapshot",
		ChatID:     42,
		ThreadID:   7,
		ProjectDir: "/repo",
		Goal:       "diagnose checklist sync",
		Status:     hermes.TaskStatusExecuting,
		TokenBudget: hermes.TokenBudget{
			MaxTotalTokens: 1000,
			UsedTokens:     100,
			StartedAt:      now.Add(-time.Hour),
		},
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := taskStore.AddModelUsageBreakdown(task.ID, "gpt-5.5", hermes.TokenUsageBreakdown{
		UncachedInputTokens:      10,
		CacheReadInputTokens:     20,
		CacheCreationInputTokens: 5,
		OutputTokens:             8,
		CostUSD:                  0.42,
	}); err != nil {
		t.Fatalf("AddModelUsageBreakdown: %v", err)
	}
	if err := taskStore.AddPhaseUsageBreakdown(task.ID, "executor", "gpt-5.5", hermes.TokenUsageBreakdown{
		UncachedInputTokens:  10,
		CacheReadInputTokens: 20,
		OutputTokens:         8,
		CostUSD:              0.21,
	}); err != nil {
		t.Fatalf("AddPhaseUsageBreakdown: %v", err)
	}

	status := hermes.TaskStatusExecuting
	currentIdx := 0
	plan := []hermes.SubTask{
		{ID: "s1", Description: "sync checklist", Status: hermes.SubTaskDone, Result: "done"},
	}
	if _, err := taskStore.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     task.ID,
		Updates:    []hermes.StateUpdate{{Status: &status, CurrentIdx: &currentIdx, Plan: plan}},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata: hermes.SnapshotMetadata{
			Source: "test",
			Reason: "seed_snapshot",
		},
	}); err != nil {
		t.Fatalf("CommitRuntimeStep: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/hermes/snapshots?task_id="+task.ID, nil)
	w := httptest.NewRecorder()
	(&WebInterface{}).handleHermesSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		TaskID      string           `json:"task_id"`
		Total       int              `json:"total"`
		Accumulated string           `json:"accumulated"`
		Task        hermes.TaskState `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TaskID != task.ID || resp.Total != 1 {
		t.Fatalf("unexpected response header: %+v", resp)
	}
	if len(resp.Task.ModelUsages) != 1 || len(resp.Task.PhaseUsages) != 1 {
		t.Fatalf("usage details missing from task payload: %+v", resp.Task)
	}
	if resp.Task.ModelUsages[0].CacheReadInputTokens != 20 || resp.Task.PhaseUsages[0].Phase != "executor" {
		t.Fatalf("usage payload mismatch: %+v %+v", resp.Task.ModelUsages[0], resp.Task.PhaseUsages[0])
	}
}
