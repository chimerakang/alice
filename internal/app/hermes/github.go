package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// IssueContext holds the GitHub Issue data fetched before starting a Hermes task.
type IssueContext struct {
	Number    int
	Title     string
	Body      string
	State     string
	Labels    []string
	Checklist []ChecklistItem // parsed from body - [ ] items
	Comments  []IssueComment
}

// IssueComment is a compact GitHub issue comment shape used for state
// reconstruction before planning.
type IssueComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

// ChecklistItem represents one `- [ ]` or `- [x]` line in an Issue body.
type ChecklistItem struct {
	Text       string
	Checked    bool
	LineNumber int // 0-indexed line in Issue body, for sync anchoring
}

// IssueReconciliation is the post-run view of whether the Hermes job also
// satisfied the linked GitHub issue checklist.
type IssueReconciliation struct {
	IssueNumber    int
	IssueState     string
	ChecklistTotal int
	CheckedCount   int
	Unchecked      []ChecklistItem
}

// ghIssueJSON is the JSON shape returned by `gh issue view --json ...`.
type ghIssueJSON struct {
	State  string `json:"state"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Comments []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"createdAt"`
	} `json:"comments"`
}

// IssueListItem is a lightweight summary of one open issue.
type IssueListItem struct {
	Number    int
	Title     string
	Labels    []string
	Milestone string
	UpdatedAt time.Time
	Priority  int // computed score: higher = more urgent
}

// ghIssueListJSON is the JSON shape from `gh issue list --json`.
type ghIssueListJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// checklistRe matches Markdown task list items: `- [ ] text` or `- [x] text`
var checklistRe = regexp.MustCompile(`(?m)^- \[( |x|X)\] (.+)$`)
var hermesCompleteRe = regexp.MustCompile(`Hermes 完成\*\*\s+(\d+)/(\d+)\s+SubTasks`)

// gh timeouts cap every `gh` invocation so a hung CLI (network stall,
// API rate-limit, malformed argument) cannot block the caller forever. The
// timeout scales with the operation: list/search calls fan out across many
// issues and need more headroom; single-issue reads and mutations finish
// fast. (H) Replaced the previous flat 30 s ghDefaultTimeout — large repos
// hit the cap on `gh issue list` while every other op only needed a few
// seconds.
const (
	ghTimeoutShort  = 10 * time.Second // single-issue reads / mutations
	ghTimeoutNormal = 30 * time.Second // default for unclassified ops
	ghTimeoutLong   = 90 * time.Second // list/search across issues
)

const ghOutputLimit = 256 * 1024

func ghProcessOptions(projectDir string, timeout time.Duration) ProcessOptions {
	if timeout <= 0 {
		timeout = ghTimeoutNormal
	}
	return ProcessOptions{
		Dir:         projectDir,
		Timeout:     timeout,
		OutputLimit: ghOutputLimit,
	}
}

// ghOutput runs a gh subcommand rooted at projectDir so `gh` resolves the
// correct repository from that directory's git remote. An empty projectDir
// falls back to the bot's cwd (compatible with older single-project setups).
// timeout=0 picks ghTimeoutNormal.
func ghOutput(ctx context.Context, projectDir string, timeout time.Duration, args ...string) ([]byte, error) {
	return runProcessOutput(ctx, ghProcessOptions(projectDir, timeout), "gh", args...)
}

func ghCombinedOutput(ctx context.Context, projectDir string, timeout time.Duration, args ...string) ([]byte, error) {
	return runProcessCombinedOutput(ctx, ghProcessOptions(projectDir, timeout), "gh", args...)
}

