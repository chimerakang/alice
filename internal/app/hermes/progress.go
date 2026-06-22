package hermes

import (
	"fmt"
	"strings"
	"time"
)

// ProgressReporter receives lifecycle events and delivers them to the user.
// All methods may be called from a goroutine different from the one that created it.
type ProgressReporter interface {
	// OnPlanReady is called once the Planner has produced a sub-task list.
	OnPlanReady(tasks []SubTask)

	// OnSubTaskStart is called immediately before dispatching an Executor.
	OnSubTaskStart(idx, total int, task SubTask)

	// OnSubTaskDone is called when a sub-task completes (success or failure).
	OnSubTaskDone(idx, total int, task SubTask, success bool, result string)

	// OnRetry is called when a sub-task validator failed and a retry is starting.
	OnRetry(idx, attempt, maxAttempts int, validationErr string)

	// OnDone is called when the entire task reaches a terminal state.
	OnDone(state TaskState)

	// OnBudgetWarning is called when token or wallclock usage exceeds 80%.
	OnBudgetWarning(budget TokenBudget)

	// OnError reports an unrecoverable error (e.g. Planner JSON failure after retries).
	OnError(err error)
}

// TextProgressReporter formats events as human-readable strings and forwards
// them to a send function (e.g. TelegramBot.send).
//
// When configured with edit-in-place via WithEditCapability, plan summary +
// per-sub-task progress collapse onto a single message that updates as work
// advances. Failures still post their own diagnostic so operators see the
// error immediately; OnDone posts the final report as a separate message.
//
// Without edit support, the reporter falls back to a quiet stream: plan
// summary up front, diagnostic only on failure, and the OnDone final report.
type TextProgressReporter struct {
	sendFn        func(text string, notify bool)
	sendCaptureFn func(text string) (int, error) // optional; nil disables edit-in-place
	editFn        func(messageID int, text string) error

	progressMsgID int
	planSummary   string // header reused across edits

	// Edit throttling. Telegram per-chat rate limits aggressively when bursts
	// of edits hit the same message; without these guards a 7-sub-task plan
	// can drive ~14 edits in a minute on the same chat and trigger 429s.
	lastEditText string
	lastEditAt   time.Time
}

// editCooldown is the minimum gap between two edits to the same progress
// message. Picked just above Telegram's per-chat 1 msg/s soft limit so a fast
// run does not stack edits faster than the API tolerates.
const editCooldown = 1500 * time.Millisecond

// NewTextProgressReporter creates a reporter that calls sendFn for each event.
func NewTextProgressReporter(sendFn func(string)) *TextProgressReporter {
	return NewTextProgressReporterWithNotify(func(text string, _ bool) {
		sendFn(text)
	})
}

// NewTextProgressReporterWithNotify creates a reporter that labels whether each
// event should produce a user-visible notification sound.
func NewTextProgressReporterWithNotify(sendFn func(text string, notify bool)) *TextProgressReporter {
	return &TextProgressReporter{sendFn: sendFn}
}

// WithEditCapability enables edit-in-place mode: OnPlanReady posts an initial
// progress message and remembers its ID; subsequent OnSubTaskStart and
// successful OnSubTaskDone events rewrite that single message instead of
// flooding the chat. Pass nil functions to leave the reporter in fallback
// (send-only) mode.
func (r *TextProgressReporter) WithEditCapability(
	sendCapture func(text string) (int, error),
	edit func(messageID int, text string) error,
) *TextProgressReporter {
	r.sendCaptureFn = sendCapture
	r.editFn = edit
	return r
}

func (r *TextProgressReporter) editProgress(text string) {
	if r.editFn == nil || r.progressMsgID == 0 {
		return
	}
	// Dedup: Telegram returns 400 "message is not modified" for identical
	// content but we still pay an API call and a rate-limit slot.
	if text == r.lastEditText {
		return
	}
	// Cooldown: skip edits that arrive faster than editCooldown so bursts of
	// sub-task transitions cannot starve the per-chat rate budget. The user
	// loses the dropped intermediate frame; the next edit overwrites with the
	// most recent state anyway.
	if !r.lastEditAt.IsZero() && time.Since(r.lastEditAt) < editCooldown {
		return
	}
	if err := r.editFn(r.progressMsgID, text); err != nil {
		// Edit failed (e.g. message deleted or unchanged). Disable further
		// edit attempts so we do not spam the API with retries.
		r.progressMsgID = 0
		return
	}
	r.lastEditText = text
	r.lastEditAt = time.Now()
}

