package hermes

import (
	"reflect"
	"testing"
	"time"
)

func TestApplyStateUpdatesOverwriteFields(t *testing.T) {
	current := HermesState{
		TaskID:            "task-1",
		Status:            TaskStatusPlanning,
		CurrentIdx:        0,
		GithubIssueNumber: 12,
		Plan:              []SubTask{{ID: "old", Status: SubTaskPending}},
	}
	status := TaskStatusExecuting
	currentIdx := 2
	issueNumber := 161
	plan := []SubTask{{ID: "new", Status: SubTaskPending}}

	got, err := ApplyStateUpdates(current, []StateUpdate{
		{
			Status:            &status,
			CurrentIdx:        &currentIdx,
			Plan:              plan,
			GithubIssueNumber: &issueNumber,
		},
	})
	if err != nil {
		t.Fatalf("ApplyStateUpdates: %v", err)
	}
	if got.Status != TaskStatusExecuting || got.CurrentIdx != 2 || got.GithubIssueNumber != 161 {
		t.Fatalf("overwrite fields mismatch: %+v", got)
	}
	if len(got.Plan) != 1 || got.Plan[0].ID != "new" {
		t.Fatalf("plan overwrite mismatch: %+v", got.Plan)
	}

	plan[0].ID = "mutated"
	if got.Plan[0].ID != "new" {
		t.Fatalf("plan should be cloned, got %+v", got.Plan)
	}
}

func TestApplyStateUpdatesRejectsInvalidStatusTransition(t *testing.T) {
	status := TaskStatusExecuting
	current := HermesState{TaskID: "task-terminal", Status: TaskStatusDone}
	if _, err := ApplyStateUpdates(current, []StateUpdate{{Status: &status}}); err == nil {
		t.Fatal("ApplyStateUpdates terminal -> executing error = nil, want validation error")
	}
}

func TestApplyStateUpdatesAccumulatedBatch(t *testing.T) {
	absolute := "reset"
	tests := []struct {
		name    string
		current string
		updates []StateUpdate
		want    string
	}{
		{
			name:    "single delta",
			current: "first",
			updates: []StateUpdate{
				{AccumulatedDelta: "second"},
			},
			want: "first\nsecond",
		},
		{
			name:    "multi update batch",
			current: "first",
			updates: []StateUpdate{
				{AccumulatedDelta: "second"},
				{AccumulatedDelta: "third"},
			},
			want: "first\nsecond\nthird",
		},
		{
			name:    "preserves existing trailing separator",
			current: "first\n",
			updates: []StateUpdate{
				{AccumulatedDelta: "second"},
			},
			want: "first\nsecond",
		},
		{
			name:    "absolute reset then append",
			current: "first",
			updates: []StateUpdate{
				{Accumulated: &absolute},
				{AccumulatedDelta: "after"},
			},
			want: "reset\nafter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyStateUpdates(HermesState{Accumulated: tt.current}, tt.updates)
			if err != nil {
				t.Fatalf("ApplyStateUpdates: %v", err)
			}
			if got.Accumulated != tt.want {
				t.Fatalf("Accumulated = %q, want %q", got.Accumulated, tt.want)
			}
		})
	}
}

func TestApplyStateUpdatesAppendReducers(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	current := HermesState{
		Artifacts:   []Artifact{{Path: "a.go"}},
		ModelUsages: []ModelUsage{{Model: "planner"}},
		PhaseUsages: []PhaseUsage{{Phase: "planner", Model: "planner"}},
		Errors:      []HermesStateError{{Message: "first"}},
	}
	got, err := ApplyStateUpdates(current, []StateUpdate{
		{
			Artifacts:   []Artifact{{Path: "b.go"}},
			ModelUsages: []ModelUsage{{Model: "executor"}},
			PhaseUsages: []PhaseUsage{{Phase: "executor", Model: "executor"}},
			Errors:      []HermesStateError{{Step: RuntimeStepExecutor, Message: "second", CreatedAt: now}},
		},
	})
	if err != nil {
		t.Fatalf("ApplyStateUpdates: %v", err)
	}
	if len(got.Artifacts) != 2 || got.Artifacts[1].Path != "b.go" {
		t.Fatalf("artifacts append mismatch: %+v", got.Artifacts)
	}
	if len(got.ModelUsages) != 2 || got.ModelUsages[1].Model != "executor" {
		t.Fatalf("model usage append mismatch: %+v", got.ModelUsages)
	}
	if len(got.PhaseUsages) != 2 || got.PhaseUsages[1].Phase != "executor" {
		t.Fatalf("phase usage append mismatch: %+v", got.PhaseUsages)
	}
	if len(got.Errors) != 2 || got.Errors[1].Message != "second" {
		t.Fatalf("errors append mismatch: %+v", got.Errors)
	}
}

func TestApplyStateUpdatesInterruptOverwriteAndClear(t *testing.T) {
	current := HermesState{Interrupt: &HermesInterrupt{ID: "old"}}
	got, err := ApplyStateUpdates(current, []StateUpdate{
		{Interrupt: &HermesInterrupt{ID: "new"}},
	})
	if err != nil {
		t.Fatalf("ApplyStateUpdates: %v", err)
	}
	if got.Interrupt == nil || got.Interrupt.ID != "new" {
		t.Fatalf("interrupt overwrite mismatch: %+v", got.Interrupt)
	}

	got, err = ApplyStateUpdates(got, []StateUpdate{{ClearInterrupt: true}})
	if err != nil {
		t.Fatalf("ApplyStateUpdates clear: %v", err)
	}
	if got.Interrupt != nil {
		t.Fatalf("interrupt should be cleared: %+v", got.Interrupt)
	}
}

