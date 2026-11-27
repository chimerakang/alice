package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/ghissue"
)

// Fleet → GitHub issue sync.
//
// alice-core's IssueTracker posts its own "Alice 完成" comment as soon as a
// task reaches review_ready, which is before anything is merged. The fleet
// turns that comment off (IssueTracker.NoComments) and this file owns what an
// issue is told instead: one comment per task, found again by a hidden marker
// and edited in place as the task moves from running to review to merged, so
// a retry or a second sync never adds a duplicate. The issue's body and its
// checklist are never written here; ticking items stays with the tracker.
//
// An issue is closed only when every condition in decideClose holds.

const (
	// FleetProjectBindingIssueAutoClose is the project binding that lets the
	// fleet close an issue once its work is merged. Anything but "true"
	// leaves closing to people.
	FleetProjectBindingIssueAutoClose = "issue_auto_close"

	// Event types recorded on the task by the sync.
	fleetEventIssueSyncResult = "issue_sync.result"
	fleetEventIssueSyncError  = "issue_sync.error"
	fleetEventIssueClosed     = "issue_sync.closed"

	fleetIssueSyncTimeout  = 2 * time.Minute
	fleetIssueSyncInterval = 2 * time.Minute
	// fleetIssueSyncLookback bounds how old a finished task may be and still
	// be re-checked, e.g. for a pull request merged on the forge later.
	fleetIssueSyncLookback = 14 * 24 * time.Hour
	fleetIssueFilesShown   = 10
)

// fleetIssueMarkerPrefix opens the hidden marker that ties a comment to a task.
const fleetIssueMarkerPrefix = "<!-- alice-fleet:task="

func fleetIssueMarker(taskID string) string {
	return fleetIssueMarkerPrefix + taskID + " -->"
}

// FleetPullRequest is what the forge says about a pull request.
type FleetPullRequest struct {
	State       string // OPEN, MERGED or CLOSED
	Title       string
	URL         string
	BaseBranch  string
	MergeCommit string
	Files       []string
}

// Merged reports whether the pull request has been merged.
func (p FleetPullRequest) Merged() bool { return strings.EqualFold(p.State, "MERGED") }

// FleetIssueHost is the part of GitHub and git the sync talks to. Tests
// replace it, so no test ever writes to a real repository.
type FleetIssueHost interface {
	Fetch(ctx context.Context, dir string, number int) (*ghissue.IssueContext, error)
	// UpsertComment makes the comment holding marker say body: it creates
	// it, edits it, or does nothing when it already matches.
	UpsertComment(ctx context.Context, dir string, number int, marker, body string) error
	CloseIssue(ctx context.Context, dir string, number int) error
	PullRequest(ctx context.Context, dir string, number int) (FleetPullRequest, error)
	// LocalHead describes the tip of a local branch.
	LocalHead(ctx context.Context, dir, branch string) (sha, subject string, err error)
}

// FleetIssueSync keeps GitHub issues informed of their fleet tasks.
type FleetIssueSync struct {
	Store      *fleet.Store
	Host       FleetIssueHost
	Acceptance func(context.Context, string) (acceptState, error)
	// Interval is how often unsettled tasks are re-checked. Zero uses the
	// default.
	Interval time.Duration

	mu sync.Mutex
}

// IssueSyncOptions adjusts one sync.
type IssueSyncOptions struct {
	// AuthorizeClose is the operator's go-ahead to close this issue, on top
	// of the project's own setting. The other conditions still apply.
	AuthorizeClose bool
}

// IssueSyncResult is what one sync did.
type IssueSyncResult struct {
	TaskID      string `json:"task_id"`
	Issue       int    `json:"issue"`
	Stage       string `json:"stage"`
	Closed      bool   `json:"closed"`
	KeptOpen    string `json:"kept_open,omitempty"`
	Settled     bool   `json:"settled"`
	CommentHash string `json:"comment_hash"`
}

