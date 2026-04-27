package hermes

import (
	"fmt"
	"strings"
	"time"
)

// Verbosity controls how much progress information is sent to the user.
type Verbosity int

const (
	VerbosityMinimal Verbosity = iota // plan summary + final result only
	VerbosityNormal                   // + sub-task start/done events
	VerbosityVerbose                  // + retry and validation details
)

// ParseVerbosity converts a config string to Verbosity.
func ParseVerbosity(s string) Verbosity {
	switch s {
	case "verbose":
		return VerbosityVerbose
	case "minimal":
		return VerbosityMinimal
	default:
		return VerbosityNormal
	}
}

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
type TextProgressReporter struct {
	verbosity Verbosity
	sendFn    func(text string)
}

// NewTextProgressReporter creates a reporter that calls sendFn for each event.
func NewTextProgressReporter(verbosity Verbosity, sendFn func(string)) *TextProgressReporter {
	return &TextProgressReporter{verbosity: verbosity, sendFn: sendFn}
}

func (r *TextProgressReporter) OnPlanReady(tasks []SubTask) {
	var lines []string
	lines = append(lines, fmt.Sprintf("計畫完成，共 %d 個子任務：", len(tasks)))
	for i, t := range tasks {
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, t.Description))
	}
	r.sendFn(join(lines))
}

func (r *TextProgressReporter) OnSubTaskStart(idx, total int, task SubTask) {
	if r.verbosity < VerbosityNormal {
		return
	}
	r.sendFn(fmt.Sprintf("[%d/%d] 執行：%s", idx+1, total, task.Description))
}

func (r *TextProgressReporter) OnSubTaskDone(idx, total int, task SubTask, success bool, result string) {
	// Always notify when there is only one sub-task — otherwise the user sees
	// the start message and then silence until OnDone, which looks like a hang.
	if r.verbosity < VerbosityNormal && total > 1 {
		return
	}
	icon := "✅"
	if !success {
		icon = "❌"
	}
	msg := fmt.Sprintf("%s [%d/%d] %s", icon, idx+1, total, task.Description)
	// Always surface the result body when the sub-task failed — otherwise
	// the user sees a bare ❌ line with no diagnostics. Successful results
	// stay gated by Verbose since they are usually long four-section
	// reports already shown via OnContent.
	if result != "" && (!success || r.verbosity >= VerbosityVerbose) {
		msg += "\n" + result
	}
	r.sendFn(msg)
}

func (r *TextProgressReporter) OnRetry(idx, attempt, maxAttempts int, validationErr string) {
	if r.verbosity < VerbosityVerbose {
		return
	}
	r.sendFn(fmt.Sprintf("⚠️ 子任務 %d 重試中 (%d/%d)：%s", idx+1, attempt, maxAttempts, validationErr))
}

func (r *TextProgressReporter) OnDone(state TaskState) {
	total := len(state.Plan)
	done := 0
	for _, t := range state.Plan {
		if t.Status == SubTaskDone {
			done++
		}
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

	r.sendFn(join(lines))
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
	r.sendFn(fmt.Sprintf("⚠️ 預算即將耗盡（%s），是否繼續？", budget.BudgetStatus()))
}

func (r *TextProgressReporter) OnError(err error) {
	r.sendFn(fmt.Sprintf("❌ Hermes 錯誤：%v", err))
}

// NoopProgressReporter silently discards all events.
type NoopProgressReporter struct{}

func (n *NoopProgressReporter) OnPlanReady(_ []SubTask)                                         {}
func (n *NoopProgressReporter) OnSubTaskStart(_, _ int, _ SubTask)                              {}
func (n *NoopProgressReporter) OnSubTaskDone(_, _ int, _ SubTask, _ bool, _ string)             {}
func (n *NoopProgressReporter) OnRetry(_, _, _ int, _ string)                                   {}
func (n *NoopProgressReporter) OnDone(_ TaskState)                                               {}
func (n *NoopProgressReporter) OnBudgetWarning(_ TokenBudget)                                    {}
func (n *NoopProgressReporter) OnError(_ error)                                                  {}

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