// ListIssues fetches open issues and returns them sorted by computed priority:
// bug label > has milestone > recently updated. limit=0 defaults to 15.
func ListIssues(ctx context.Context, projectDir string, limit int) ([]IssueListItem, error) {
	if limit <= 0 {
		limit = 15
	}
	out, err := ghOutput(ctx, projectDir, ghTimeoutLong, "issue", "list",
		"--state", "open",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "number,title,labels,milestone,updatedAt",
	)
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}

	var raw []ghIssueListJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse issue list JSON: %w", err)
	}

	items := make([]IssueListItem, 0, len(raw))
	for _, r := range raw {
		labels := make([]string, len(r.Labels))
		for i, l := range r.Labels {
			labels[i] = l.Name
		}
		milestone := ""
		if r.Milestone != nil {
			milestone = r.Milestone.Title
		}

		score := 0
		for _, l := range labels {
			switch strings.ToLower(l) {
			case "bug":
				score += 10
			case "blocked":
				score -= 5
			case "priority: high", "high priority":
				score += 8
			case "priority: medium", "medium priority":
				score += 4
			}
		}
		if milestone != "" {
			score += 5
		}
		// Recency bonus: issues updated within last 3 days get +3
		if time.Since(r.UpdatedAt) < 72*time.Hour {
			score += 3
		}

		items = append(items, IssueListItem{
			Number:    r.Number,
			Title:     r.Title,
			Labels:    labels,
			Milestone: milestone,
			UpdatedAt: r.UpdatedAt,
			Priority:  score,
		})
	}

	// Sort: higher priority first, then more recent
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && (items[j].Priority > items[j-1].Priority ||
			(items[j].Priority == items[j-1].Priority && items[j].UpdatedAt.After(items[j-1].UpdatedAt))); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	return items, nil
}

