package hermes

import (
	"fmt"
	"sort"
	"time"
)

const accumulatedUpdateSeparator = "\n"

// ApplyStateUpdates merges a batch of node writes into a new canonical state.
// The input state and update slices are not mutated.
func ApplyStateUpdates(current HermesState, updates []StateUpdate) (HermesState, error) {
	next := cloneHermesState(current)
	if len(updates) == 0 {
		return next, nil
	}

	var statusWrite *TaskStatus
	var currentIdxWrite *int
	var planWrite []SubTask
	var githubIssueWrite *int
	var subTaskResultWrites []SubTaskResult

	for i := range updates {
		update := updates[i]
		if update.Status != nil {
			status := *update.Status
			statusWrite = &status
		}
		if update.CurrentIdx != nil {
			currentIdx := *update.CurrentIdx
			currentIdxWrite = &currentIdx
		}
		if update.Plan != nil {
			planWrite = append([]SubTask(nil), update.Plan...)
		}
		if update.Accumulated != nil {
			next.Accumulated = *update.Accumulated
		}
		if update.AccumulatedDelta != "" {
			next.Accumulated = appendAccumulatedDelta(next.Accumulated, update.AccumulatedDelta)
		}
		if len(update.Artifacts) > 0 {
			next.Artifacts = append(next.Artifacts, update.Artifacts...)
		}
		if len(update.ModelUsages) > 0 {
			next.ModelUsages = append(next.ModelUsages, update.ModelUsages...)
		}
		if len(update.PhaseUsages) > 0 {
			next.PhaseUsages = append(next.PhaseUsages, update.PhaseUsages...)
		}
		subTaskResultWrites = append(subTaskResultWrites, update.SubTaskResults...)
		if update.ClearInterrupt {
			next.Interrupt = nil
		}
		if update.Interrupt != nil {
			next.Interrupt = cloneHermesInterrupt(update.Interrupt)
		}
		if len(update.Errors) > 0 {
			next.Errors = append(next.Errors, update.Errors...)
		}
		if update.GithubIssueNumber != nil {
			issueNumber := *update.GithubIssueNumber
			githubIssueWrite = &issueNumber
		}
	}

	if statusWrite != nil {
		if err := ValidateTaskStatusTransition(next.TaskID, next.Status, *statusWrite); err != nil {
			return current, err
		}
		next.Status = *statusWrite
	}
	if currentIdxWrite != nil {
		next.CurrentIdx = *currentIdxWrite
	}
	if planWrite != nil {
		next.Plan = planWrite
	}
	if len(subTaskResultWrites) > 0 {
		next.SubTaskResults = mergeSubTaskResults(next.SubTaskResults, subTaskResultWrites)
	}
	if githubIssueWrite != nil {
		next.GithubIssueNumber = *githubIssueWrite
	}

	return next, nil
}

// StateUpdateForTaskAdvance represents the existing AdvanceTask mutation.
func StateUpdateForTaskAdvance(nextIdx int, status TaskStatus) StateUpdate {
	return StateUpdate{CurrentIdx: &nextIdx, Status: &status}
}

// StateUpdateForAccumulatedAppend represents the existing append-then-update
// accumulated mutation without requiring callers to replace the whole string.
func StateUpdateForAccumulatedAppend(delta string) StateUpdate {
	return StateUpdate{AccumulatedDelta: delta}
}

// StateUpdateForSubTaskResult represents the result portion of UpdateSubTask.
func StateUpdateForSubTaskResult(subTask SubTask, idx int) StateUpdate {
	return StateUpdate{
		SubTaskResults: []SubTaskResult{
			{
				SubTaskID:  subTask.ID,
				Index:      idx,
				Status:     subTask.Status,
				Result:     subTask.Result,
				TokensUsed: subTask.TokensUsed,
				Attempts:   subTask.Attempts,
			},
		},
	}
}

func appendAccumulatedDelta(current string, delta string) string {
	if current == "" {
		return delta
	}
	if delta == "" {
		return current
	}
	if current[len(current)-len(accumulatedUpdateSeparator):] == accumulatedUpdateSeparator {
		return current + delta
	}
	return current + accumulatedUpdateSeparator + delta
}

func mergeSubTaskResults(current []SubTaskResult, incoming []SubTaskResult) []SubTaskResult {
	out := append([]SubTaskResult(nil), current...)
	positions := make(map[string]int, len(out))
	for i := range out {
		positions[subTaskResultKey(out[i])] = i
	}

	collapsed := make(map[string]SubTaskResult, len(incoming))
	order := make([]string, 0, len(incoming))
	for _, result := range incoming {
		key := subTaskResultKey(result)
		if _, ok := collapsed[key]; !ok {
			order = append(order, key)
		}
		collapsed[key] = cloneSubTaskResult(result)
	}

	var additions []SubTaskResult
	for _, key := range order {
		result := collapsed[key]
		if idx, ok := positions[key]; ok {
			out[idx] = overwriteSubTaskResult(out[idx], result)
			continue
		}
		additions = append(additions, result)
	}
	sort.SliceStable(additions, func(i, j int) bool {
		if additions[i].Index == additions[j].Index {
			return subTaskResultKey(additions[i]) < subTaskResultKey(additions[j])
		}
		return additions[i].Index < additions[j].Index
	})
	return append(out, additions...)
}

func overwriteSubTaskResult(existing, update SubTaskResult) SubTaskResult {
	existing.Status = update.Status
	existing.Result = update.Result
	existing.TokensUsed = update.TokensUsed
	existing.Attempts = update.Attempts
	existing.EndedAt = cloneTimePtr(update.EndedAt)
	if update.Index != 0 {
		existing.Index = update.Index
	}
	if update.SubTaskID != "" {
		existing.SubTaskID = update.SubTaskID
	}
	return existing
}

func subTaskResultKey(result SubTaskResult) string {
	if result.SubTaskID != "" {
		return "id:" + result.SubTaskID
	}
	return fmt.Sprintf("idx:%d", result.Index)
}

func cloneHermesState(state HermesState) HermesState {
	state.Plan = append([]SubTask(nil), state.Plan...)
	state.Artifacts = append([]Artifact(nil), state.Artifacts...)
	state.ModelUsages = append([]ModelUsage(nil), state.ModelUsages...)
	state.PhaseUsages = append([]PhaseUsage(nil), state.PhaseUsages...)
	state.SubTaskResults = cloneSubTaskResults(state.SubTaskResults)
	state.Interrupt = cloneHermesInterrupt(state.Interrupt)
	state.Errors = append([]HermesStateError(nil), state.Errors...)
	return state
}

func cloneSubTaskResults(results []SubTaskResult) []SubTaskResult {
	out := make([]SubTaskResult, len(results))
	for i := range results {
		out[i] = cloneSubTaskResult(results[i])
	}
	return out
}

func cloneSubTaskResult(result SubTaskResult) SubTaskResult {
	result.EndedAt = cloneTimePtr(result.EndedAt)
	return result
}

func cloneHermesInterrupt(interrupt *HermesInterrupt) *HermesInterrupt {
	if interrupt == nil {
		return nil
	}
	out := *interrupt
	out.ExpiresAt = cloneTimePtr(interrupt.ExpiresAt)
	return &out
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	out := *t
	return &out
}