func (r *TextProgressReporter) OnPlanReady(tasks []SubTask) {
	r.planSummary = fmt.Sprintf("📋 計畫完成，共 %d 個子任務", len(tasks))
	initial := r.planSummary + "\n⏳ 準備開始…"
	if r.sendCaptureFn != nil {
		if id, err := r.sendCaptureFn(initial); err == nil {
			r.progressMsgID = id
			return
		}
	}
	r.sendFn(initial, false)
}

func (r *TextProgressReporter) OnSubTaskStart(idx, total int, task SubTask) {
	// Intentionally silent. OnSubTaskDone is what edits the progress message;
	// emitting an extra "▶ [N/M]" edit on every start doubles the API call
	// rate and triggers Telegram per-chat 429s on long plans.
}

func (r *TextProgressReporter) OnSubTaskDone(idx, total int, task SubTask, success bool, result string) {
	if !success {
		icon := "❌"
		label := ClassifyFailure(result).Label()
		if task.Status == SubTaskSkipped || isPartialSubTaskResult(result) {
			icon = "⚠️"
			label = "部分完成，待確認"
		}
		// Surface incomplete sub-tasks as a new sticky, *notifying* message —
		// operators must see these immediately. Keep true execution failures
		// visually distinct from strict-review partial results.
		header := fmt.Sprintf("%s [%d/%d] %s — %s", icon, idx+1, total, task.Description, label)
		msg := header
		if result != "" {
			msg += "\n" + result
		}
		r.sendFn(msg, true)
		if r.progressMsgID != 0 {
			r.editProgress(fmt.Sprintf("%s\n%s [%d/%d] %s", r.planSummary, icon, idx+1, total, label))
		}
		return
	}
	if r.progressMsgID == 0 {
		return
	}
	r.editProgress(fmt.Sprintf("%s\n✓ [%d/%d] 完成，準備下一步…", r.planSummary, idx+1, total))
}

func isPartialSubTaskResult(result string) bool {
	return strings.HasPrefix(strings.TrimSpace(result), "PARTIAL")
}

func (r *TextProgressReporter) OnRetry(idx, attempt, maxAttempts int, validationErr string) {
	if r.progressMsgID == 0 {
		return
	}
	r.editProgress(fmt.Sprintf("%s\n🔁 [%d/?] 重試中 (%d/%d)", r.planSummary, idx+1, attempt, maxAttempts))
}