// FormatIssueList renders a Telegram-friendly issue list message.
func FormatIssueList(items []IssueListItem) string {
	if len(items) == 0 {
		return "✅ 目前沒有未解決的 Issue。"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 *開放 Issues（共 %d 個，依優先排序）*\n\n", len(items)))
	for i, item := range items {
		// Priority indicator
		indicator := "⚪"
		switch {
		case item.Priority >= 13:
			indicator = "🔴"
		case item.Priority >= 8:
			indicator = "🟠"
		case item.Priority >= 3:
			indicator = "🟡"
		}

		sb.WriteString(fmt.Sprintf("%s `#%d` %s\n", indicator, item.Number, item.Title))
		if len(item.Labels) > 0 {
			sb.WriteString(fmt.Sprintf("   🏷 %s", strings.Join(item.Labels, ", ")))
		}
		if item.Milestone != "" {
			sb.WriteString(fmt.Sprintf("  📌 %s", item.Milestone))
		}
		sb.WriteString("\n")
		if i < len(items)-1 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n💡 使用 `/hermes #<編號>` 立即啟動執行。")
	return sb.String()
}

// FetchIssue calls `gh issue view N --json title,body,state,labels,comments` in projectDir
// and returns parsed data. projectDir must match the chat's configured repo
// so gh resolves the correct remote; empty string falls back to cwd.
func FetchIssue(ctx context.Context, projectDir string, number int) (*IssueContext, error) {
	out, err := ghOutput(ctx, projectDir, ghTimeoutShort, "issue", "view",
		fmt.Sprintf("%d", number),
		"--json", "title,body,state,labels,comments",
	)
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
	comments := make([]IssueComment, 0, len(raw.Comments))
	for _, c := range raw.Comments {
		comments = append(comments, IssueComment{
			Author:    c.Author.Login,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}

	return &IssueContext{
		Number:    number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     raw.State,
		Labels:    labels,
		Checklist: ExtractChecklist(raw.Body),
		Comments:  comments,
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

// ReconcileIssueCompletion summarizes the current issue checklist after a
// Hermes run. A Hermes task can be done while the issue still has unchecked
// acceptance criteria; callers use this to keep those lifecycles separate.
func ReconcileIssueCompletion(issue *IssueContext) IssueReconciliation {
	if issue == nil {
		return IssueReconciliation{}
	}
	rec := IssueReconciliation{
		IssueNumber:    issue.Number,
		IssueState:     issue.State,
		ChecklistTotal: len(issue.Checklist),
		Unchecked:      make([]ChecklistItem, 0),
	}
	for _, item := range issue.Checklist {
		if item.Checked {
			rec.CheckedCount++
			continue
		}
		rec.Unchecked = append(rec.Unchecked, item)
	}
	return rec
}

func (r IssueReconciliation) HasUnchecked() bool {
	return len(r.Unchecked) > 0
}

func (r IssueReconciliation) ChecklistComplete() bool {
	return r.ChecklistTotal > 0 && len(r.Unchecked) == 0
}

func (r IssueReconciliation) CommentNote() string {
	var sb strings.Builder
	switch {
	case r.ChecklistTotal == 0:
		sb.WriteString("Post-run issue reconciliation: no GitHub issue checklist items were found to verify.")
	case r.HasUnchecked():
		fmt.Fprintf(&sb, "本輪 Hermes job 已完成，但 GitHub Issue checklist 尚未完成（%d/%d checked）。\n\nRemaining unchecked items:\n", r.CheckedCount, r.ChecklistTotal)
		for _, item := range r.Unchecked {
			fmt.Fprintf(&sb, "- [ ] %s\n", item.Text)
		}
	default:
		fmt.Fprintf(&sb, "Post-run issue reconciliation: GitHub Issue checklist is complete (%d/%d checked).", r.CheckedCount, r.ChecklistTotal)
	}
	return strings.TrimSpace(sb.String())
}

// BuildGoalFromIssue assembles the Planner goal string from an Issue.
// If the issue has an unchecked checklist, the Planner is instructed to use it.
func BuildGoalFromIssue(issue *IssueContext) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[GitHub #%d] %s\n\n", issue.Number, issue.Title))
	sb.WriteString(BuildIssueStateSnapshot(issue))
	sb.WriteString("\n")

	var unchecked, checked []ChecklistItem
	for _, item := range issue.Checklist {
		if !item.Checked {
			unchecked = append(unchecked, item)
		} else {
			checked = append(checked, item)
		}
	}

	if len(unchecked) > 0 {
		sb.WriteString("Remaining unchecked issue task list — plan ONLY these as SubTask descriptions:\n")
		for _, item := range unchecked {
			sb.WriteString("  - [ ] ")
			sb.WriteString(item.Text)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		if len(checked) > 0 {
			sb.WriteString("Already checked issue items — treat as completed context; DO NOT plan or redo these unless explicitly requested:\n")
			for _, item := range checked {
				sb.WriteString("  - [x] ")
				sb.WriteString(item.Text)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if issue.Body != "" {
		body := issue.Body
		if len(issue.Checklist) > 0 {
			body = stripChecklistLines(body)
		}
		if strings.TrimSpace(body) != "" {
			sb.WriteString("Issue description (checklist lines omitted to avoid re-planning completed items):\n")
			sb.WriteString(body)
		}
	}

	return sb.String()
}

// BuildIssueStateSnapshot creates a compact deterministic summary for Planner
// and preflight. It is intentionally shorter than raw comments/body: the goal
// is to preserve state signals without replaying the whole GitHub thread.
func BuildIssueStateSnapshot(issue *IssueContext) string {
	if issue == nil {
		return "=== Issue State Snapshot ===\n(issue unavailable)\n"
	}
	var unchecked, checked []ChecklistItem
	for _, item := range issue.Checklist {
		if item.Checked {
			checked = append(checked, item)
		} else {
			unchecked = append(unchecked, item)
		}
	}

	var sb strings.Builder
	sb.WriteString("=== Issue State Snapshot ===\n")
	state := strings.TrimSpace(issue.State)
	if state == "" {
		state = "UNKNOWN"
	}
	fmt.Fprintf(&sb, "- Issue: #%d %s\n", issue.Number, issue.Title)
	fmt.Fprintf(&sb, "- State: %s\n", state)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&sb, "- Labels: %s\n", strings.Join(issue.Labels, ", "))
	} else {
		sb.WriteString("- Labels: (none)\n")
	}
	if signal, ok := RecentSuccessfulHermesCompletion(issue); ok {
		fmt.Fprintf(&sb, "- Latest Hermes status: complete (%d/%d SubTasks", signal.Done, signal.Total)
		if !signal.CreatedAt.IsZero() {
			fmt.Fprintf(&sb, ", %s", signal.CreatedAt.Format(time.RFC3339))
		}
		sb.WriteString(")\n")
		if len(unchecked) > 0 {
			sb.WriteString("- IssueOps state: checklist_unsynced\n")
			sb.WriteString("- Planner instruction: treat the unchecked checklist as stale until GitHub checklist is synced; do not continue implementation work, and prefer IssueOps actions to sync checklist, revalidate evidence, replan remaining items, or ask for human decision.\n")
		}
	}
	fmt.Fprintf(&sb, "- Checklist: %d unchecked, %d checked\n", len(unchecked), len(checked))
	if len(unchecked) > 0 {
		sb.WriteString("- Remaining checklist items:\n")
		for _, item := range unchecked {
			fmt.Fprintf(&sb, "  - %s\n", item.Text)
		}
	}
	if len(checked) > 0 {
		sb.WriteString("- Completed checklist items:\n")
		for _, item := range checked {
			fmt.Fprintf(&sb, "  - %s\n", item.Text)
		}
	}
	signals := recentIssueCommentSignals(issue.Comments, 5)
	if len(signals) > 0 {
		sb.WriteString("- Recent Hermes/comment signals:\n")
		for _, signal := range signals {
			sb.WriteString("  - ")
			sb.WriteString(signal)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("Planner instruction: treat this snapshot as the source of truth for issue state; do not redo checked/completed items unless repo evidence contradicts them.\n")
	return sb.String()
}

// HermesCompletionSignal summarizes the latest successful terminal Hermes
// lifecycle comment for an issue.
type HermesCompletionSignal struct {
	Done      int
	Total     int
	Author    string
	CreatedAt time.Time
}

// RecentSuccessfulHermesCompletion returns true only when the latest terminal
// Hermes lifecycle signal in the issue comments is a complete x/x run. This is
// used as a deterministic guard for legacy issues whose original acceptance
// checklists remain unchecked even though Hermes already finished and verified
// them in comments.
func RecentSuccessfulHermesCompletion(issue *IssueContext) (HermesCompletionSignal, bool) {
	if issue == nil || len(issue.Comments) == 0 {
		return HermesCompletionSignal{}, false
	}
	var latest HermesCompletionSignal
	hasLatest := false
	latestFailed := false
	for _, comment := range issue.Comments {
		body := strings.TrimSpace(comment.Body)
		if body == "" {
			continue
		}
		switch {
		case strings.Contains(body, "Hermes 執行失敗") || strings.Contains(body, "Hermes Budget 耗盡"):
			hasLatest = false
			latestFailed = true
		case strings.Contains(body, "Hermes 完成"):
			done, total, ok := parseHermesCompleteCounts(body)
			if !ok || total <= 0 || done != total {
				hasLatest = false
				latestFailed = false
				continue
			}
			latest = HermesCompletionSignal{
				Done:      done,
				Total:     total,
				Author:    comment.Author,
				CreatedAt: comment.CreatedAt,
			}
			hasLatest = true
			latestFailed = false
		}
	}
	if latestFailed || !hasLatest {
		return HermesCompletionSignal{}, false
	}
	return latest, true
}

func parseHermesCompleteCounts(body string) (int, int, bool) {
	matches := hermesCompleteRe.FindStringSubmatch(body)
	if len(matches) != 3 {
		return 0, 0, false
	}
	done, err1 := strconv.Atoi(matches[1])
	total, err2 := strconv.Atoi(matches[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return done, total, true
}

func recentIssueCommentSignals(comments []IssueComment, limit int) []string {
	if limit <= 0 || len(comments) == 0 {
		return nil
	}
	signals := make([]string, 0, limit)
	for i := len(comments) - 1; i >= 0 && len(signals) < limit; i-- {
		body := strings.TrimSpace(comments[i].Body)
		if body == "" || !looksLikeHermesStateComment(body) {
			continue
		}
		signal := extractIssueCommentSignal(body)
		if signal == "" {
			continue
		}
		prefix := comments[i].Author
		if comments[i].CreatedAt.IsZero() {
			prefix = strings.TrimSpace(prefix)
		} else if prefix == "" {
			prefix = comments[i].CreatedAt.Format(time.RFC3339)
		} else {
			prefix = fmt.Sprintf("%s %s", prefix, comments[i].CreatedAt.Format(time.RFC3339))
		}
		if prefix != "" {
			signal = prefix + ": " + signal
		}
		signals = append(signals, truncateOneLine(signal, 500))
	}
	return signals
}

func looksLikeHermesStateComment(body string) bool {
	lower := strings.ToLower(body)
	keywords := []string{
		"hermes", "子任務進度", "執行完成", "執行失敗", "完成",
		"結論", "下一步", "可關閉", "未驗證", "pass", "fail",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func extractIssueCommentSignal(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, 6)
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimLeft(line, "-*># "))
		if line == "" {
			continue
		}
		if isSignalLine(line) {
			kept = append(kept, line)
			if len(kept) >= 6 {
				break
			}
		}
	}
	if len(kept) == 0 {
		return truncateOneLine(body, 500)
	}
	return truncateOneLine(strings.Join(kept, " | "), 500)
}

func isSignalLine(line string) bool {
	lower := strings.ToLower(line)
	keywords := []string{
		"hermes 完成", "hermes 執行失敗", "子任務進度", "結論", "下一步",
		"可關閉", "未驗證", "全部 pass", "pass", "fail", "失敗", "完成",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func truncateOneLine(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func stripChecklistLines(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if checklistRe.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func matchesChecklistItem(itemText, desc string) bool {
	itemText = strings.TrimSpace(itemText)
	desc = strings.TrimSpace(desc)
	if itemText == "" || desc == "" {
		return false
	}
	if strings.EqualFold(itemText, desc) {
		return true
	}
	descRunes := []rune(desc)
	if len(descRunes) > 16 {
		desc = string(descRunes[:16])
	}
	return strings.HasPrefix(strings.ToLower(itemText), strings.ToLower(desc))
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
			if matchesChecklistItem(itemText, desc) {
				return "- [x] " + m[2]
			}
		}
		return match
	})
}

// ChecklistSyncPreview captures the checklist body patch derived from a
// completed-subtask list.
type ChecklistSyncPreview struct {
	CompletedDescriptions []string
	UpdatedChecklistItems []string
	BodyBefore            string
	BodyAfter             string
	Changed               bool
}

// BuildChecklistSyncPreview computes the checklist body patch without mutating
// remote state.
func BuildChecklistSyncPreview(body string, subtasks []SubTask) ChecklistSyncPreview {
	completed := make([]string, 0, len(subtasks))
	updatedItems := make([]string, 0, len(subtasks))
	for _, st := range subtasks {
		if st.Status != SubTaskDone {
			continue
		}
		completed = append(completed, st.Description)
	}
	if len(completed) == 0 {
		return ChecklistSyncPreview{
			CompletedDescriptions: completed,
			BodyBefore:            body,
			BodyAfter:             body,
		}
	}

	updatedBody := UpdateChecklistInBody(body, completed)
	if updatedBody != body {
		for _, line := range strings.Split(body, "\n") {
			m := checklistRe.FindStringSubmatch(line)
			if len(m) < 3 || strings.ToLower(m[1]) == "x" {
				continue
			}
			for _, desc := range completed {
				if matchesChecklistItem(m[2], desc) {
					updatedItems = append(updatedItems, strings.TrimSpace(m[2]))
					break
				}
			}
		}
	}

	return ChecklistSyncPreview{
		CompletedDescriptions: completed,
		UpdatedChecklistItems: updatedItems,
		BodyBefore:            body,
		BodyAfter:             updatedBody,
		Changed:               updatedBody != body,
	}
}

// UpdateIssueBody writes the supplied body to GitHub issue state.
func UpdateIssueBody(ctx context.Context, projectDir string, number int, body string) error {
	if out, err := ghCombinedOutput(ctx, projectDir, ghTimeoutShort, "issue", "edit",
		fmt.Sprintf("%d", number),
		"--body", body,
	); err != nil {
		return fmt.Errorf("gh issue edit body %d: %w (output: %s)", number, err, out)
	}
	return nil
}

// PostComment posts a comment to the given Issue via `gh issue comment`.
func PostComment(ctx context.Context, projectDir string, number int, body string) error {
	if out, err := ghCombinedOutput(ctx, projectDir, ghTimeoutShort, "issue", "comment",
		fmt.Sprintf("%d", number),
		"--body", body,
	); err != nil {
		return fmt.Errorf("gh issue comment %d: %w (output: %s)", number, err, out)
	}
	return nil
}

// SyncChecklist fetches the current Issue body and checks off completed sub-tasks.
func SyncChecklist(ctx context.Context, projectDir string, number int, subtasks []SubTask) error {
	// Fetch current body
	out, err := ghOutput(ctx, projectDir, ghTimeoutShort, "issue", "view",
		fmt.Sprintf("%d", number),
		"--json", "body",
	)
	if err != nil {
		return fmt.Errorf("gh issue view body: %w", err)
	}

	var raw struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return fmt.Errorf("parse body JSON: %w", err)
	}

	preview := BuildChecklistSyncPreview(raw.Body, subtasks)
	if len(preview.CompletedDescriptions) == 0 {
		return nil
	}
	if !preview.Changed {
		return nil // nothing changed
	}

	return UpdateIssueBody(ctx, projectDir, number, preview.BodyAfter)
}

// ApplyLabel adds a label to the Issue (creates if needed via gh's built-in behaviour).
func ApplyLabel(ctx context.Context, projectDir string, number int, label string) error {
	if out, err := ghCombinedOutput(ctx, projectDir, ghTimeoutShort, "issue", "edit",
		fmt.Sprintf("%d", number),
		"--add-label", label,
	); err != nil {
		return fmt.Errorf("gh apply label %q to #%d: %w (output: %s)", label, number, err, out)
	}
	return nil
}

// CloseIssue closes the Issue.
func CloseIssue(ctx context.Context, projectDir string, number int) error {
	if out, err := ghCombinedOutput(ctx, projectDir, ghTimeoutShort, "issue", "close",
		fmt.Sprintf("%d", number),
	); err != nil {
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
//
// The body is always re-fetched from GitHub before mutation — the caller's
// notion of "originalBody" was historically the chat-side goal augmented
// with conversation context (state.Goal), which would overwrite the issue
// body with chat history every run and bloat it geometrically. This
// function now ignores any caller-supplied body, fetches the current
// remote body, strips any previously-written "## Hermes 執行計劃"
// section, and appends a fresh one.
//
// originalBody is retained for backward compatibility but no longer used.
func WritePlanToIssue(ctx context.Context, projectDir string, number int, originalBody string, tasks []SubTask) error {
	_ = originalBody // intentionally ignored; see doc comment

	out, err := ghOutput(ctx, projectDir, ghTimeoutShort, "issue", "view",
		fmt.Sprintf("%d", number),
		"--json", "body",
	)
	if err != nil {
		return fmt.Errorf("gh issue view body for plan write: %w", err)
	}
	var raw struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return fmt.Errorf("parse body JSON: %w", err)
	}

	cleanBody := stripHermesPlanSections(raw.Body)

	var sb strings.Builder
	sb.WriteString(cleanBody)
	if cleanBody != "" && !strings.HasSuffix(cleanBody, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n## Hermes 執行計劃\n\n")
	for _, t := range tasks {
		sb.WriteString("- [ ] ")
		sb.WriteString(t.Description)
		sb.WriteString("\n")
	}

	if cmdOut, err := ghCombinedOutput(ctx, projectDir, ghTimeoutShort, "issue", "edit",
		fmt.Sprintf("%d", number),
		"--body", sb.String(),
	); err != nil {
		return fmt.Errorf("gh issue edit plan: %w (output: %s)", err, cmdOut)
	}
	return nil
}

// stripHermesPlanSections removes every "## Hermes 執行計劃" block from
// the body — a section header followed by content up to the next "## "
// heading or end-of-body. Idempotent; bodies without such a section are
// returned unchanged. Used to clean up bodies that previous Hermes runs
// appended duplicate plan sections to before this function was fixed.
func stripHermesPlanSections(body string) string {
	const header = "## Hermes 執行計劃"
	for {
		idx := strings.Index(body, header)
		if idx < 0 {
			break
		}
		// Find the end of this section: next "## " heading after the header,
		// or end of body.
		tail := body[idx+len(header):]
		nextIdx := strings.Index(tail, "\n## ")
		var end int
		if nextIdx < 0 {
			end = len(body)
		} else {
			end = idx + len(header) + nextIdx + 1 // keep the newline before next heading
		}
		// Trim trailing blank line just before the section if present.
		start := idx
		for start > 0 && (body[start-1] == '\n') {
			start--
		}
		body = body[:start] + body[end:]
	}
	return strings.TrimRight(body, "\n")
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
	return CommentDoneWithNote(state, "")
}

func CommentDoneWithNote(state TaskState, note string) string {
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
	if strings.TrimSpace(note) != "" {
		sb.WriteString("\n**Note:**\n")
		sb.WriteString(strings.TrimSpace(note))
		sb.WriteString("\n")
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