func TestApplyStateUpdatesSubTaskResults(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		current []SubTaskResult
		updates []StateUpdate
		want    []SubTaskResult
	}{
		{
			name: "pure append sorted by index",
			updates: []StateUpdate{
				{SubTaskResults: []SubTaskResult{
					{SubTaskID: "b", Index: 1, Status: SubTaskDone, Result: "b"},
					{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "a"},
				}},
			},
			want: []SubTaskResult{
				{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "a"},
				{SubTaskID: "b", Index: 1, Status: SubTaskDone, Result: "b"},
			},
		},
		{
			name: "pure overwrite",
			current: []SubTaskResult{
				{SubTaskID: "a", Index: 0, Status: SubTaskFailed, Result: "old", TokensUsed: 10, Attempts: 1},
			},
			updates: []StateUpdate{
				{SubTaskResults: []SubTaskResult{
					{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "new", TokensUsed: 20, Attempts: 2, EndedAt: &now},
				}},
			},
			want: []SubTaskResult{
				{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "new", TokensUsed: 20, Attempts: 2, EndedAt: &now},
			},
		},
		{
			name: "mixed in one batch",
			current: []SubTaskResult{
				{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "old-a"},
			},
			updates: []StateUpdate{
				{SubTaskResults: []SubTaskResult{
					{SubTaskID: "c", Index: 2, Status: SubTaskDone, Result: "new-c"},
					{SubTaskID: "a", Index: 0, Status: SubTaskFailed, Result: "new-a"},
					{SubTaskID: "b", Index: 1, Status: SubTaskDone, Result: "new-b"},
				}},
			},
			want: []SubTaskResult{
				{SubTaskID: "a", Index: 0, Status: SubTaskFailed, Result: "new-a"},
				{SubTaskID: "b", Index: 1, Status: SubTaskDone, Result: "new-b"},
				{SubTaskID: "c", Index: 2, Status: SubTaskDone, Result: "new-c"},
			},
		},
		{
			name: "last write wins per id within batch",
			updates: []StateUpdate{
				{SubTaskResults: []SubTaskResult{
					{SubTaskID: "a", Index: 0, Status: SubTaskFailed, Result: "old"},
				}},
				{SubTaskResults: []SubTaskResult{
					{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "new"},
				}},
			},
			want: []SubTaskResult{
				{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "new"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyStateUpdates(HermesState{SubTaskResults: tt.current}, tt.updates)
			if err != nil {
				t.Fatalf("ApplyStateUpdates: %v", err)
			}
			if !reflect.DeepEqual(got.SubTaskResults, tt.want) {
				t.Fatalf("SubTaskResults = %#v, want %#v", got.SubTaskResults, tt.want)
			}
		})
	}
}

func TestApplyStateUpdatesSubTaskResultsIdempotentReplay(t *testing.T) {
	update := []StateUpdate{
		{SubTaskResults: []SubTaskResult{
			{SubTaskID: "a", Index: 0, Status: SubTaskDone, Result: "done"},
			{SubTaskID: "b", Index: 1, Status: SubTaskDone, Result: "done"},
		}},
	}
	first, err := ApplyStateUpdates(HermesState{}, update)
	if err != nil {
		t.Fatalf("ApplyStateUpdates first: %v", err)
	}
	second, err := ApplyStateUpdates(first, update)
	if err != nil {
		t.Fatalf("ApplyStateUpdates second: %v", err)
	}
	if !reflect.DeepEqual(first.SubTaskResults, second.SubTaskResults) {
		t.Fatalf("replay should be idempotent: first=%#v second=%#v", first.SubTaskResults, second.SubTaskResults)
	}
}

func TestExistingMutationsCanBeExpressedAsStateUpdates(t *testing.T) {
	status := TaskStatusExecuting
	current := HermesState{
		TaskID:     "task-1",
		Status:     TaskStatusPlanning,
		CurrentIdx: 0,
	}
	subTask := SubTask{
		ID:         "s1",
		Status:     SubTaskDone,
		Result:     "completed",
		TokensUsed: 17,
		Attempts:   1,
	}

	got, err := ApplyStateUpdates(current, []StateUpdate{
		StateUpdateForSubTaskResult(subTask, 0),
		StateUpdateForAccumulatedAppend("completed"),
		StateUpdateForTaskAdvance(1, status),
	})
	if err != nil {
		t.Fatalf("ApplyStateUpdates: %v", err)
	}
	if got.CurrentIdx != 1 || got.Status != TaskStatusExecuting {
		t.Fatalf("advance mismatch: %+v", got)
	}
	if got.Accumulated != "completed" {
		t.Fatalf("accumulated mismatch: %q", got.Accumulated)
	}
	if len(got.SubTaskResults) != 1 || got.SubTaskResults[0].SubTaskID != "s1" || got.SubTaskResults[0].TokensUsed != 17 {
		t.Fatalf("subtask result mismatch: %+v", got.SubTaskResults)
	}
}
