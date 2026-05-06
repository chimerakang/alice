package issueops

import (
	"fmt"
	"strings"
	"unicode"

	"claude-tg-agent/internal/app/hermes"
)

// ChecklistMappingConfidence represents the certainty of a sub-task to checklist match.
type ChecklistMappingConfidence string

const (
	// ChecklistMappingConfidenceHigh means the mapping is strong enough to sync automatically.
	ChecklistMappingConfidenceHigh ChecklistMappingConfidence = "high"
	// ChecklistMappingConfidenceLow means the mapping needs human confirmation before sync.
	ChecklistMappingConfidenceLow ChecklistMappingConfidence = "low"
)

// ChecklistMapping links one Hermes sub-task to one issue checklist item.
type ChecklistMapping struct {
	SubTaskIndex              int
	SubTaskID                 string
	SubTaskDescription        string
	ChecklistIndex            int
	ChecklistText             string
	ChecklistLineNumber       int
	Score                     int
	Confidence                ChecklistMappingConfidence
	Reason                    string
	RequiresHumanConfirmation bool
}

// ChecklistMappingResult captures the current checklist matching state.
type ChecklistMappingResult struct {
	Issue                  *hermes.IssueContext
	State                  hermes.IssueState
	NeedsHumanConfirmation bool
	ChecklistSynced        bool
	TotalChecklistItems    int
	TotalSubTasks          int
	Mappings               []ChecklistMapping
	UnmappedSubTasks       []hermes.SubTask
	UnmappedChecklistItems []hermes.ChecklistItem
	Notes                  []string
}

func buildChecklistMapping(issue *hermes.IssueContext, subtasks []hermes.SubTask) ChecklistMappingResult {
	result := ChecklistMappingResult{
		Issue:                  issue,
		TotalSubTasks:          len(subtasks),
		TotalChecklistItems:    0,
		Mappings:               make([]ChecklistMapping, 0, len(subtasks)),
		UnmappedSubTasks:       make([]hermes.SubTask, 0),
		UnmappedChecklistItems: make([]hermes.ChecklistItem, 0),
		Notes:                  make([]string, 0, 4),
	}
	if issue == nil {
		result.State = hermes.IssueStateBlocked
		result.NeedsHumanConfirmation = true
		result.Notes = append(result.Notes, "issue unavailable")
		return result
	}

	result.TotalChecklistItems = len(issue.Checklist)
	if len(issue.Checklist) == 0 {
		result.State = hermes.IssueStateDrafted
		result.Notes = append(result.Notes, "no checklist items found in issue body")
		return result
	}
	if len(subtasks) == 0 {
		result.State = hermes.IssueStatePlanned
		result.UnmappedChecklistItems = append(result.UnmappedChecklistItems, issue.Checklist...)
		result.Notes = append(result.Notes, "no Hermes subtasks available for checklist mapping")
		return result
	}

	usedChecklist := make([]bool, len(issue.Checklist))
	for idx, subTask := range subtasks {
		bestIdx := -1
		bestScore := 0
		secondScore := 0
		bestReason := ""
		for checklistIdx, checklistItem := range issue.Checklist {
			if usedChecklist[checklistIdx] {
				continue
			}
			score, reason := scoreChecklistMapping(subTask.Description, checklistItem.Text)
			if score > bestScore {
				secondScore = bestScore
				bestScore = score
				bestIdx = checklistIdx
				bestReason = reason
				continue
			}
			if score > secondScore {
				secondScore = score
			}
		}

		if bestIdx < 0 || bestScore == 0 {
			result.UnmappedSubTasks = append(result.UnmappedSubTasks, subTask)
			continue
		}

		confidence := ChecklistMappingConfidenceLow
		needsHuman := true
		if bestScore >= 80 && bestScore-secondScore >= 10 {
			confidence = ChecklistMappingConfidenceHigh
			needsHuman = false
		}

		checklistItem := issue.Checklist[bestIdx]
		result.Mappings = append(result.Mappings, ChecklistMapping{
			SubTaskIndex:              idx,
			SubTaskID:                 subTask.ID,
			SubTaskDescription:        subTask.Description,
			ChecklistIndex:            bestIdx,
			ChecklistText:             checklistItem.Text,
			ChecklistLineNumber:       checklistItem.LineNumber,
			Score:                     bestScore,
			Confidence:                confidence,
			Reason:                    bestReason,
			RequiresHumanConfirmation: needsHuman,
		})
		usedChecklist[bestIdx] = true
		if needsHuman {
			result.NeedsHumanConfirmation = true
			result.Notes = append(result.Notes, fmt.Sprintf("low confidence checklist mapping: sub-task %q -> checklist %q (score=%d)", subTask.Description, checklistItem.Text, bestScore))
		}
	}

	for checklistIdx, checklistItem := range issue.Checklist {
		if usedChecklist[checklistIdx] {
			continue
		}
		result.UnmappedChecklistItems = append(result.UnmappedChecklistItems, checklistItem)
	}

	switch {
	case result.NeedsHumanConfirmation:
		result.State = hermes.IssueStateBlocked
	case len(result.UnmappedSubTasks) > 0 || len(result.UnmappedChecklistItems) > 0:
		result.State = hermes.IssueStateChecklistUnsynced
	default:
		result.State = hermes.IssueStateChecklistSynced
		result.ChecklistSynced = true
	}

	if len(result.UnmappedSubTasks) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("%d Hermes sub-task(s) did not map to checklist items", len(result.UnmappedSubTasks)))
	}
	if len(result.UnmappedChecklistItems) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("%d checklist item(s) remain unmatched", len(result.UnmappedChecklistItems)))
	}
	return result
}

func scoreChecklistMapping(subTaskDesc, checklistText string) (int, string) {
	left := normalizeMappingText(subTaskDesc)
	right := normalizeMappingText(checklistText)
	if left == "" || right == "" {
		return 0, "empty normalized text"
	}
	if left == right {
		return 100, "exact normalized match"
	}
	if strings.Contains(left, right) || strings.Contains(right, left) {
		return 95, "substring match"
	}

	leftSet := tokenSet(left)
	rightSet := tokenSet(right)
	if len(leftSet) == 0 || len(rightSet) == 0 {
		return 0, "no tokens"
	}

	intersection := 0
	for token := range leftSet {
		if _, ok := rightSet[token]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0, "no token overlap"
	}

	maxTokens := len(leftSet)
	if len(rightSet) > maxTokens {
		maxTokens = len(rightSet)
	}
	score := intersection * 100 / maxTokens
	return score, fmt.Sprintf("%d shared token(s)", intersection)
}

func normalizeMappingText(text string) string {
	var sb strings.Builder
	lastSpace := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			sb.WriteRune(r)
			lastSpace = false
		case unicode.IsSpace(r):
			if !lastSpace {
				sb.WriteByte(' ')
				lastSpace = true
			}
		default:
			if !lastSpace {
				sb.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func tokenSet(text string) map[string]struct{} {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set
}
