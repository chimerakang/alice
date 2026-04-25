package hermes

import "strings"

const (
	// cheapPathMaxBytes is the FIFO truncation limit for the cheap path.
	cheapPathMaxBytes = 2 * 1024 // 2 KB

	// expensivePathTriggerBytes triggers a compression call when accumulated exceeds this.
	expensivePathTriggerBytes = 8 * 1024 // 8 KB

	// expensivePathTriggerCount triggers compression after this many completed sub-tasks.
	expensivePathTriggerCount = 10
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

// AppendResult appends a sub-task result to the accumulated summary using the cheap path:
// append + FIFO truncate to cheapMax bytes (keeping the tail, which is most recent).
// Returns the updated accumulated string and whether the expensive path should be triggered.
func AppendResult(accumulated, subtaskResult string, completedCount int, cfg AccumulatedConfig) (updated string, needsCompression bool) {
	if subtaskResult == "" {
		return accumulated, false
	}

	updated = accumulated
	if updated != "" {
		updated += "\n"
	}
	updated += subtaskResult

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
