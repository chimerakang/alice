package engine

import "claude-tg-agent/internal/app/hermes"

type TaskResumeDecision struct {
	TaskID      string
	CanResume   bool
	Terminal    bool
	FromIdx     int
	Preserved   []hermes.SubTask
	Remaining   []hermes.SubTask
	Reason      string
	Accumulated string
}

func DecideTaskResume(state hermes.TaskState) TaskResumeDecision {
	decision := TaskResumeDecision{
		TaskID:      state.ID,
		FromIdx:     len(state.Plan),
		Accumulated: state.Accumulated,
	}
	if state.ID == "" {
		decision.Reason = "missing_task_id"
		return decision
	}
	if state.IsTerminal() {
		decision.Terminal = true
		decision.Reason = "task_terminal"
		return decision
	}
	if len(state.Plan) == 0 {
		decision.Reason = "missing_plan"
		return decision
	}

	for idx, subTask := range state.Plan {
		switch subTask.Status {
		case hermes.SubTaskDone, hermes.SubTaskSkipped:
			decision.Preserved = append(decision.Preserved, subTask)
			continue
		default:
			decision.FromIdx = idx
			decision.Remaining = append(decision.Remaining, state.Plan[idx:]...)
			decision.CanResume = true
			decision.Reason = "resume_from_incomplete_subtask"
			return decision
		}
	}

	decision.CanResume = true
	decision.Reason = "plan_complete_mark_done"
	return decision
}