// Stages of a task as its issue is told about them.
const (
	issueStageWorking          = "working"
	issueStageReview           = "review"
	issueStagePRReady          = "pr_ready"
	issueStageEvidenceComplete = "evidence_complete"
	issueStageMerged           = "merged"
	issueStageLocalMerged      = "local_merged"
	issueStageUnconfirmed      = "unconfirmed"
	issueStagePRClosed         = "pr_closed"
	issueStageProblem          = "problem"
)

func (s *FleetIssueSync) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return fleetIssueSyncInterval
}

// issueSyncEventTypes are the events that may change what an issue should say.
var issueSyncEventTypes = []string{
	fleet.EventStatus, fleet.EventIssueSynced, fleet.EventDeliveryPROpened, fleet.EventDeliveryApproved,
}

// Run syncs on task events and re-checks unsettled tasks on a timer, until
// ctx ends. The timer is what retries a failed GitHub call and notices a pull
// request merged on the forge.
func (s *FleetIssueSync) Run(ctx context.Context, bus *fleet.Bus) error {
	events, err := bus.Subscribe(ctx, fleet.EventFilter{Types: issueSyncEventTypes})
	if err != nil {
		return err
	}
	tick := time.NewTicker(s.interval())
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if _, err := s.Sync(ctx, ev.TaskID, IssueSyncOptions{}); err != nil {
				log.Printf("[fleet] issue sync for task %s: %v", ev.TaskID, err)
			}
		case <-tick.C:
			s.Reconcile(ctx)
		}
	}
}

// Reconcile retries every tracked task whose last sync did not settle.
func (s *FleetIssueSync) Reconcile(ctx context.Context) {
	tasks, err := s.Store.ListTasks(ctx, fleet.TaskFilter{Statuses: []fleet.Status{
		fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady, fleet.StatusNeedsDecision, fleet.StatusFailed, fleet.StatusDone,
	}})
	if err != nil {
		log.Printf("[fleet] issue sync: list tasks: %v", err)
		return
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		if !isIssueTask(task) || time.Since(task.UpdatedAt) > fleetIssueSyncLookback {
			continue
		}
		if s.settled(ctx, task) {
			continue
		}
		if _, err := s.Sync(ctx, task.ID, IssueSyncOptions{}); err != nil {
			log.Printf("[fleet] issue sync retry for task %s: %v", task.ID, err)
		}
	}
}

func isIssueTask(t fleet.Task) bool {
	return t.Source.Kind == fleet.SourceIssue && t.Source.IssueNumber > 0
}

type issueSyncRecord struct {
	Stage   string `json:"stage"`
	Hash    string `json:"hash"`
	Settled bool   `json:"settled"`
	Status  string `json:"status"`
}

// settled reports whether the last sync of a task is final for its present
// status, so there is nothing to re-check.
func (s *FleetIssueSync) settled(ctx context.Context, task fleet.Task) bool {
	res, ok, err := s.Store.LastEvent(ctx, task.ID, fleetEventIssueSyncResult)
	if err != nil || !ok {
		return false
	}
	// A later error means the last attempt failed.
	if bad, ok, _ := s.Store.LastEvent(ctx, task.ID, fleetEventIssueSyncError); ok && bad.ID > res.ID {
		return false
	}
	var rec issueSyncRecord
	if json.Unmarshal(res.Payload, &rec) != nil {
		return false
	}
	return rec.Settled && rec.Status == string(task.Status)
}

// Sync brings the issue behind a task up to date. It does nothing for a task
// with no issue. Failures are recorded on the task and returned; calling Sync
// again is always safe.
func (s *FleetIssueSync) Sync(ctx context.Context, taskID string, opts IssueSyncOptions) (*IssueSyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fleetIssueSyncTimeout)
	defer cancel()

	task, err := s.Store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !isIssueTask(task) {
		return nil, nil
	}
	switch task.Status {
	case fleet.StatusQueued, fleet.StatusCancelled:
		return nil, nil
	}
	project, err := s.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	res, err := s.sync(ctx, task, project, opts)
	if err != nil {
		s.recordError(ctx, task, err)
		return res, err
	}
	payload := issueSyncRecord{Stage: res.Stage, Hash: res.CommentHash, Settled: res.Settled, Status: string(task.Status)}
	// Record a result only when something changed: a quiet re-check must
	// not grow the task's history.
	if last, ok, _ := s.Store.LastEvent(ctx, task.ID, fleetEventIssueSyncResult); !ok || !sameSyncRecord(last.Payload, payload) || s.errorAfter(ctx, task.ID, last.ID) {
		_, _ = s.Store.AppendEvent(ctx, task.ID, fleetEventIssueSyncResult, payload)
	}
	return res, nil
}

