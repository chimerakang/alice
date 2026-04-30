package hermes

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// cheapPathMaxBytes is the FIFO truncation limit for the cheap path.
	// Tightened from 2 KB to 1.5 KB so accumulated stays small enough that
	// the executor prompt does not bloat into "Prompt is too long" mid-task.
	cheapPathMaxBytes = 1536 // 1.5 KB

	// expensivePathTriggerBytes triggers a compression call when accumulated
	// exceeds this. Tightened from 8 KB to 3 KB — by the time accumulated
	// reaches 8 KB the compounded prompt is already too large.
	expensivePathTriggerBytes = 3 * 1024

	// expensivePathTriggerCount triggers compression after this many completed
	// sub-tasks. Tightened from 10 → 5 so long plans collapse early rather
	// than collecting four-section reports verbatim.
	expensivePathTriggerCount = 5

	// perSubtaskConclusionMaxBytes caps each sub-task's contribution to
	// accumulated to its conclusion line plus light context. Aggressively
	// drops the executor's "證據 / 未驗證 / 下一步" sections — those live in
	// state.Plan[i].Result and the dashboard, but do not need to be replayed
	// into every following sub-task's prompt.
	perSubtaskConclusionMaxBytes = 320
)

// AccumulatedConfig allows overriding the default thresholds via config.
type AccumulatedConfig struct {
	CheapMaxBytes      int `json:"cheap_max_bytes"`       // default 2048
	ExpensiveTriggerKB int `json:"expensive_trigger_kb"`  // default 8
	ExpensiveTriggerN  int `json:"expensive_trigger_n"`   // default 10
}

func (c AccumulatedConfig) cheapMax() int {
	if c.CheapMaxBytes > 0 {
		return c.CheapMaxBytes
	}
	return cheapPathMaxBytes
}

func (c AccumulatedConfig) expensiveTriggerBytes() int {
	if c.ExpensiveTriggerKB > 0 {
		return c.ExpensiveTriggerKB * 1024
	}
	return expensivePathTriggerBytes
}

func (c AccumulatedConfig) expensiveTriggerN() int {
	if c.ExpensiveTriggerN > 0 {
		return c.ExpensiveTriggerN
	}
	return expensivePathTriggerCount
}

// conclusionPattern matches the executor's structured "**結論**：…" block. The
// executor rules require a four-section report (結論/證據/未驗證/下一步); for
// downstream sub-tasks we only need the conclusion — evidence/gaps/next-steps
// belong in state.Plan[i].Result and the dashboard, not in every following
// prompt. Both Chinese full-width and ASCII colons are tolerated.
var conclusionPattern = regexp.MustCompile(`\*\*結論\*\*[：:]\s*([^\n]+)`)

// extractConclusion returns a compact summary of a sub-task result suitable for
// re-injection into the next sub-task's prompt. Prefers the "**結論**：…" line
// when present; otherwise falls back to the first non-empty line. Always
// truncated to perSubtaskConclusionMaxBytes runes to keep the rolling
// accumulated bounded regardless of executor verbosity.
func extractConclusion(subtaskResult string) string {
	trimmed := strings.TrimSpace(subtaskResult)
	if trimmed == "" {
		return ""
	}
	if m := conclusionPattern.FindStringSubmatch(trimmed); len(m) >= 2 {
		conclusion := strings.TrimSpace(m[1])
		return "結論：" + truncateRunesAccumulated(conclusion, perSubtaskConclusionMaxBytes)
	}
	// Fallback: first non-empty line, capped.
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncateRunesAccumulated(line, perSubtaskConclusionMaxBytes)
		}
	}
	return ""
}

// FailureKind labels a sub-task failure so the reporter can show whether the
// error was an environment problem (prompt too long, network timeout, etc)
// versus a content failure inside the executor's actual work. Operators react
// differently: env failures usually mean "shrink scope or wait", content
// failures mean "look at the diagnostic".
type FailureKind int

const (
	FailureUnknown FailureKind = iota
	FailureEnv
	FailureContent
)

func (k FailureKind) Label() string {
	switch k {
	case FailureEnv:
		return "環境錯誤"
	case FailureContent:
		return "執行錯誤"
	default:
		return "失敗"
	}
}

// envFailurePatterns matches error messages that come from the CLI/network
// layer rather than the executor's own work. These typically resolve by
// shrinking prompt scope or retrying later, not by editing code.
var envFailurePatterns = []string{
	"prompt is too long",
	"context length",
	"request entity too large",
	"context_length_exceeded",
	"deadline exceeded",
	"context deadline",
	"i/o timeout",
	"connection refused",
	"connection reset",
	"connection timed out",
	"no such host",
	"tls handshake",
	"eof",
	"signal: killed",
	"rate limit",
	"too many requests",
	"503 service unavailable",
	"504 gateway timeout",
	"upstream connect error",
}

// ClassifyFailure returns the failure category for a sub-task error string.
// Returns FailureUnknown for empty input so callers can short-circuit.
func ClassifyFailure(errText string) FailureKind {
	trimmed := strings.TrimSpace(errText)
	if trimmed == "" {
		return FailureUnknown
	}
	lower := strings.ToLower(trimmed)
	for _, p := range envFailurePatterns {
		if strings.Contains(lower, p) {
			return FailureEnv
		}
	}
	return FailureContent
}

func truncateRunesAccumulated(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + "…"
}

