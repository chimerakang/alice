package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// IssueContext holds the GitHub Issue data fetched before starting a Hermes task.
type IssueContext struct {
	Number    int
	Title     string
	Body      string
	Labels    []string
	Checklist []ChecklistItem // parsed from body - [ ] items
}

// ChecklistItem represents one `- [ ]` or `- [x]` line in an Issue body.
type ChecklistItem struct {
	Text       string
	Checked    bool
	LineNumber int // 0-indexed line in Issue body, for sync anchoring
}

// ghIssueJSON is the JSON shape returned by `gh issue view --json title,body,labels`.
type ghIssueJSON struct {
	Title  string             `json:"title"`
	Body   string             `json:"body"`
	Labels []struct{ Name string `json:"name"` } `json:"labels"`
}

// checklistRe matches Markdown task list items: `- [ ] text` or `- [x] text`
var checklistRe = regexp.MustCompile(`(?m)^- \[( |x|X)\] (.+)$`)

// ghCommand builds a gh subcommand rooted at projectDir so `gh` resolves the
// correct repository from that directory's git remote. An empty projectDir
// falls back to the bot's cwd (compatible with older single-project setups).
func ghCommand(ctx context.Context, projectDir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if projectDir != "" {
		cmd.Dir = projectDir
	}
	return cmd
}

// FetchIssue calls `gh issue view N --json title,body,labels` in projectDir
// and returns parsed data. projectDir must match the chat's configured repo
// so gh resolves the correct remote; empty string falls back to cwd.
func FetchIssue(ctx context.Context, projectDir string, number int) (*IssueContext, error) {
	out, err := ghCommand(ctx, projectDir, "issue", "view",
		fmt.Sprintf("%d", number),
		"--json", "title,body,labels",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue view %d: %w", number, err)
	}

	var raw ghIssueJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse issue JSON: %w", err)
	}

	labels := make([]string, len(raw.Labels))
	for i, l := range raw.Labels {
		labels[i] = l.Name
	}

	return &IssueContext{
		Number:    number,
		Title:     raw.Title,
		Body:      raw.Body,
		Labels:    labels,
		Checklist: ExtractChecklist(raw.Body),
	}, nil
}

// ExtractChecklist parses `- [ ]` and `- [x]` items from a Markdown body with line numbers.
func ExtractChecklist(body string) []ChecklistItem {
	lines := strings.Split(body, "\n")
	items := make([]ChecklistItem, 0)
	for lineIdx, line := range lines {
		m := checklistRe.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}
		items = append(items, ChecklistItem{
			Text:       strings.TrimSpace(m[2]),
			Checked:    strings.ToLower(m[1]) == "x",
			LineNumber: lineIdx,
		})
	}
	return items
}