func (s *FleetIssueSync) errorAfter(ctx context.Context, taskID string, id int64) bool {
	bad, ok, _ := s.Store.LastEvent(ctx, taskID, fleetEventIssueSyncError)
	return ok && bad.ID > id
}

func sameSyncRecord(raw json.RawMessage, rec issueSyncRecord) bool {
	var old issueSyncRecord
	return json.Unmarshal(raw, &old) == nil && old == rec
}

// recordError notes a failed sync once per distinct failure, so an outage
// does not fill the history while the timer keeps retrying.
func (s *FleetIssueSync) recordError(ctx context.Context, task fleet.Task, cause error) {
	msg := cause.Error()
	if last, ok, _ := s.Store.LastEvent(ctx, task.ID, fleetEventIssueSyncError); ok {
		var prev struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(last.Payload, &prev)
		if prev.Error == msg {
			if res, ok, _ := s.Store.LastEvent(ctx, task.ID, fleetEventIssueSyncResult); !ok || res.ID < last.ID {
				return
			}
		}
	}
	_, _ = s.Store.AppendEvent(ctx, task.ID, fleetEventIssueSyncError, map[string]any{"error": msg, "status": string(task.Status)})
}

// ── inputs and decisions (pure) ──────────────────────────────────────────────

// issueSyncInput is everything the comment and the close decision depend on.
type issueSyncInput struct {
	Task    fleet.Task
	Project fleet.Project
	Checks  []fleet.Verification
	// Items are the acceptance items the task was given; Ticked are those
	// the tracker ticked after verification.
	Items  []fleet.IssueItem
	Ticked []string

	Stage string
	// PR is set when the forge was asked.
	PR *FleetPullRequest
	// PRError is why the forge could not be asked or did not answer.
	PRError string
	// LocalSHA and LocalSubject describe the branch tip for a local merge.
	LocalSHA, LocalSubject string

	// Issue is the issue as it is now; nil when it could not be read.
	Issue *ghissue.IssueContext
	// Siblings are the other tasks for the same issue.
	Siblings []siblingState

	Authorized bool
	Acceptance *acceptGate
}

type siblingState struct {
	Task   fleet.Task
	Landed bool // done and merged on the remote
}

// closeDecision says whether to close the issue, or why not.
type closeDecision struct {
	Close  bool
	Reason string // why it stays open; empty when closing or already closed
	// AlreadyClosed is true when there is nothing left to do.
	AlreadyClosed bool
}