func (r *TextProgressReporter) OnDone(state TaskState) {
	total := len(state.Plan)
	done := 0
	for _, t := range state.Plan {
		if t.Status == SubTaskDone {
			done++
		}
	}

	// Wrap up the in-place progress message so it stops looking pending. The
	// real summary is sent below as a fresh, sticky message.
	if r.progressMsgID != 0 {
		r.editProgress(fmt.Sprintf("%s\n✅ 完成（%d/%d 子任務成功）", r.planSummary, done, total))
		r.progressMsgID = 0
	}

	lines := []string{
		fmt.Sprintf("✅ Hermes 任務完成（%d/%d 子任務成功）", done, total),
	}

	// Per sub-task conclusion: read each sub-task's Result field, which is the
	// Executor's ≤ 2-line summary. Far more useful than the rolling
	// Accumulated text, which is mid-task narration.
	if hasAnyResult(state.Plan) {
		lines = append(lines, "", "📋 子任務結果：")
		for i, t := range state.Plan {
			icon := "✅"
			switch t.Status {
			case SubTaskFailed:
				icon = "❌"
			case SubTaskPending, SubTaskInProgress:
				icon = "⏸️"
			case SubTaskSkipped:
				icon = "⏭️"
			}
			result := strings.TrimSpace(t.Result)
			if result == "" {
				result = "（無回報）"
			}
			lines = append(lines, fmt.Sprintf("  %s %d. %s", icon, i+1, truncateRunes(result, 240)))
		}
	}

	if len(state.Artifacts) > 0 {
		lines = append(lines, "", "📝 已修改檔案：")
		for _, a := range state.Artifacts {
			lines = append(lines, "  • "+a.Path)
		}
	}

	// Token usage summary (#102). Always render when ModelUsages is populated —
	// the Coordinator only populates it once operators opt-in via
	// hermes.summary.enabled, so absence here is the disabled case.
	if len(state.ModelUsages) > 0 {
		wallclock := 0
		if !state.TokenBudget.StartedAt.IsZero() {
			wallclock = int(time.Since(state.TokenBudget.StartedAt).Seconds())
		}
		summary := TaskSummary{
			TaskState:        &state,
			WallclockSeconds: wallclock,
			Verbosity:        "minimal",
		}
		lines = append(lines, "", summary.GenerateSummary())
	}

	// Telegram caps each text message at 4096 UTF-8 chars. Long plans
	// (15-step issue checklists) easily blow past that and Telegram returns
	// 400 "message is too long", silently dropping the entire OnDone
	// summary. paginate splits on logical line breaks under telegramMessageMaxRunes.
	for i, page := range paginateForTelegram(lines, telegramMessageMaxRunes) {
		// Notify only on the first page so the user gets one ping, not three.
		r.sendFn(page, i == 0)
	}
}

// telegramMessageMaxRunes leaves headroom under Telegram's 4096-char limit so
// emoji-heavy summaries (each emoji counts as 2-4 bytes) stay well within
// bounds even after sanitiseUTF8 rewrites.
const telegramMessageMaxRunes = 3500

// paginateForTelegram splits a slice of summary lines into chunks that each
// fit under maxRunes. Returns at least one page (possibly empty when input
// is empty). Adds a "(n/m)" footer to each page when more than one page is
// produced so the operator can see they have a multi-part summary.
func paginateForTelegram(lines []string, maxRunes int) []string {
	if len(lines) == 0 {
		return []string{""}
	}

	var pages []string
	var current []string
	currentRunes := 0
	for _, line := range lines {
		lineRunes := len([]rune(line)) + 1 // +1 for the join newline
		if currentRunes+lineRunes > maxRunes && len(current) > 0 {
			pages = append(pages, join(current))
			current = current[:0]
			currentRunes = 0
		}
		current = append(current, line)
		currentRunes += lineRunes
	}
	if len(current) > 0 {
		pages = append(pages, join(current))
	}

	if len(pages) <= 1 {
		return pages
	}
	for i := range pages {
		pages[i] = fmt.Sprintf("%s\n\n（%d/%d）", pages[i], i+1, len(pages))
	}
	return pages
}

func hasAnyResult(plan []SubTask) bool {
	for _, t := range plan {
		if strings.TrimSpace(t.Result) != "" {
			return true
		}
	}
	return false
}

func truncateRunes(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + "…"
}

func (r *TextProgressReporter) OnBudgetWarning(budget TokenBudget) {
	r.sendFn(fmt.Sprintf("⚠️ 預算即將耗盡（%s），是否繼續？", budget.BudgetStatus()), true)
}

func (r *TextProgressReporter) OnError(err error) {
	r.sendFn(fmt.Sprintf("❌ Hermes 錯誤：%v", err), true)
}

// NoopProgressReporter silently discards all events.
type NoopProgressReporter struct{}

func (n *NoopProgressReporter) OnPlanReady(_ []SubTask)                             {}
func (n *NoopProgressReporter) OnSubTaskStart(_, _ int, _ SubTask)                  {}
func (n *NoopProgressReporter) OnSubTaskDone(_, _ int, _ SubTask, _ bool, _ string) {}
func (n *NoopProgressReporter) OnRetry(_, _, _ int, _ string)                       {}
func (n *NoopProgressReporter) OnDone(_ TaskState)                                  {}
func (n *NoopProgressReporter) OnBudgetWarning(_ TokenBudget)                       {}
func (n *NoopProgressReporter) OnError(_ error)                                     {}

func join(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
