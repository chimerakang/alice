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
			next.ModelUsages = mergeModelUsages(next.ModelUsages, update.ModelUsages)
		}
		if len(update.PhaseUsages) > 0 {
			next.PhaseUsages = mergePhaseUsages(next.PhaseUsages, update.PhaseUsages)
		}
		if update.TokenUsageDelta != 0 {
			next.TokenBudget.UsedTokens += update.TokenUsageDelta
		}
		if update.BudgetStartedAt != nil {
			next.TokenBudget.StartedAt = *update.BudgetStartedAt
		}
		if update.PlannerSessionID != nil {
			next.PlannerSessionID = *update.PlannerSessionID
		}
		if update.ExecutorSessionID != nil {
			next.ExecutorSessionID = *update.ExecutorSessionID
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
		if update.ClearReplan {
			next.Replan = nil
		}
		if update.Replan != nil {
			next.Replan = cloneReplanContext(update.Replan)
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

func collapseStateUpdates(updates []StateUpdate) StateUpdate {
	var collapsed StateUpdate
	for _, update := range updates {
		if update.Status != nil {
			status := *update.Status
			collapsed.Status = &status
		}
		if update.CurrentIdx != nil {
			currentIdx := *update.CurrentIdx
			collapsed.CurrentIdx = &currentIdx
		}
		if update.Plan != nil {
			collapsed.Plan = append([]SubTask(nil), update.Plan...)
		}
		if update.Accumulated != nil {
			accumulated := *update.Accumulated
			collapsed.Accumulated = &accumulated
		}
		if update.AccumulatedDelta != "" {
			collapsed.AccumulatedDelta = appendAccumulatedDelta(collapsed.AccumulatedDelta, update.AccumulatedDelta)
		}
		collapsed.Artifacts = append(collapsed.Artifacts, update.Artifacts...)
		collapsed.ModelUsages = append(collapsed.ModelUsages, update.ModelUsages...)
		collapsed.PhaseUsages = append(collapsed.PhaseUsages, update.PhaseUsages...)
		collapsed.SubTaskResults = append(collapsed.SubTaskResults, update.SubTaskResults...)
		if update.ClearInterrupt {
			collapsed.ClearInterrupt = true
			collapsed.Interrupt = nil
		}
		if update.Interrupt != nil {
			collapsed.Interrupt = cloneHermesInterrupt(update.Interrupt)
			collapsed.ClearInterrupt = false
		}
		if update.ClearReplan {
			collapsed.ClearReplan = true
			collapsed.Replan = nil
		}
		if update.Replan != nil {
			collapsed.Replan = cloneReplanContext(update.Replan)
			collapsed.ClearReplan = false
		}
		collapsed.Errors = append(collapsed.Errors, update.Errors...)
		if update.GithubIssueNumber != nil {
			issueNumber := *update.GithubIssueNumber
			collapsed.GithubIssueNumber = &issueNumber
		}
		collapsed.TokenUsageDelta += update.TokenUsageDelta
		if update.BudgetStartedAt != nil {
			ts := *update.BudgetStartedAt
			collapsed.BudgetStartedAt = &ts
		}
		if update.PlannerSessionID != nil {
			s := *update.PlannerSessionID
			collapsed.PlannerSessionID = &s
		}
		if update.ExecutorSessionID != nil {
			s := *update.ExecutorSessionID
			collapsed.ExecutorSessionID = &s
		}
	}
	return collapsed
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

// mergeModelUsages applies StateUpdate.ModelUsages onto an existing list,
// summing token / cost fields and bumping CallCount when the Model key
// already exists. Mirrors the read-modify-write logic in
// SQLiteTaskStore.AddModelUsageBreakdown so reducer-driven and legacy
// callsites converge on the same totals.
func mergeModelUsages(current []ModelUsage, incoming []ModelUsage) []ModelUsage {
	out := append([]ModelUsage(nil), current...)
	idx := make(map[string]int, len(out))
	for i := range out {
		idx[out[i].Model] = i
	}
	for _, delta := range incoming {
		if i, ok := idx[delta.Model]; ok {
			out[i].InputTokens += delta.InputTokens
			out[i].UncachedInputTokens += delta.UncachedInputTokens
			out[i].CacheReadInputTokens += delta.CacheReadInputTokens
			out[i].CacheCreationInputTokens += delta.CacheCreationInputTokens
			out[i].OutputTokens += delta.OutputTokens
			out[i].CostUSD += delta.CostUSD
			callCount := delta.CallCount
			if callCount <= 0 {
				callCount = 1
			}
			out[i].CallCount += callCount
			continue
		}
		entry := delta
		if entry.CallCount <= 0 {
			entry.CallCount = 1
		}
		out = append(out, entry)
		idx[entry.Model] = len(out) - 1
	}
	return out
}

// mergePhaseUsages mirrors mergeModelUsages keyed by (Phase, Model).
func mergePhaseUsages(current []PhaseUsage, incoming []PhaseUsage) []PhaseUsage {
	out := append([]PhaseUsage(nil), current...)
	idx := make(map[string]int, len(out))
	key := func(p PhaseUsage) string { return p.Phase + "\x00" + p.Model }
	for i := range out {
		idx[key(out[i])] = i
	}
	for _, delta := range incoming {
		k := key(delta)
		if i, ok := idx[k]; ok {
			out[i].InputTokens += delta.InputTokens
			out[i].UncachedInputTokens += delta.UncachedInputTokens
			out[i].CacheReadInputTokens += delta.CacheReadInputTokens
			out[i].CacheCreationInputTokens += delta.CacheCreationInputTokens
			out[i].OutputTokens += delta.OutputTokens
			out[i].CostUSD += delta.CostUSD
			callCount := delta.CallCount
			if callCount <= 0 {
				callCount = 1
			}
			out[i].CallCount += callCount
			continue
		}
		entry := delta
		if entry.CallCount <= 0 {
			entry.CallCount = 1
		}
		out = append(out, entry)
		idx[k] = len(out) - 1
	}
	return out
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
	state.Replan = cloneReplanContext(state.Replan)
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

func cloneReplanContext(rc *ReplanContext) *ReplanContext {
	if rc == nil {
		return nil
	}
	out := *rc
	out.PreservedSubTasks = append([]SubTask(nil), rc.PreservedSubTasks...)
	return &out
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
