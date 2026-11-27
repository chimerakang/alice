package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appengine "github.com/chimerakang/alice-core/engine"
	"github.com/chimerakang/alice-core/ghissue"
)

type issueQualityGateAction string

const (
	issueQualityGateAllow              issueQualityGateAction = "allow"
	issueQualityGateSkip               issueQualityGateAction = "skip"
	issueQualityGateNeedsClarification issueQualityGateAction = "needs_clarification"
)

type issueQualityGateDecision struct {
	Action  issueQualityGateAction
	Reason  string
	Message string
	Signal  *ghissue.HermesCompletionSignal
}

type issueQualityGatePayload struct {
	Action           issueQualityGateAction `json:"action"`
	Reason           string                 `json:"reason"`
	ForceRestart     bool                   `json:"force_restart"`
	IssueState       string                 `json:"issue_state,omitempty"`
	ChecklistTotal   int                    `json:"checklist_total"`
	UncheckedCount   int                    `json:"unchecked_count"`
	CheckedCount     int                    `json:"checked_count"`
	CommentCount     int                    `json:"comment_count"`
	CompletionDone   int                    `json:"completion_done,omitempty"`
	CompletionTotal  int                    `json:"completion_total,omitempty"`
	CompletionAt     string                 `json:"completion_at,omitempty"`
	CompletionAuthor string                 `json:"completion_author,omitempty"`
}

func decideIssueQualityGate(issue *ghissue.IssueContext, forceRestart bool) issueQualityGateDecision {
	if issue == nil {
		return issueQualityGateDecision{Action: issueQualityGateAllow, Reason: "issue_unavailable"}
	}
	state := strings.ToLower(strings.TrimSpace(issue.State))
	if state == "closed" && !forceRestart {
		return issueQualityGateDecision{
			Action:  issueQualityGateSkip,
			Reason:  "issue_closed",
			Message: fmt.Sprintf("ℹ️ Issue #%d 已經是 closed，已停止本輪以避免重複消耗 token。若要強制重跑，請使用 restart/fresh 流程。", issue.Number),
		}
	}
	if signal, ok := ghissue.RecentSuccessfulHermesCompletion(issue); ok && !forceRestart {
		return issueQualityGateDecision{
			Action:  issueQualityGateSkip,
			Reason:  "recent_hermes_completion",
			Message: formatRecentCompletionSkipMessage(issue, signal),
			Signal:  &signal,
		}
	}
	if issueNeedsAcceptanceClarification(issue) && !forceRestart {
		return issueQualityGateDecision{
			Action:  issueQualityGateNeedsClarification,
			Reason:  "missing_acceptance_context",
			Message: fmt.Sprintf("⚠️ Issue #%d 缺少可判斷的描述、checklist 或近期 comments。先不要派工，以免 Agent 只能猜。請補 acceptance criteria，或使用 restart/fresh 流程強制執行。", issue.Number),
		}
	}
	return issueQualityGateDecision{Action: issueQualityGateAllow, Reason: "gate_passed"}
}

func issueNeedsAcceptanceClarification(issue *ghissue.IssueContext) bool {
	if issue == nil {
		return false
	}
	if len(issue.Checklist) > 0 || len(issue.Comments) > 0 {
		return false
	}
	body := strings.TrimSpace(issue.Body)
	if body == "" {
		return true
	}
	return len([]rune(body)) < 80
}

func recordIssueQualityGateDecision(ctx context.Context, key chatKey, issue *ghissue.IssueContext, decision issueQualityGateDecision, forceRestart bool) {
	if issue == nil {
		return
	}
	payload := issueQualityGatePayload{
		Action:         decision.Action,
		Reason:         strings.TrimSpace(decision.Reason),
		ForceRestart:   forceRestart,
		IssueState:     strings.TrimSpace(issue.State),
		ChecklistTotal: len(issue.Checklist),
		CommentCount:   len(issue.Comments),
	}
	for _, item := range issue.Checklist {
		if item.Checked {
			payload.CheckedCount++
		} else {
			payload.UncheckedCount++
		}
	}
	if decision.Signal != nil {
		payload.CompletionDone = decision.Signal.Done
		payload.CompletionTotal = decision.Signal.Total
		payload.CompletionAuthor = strings.TrimSpace(decision.Signal.Author)
		if !decision.Signal.CreatedAt.IsZero() {
			payload.CompletionAt = decision.Signal.CreatedAt.Format(time.RFC3339)
		}
	}
	recordRuntimeEvent(ctx, appengine.Event{
		Type:      "IssueQualityGate",
		Timestamp: time.Now(),
		ChatID:    key.chatID,
		ThreadID:  key.threadID,
		Issue:     issue.Number,
		Payload:   payload,
	})
}

func formatRecentCompletionSkipMessage(issue *ghissue.IssueContext, signal ghissue.HermesCompletionSignal) string {
	when := ""
	if !signal.CreatedAt.IsZero() {
		when = " at " + signal.CreatedAt.Format(time.RFC3339)
	}
	state := strings.ToUpper(strings.TrimSpace(issue.State))
	if state == "" {
		state = "UNKNOWN"
	}
	msg := fmt.Sprintf("ℹ️ Issue #%d 的最新一次 Hermes 紀錄顯示已完成（%d/%d SubTasks%s），已停止本輪以避免重複消耗 token。", issue.Number, signal.Done, signal.Total, when)
	if state != "CLOSED" {
		msg += fmt.Sprintf("\n\n注意：GitHub Issue #%d 目前仍是 %s。若確認驗收已完成，請使用 `/close #%d` 關閉；若還有缺口請強制重跑。", issue.Number, state, issue.Number)
		return msg
	}
	return msg + "\n\nIssue 已是 CLOSED；若要重做請另開 issue 或重新開啟。"
}