// BuildGoalFromIssue assembles the Planner goal string from an Issue.
// If the issue has an unchecked checklist, the Planner is instructed to use it.
func BuildGoalFromIssue(issue *IssueContext) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[GitHub #%d] %s\n\n", issue.Number, issue.Title))

	hasChecklist := false
	for _, item := range issue.Checklist {
		if !item.Checked {
			hasChecklist = true
			break
		}
	}

	if hasChecklist {
		sb.WriteString("Issue has an existing unchecked task list — use these as SubTask descriptions:\n")
		for _, item := range issue.Checklist {
			if !item.Checked {
				sb.WriteString("  - ")
				sb.WriteString(item.Text)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}

	if issue.Body != "" {
		sb.WriteString("Full issue description:\n")
		sb.WriteString(issue.Body)
	}

	return sb.String()
}

// UpdateChecklistInBody replaces `- [ ] <text>` with `- [x] <text>` for each
// completedDesc entry (case-insensitive prefix match).
func UpdateChecklistInBody(body string, completedDescs []string) string {
	return checklistRe.ReplaceAllStringFunc(body, func(match string) string {
		m := checklistRe.FindStringSubmatch(match)
		if len(m) < 3 || strings.ToLower(m[1]) == "x" {
			return match // already checked
		}
		itemText := strings.TrimSpace(m[2])
		for _, desc := range completedDescs {
			if strings.EqualFold(itemText, strings.TrimSpace(desc)) ||
				strings.HasPrefix(strings.ToLower(itemText), strings.ToLower(strings.TrimSpace(desc))[:min16(len(desc))]) {
				return "- [x] " + m[2]
			}
		}
		return match
	})
}

func min16(n int) int {
	if n > 16 {
		return 16
	}
	return n
}

// PostComment posts a comment to the given Issue via `gh issue comment`.
func PostComment(ctx context.Context, projectDir string, number int, body string) error {
	cmd := ghCommand(ctx, projectDir, "issue", "comment",
		fmt.Sprintf("%d", number),
		"--body", body,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue comment %d: %w (output: %s)", number, err, out)
	}
	return nil
}

// SyncChecklist fetches the current Issue body and checks off completed sub-tasks.
func SyncChecklist(ctx context.Context, projectDir string, number int, subtasks []SubTask) error {
	// Fetch current body
	out, err := ghCommand(ctx, projectDir, "issue", "view",
		fmt.Sprintf("%d", number),
		"--json", "body",
	).Output()
	if err != nil {
		return fmt.Errorf("gh issue view body: %w", err)
	}

	var raw struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return fmt.Errorf("parse body JSON: %w", err)
	}

	var completed []string
	for _, st := range subtasks {
		if st.Status == SubTaskDone {
			completed = append(completed, st.Description)
		}
	}
	if len(completed) == 0 {
		return nil
	}

	updatedBody := UpdateChecklistInBody(raw.Body, completed)
	if updatedBody == raw.Body {
		return nil // nothing changed
	}

	cmd := ghCommand(ctx, projectDir, "issue", "edit",
		fmt.Sprintf("%d", number),
		"--body", updatedBody,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue edit checklist: %w (output: %s)", err, out)
	}
	return nil
}

// ApplyLabel adds a label to the Issue (creates if needed via gh's built-in behaviour).
func ApplyLabel(ctx context.Context, projectDir string, number int, label string) error {
	cmd := ghCommand(ctx, projectDir, "issue", "edit",
		fmt.Sprintf("%d", number),
		"--add-label", label,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh apply label %q to #%d: %w (output: %s)", label, number, err, out)
	}
	return nil
}

// CloseIssue closes the Issue.
func CloseIssue(ctx context.Context, projectDir string, number int) error {
	cmd := ghCommand(ctx, projectDir, "issue", "close",
		fmt.Sprintf("%d", number),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue close #%d: %w (output: %s)", number, err, out)
	}
	return nil
}

// HasLabel returns true if the given label is present in the issue's label list.
func HasLabel(issue *IssueContext, label string) bool {
	for _, l := range issue.Labels {
		if strings.EqualFold(l, label) {
			return true
		}
	}
	return false
}

// WritePlanToIssue writes the generated sub-task plan as a checklist to the Issue body.
// This is called when the Issue had no checklist and the Planner generated a plan.
func WritePlanToIssue(ctx context.Context, projectDir string, number int, originalBody string, tasks []SubTask) error {
	var sb strings.Builder
	sb.WriteString(originalBody)
	if !strings.HasSuffix(originalBody, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n## Hermes 執行計劃\n\n")
	for _, t := range tasks {
		sb.WriteString("- [ ] ")
		sb.WriteString(t.Description)
		sb.WriteString("\n")
	}

	cmd := ghCommand(ctx, projectDir, "issue", "edit",
		fmt.Sprintf("%d", number),
		"--body", sb.String(),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh issue edit plan: %w (output: %s)", err, out)
	}
	return nil
}

// ── Comment builders ──────────────────────────────────────────────────────────

// CommentStarted builds the "Hermes started" lifecycle comment.
func CommentStarted(plannerModel, executorModel string) string {
	return fmt.Sprintf("🤖 **Hermes 開始執行**\n\n- Planner: `%s`\n- Executor: `%s`\n- 開始時間: %s",
		plannerModel, executorModel,
		time.Now().Format("2006-01-02 15:04:05"))
}

// CommentDone builds the "all done" lifecycle comment.
func CommentDone(state TaskState) string {
	var sb strings.Builder
	done := 0
	for _, st := range state.Plan {
		if st.Status == SubTaskDone {
			done++
		}
	}
	elapsed := ""
	if !state.TokenBudget.StartedAt.IsZero() {
		elapsed = time.Since(state.TokenBudget.StartedAt).Round(time.Second).String()
	}

	sb.WriteString(fmt.Sprintf("✅ **Hermes 完成** %d/%d SubTasks\n\n", done, len(state.Plan)))
	sb.WriteString(fmt.Sprintf("- Token 用量: %d", state.TokenBudget.UsedTokens))
	if state.TokenBudget.MaxTotalTokens > 0 {
		sb.WriteString(fmt.Sprintf("/%d", state.TokenBudget.MaxTotalTokens))
	}
	sb.WriteString("\n")
	if elapsed != "" {
		sb.WriteString(fmt.Sprintf("- 執行時間: %s\n", elapsed))
	}

	if len(state.Artifacts) > 0 {
		sb.WriteString("\n**修改的檔案：**\n")
		for _, a := range state.Artifacts {
			if a.Hash != "" {
				sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", a.Path, a.Hash[:min8(len(a.Hash))]))
			} else {
				sb.WriteString(fmt.Sprintf("- `%s`\n", a.Path))
			}
		}
	}
	return sb.String()
}

// CommentFailed builds the "task failed" lifecycle comment.
func CommentFailed(failedDesc, reason string, doneCount, totalCount int) string {
	return fmt.Sprintf("❌ **Hermes 執行失敗**\n\n"+
		"- 失敗於子任務: `%s`\n"+
		"- 原因: %s\n"+
		"- 已完成: %d/%d\n\n"+
		"建議：修正上述問題後重新執行 `/hermes #<issue>`",
		failedDesc, reason, doneCount, totalCount)
}

// CommentBudgetExceeded builds the "budget exceeded" lifecycle comment.
func CommentBudgetExceeded(used, max int) string {
	return fmt.Sprintf("⚠️ **Hermes Budget 耗盡**\n\n"+
		"- Token 用量: %d / %d\n\n"+
		"可增加 `hermes.budget.max_total_tokens` 後重新執行。",
		used, max)
}

// CommentSubTaskProgress builds a per-subtask progress comment.
func CommentSubTaskProgress(idx, totalCount int, subTask SubTask, resultText string, execTokens, completedCount int) string {
	return fmt.Sprintf("✅ **子任務進度** (%d/%d)\n\n"+
		"- 子任務: `%s`\n"+
		"- 結果: %s\n"+
		"- Token 用量: %d\n"+
		"- 完成進度: %d/%d\n",
		idx+1, totalCount, subTask.Description, resultText, execTokens, completedCount, totalCount)
}

// PostCommentEvent posts a lifecycle event comment to the Issue.
// event must be one of: "start", "complete", "fail", "budget_exceeded".
// payload type depends on the event:
//   - "start": map[string]string with keys "planner_model", "executor_model"
//   - "complete": TaskState
//   - "fail": map[string]interface{} with keys "failed_description" (string), "reason" (string), "done_count" (int), "total_count" (int)
//   - "budget_exceeded": map[string]int with keys "used_tokens", "max_tokens"
func PostCommentEvent(ctx context.Context, projectDir string, number int, event string, payload interface{}) error {
	var body string
	switch event {
	case "start":
		pm := payload.(map[string]string)
		body = CommentStarted(pm["planner_model"], pm["executor_model"])
	case "complete":
		state := payload.(TaskState)
		body = CommentDone(state)
	case "fail":
		p := payload.(map[string]interface{})
		failedDesc := p["failed_description"].(string)
		reason := p["reason"].(string)
		doneCount := p["done_count"].(int)
		totalCount := p["total_count"].(int)
		body = CommentFailed(failedDesc, reason, doneCount, totalCount)
	case "budget_exceeded":
		p := payload.(map[string]int)
		body = CommentBudgetExceeded(p["used_tokens"], p["max_tokens"])
	default:
		return fmt.Errorf("unknown event type: %s", event)
	}

	return PostComment(ctx, projectDir, number, body)
}
