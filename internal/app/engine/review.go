package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

// ReviewVerdict is the normalized overall review outcome for strict review.
// Legacy advisory verdicts (partial/fail) are kept for backward compatibility.
type ReviewVerdict string

const (
	ReviewVerdictAllow   ReviewVerdict = "allow"
	ReviewVerdictBlock   ReviewVerdict = "block"
	ReviewVerdictPass    ReviewVerdict = "pass"
	ReviewVerdictFail    ReviewVerdict = "fail"
	ReviewVerdictPartial ReviewVerdict = "partial"

	VerdictAllow   = ReviewVerdictAllow
	VerdictBlock   = ReviewVerdictBlock
	VerdictPass    = ReviewVerdictPass
	VerdictFail    = ReviewVerdictFail
	VerdictPartial = ReviewVerdictPartial
)

// Verdict is kept as a compatibility alias for older callers.
type Verdict = ReviewVerdict

// ReviewPhase evaluates a completed execution and returns advisory feedback.
type ReviewPhase interface {
	Review(ctx context.Context, req ReviewRequest) (ReviewResult, error)
}

// ReviewResultStore persists a completed review for later dashboard display.
type ReviewResultStore interface {
	StoreReview(ctx context.Context, taskID string, review ReviewResult) error
}

// ReviewRequest is the execution payload sent to a reviewer model.
type ReviewRequest struct {
	TaskID         string
	ProjectDir     string
	Goal           string
	Accumulated    string
	Plan           []hermes.SubTask
	SubTaskResults []ReviewSubTaskInput
	Artifacts      []Artifact
}

