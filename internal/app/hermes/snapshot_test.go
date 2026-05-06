package hermes

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSnapshotTypesSerialize(t *testing.T) {
	status := TaskStatusExecuting
	idx := 1
	issueNumber := 161
	createdAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		ID:       "snap-1",
		TaskID:   "task-1",
		ChatID:   42,
		ThreadID: 7,
		Step:     3,
		State: HermesState{
			TaskID:      "task-1",
			ChatID:      42,
			ThreadID:    7,
			Status:      TaskStatusExecuting,
			CurrentIdx:  1,
			Goal:        "add snapshots",
			Accumulated: "planner done",
			Plan: []SubTask{
				{ID: "s1", Description: "write schema", Status: SubTaskDone},
			},
			SubTaskResults: []SubTaskResult{
				{SubTaskID: "s1", Index: 0, Status: SubTaskDone, Result: "schema added"},
			},
			Interrupt: &HermesInterrupt{
				ID:         "int-1",
				SourceStep: RuntimeStepApproval,
				ResumeStep: RuntimeStepExecutor,
				Reason:     "approval",
				CreatedAt:  createdAt,
			},
			Errors: []HermesStateError{
				{Step: RuntimeStepExecutor, Message: "retryable", Retryable: true, CreatedAt: createdAt},
			},
		},
		NextStep:   RuntimeStepReviewer,
		SourceNode: RuntimeStepExecutor,
		Writes: StateUpdate{
			Status:            &status,
			CurrentIdx:        &idx,
			AccumulatedDelta:  "executor result",
			GithubIssueNumber: &issueNumber,
		},
		Metadata: SnapshotMetadata{
			Source: "test",
			Reason: "serialization",
			Tags:   []string{"phase1"},
			Extra:  map[string]any{"ok": true},
		},
		ChannelVersions: map[string]int64{"status": 2},
		CreatedAt:       createdAt,
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Snapshot
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.NextStep != RuntimeStepReviewer || got.SourceNode != RuntimeStepExecutor {
		t.Fatalf("runtime steps did not round-trip: %+v", got)
	}
	if got.Writes.Status == nil || *got.Writes.Status != TaskStatusExecuting {
		t.Fatalf("status update did not round-trip: %+v", got.Writes)
	}
	if got.Writes.GithubIssueNumber == nil || *got.Writes.GithubIssueNumber != 161 {
		t.Fatalf("issue update did not round-trip: %+v", got.Writes)
	}
	if got.ChannelVersions["status"] != 2 {
		t.Fatalf("channel versions did not round-trip: %+v", got.ChannelVersions)
	}
}

func TestHermesStateFromTaskState(t *testing.T) {
	messageID := int64(99)
	task := TaskState{
		ID:               "task-state",
		ChatID:           42,
		ThreadID:         7,
		PlannerSessionID: "planner-session",
		ProjectDir:       "/repo",
		Goal:             "snapshot conversion",
		Status:           TaskStatusInterrupted,
		CurrentIdx:       2,
		Plan:             []SubTask{{ID: "s1", Status: SubTaskDone}},
		Artifacts:        []Artifact{{Path: "a.go", Hash: "abc", SubTaskID: "s1"}},
		InterruptedBy:    &messageID,
		ModelUsages:      []ModelUsage{{Model: "gpt-5.5", InputTokens: 10}},
	}

	state := HermesStateFromTaskState(task)
	if state.TaskID != task.ID || state.ChatID != task.ChatID || state.ThreadID != task.ThreadID {
		t.Fatalf("identity mismatch: %+v", state)
	}
	if state.Interrupt == nil || state.Interrupt.MessageID != messageID {
		t.Fatalf("interrupt not converted: %+v", state.Interrupt)
	}

	task.Plan[0].Status = SubTaskFailed
	task.Artifacts[0].Path = "changed.go"
	task.ModelUsages[0].InputTokens = 99
	if state.Plan[0].Status != SubTaskDone || state.Artifacts[0].Path != "a.go" || state.ModelUsages[0].InputTokens != 10 {
		t.Fatalf("HermesStateFromTaskState should clone slices: %+v", state)
	}
}
