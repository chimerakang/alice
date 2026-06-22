package issueops

import (
	"fmt"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

func buildEvidenceRecords(req RecordEvidenceRequest) []EvidenceRecord {
	if len(req.Evidence) > 0 {
		return append([]EvidenceRecord(nil), req.Evidence...)
	}

	records := make([]EvidenceRecord, 0, 3)
	if req.SubTask.Description != "" || req.Result != "" || req.Tokens > 0 || req.Completed > 0 {
		summary := req.SubTask.Description
		if summary == "" {
			summary = fmt.Sprintf("sub-task %d/%d", req.Index+1, req.Total)
		}
		detailParts := make([]string, 0, 3)
		if strings.TrimSpace(req.Result) != "" {
			detailParts = append(detailParts, fmt.Sprintf("result=%s", strings.TrimSpace(req.Result)))
		}
		if req.Tokens > 0 {
			detailParts = append(detailParts, fmt.Sprintf("tokens=%d", req.Tokens))
		}
		if req.Completed > 0 {
			detailParts = append(detailParts, fmt.Sprintf("completed=%d", req.Completed))
		}
		records = append(records, EvidenceRecord{
			Kind:      EvidenceKindSubTask,
			Summary:   summary,
			Detail:    strings.Join(detailParts, ", "),
			Reference: traceSubTaskReference(req.SubTask, req.Index, req.Total),
		})
	}
	if req.Review != nil {
		summary := strings.TrimSpace(req.Review.Verdict)
		if req.Review.Score > 0 {
			if summary != "" {
				summary = fmt.Sprintf("%s score=%d", summary, req.Review.Score)
			} else {
				summary = fmt.Sprintf("score=%d", req.Review.Score)
			}
		}
		records = append(records, EvidenceRecord{
			Kind:      EvidenceKindReview,
			Summary:   summary,
			Detail:    strings.TrimSpace(joinNonEmpty(req.Review.Summary, req.Review.Detail)),
			Reference: strings.TrimSpace(req.Review.Reference),
		})
	}
	if req.Validation != nil {
		summary := strings.TrimSpace(req.Validation.Command)
		if summary == "" {
			summary = "validation"
		}
		status := "failed"
		if req.Validation.Passed {
			status = "passed"
		}
		detail := fmt.Sprintf("%s exit=%d", status, req.Validation.ExitCode)
		if strings.TrimSpace(req.Validation.Output) != "" {
			detail = detail + " | " + strings.TrimSpace(req.Validation.Output)
		}
		records = append(records, EvidenceRecord{
			Kind:      EvidenceKindValidation,
			Summary:   summary,
			Detail:    detail,
			Reference: strings.TrimSpace(req.Validation.Reference),
		})
	}
	return records
}

func traceSubTaskReference(task hermes.SubTask, idx, total int) string {
	if strings.TrimSpace(task.ID) != "" {
		return "subtask:" + strings.TrimSpace(task.ID)
	}
	return fmt.Sprintf("subtask:%d/%d", idx+1, total)
}

func joinNonEmpty(parts ...string) string {
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		items = append(items, strings.TrimSpace(part))
	}
	return strings.Join(items, " | ")
}

func buildEvidenceMappingSummary(mapping *ChecklistMapping, records []EvidenceRecord) string {
	if mapping == nil && len(records) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Evidence mapping summary:\n")
	if mapping != nil {
		sb.WriteString(fmt.Sprintf("- Checklist item: %s", strings.TrimSpace(mapping.ChecklistText)))
		if mapping.ChecklistLineNumber >= 0 {
			sb.WriteString(fmt.Sprintf(" (line %d)", mapping.ChecklistLineNumber+1))
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("- Hermes sub-task: %s\n", strings.TrimSpace(mapping.SubTaskDescription)))
		if strings.TrimSpace(mapping.SubTaskID) != "" {
			sb.WriteString(fmt.Sprintf("- Sub-task ID: %s\n", strings.TrimSpace(mapping.SubTaskID)))
		}
		sb.WriteString(fmt.Sprintf("- Confidence: %s (%d)\n", mapping.Confidence, mapping.Score))
	}
	if len(records) > 0 {
		sb.WriteString("- Evidence sources:\n")
		for _, record := range records {
			sb.WriteString("  - ")
			sb.WriteString(string(record.Kind))
			if strings.TrimSpace(record.Summary) != "" {
				sb.WriteString(": ")
				sb.WriteString(strings.TrimSpace(record.Summary))
			}
			if strings.TrimSpace(record.Reference) != "" {
				sb.WriteString(" [")
				sb.WriteString(strings.TrimSpace(record.Reference))
				sb.WriteString("]")
			}
			sb.WriteString("\n")
			if strings.TrimSpace(record.Detail) != "" {
				sb.WriteString("    ")
				sb.WriteString(strings.TrimSpace(record.Detail))
				sb.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(sb.String())
}