// ReviewSubTaskInput is the normalized sub-task payload for the reviewer.
type ReviewSubTaskInput struct {
	ID          string   `json:"id"`
	Index       int      `json:"index"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Result      string   `json:"result"`
	ToolHints   []string `json:"tool_hints,omitempty"`
}

// ReviewIssueTag describes a categorized issue surfaced by the reviewer.
type ReviewIssueTag string

const (
	ReviewIssueTagAmbiguousGoal               ReviewIssueTag = "ambiguous_goal"
	ReviewIssueTagMissingContext              ReviewIssueTag = "missing_context"
	ReviewIssueTagWrongToolHint               ReviewIssueTag = "wrong_tool_hint"
	ReviewIssueTagUnderspecifiedInput         ReviewIssueTag = "underspecified_input"
	ReviewIssueTagMissingValidation           ReviewIssueTag = "missing_validation"
	ReviewIssueTagMissingRuntimeValidation    ReviewIssueTag = "missing_runtime_validation"
	ReviewIssueTagRuntimePackagingNotVerified ReviewIssueTag = "runtime_packaging_not_verified"
	ReviewIssueTagScopeCreep                  ReviewIssueTag = "scope_creep"
	ReviewIssueTagIncompleteTraceability      ReviewIssueTag = "incomplete_traceability"
	ReviewIssueTagLimitedTestCoverage         ReviewIssueTag = "limited_test_coverage"

	ReviewTagAmbiguousGoal               = ReviewIssueTagAmbiguousGoal
	ReviewTagMissingContext              = ReviewIssueTagMissingContext
	ReviewTagWrongToolHint               = ReviewIssueTagWrongToolHint
	ReviewTagUnderspecifiedInput         = ReviewIssueTagUnderspecifiedInput
	ReviewTagMissingValidation           = ReviewIssueTagMissingValidation
	ReviewTagMissingRuntimeValidation    = ReviewIssueTagMissingRuntimeValidation
	ReviewTagRuntimePackagingNotVerified = ReviewIssueTagRuntimePackagingNotVerified
	ReviewTagScopeCreep                  = ReviewIssueTagScopeCreep
	ReviewTagIncompleteTraceability      = ReviewIssueTagIncompleteTraceability
	ReviewTagLimitedTestCoverage         = ReviewIssueTagLimitedTestCoverage
)

// ReviewTag is kept as a compatibility alias for older callers.
type ReviewTag = ReviewIssueTag

// ReviewSubTaskResult captures reviewer feedback for one sub-task.
type ReviewSubTaskResult struct {
	SubTaskID string           `json:"sub_task_id"`
	Score     int              `json:"score"`
	Feedback  string           `json:"feedback"`
	IssueTags []ReviewIssueTag `json:"issue_tags"`
}

// ReviewResult is the normalized output returned by ReviewPhase.
type ReviewResult struct {
	ReviewerModel  string                `json:"reviewer_model,omitempty"`
	Verdict        ReviewVerdict         `json:"verdict"`
	OverallScore   int                   `json:"overall_score"`
	Feedback       string                `json:"feedback"`
	IssueTags      []ReviewIssueTag      `json:"issue_tags"`
	SubTaskResults []ReviewSubTaskResult `json:"sub_task_results"`
	InputTokens    int                   `json:"input_tokens,omitempty"`
	OutputTokens   int                   `json:"output_tokens,omitempty"`
	CostUSD        float64               `json:"cost_usd,omitempty"`
	BlockCount     int                   `json:"block_count,omitempty"`
	AutoFixedCount int                   `json:"auto_fixed_count,omitempty"`
}

// ReviewNotification is a compact summary for user-facing notifications.
type ReviewNotification struct {
	TaskID          string                            `json:"task_id"`
	ReviewerModel   string                            `json:"reviewer_model,omitempty"`
	Verdict         ReviewVerdict                     `json:"verdict"`
	OverallScore    int                               `json:"overall_score"`
	IssueTags       []ReviewIssueTag                  `json:"issue_tags,omitempty"`
	SubTaskResults  []ReviewNotificationSubTaskResult `json:"sub_task_results,omitempty"`
	AdvisoryRetry   bool                              `json:"advisory_retry"`
	FailingSubTasks int                               `json:"failing_subtasks"`
	RetryNote       string                            `json:"retry_note,omitempty"`
}

// ReviewNotificationSubTaskResult is the user-facing per-subtask review summary.
type ReviewNotificationSubTaskResult struct {
	Index     int              `json:"index"`
	SubTaskID string           `json:"sub_task_id"`
	Score     int              `json:"score"`
	Feedback  string           `json:"feedback,omitempty"`
	IssueTags []ReviewIssueTag `json:"issue_tags,omitempty"`
}

// StrictModeConfig controls hard-gate review behavior.
type StrictModeConfig struct {
	Enabled          bool
	BlockTags        []ReviewIssueTag
	MaxRetriesPerSub int
	ReviewTimeout    time.Duration
	OpponentBackend  BackendKind
}

// StrictReviewDecision summarizes strict-mode gating for a review.
type StrictReviewDecision struct {
	Verdict     ReviewVerdict    `json:"verdict"`
	MatchedTags []ReviewIssueTag `json:"matched_tags,omitempty"`
	BlockTags   []ReviewIssueTag `json:"block_tags,omitempty"`
	TimedOut    bool             `json:"timed_out,omitempty"`
	UsedBackend BackendKind      `json:"used_backend,omitempty"`
	Retryable   bool             `json:"retryable"`
}

var DefaultStrictBlockTags = []ReviewIssueTag{
	ReviewIssueTagMissingValidation,
	ReviewIssueTagMissingRuntimeValidation,
	ReviewIssueTagRuntimePackagingNotVerified,
	ReviewIssueTagScopeCreep,
	ReviewIssueTagIncompleteTraceability,
}

// DefaultStrictModeConfig returns the hard-gate defaults described by #124.
// ReviewTimeout: 120s. opus-class reviewers with --max-turns 3 (one tool round
// allowed) regularly run 20-28s on prompts up to ~40KB; the previous 30s ceiling
// killed ~21% of reviews mid-flight. See issue #147.
func DefaultStrictModeConfig() StrictModeConfig {
	return StrictModeConfig{
		BlockTags:        append([]ReviewIssueTag(nil), DefaultStrictBlockTags...),
		MaxRetriesPerSub: 2,
		ReviewTimeout:    120 * time.Second,
		OpponentBackend:  BackendAuto,
	}
}

// WithDefaults fills zero-value fields with the default strict mode settings.
func (c StrictModeConfig) WithDefaults() StrictModeConfig {
	if len(c.BlockTags) == 0 {
		c.BlockTags = append([]ReviewIssueTag(nil), DefaultStrictBlockTags...)
	}
	if c.MaxRetriesPerSub <= 0 {
		c.MaxRetriesPerSub = 2
	}
	if c.ReviewTimeout <= 0 {
		c.ReviewTimeout = 120 * time.Second
	}
	if c.OpponentBackend != BackendAuto && c.OpponentBackend != BackendCodex {
		c.OpponentBackend = BackendAuto
	}
	return c
}

func (c StrictModeConfig) blockTagSet() map[ReviewIssueTag]struct{} {
	c = c.WithDefaults()
	set := make(map[ReviewIssueTag]struct{}, len(c.BlockTags))
	for _, tag := range c.BlockTags {
		set[tag] = struct{}{}
	}
	return set
}

// ReviewTags returns the de-duplicated union of overall and sub-task issue tags.
func ReviewTags(review ReviewResult) []ReviewIssueTag {
	seen := make(map[ReviewIssueTag]struct{})
	out := make([]ReviewIssueTag, 0, len(review.IssueTags))
	add := func(tags []ReviewIssueTag) {
		for _, tag := range tags {
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			out = append(out, tag)
		}
	}
	add(review.IssueTags)
	for _, subTask := range review.SubTaskResults {
		add(subTask.IssueTags)
	}
	return out
}

// ReviewDecisionFromStrictTags maps reviewer output to a hard-gate verdict.
func ReviewDecisionFromStrictTags(review ReviewResult, cfg StrictModeConfig) StrictReviewDecision {
	cfg = cfg.WithDefaults()
	if !cfg.Enabled {
		return StrictReviewDecision{
			Verdict:   VerdictPass,
			Retryable: false,
		}
	}

	tags := ReviewTags(review)
	blockTags := strictMatchedBlockTags(tags, cfg.blockTagSet())
	decision := StrictReviewDecision{
		MatchedTags: tags,
		BlockTags:   append([]ReviewIssueTag(nil), blockTags...),
		UsedBackend: cfg.OpponentBackend,
	}
	if len(blockTags) > 0 {
		decision.Verdict = VerdictBlock
		decision.Retryable = true
		return decision
	}
	decision.Verdict = VerdictAllow
	return decision
}

func strictMatchedBlockTags(tags []ReviewIssueTag, blockSet map[ReviewIssueTag]struct{}) []ReviewIssueTag {
	matches := make([]ReviewIssueTag, 0)
	for _, tag := range tags {
		if _, ok := blockSet[tag]; ok {
			matches = append(matches, tag)
		}
	}
	return matches
}

// ResolveStrictReviewBackend chooses the review backend, preferring the opposite
// backend when available and falling back to the executor backend when not.
func ResolveStrictReviewBackend(executor BackendKind, cfg StrictModeConfig, available ...BackendKind) BackendKind {
	cfg = cfg.WithDefaults()
	if !cfg.Enabled {
		return executor
	}

	preferred := cfg.OpponentBackend
	if preferred == BackendAuto {
		preferred = oppositeBackend(executor)
	}
	if preferred == BackendClaude || preferred == BackendCodex {
		if backendAvailable(preferred, available...) {
			return preferred
		}
	}
	if backendAvailable(executor, available...) {
		return executor
	}
	if preferred == BackendClaude || preferred == BackendCodex {
		return preferred
	}
	return executor
}

func backendAvailable(backend BackendKind, available ...BackendKind) bool {
	if len(available) == 0 {
		return true
	}
	for _, candidate := range available {
		if candidate == backend {
			return true
		}
	}
	return false
}

func oppositeBackend(backend BackendKind) BackendKind {
	switch backend {
	case BackendClaude:
		return BackendCodex
	case BackendCodex:
		return BackendClaude
	default:
		return BackendClaude
	}
}

// ReviewPhaseWithTimeout wraps a review call with a hard timeout. When the
// timeout is hit, callers can treat the result as advisory pass.
func ReviewPhaseWithTimeout(ctx context.Context, phase ReviewPhase, req ReviewRequest, timeout time.Duration) (ReviewResult, error) {
	if phase == nil {
		return ReviewResult{}, fmt.Errorf("review phase is not configured")
	}
	if timeout <= 0 {
		return phase.Review(ctx, req)
	}

	reviewCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	result, err := phase.Review(reviewCtx, req)
	elapsed := time.Since(start)
	// Surface near-timeout reviews so operators can tell whether the limit is
	// being approached without (yet) firing. See issue #147.
	if elapsed >= 60*time.Second {
		log.Printf("[review] slow review: model=%s duration=%s timeout=%s", strings.TrimSpace(result.ReviewerModel), elapsed.Round(time.Millisecond), timeout)
	}
	if err == nil {
		return result, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(reviewCtx.Err(), context.DeadlineExceeded) {
		return ReviewResult{Verdict: VerdictPass}, fmt.Errorf("review timeout after %s: %w", timeout, err)
	}
	return result, err
}

// IsStrictRetryable reports whether a review should be retried under strict mode.
func IsStrictRetryable(decision StrictReviewDecision) bool {
	return decision.Verdict == VerdictBlock
}

// BuildReviewNotification normalizes a review into a concise summary.
func BuildReviewNotification(taskID string, review ReviewResult) ReviewNotification {
	failingSubTasks := 0
	subTaskResults := make([]ReviewNotificationSubTaskResult, 0, len(review.SubTaskResults))
	for idx, subTask := range review.SubTaskResults {
		if subTask.Score < 70 {
			failingSubTasks++
		}
		subTaskResults = append(subTaskResults, ReviewNotificationSubTaskResult{
			Index:     reviewSubTaskDisplayIndex(subTask.SubTaskID, idx),
			SubTaskID: strings.TrimSpace(subTask.SubTaskID),
			Score:     subTask.Score,
			Feedback:  strings.TrimSpace(subTask.Feedback),
			IssueTags: append([]ReviewIssueTag(nil), subTask.IssueTags...),
		})
	}
	sort.SliceStable(subTaskResults, func(i, j int) bool {
		if subTaskResults[i].Score != subTaskResults[j].Score {
			return subTaskResults[i].Score < subTaskResults[j].Score
		}
		return subTaskResults[i].Index < subTaskResults[j].Index
	})

	notification := ReviewNotification{
		TaskID:          strings.TrimSpace(taskID),
		ReviewerModel:   strings.TrimSpace(review.ReviewerModel),
		Verdict:         review.Verdict,
		OverallScore:    review.OverallScore,
		IssueTags:       append([]ReviewTag(nil), review.IssueTags...),
		SubTaskResults:  subTaskResults,
		AdvisoryRetry:   review.Verdict != VerdictPass && review.Verdict != VerdictAllow || failingSubTasks > 0,
		FailingSubTasks: failingSubTasks,
	}
	if notification.AdvisoryRetry {
		if failingSubTasks > 0 {
			notification.RetryNote = fmt.Sprintf("建議人工評估後重跑 %d 個失敗/低分子任務", failingSubTasks)
		} else {
			notification.RetryNote = "建議人工評估後決定是否重跑"
		}
	} else {
		notification.RetryNote = "暫無需重跑"
	}
	return notification
}

// TelegramText renders the notification as a concise chat message.
func (n ReviewNotification) TelegramText() string {
	var b strings.Builder
	b.WriteString("🔎 複審完成\n")
	if n.TaskID != "" {
		b.WriteString("任務：" + shortReviewTaskID(n.TaskID) + "\n\n")
	}
	b.WriteString(fmt.Sprintf("結果：%s（%d/100）\n", n.Verdict, n.OverallScore))
	b.WriteString("整體標籤：" + formatReviewTags(n.IssueTags) + "\n")
	if len(n.SubTaskResults) > 0 {
		b.WriteString("\n子任務：\n")
		for _, subTask := range n.SubTaskResults {
			marker := "✅"
			if subTask.Score < 60 {
				marker = "❌"
			} else if subTask.Score < 80 {
				marker = "⚠️"
			}
			id := strings.TrimSpace(subTask.SubTaskID)
			if id == "" {
				id = "unknown"
			}
			b.WriteString(fmt.Sprintf("  %s #%d %s (%d/100)", marker, subTask.Index, id, subTask.Score))
			if len(subTask.IssueTags) > 0 {
				b.WriteString(" — " + formatReviewTags(subTask.IssueTags))
			}
			b.WriteString("\n")
			if feedback := truncateReviewFeedback(subTask.Feedback, 80); feedback != "" {
				b.WriteString("     " + feedback + "\n")
			}
		}
	}
	b.WriteString("\n重跑建議：" + n.RetryNote)
	if n.TaskID != "" {
		shortID := shortReviewTaskID(n.TaskID)
		if len(n.SubTaskResults) > 0 {
			b.WriteString(fmt.Sprintf("\n重跑：/retry %s %d  （單一）/  /retry %s all-failed  （全部低分）", shortID, n.SubTaskResults[0].Index, shortID))
		} else {
			b.WriteString(fmt.Sprintf("\n重跑：/retry %s", shortID))
		}
	}
	return b.String()
}

func formatReviewTags(tags []ReviewIssueTag) string {
	if len(tags) == 0 {
		return "無"
	}
	items := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		items = append(items, string(tag))
	}
	if len(items) == 0 {
		return "無"
	}
	return strings.Join(items, ", ")
}

func shortReviewTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if len(taskID) <= 8 {
		return taskID
	}
	return taskID[:8]
}

func truncateReviewFeedback(feedback string, maxRunes int) string {
	feedback = strings.Join(strings.Fields(strings.TrimSpace(feedback)), " ")
	if feedback == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(feedback)
	if len(runes) <= maxRunes {
		return feedback
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

var reviewSubTaskIndexRe = regexp.MustCompile(`(?i)(?:^|[^0-9])s?([1-9][0-9]*)$`)

func reviewSubTaskDisplayIndex(subTaskID string, fallbackZeroBased int) int {
	if match := reviewSubTaskIndexRe.FindStringSubmatch(strings.TrimSpace(subTaskID)); len(match) == 2 {
		var idx int
		if _, err := fmt.Sscanf(match[1], "%d", &idx); err == nil && idx > 0 {
			return idx
		}
	}
	return fallbackZeroBased + 1
}

// BuildReviewPrompt assembles the first-version reviewer prompt for #119.
func BuildReviewPrompt(req ReviewRequest) string {
	type reviewPromptArtifact struct {
		Path      string `json:"path"`
		Hash      string `json:"hash,omitempty"`
		SubTaskID string `json:"sub_task_id,omitempty"`
	}

	type reviewPromptPayload struct {
		TaskID         string                 `json:"task_id,omitempty"`
		ProjectDir     string                 `json:"project_dir,omitempty"`
		Goal           string                 `json:"goal"`
		Accumulated    string                 `json:"accumulated,omitempty"`
		Plan           []hermes.SubTask       `json:"plan"`
		SubTaskResults []ReviewSubTaskInput   `json:"sub_task_results"`
		Artifacts      []reviewPromptArtifact `json:"artifacts,omitempty"`
	}

	payload := reviewPromptPayload{
		TaskID:         strings.TrimSpace(req.TaskID),
		ProjectDir:     strings.TrimSpace(req.ProjectDir),
		Goal:           strings.TrimSpace(req.Goal),
		Accumulated:    strings.TrimSpace(req.Accumulated),
		Plan:           req.Plan,
		SubTaskResults: req.SubTaskResults,
	}
	if len(req.Artifacts) > 0 {
		payload.Artifacts = make([]reviewPromptArtifact, 0, len(req.Artifacts))
		for _, artifact := range req.Artifacts {
			payload.Artifacts = append(payload.Artifacts, reviewPromptArtifact{
				Path:      artifact.Path,
				Hash:      artifact.Hash,
				SubTaskID: artifact.SubTaskID,
			})
		}
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		body = []byte("{}")
	}

	var b strings.Builder
	b.WriteString("You are the ReviewPhase reviewer for a Hermes execution pipeline.\n")
	b.WriteString("Evaluate the completed task conservatively. Your output is advisory, but it must be concrete and evidence-based.\n")
	b.WriteString("Return exactly one JSON object with this shape:\n")
	b.WriteString("{\"verdict\":\"pass|partial|fail\",\"overall_score\":0,\"feedback\":\"...\",\"issue_tags\":[\"...\"],\"sub_task_results\":[{\"sub_task_id\":\"...\",\"score\":0,\"feedback\":\"...\",\"issue_tags\":[\"...\"]}]}\n")
	b.WriteString("Rules:\n")
	b.WriteString("- overall_score and every sub-task score must be integers from 0 to 100.\n")
	b.WriteString("- verdict must be one of pass, partial, fail.\n")
	b.WriteString("- issue_tags must be JSON arrays of snake_case labels such as ambiguous_goal, missing_context, wrong_tool_hint, underspecified_input, missing_validation, missing_runtime_validation, runtime_packaging_not_verified, scope_creep, incomplete_traceability.\n")
	b.WriteString("- Give specific feedback tied to the goal, plan, sub-task result quality, and artifacts.\n")
	b.WriteString("- Mark pass only when the goal appears satisfied and no material risks remain.\n")
	b.WriteString("- If context is insufficient, say so explicitly and add the relevant issue_tags.\n")
	b.WriteString("- Do not wrap the JSON in Markdown fences.\n\n")
	b.WriteString("Execution payload:\n")
	b.Write(body)
	b.WriteString("\n\n")
	b.WriteString("Review the execution now and return JSON only.")
	return b.String()
}

// ReviewInputsFromPlan normalizes planner sub-tasks plus their final results.
func ReviewInputsFromPlan(plan []hermes.SubTask) []ReviewSubTaskInput {
	out := make([]ReviewSubTaskInput, 0, len(plan))
	for idx, subTask := range plan {
		out = append(out, ReviewSubTaskInput{
			ID:          subTask.ID,
			Index:       idx,
			Description: subTask.Description,
			Status:      string(subTask.Status),
			Result:      strings.TrimSpace(subTask.Result),
			ToolHints:   append([]string(nil), subTask.ToolHints...),
		})
	}
	return out
}

func (v Verdict) Valid() bool {
	switch v {
	case VerdictAllow, VerdictBlock, VerdictPass, VerdictFail, VerdictPartial:
		return true
	default:
		return false
	}
}

func (r ReviewResult) Validate() error {
	if !r.Verdict.Valid() {
		return fmt.Errorf("invalid verdict %q", r.Verdict)
	}
	if r.OverallScore < 0 || r.OverallScore > 100 {
		return fmt.Errorf("overall_score out of range: %d", r.OverallScore)
	}
	// Reject verdict/score contradictions. Reviewer models occasionally return
	// {"verdict":"pass","overall_score":0} or similar nonsense; without this
	// guard the engine stored and notified the user with "pass (0/100)" — the
	// verdict claimed success but the score said the opposite.
	switch r.Verdict {
	case VerdictPass:
		if r.OverallScore < 70 {
			return fmt.Errorf("verdict=pass requires overall_score >= 70 (got %d)", r.OverallScore)
		}
	case VerdictFail:
		if r.OverallScore >= 70 {
			return fmt.Errorf("verdict=fail requires overall_score < 70 (got %d)", r.OverallScore)
		}
	}
	for _, subTask := range r.SubTaskResults {
		if subTask.Score < 0 || subTask.Score > 100 {
			return fmt.Errorf("sub-task %q score out of range: %d", subTask.SubTaskID, subTask.Score)
		}
	}
	return nil
}

var reviewJSONBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*?\\})\\s*```")

// ParseReviewResult decodes reviewer JSON from either raw text or a fenced code block.
func ParseReviewResult(text string) (ReviewResult, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return ReviewResult{}, fmt.Errorf("review output is empty")
	}
	if m := reviewJSONBlockRe.FindStringSubmatch(raw); len(m) > 1 {
		raw = strings.TrimSpace(m[1])
	} else if !strings.HasPrefix(raw, "{") {
		// Model returned prose before the JSON object — extract the first {...} block.
		if start := strings.Index(raw, "{"); start >= 0 {
			raw = raw[start:]
			if end := strings.LastIndex(raw, "}"); end >= 0 {
				raw = raw[:end+1]
			}
		}
	}
	var result ReviewResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return ReviewResult{}, err
	}
	if err := result.Validate(); err != nil {
		return ReviewResult{}, err
	}
	return result, nil
}
