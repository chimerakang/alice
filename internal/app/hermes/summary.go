package hermes

import (
	"fmt"
	"strings"
)

// TaskSummary encapsulates all metrics for task completion.
type TaskSummary struct {
	TaskState        *TaskState
	WallclockSeconds int // execution duration
	SuccessRate      float64
	ModelCostRates   map[string]CostRate
	Verbosity        string // "minimal" or "detailed"
	IncludeCostEst   bool
}

// CostRate defines per-model pricing.
type CostRate struct {
	InputPerMToken  float64 // cost per 1M input tokens
	OutputPerMToken float64 // cost per 1M output tokens
}

// GenerateSummary produces a formatted summary string based on verbosity level.
func (s *TaskSummary) GenerateSummary() string {
	switch s.Verbosity {
	case "detailed":
		return s.generateDetailed()
	default:
		return s.generateMinimal()
	}
}

// generateMinimal returns a compact summary.
func (s *TaskSummary) generateMinimal() string {
	var buf strings.Builder
	buf.WriteString("🤖 Hermes 完成\n")

	// Model-wise breakdown
	totalTokens := 0
	for _, usage := range s.TaskState.ModelUsages {
		label := formatModelLabel(usage.Model)
		total := usage.TotalTokens()
		totalTokens += total
		buf.WriteString(fmt.Sprintf("├─ %s: %d calls, %s tokens\n",
			label, usage.CallCount, formatNumber(total)))
	}

	// Summary line
	buf.WriteString(fmt.Sprintf("└─ Total: %s tokens · %s\n",
		formatNumber(totalTokens), formatDuration(s.WallclockSeconds)))

	return strings.TrimSuffix(buf.String(), "\n")
}

// generateDetailed returns a comprehensive summary with breakdown and cost.
func (s *TaskSummary) generateDetailed() string {
	var buf strings.Builder
	buf.WriteString("🤖 Hermes 完成\n\n")

	// Model usage section
	buf.WriteString("📊 模型用量\n")
	totalTokens := 0
	for _, usage := range s.TaskState.ModelUsages {
		label := formatModelLabel(usage.Model)
		total := usage.TotalTokens()
		totalTokens += total
		buf.WriteString(fmt.Sprintf("  %-10s %d calls   in: %-8s out: %-8s total: %s\n",
			label, usage.CallCount,
			formatNumber(usage.InputTokens),
			formatNumber(usage.OutputTokens),
			formatNumber(total)))
	}
	buf.WriteString("\n")

	// Task breakdown section
	buf.WriteString("📋 任務拆解\n")
	successCount := 0
	for _, st := range s.TaskState.Plan {
		if st.Status == SubTaskDone {
			successCount++
		}
	}
	avgTokens := 0
	if len(s.TaskState.Plan) > 0 {
		avgTokens = totalTokens / len(s.TaskState.Plan)
	}
	buf.WriteString(fmt.Sprintf("  Planner 拆出 %d 個 SubTasks，%d 個通過\n",
		len(s.TaskState.Plan), successCount))
	buf.WriteString(fmt.Sprintf("  平均每個 SubTask: %s tokens / %ds\n",
		formatNumber(avgTokens), s.WallclockSeconds/max(1, len(s.TaskState.Plan))))
	buf.WriteString("\n")

	// Execution time section
	buf.WriteString("⏱️ 執行時間\n")
	buf.WriteString(fmt.Sprintf("  Total:      %s\n", formatDuration(s.WallclockSeconds)))
	buf.WriteString("\n")

	// Cost estimate section (if enabled)
	if s.IncludeCostEst && len(s.ModelCostRates) > 0 {
		buf.WriteString("💰 成本估算（按公開定價）\n")
		totalCost := 0.0
		for _, usage := range s.TaskState.ModelUsages {
			if rate, ok := s.ModelCostRates[usage.Model]; ok {
				inputCost := float64(usage.InputTokens) / 1e6 * rate.InputPerMToken
				outputCost := float64(usage.OutputTokens) / 1e6 * rate.OutputPerMToken
				totalCost += inputCost + outputCost
			}
		}
		buf.WriteString(fmt.Sprintf("  約 $%.2f USD（若走 API）\n", totalCost))
		buf.WriteString("  訂閱模式實際 marginal cost ≈ 0\n")
	}

	return strings.TrimSuffix(buf.String(), "\n")
}

// formatModelLabel returns a short label for a model name.
func formatModelLabel(model string) string {
	if strings.Contains(model, "opus") {
		return "Opus"
	}
	if strings.Contains(model, "sonnet") {
		return "Sonnet"
	}
	if strings.Contains(model, "haiku") {
		return "Haiku"
	}
	return "Unknown"
}

// formatNumber adds commas to a number for readability.
func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1e6 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%dM", n/1e6)
}

// formatDuration converts seconds to a human-readable string like "4m 12s".
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	mins := seconds / 60
	secs := seconds % 60
	if secs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm %ds", mins, secs)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