// decideClose applies the rules for closing an issue. Every one must hold:
// the change is merged on the remote, the project or operator allows closing,
// verification passed, every acceptance item is ticked, and no other task for
// the issue is still open.
func decideClose(in issueSyncInput) closeDecision {
	if in.Issue != nil && strings.EqualFold(strings.TrimSpace(in.Issue.State), "closed") {
		return closeDecision{AlreadyClosed: true}
	}
	switch in.Stage {
	case issueStageMerged, issueStageEvidenceComplete:
	case issueStageLocalMerged:
		return closeDecision{Reason: "變更只合併到本機，尚未推送到遠端"}
	case issueStageUnconfirmed:
		return closeDecision{Reason: "無法確認變更是否已合併"}
	case issueStagePRClosed:
		return closeDecision{Reason: "PR 已關閉但沒有合併"}
	default:
		return closeDecision{Reason: "變更尚未合併"}
	}
	if !in.Authorized {
		return closeDecision{Reason: "尚未授權自動關單（專案未啟用 " + FleetProjectBindingIssueAutoClose + "，操作者也未授權）"}
	}
	if in.Acceptance != nil && !in.Acceptance.OK {
		return closeDecision{Reason: in.Acceptance.Message}
	}
	if in.Stage == issueStageEvidenceComplete {
		if in.Acceptance == nil {
			return closeDecision{Reason: "沒有逐項驗收紀錄"}
		}
	} else {
		if len(in.Checks) == 0 {
			return closeDecision{Reason: "沒有驗收紀錄，視為未驗收"}
		}
		for _, c := range in.Checks {
			if !c.Passed {
				return closeDecision{Reason: "驗收未通過"}
			}
		}
	}
	if in.Issue == nil {
		return closeDecision{Reason: "無法讀取 issue，無法確認驗收範圍"}
	}
	if in.Acceptance == nil {
		acceptance, open := 0, []string{}
		for _, item := range in.Issue.Checklist {
			if !ghissue.IsAcceptanceSection(item.Section) {
				continue
			}
			acceptance++
			if !item.Checked {
				open = append(open, item.Text)
			}
		}
		if acceptance == 0 {
			return closeDecision{Reason: "issue 沒有可核對的驗收項目"}
		}
		if len(open) > 0 {
			return closeDecision{Reason: fmt.Sprintf("仍有 %d 項驗收條件未完成（部分交付）", len(open))}
		}
	}
	for _, sib := range in.Siblings {
		if !sib.Landed {
			return closeDecision{Reason: fmt.Sprintf("關聯任務 `%s` 尚未完成（%s）", sib.Task.ID, sib.Task.Status)}
		}
	}
	return closeDecision{Close: true}
}