// AppendResult appends a sub-task result to the accumulated summary. Each
// sub-task contributes only its conclusion line (see extractConclusion); the
// full four-section report stays in state.Plan[i].Result so the dashboard and
// final OnDone summary still have it. The aggregate is FIFO-truncated to
// cheapMax bytes to bound the rolling buffer.
func AppendResult(accumulated, subtaskResult string, completedCount int, cfg AccumulatedConfig) (updated string, needsCompression bool) {
	conclusion := extractConclusion(subtaskResult)
	if conclusion == "" {
		return accumulated, false
	}

	updated = accumulated
	if updated != "" {
		updated += "\n"
	}
	updated += conclusion

	// FIFO truncation: keep the tail (most recent content)
	if len(updated) > cfg.cheapMax() {
		updated = updated[len(updated)-cfg.cheapMax():]
		// Trim to the next newline boundary to avoid cutting mid-line
		if idx := strings.Index(updated, "\n"); idx >= 0 {
			updated = updated[idx+1:]
		}
	}

	needsCompression = len(updated) > cfg.expensiveTriggerBytes() ||
		completedCount >= cfg.expensiveTriggerN()

	return updated, needsCompression
}

// CompressRequest represents a request sent to the Planner to compress the accumulated summary.
// The actual Planner call is handled by the coordinator (implemented in #98).
type CompressRequest struct {
	Goal        string
	Accumulated string
	Artifacts   []Artifact
}

// CompressPrompt returns the prompt text to send to the Planner for compression.
func CompressPrompt(req CompressRequest) string {
	var sb strings.Builder
	sb.WriteString("Summarise the following execution log into at most 1500 characters.\n")
	sb.WriteString("Preserve: file paths changed, decisions made, errors encountered.\n")
	sb.WriteString("Omit: verbose output, tool call details, repeated status messages.\n\n")
	sb.WriteString("Goal: ")
	sb.WriteString(req.Goal)
	sb.WriteString("\n\nExecution log:\n")
	sb.WriteString(req.Accumulated)
	if len(req.Artifacts) > 0 {
		sb.WriteString("\n\nArtifacts:\n")
		for _, a := range req.Artifacts {
			sb.WriteString("  ")
			sb.WriteString(a.Path)
			if a.Hash != "" {
				sb.WriteString(" (")
				sb.WriteString(a.Hash[:8])
				sb.WriteString("...)")
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// BuildExecutorPrompt assembles the context block injected at the start of each
// cold-start Executor invocation.
func BuildExecutorPrompt(state TaskState, coreRules string) string {
	sub := state.CurrentSubTask()
	if sub == nil {
		return ""
	}

	var sb strings.Builder
	if coreRules != "" {
		sb.WriteString(coreRules)
		sb.WriteString("\n\n")
	}
	sb.WriteString("=== Hermes Executor 上下文 ===\n")
	sb.WriteString("語言要求：所有回應必須使用繁體中文。摘要、結論、註解全部繁中。\n\n")
	sb.WriteString("目標：")
	sb.WriteString(state.Goal)
	sb.WriteString("\n\n")

	if state.Accumulated != "" {
		sb.WriteString("累積進度：\n")
		sb.WriteString(state.Accumulated)
		sb.WriteString("\n\n")
	}

	// Inject prior sub-task outcomes so the executor can see what was actually
	// done in earlier rounds — accumulated only carries the conclusion line of
	// each, which is too thin when picking up a task across days/sessions or
	// after a long plan. Skip pending sub-tasks (those are noise here) and cap
	// each entry to keep the prompt bounded.
	if state.CurrentIdx > 0 && len(state.Plan) > 0 {
		var priorLines []string
		limit := state.CurrentIdx
		if limit > len(state.Plan) {
			limit = len(state.Plan)
		}
		for i := 0; i < limit; i++ {
			st := state.Plan[i]
			if st.Status == SubTaskPending || st.Status == SubTaskInProgress {
				continue
			}
			icon := "✓"
			switch st.Status {
			case SubTaskFailed:
				icon = "✗"
			case SubTaskSkipped:
				icon = "⏭"
			}
			desc := strings.TrimSpace(st.Description)
			if desc == "" {
				desc = "(無描述)"
			}
			result := strings.TrimSpace(st.Result)
			if result == "" {
				result = "(無回報)"
			}
			line := fmt.Sprintf("  %s [%d] %s\n     → %s",
				icon, i+1,
				truncateRunesAccumulated(desc, 160),
				truncateRunesAccumulated(result, 240))
			priorLines = append(priorLines, line)
		}
		if len(priorLines) > 0 {
			sb.WriteString("前序子任務結果：\n")
			sb.WriteString(strings.Join(priorLines, "\n"))
			sb.WriteString("\n\n")
		}
	}

	if len(state.Artifacts) > 0 {
		sb.WriteString("已修改檔案：\n")
		for _, a := range state.Artifacts {
			sb.WriteString("  ")
			sb.WriteString(a.Path)
			if a.Hash != "" {
				sb.WriteString(" [")
				sb.WriteString(a.Hash[:min8(len(a.Hash))])
				sb.WriteString("]")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("當前子任務 (")
	sb.WriteString(itoa(state.CurrentIdx+1))
	sb.WriteString("/")
	sb.WriteString(itoa(len(state.Plan)))
	sb.WriteString(")：")
	sb.WriteString(sub.Description)
	sb.WriteString("\n")

	if len(sub.ToolHints) > 0 {
		sb.WriteString("建議工具：")
		sb.WriteString(strings.Join(sub.ToolHints, ", "))
		sb.WriteString("\n")
	}

	if !state.TokenBudget.Exceeded() && (state.TokenBudget.MaxTotalTokens > 0 || state.TokenBudget.MaxWallclockSeconds > 0) {
		sb.WriteString("Budget: ")
		sb.WriteString(state.TokenBudget.BudgetStatus())
		sb.WriteString("\n")
	}

	return sb.String()
}

func min8(n int) int {
	if n < 8 {
		return n
	}
	return 8
}
