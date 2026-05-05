package engine

import (
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

func TestDecideTaskResumeFromFirstIncompleteSubTask(t *testing.T) {
	state := hermes.TaskState{
		ID:          "task-1",
		Status:      hermes.TaskStatusExecuting,
		Accumulated: "done summary",
		Plan: []hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskDone, Result: "ok"},
			{ID: "s2", Status: hermes.SubTaskSkipped, Result: "skipped"},
			{ID: "s3", Status: hermes.SubTaskFailed, Result: "failed"},
			{ID: "s4", Status: hermes.SubTaskPending},
		},
	}

	decision := DecideTaskResume(state)
	if !decision.CanResume || decision.Terminal {
		t.Fatalf("decision flags = %+v, want resumable non-terminal", decision)
	}
	if decision.FromIdx != 2 {
		t.Fatalf("FromIdx = %d, want 2", decision.FromIdx)
	}
	if len(decision.Preserved) != 2 {
		t.Fatalf("Preserved = %d, want 2", len(decision.Preserved))
	}
	if len(decision.Remaining) != 2 || decision.Remaining[0].ID != "s3" {
		t.Fatalf("Remaining = %#v, want s3+s4", decision.Remaining)
	}
	if decision.Accumulated != "done summary" {
		t.Fatalf("Accumulated = %q, want original summary", decision.Accumulated)
	}
}

func TestDecideTaskResumeTerminalTask(t *testing.T) {
	decision := DecideTaskResume(hermes.TaskState{
		ID:     "task-done",
		Status: hermes.TaskStatusDone,
		Plan:   []hermes.SubTask{{ID: "s1", Status: hermes.SubTaskDone}},
	})
	if !decision.Terminal || decision.CanResume {
		t.Fatalf("decision = %+v, want terminal non-resumable", decision)
	}
	if decision.Reason != "task_terminal" {
		t.Fatalf("Reason = %q, want task_terminal", decision.Reason)
	}
}

func TestDecideTaskResumeCompletePlanStillExecuting(t *testing.T) {
	decision := DecideTaskResume(hermes.TaskState{
		ID:     "task-complete-plan",
		Status: hermes.TaskStatusExecuting,
		Plan: []hermes.SubTask{
			{ID: "s1", Status: hermes.SubTaskDone},
			{ID: "s2", Status: hermes.SubTaskSkipped},
		},
	})
	if !decision.CanResume {
		t.Fatalf("CanResume = false, want true for mark-done recovery")
	}
	if decision.FromIdx != 2 {
		t.Fatalf("FromIdx = %d, want end of plan", decision.FromIdx)
	}
	if decision.Reason != "plan_complete_mark_done" {
		t.Fatalf("Reason = %q, want plan_complete_mark_done", decision.Reason)
	}
}

func TestDecideTaskResumeMissingPlan(t *testing.T) {
	decision := DecideTaskResume(hermes.TaskState{
		ID:     "task-empty",
		Status: hermes.TaskStatusExecuting,
	})
	if decision.CanResume {
		t.Fatalf("CanResume = true, want false: %+v", decision)
	}
	if decision.Reason != "missing_plan" {
		t.Fatalf("Reason = %q, want missing_plan", decision.Reason)
	}
}