// renderIssueComment builds the task's comment. It depends only on its input,
// so rendering twice gives the same text and a retry can tell nothing changed.
func renderIssueComment(in issueSyncInput, d closeDecision) string {
	task := in.Task
	var sb strings.Builder
	sb.WriteString(fleetIssueMarker(task.ID) + "\n")

	switch in.Stage {
	case issueStageWorking:
		fmt.Fprintf(&sb, "🤖 **Alice 處理中**（任務 `%s`）\n\n- 階段: %s\n", task.ID, workingPhase(task.Status))
		writeIssueFacts(&sb, task)
	case issueStageReview:
		fmt.Fprintf(&sb, "🔎 **Alice 待審查**（任務 `%s`）\n\n- 階段: 等待你審核，尚未合併\n", task.ID)
		writeIssueFacts(&sb, task)
		writeIssueAcceptance(&sb, in, true)
	case issueStagePRReady:
		fmt.Fprintf(&sb, "🔀 **PR 就緒**（任務 `%s`）\n\n- 階段: PR 已開，等待合併；合併前這個 issue 保持開啟\n", task.ID)
		if task.PRURL != "" {
			fmt.Fprintf(&sb, "- PR: %s\n", task.PRURL)
		}
		writeIssueFacts(&sb, task)
		// A PR that is only ready makes no claim about the checklist.
		writeIssueAcceptance(&sb, in, task.TrustMode != fleet.TrustDirectPR)
	case issueStageEvidenceComplete:
		fmt.Fprintf(&sb, "✅ **證據已驗收**（任務 `%s`）\n\n- 結果: 已完成逐項人工驗收，沒有需合併的檔案變更\n", task.ID)
		writeIssueFacts(&sb, task)
		writeIssueSummary(&sb, in)
	case issueStageMerged:
		fmt.Fprintf(&sb, "✅ **已合併**（任務 `%s`）\n\n", task.ID)
		sb.WriteString("- 結果: 變更已合併到遠端\n")
		if in.PR != nil && in.PR.BaseBranch != "" {
			fmt.Fprintf(&sb, "- 目標分支: `%s`\n", in.PR.BaseBranch)
		} else {
			fmt.Fprintf(&sb, "- 目標分支: `%s`\n", in.Project.DefaultBranch)
		}
		if task.PRURL != "" {
			fmt.Fprintf(&sb, "- PR: %s\n", task.PRURL)
		}
		if in.PR != nil && in.PR.MergeCommit != "" {
			fmt.Fprintf(&sb, "- Commit: `%s`\n", shortSHA(in.PR.MergeCommit))
		}
		writeIssueFacts(&sb, task)
		writeIssueSummary(&sb, in)
		writeIssueAcceptance(&sb, in, true)
	case issueStageLocalMerged:
		fmt.Fprintf(&sb, "📦 **僅合併至本機**（任務 `%s`）\n\n", task.ID)
		fmt.Fprintf(&sb, "- 結果: 變更只合併到本機的 `%s`，**沒有推送到遠端**，遠端看不到這些變更\n", in.Project.DefaultBranch)
		if in.LocalSHA != "" {
			fmt.Fprintf(&sb, "- Commit: `%s` %s\n", shortSHA(in.LocalSHA), in.LocalSubject)
		}
		writeIssueFacts(&sb, task)
		writeIssueAcceptance(&sb, in, true)
	case issueStageUnconfirmed:
		fmt.Fprintf(&sb, "❓ **無法確認是否已合併**（任務 `%s`）\n\n", task.ID)
		if in.PRError != "" {
			fmt.Fprintf(&sb, "- 原因: %s\n", in.PRError)
		}
		if task.PRURL != "" {
			fmt.Fprintf(&sb, "- PR: %s\n", task.PRURL)
		}
		writeIssueFacts(&sb, task)
		writeIssueAcceptance(&sb, in, true)
	case issueStagePRClosed:
		fmt.Fprintf(&sb, "🚫 **PR 已關閉，未合併**（任務 `%s`）\n\n", task.ID)
		if task.PRURL != "" {
			fmt.Fprintf(&sb, "- PR: %s\n", task.PRURL)
		}
		writeIssueFacts(&sb, task)
	case issueStageProblem:
		title := "⚠️ **Alice 需要你決定**"
		if task.Status == fleet.StatusFailed {
			title = "❌ **Alice 執行失敗**"
		}
		fmt.Fprintf(&sb, "%s（任務 `%s`）\n\n- 原因: %s\n", title, task.ID, issueFirstNonEmpty(task.StatusReason, "未說明"))
		writeIssueFacts(&sb, task)
	}

	sb.WriteString("\n**Issue 狀態**\n")
	switch {
	case d.AlreadyClosed || d.Close:
		// Same words either way, so the comment is stable once closed.
		sb.WriteString("- 已關閉\n")
	case d.Reason != "":
		fmt.Fprintf(&sb, "- 維持開啟：%s\n", d.Reason)
	default:
		sb.WriteString("- 維持開啟\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func workingPhase(s fleet.Status) string {
	if s == fleet.StatusVerifying {
		return "驗收中"
	}
	return "執行中"
}

func writeIssueFacts(sb *strings.Builder, task fleet.Task) {
	if task.Branch != "" {
		fmt.Fprintf(sb, "- 分支: `%s`\n", task.Branch)
	}
	if task.TrustMode != "" {
		fmt.Fprintf(sb, "- 信任模式: %s\n", task.TrustMode)
	}
}

func writeIssueSummary(sb *strings.Builder, in issueSyncInput) {
	subject := strings.TrimSpace(firstLineOf(in.Task.Goal))
	if in.PR != nil && in.PR.Title != "" {
		subject = in.PR.Title
	}
	if subject != "" {
		fmt.Fprintf(sb, "\n**變更摘要**\n- %s\n", subject)
	}
	if in.PR != nil && len(in.PR.Files) > 0 {
		files := append([]string(nil), in.PR.Files...)
		sort.Strings(files)
		shown := files
		if len(shown) > fleetIssueFilesShown {
			shown = shown[:fleetIssueFilesShown]
		}
		for _, f := range shown {
			fmt.Fprintf(sb, "- `%s`\n", f)
		}
		if extra := len(files) - len(shown); extra > 0 {
			fmt.Fprintf(sb, "- …另有 %d 個檔案\n", extra)
		}
	}
}

func writeIssueAcceptance(sb *strings.Builder, in issueSyncInput, withItems bool) {
	if len(in.Checks) == 0 {
		sb.WriteString("\n**驗收**\n- 沒有驗收紀錄（專案未設定驗收指令，或尚未執行）\n")
	} else {
		sb.WriteString("\n**驗收**\n")
		for _, c := range in.Checks {
			mark := "✅"
			if !c.Passed {
				mark = "❌"
			}
			fmt.Fprintf(sb, "- %s `%s`（exit %d）\n", mark, c.Command, c.ExitCode)
		}
	}
	if !withItems || len(in.Items) == 0 {
		return
	}
	ticked := map[string]bool{}
	for _, t := range in.Ticked {
		ticked[t] = true
	}
	var done, open []string
	for _, item := range in.Items {
		if ticked[item.Text] {
			done = append(done, item.Text)
		} else {
			open = append(open, item.Text)
		}
	}
	issueWriteList(sb, "已勾選的項目", done)
	issueWriteList(sb, "仍未完成的項目", open)
}

func issueWriteList(sb *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n**%s**\n", heading)
	for _, it := range items {
		fmt.Fprintf(sb, "- %s\n", it)
	}
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func issueFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ── gathering and acting ─────────────────────────────────────────────────────

func (s *FleetIssueSync) sync(ctx context.Context, task fleet.Task, project fleet.Project, opts IssueSyncOptions) (*IssueSyncResult, error) {
	in := issueSyncInput{Task: task, Project: project}
	in.Authorized = opts.AuthorizeClose || strings.EqualFold(strings.TrimSpace(project.Bindings[FleetProjectBindingIssueAutoClose]), "true")
	in.Checks = s.currentRunChecks(ctx, task.ID)
	in.Items, in.Ticked = s.linkedItems(ctx, task.ID)

	switch task.Status {
	case fleet.StatusRunning, fleet.StatusVerifying:
		in.Stage = issueStageWorking
	case fleet.StatusNeedsDecision, fleet.StatusFailed:
		in.Stage = issueStageProblem
	case fleet.StatusReviewReady:
		in.Stage = issueStageReview
		if task.PRNumber > 0 {
			in.Stage = issueStagePRReady
		}
	case fleet.StatusDone:
		s.resolveDone(ctx, &in)
	default:
		return nil, nil
	}

	needIssue := in.Stage == issueStageMerged || in.Stage == issueStageEvidenceComplete || in.Stage == issueStageLocalMerged || in.Stage == issueStageReview ||
		in.Stage == issueStagePRReady || in.Stage == issueStageUnconfirmed || in.Stage == issueStagePRClosed
	if needIssue {
		if issue, err := s.Host.Fetch(ctx, project.RepoPath, task.Source.IssueNumber); err == nil {
			in.Issue = issue
		}
	}
	if in.Stage == issueStageMerged || in.Stage == issueStageEvidenceComplete {
		in.Siblings = s.siblings(ctx, task)
		if s.Acceptance != nil {
			state, err := s.Acceptance(ctx, task.ID)
			if err != nil {
				in.Acceptance = &acceptGate{Message: "無法讀取逐項驗收紀錄"}
			} else if state.Structured {
				gate := evaluateAcceptGate(state, true)
				if gate.OK && in.Issue != nil && !issueCriteriaCovered(state, in.Issue) {
					gate = acceptGate{Message: "GitHub issue 的驗收條件已更新，請重新確認"}
				}
				in.Acceptance = &gate
			}
		}
	}

	d := closeDecision{}
	if in.Stage == issueStageMerged || in.Stage == issueStageEvidenceComplete || in.Stage == issueStageLocalMerged || in.Stage == issueStageUnconfirmed || in.Stage == issueStagePRClosed {
		d = decideClose(in)
	} else if in.Issue != nil && strings.EqualFold(strings.TrimSpace(in.Issue.State), "closed") {
		d = closeDecision{AlreadyClosed: true}
	}
	// Close first: the comment must not say "closed" for an issue that is open.
	var closeErr error
	if d.Close {
		if closeErr = s.Host.CloseIssue(ctx, project.RepoPath, task.Source.IssueNumber); closeErr != nil {
			d = closeDecision{Reason: "關閉 issue 失敗，稍後會自動重試"}
		}
	}
	body := renderIssueComment(in, d)
	sum := sha256.Sum256([]byte(body))
	res := &IssueSyncResult{TaskID: task.ID, Issue: task.Source.IssueNumber, Stage: in.Stage, CommentHash: hex.EncodeToString(sum[:8]), KeptOpen: d.Reason}

	if err := s.Host.UpsertComment(ctx, project.RepoPath, task.Source.IssueNumber, fleetIssueMarker(task.ID), body); err != nil {
		return res, fmt.Errorf("update issue #%d comment: %w", task.Source.IssueNumber, err)
	}
	if closeErr != nil {
		return res, fmt.Errorf("close issue #%d: %w", task.Source.IssueNumber, closeErr)
	}
	if d.Close {
		res.Closed = true
		_, _ = s.Store.AppendEvent(ctx, task.ID, fleetEventIssueClosed, map[string]any{"issue": task.Source.IssueNumber})
	}
	res.Settled = settledStage(in, d)
	return res, nil
}

// issueCriteriaCovered checks the live issue for criteria added or edited
// since the reviewed snapshot. Checked legacy items are already settled.
func issueCriteriaCovered(state acceptState, issue *ghissue.IssueContext) bool {
	if state.Proposal != nil {
		return false
	}
	approved := make(map[string]int)
	for _, item := range state.active() {
		if item.Decision == acceptDecisionApproved {
			approved[strings.TrimSpace(item.Text)]++
		}
	}
	for _, item := range openCriteria(issue) {
		text := strings.TrimSpace(item.Text)
		if approved[text] == 0 {
			return false
		}
		approved[text]--
	}
	return true
}

// settledStage reports whether later re-checks can add nothing: the change
// has landed (or is local-only or abandoned) and the issue is closed or
// closing was not authorized. An unauthorized merged task is revisited only
// when an operator asks.
func settledStage(in issueSyncInput, d closeDecision) bool {
	switch in.Stage {
	case issueStageProblem, issueStageLocalMerged, issueStagePRClosed:
		return true
	case issueStageMerged, issueStageEvidenceComplete:
		return d.Close || d.AlreadyClosed || !in.Authorized
	}
	return false
}

// resolveDone works out how a done task landed. A pull request's state is
// read from the forge rather than trusted from the task's history.
func (s *FleetIssueSync) resolveDone(ctx context.Context, in *issueSyncInput) {
	task, project := in.Task, in.Project
	if task.PRNumber == 0 {
		if strings.HasPrefix(task.StatusReason, fleetEvidenceFinishedPrefix) {
			in.Stage = issueStageEvidenceComplete
			return
		}
		if task.TrustMode == fleet.TrustLocalOnly && s.approved(ctx, task.ID) {
			in.Stage = issueStageLocalMerged
			in.LocalSHA, in.LocalSubject, _ = s.Host.LocalHead(ctx, project.RepoPath, task.Branch)
			return
		}
		in.Stage = issueStageUnconfirmed
		in.PRError = "任務沒有 PR，也沒有本機合併紀錄"
		return
	}
	pr, err := s.Host.PullRequest(ctx, project.RepoPath, task.PRNumber)
	if err != nil {
		in.Stage = issueStageUnconfirmed
		in.PRError = "查詢 PR 失敗：" + err.Error()
		return
	}
	in.PR = &pr
	switch {
	case pr.Merged():
		in.Stage = issueStageMerged
	case strings.EqualFold(pr.State, "OPEN"):
		in.Stage = issueStagePRReady
	case strings.EqualFold(pr.State, "CLOSED"):
		in.Stage = issueStagePRClosed
	default:
		in.Stage = issueStageUnconfirmed
		in.PRError = "PR 狀態未知：" + pr.State
	}
}

func (s *FleetIssueSync) approved(ctx context.Context, taskID string) bool {
	_, ok, _ := s.Store.LastEvent(ctx, taskID, fleet.EventDeliveryApproved)
	return ok
}

// currentRunChecks only uses results tied to the latest runner launch.
// An empty round records an event but leaves no check rows.
func (s *FleetIssueSync) currentRunChecks(ctx context.Context, taskID string) []fleet.Verification {
	result, ok, err := s.Store.LastEvent(ctx, taskID, fleet.EventVerification)
	if err != nil || !ok {
		return nil
	}
	start, started, err := s.Store.LastEvent(ctx, taskID, fleet.EventRunStarted)
	if err != nil || (started && result.ID <= start.ID) {
		return nil
	}
	var round struct {
		Attempt int               `json:"attempt"`
		Checks  []json.RawMessage `json:"checks"`
	}
	if err := json.Unmarshal(result.Payload, &round); err != nil || round.Attempt < 1 || len(round.Checks) == 0 {
		return nil
	}

	all, err := s.Store.ListVerifications(ctx, taskID)
	if err != nil {
		return nil
	}
	checks := make([]fleet.Verification, 0, len(round.Checks))
	for _, check := range all {
		if check.Attempt == round.Attempt {
			checks = append(checks, check)
		}
	}
	if len(checks) != len(round.Checks) {
		return nil
	}
	return checks
}

// linkedItems returns the acceptance items the task was given and the texts
// the tracker ticked for it.
func (s *FleetIssueSync) linkedItems(ctx context.Context, taskID string) ([]fleet.IssueItem, []string) {
	var items []fleet.IssueItem
	if ev, ok, _ := s.Store.LastEvent(ctx, taskID, fleet.EventIssueLinked); ok {
		var link struct {
			Items []fleet.IssueItem `json:"items"`
		}
		if json.Unmarshal(ev.Payload, &link) == nil {
			items = link.Items
		}
	}
	var ticked []string
	if ev, ok, _ := s.Store.LastEvent(ctx, taskID, fleet.EventIssueSynced); ok {
		var p struct {
			Ticked []string `json:"ticked"`
		}
		if json.Unmarshal(ev.Payload, &p) == nil {
			ticked = p.Ticked
		}
	}
	return items, ticked
}

// siblings returns the other tasks working on the same issue, with whether
// each has landed on the remote. Cancelled and failed tasks are set aside;
// the issue's own checklist says whether their work was still needed.
func (s *FleetIssueSync) siblings(ctx context.Context, task fleet.Task) []siblingState {
	all, err := s.Store.ListTasks(ctx, fleet.TaskFilter{ProjectID: task.ProjectID})
	if err != nil {
		// Cannot rule out an unfinished sibling: say so by blocking.
		return []siblingState{{Task: fleet.Task{ID: "?", Status: "unknown"}}}
	}
	var out []siblingState
	for _, other := range all {
		if other.ID == task.ID || other.Source.Kind != fleet.SourceIssue || other.Source.IssueNumber != task.Source.IssueNumber {
			continue
		}
		switch other.Status {
		case fleet.StatusCancelled, fleet.StatusFailed:
			continue
		}
		st := siblingState{Task: other, Landed: other.Status == fleet.StatusDone && other.PRNumber == 0 && strings.HasPrefix(other.StatusReason, fleetEvidenceFinishedPrefix)}
		if other.Status == fleet.StatusDone && other.PRNumber > 0 {
			if project, perr := s.Store.GetProject(ctx, other.ProjectID); perr == nil {
				if pr, perr := s.Host.PullRequest(ctx, project.RepoPath, other.PRNumber); perr == nil && pr.Merged() {
					st.Landed = true
				}
			}
		}
		if st.Landed && s.Acceptance != nil {
			state, err := s.Acceptance(ctx, other.ID)
			st.Landed = err == nil && evaluateAcceptGate(state, true).OK
		}
		out = append(out, st)
	}
	return out
}

// ErrFleetIssueSyncDisabled is returned when no sync is running.
var ErrFleetIssueSyncDisabled = errors.New("fleet: issue sync is not running")
