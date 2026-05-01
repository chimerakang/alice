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
		buf.WriteString(fmt.Sprintf("├─ %s：%d 次呼叫，%s tokens\n",
			label, usage.CallCount, formatNumber(total)))
	}
	for _, usage := range sortedPhaseUsages(s.TaskState.PhaseUsages) {
		buf.WriteString(fmt.Sprintf("├─ %s phase：%d 次呼叫，%s tokens\n",
			usage.Phase, usage.CallCount, formatNumber(usage.TotalTokens())))
	}

	// Summary line
	buf.WriteString(fmt.Sprintf("└─ 合計：%s tokens · 耗時 %s\n",
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
		buf.WriteString(fmt.Sprintf("  %-10s %d 次   輸入: %-8s 輸出: %-8s 合計: %s\n",
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

	if len(s.TaskState.PhaseUsages) > 0 {
		buf.WriteString("🧭 Phase 用量\n")
		for _, usage := range sortedPhaseUsages(s.TaskState.PhaseUsages) {
			buf.WriteString(fmt.Sprintf("  %-14s %-10s %d 次   合計: %-8s cost: $%.4f\n",
				usage.Phase, formatModelLabel(usage.Model), usage.CallCount,
				formatNumber(usage.TotalTokens()), usage.CostUSD))
		}
		buf.WriteString("\n")
	}

	// Execution time section
	buf.WriteString("⏱️ 執行時間\n")
	buf.WriteString(fmt.Sprintf("  合計：%s\n", formatDuration(s.WallclockSeconds)))
	buf.WriteString("\n")

	// Cost section. Prefer per-call costs accumulated into ModelUsage.CostUSD
	// (authoritative — comes from CLI total_cost_usd or, under Max sub, from
	// EstimateClaudeCost using cache-aware pricing). Only fall back to a
	// token×rate estimate when no model usage rows have a real cost yet —
	// that happens for older tasks completed before #148 1E shipped, or for
	// runs where every CLI call returned $0 with unknown pricing. See #148.
	if s.IncludeCostEst {
		realCost := 0.0
		anyRealCost := false
		for _, usage := range s.TaskState.ModelUsages {
			if usage.CostUSD > 0 {
				realCost += usage.CostUSD
				anyRealCost = true
			}
		}
		if anyRealCost {
			buf.WriteString("💰 實際成本\n")
			buf.WriteString(fmt.Sprintf("  合計：$%.4f USD（含 cache_read 0.1x、cache_creation 1.25x 折/加）\n", realCost))
		} else if len(s.ModelCostRates) > 0 {
			buf.WriteString("💰 成本估算（按公開定價，無 cache 折扣）\n")
			totalCost := 0.0
			for _, usage := range s.TaskState.ModelUsages {
				if rate, ok := s.ModelCostRates[usage.Model]; ok {
					inputCost := float64(usage.InputTokens) / 1e6 * rate.InputPerMToken
					outputCost := float64(usage.OutputTokens) / 1e6 * rate.OutputPerMToken
					totalCost += inputCost + outputCost
				}
			}
			buf.WriteString(fmt.Sprintf("  約 $%.4f USD（若走 API）\n", totalCost))
			buf.WriteString("  訂閱模式實際 marginal cost ≈ 0\n")
		}
	}

	return strings.TrimSuffix(buf.String(), "\n")
}

func sortedPhaseUsages(usages []PhaseUsage) []PhaseUsage {
	out := append([]PhaseUsage(nil), usages...)
	order := map[string]int{
		"preflight":      0,
		"planner":        1,
		"retry_planner":  2,
		"executor":       3,
		"retry_executor": 4,
		"reviewer":       5,
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			oi, okI := order[out[i].Phase]
			if !okI {
				oi = 99
			}
			oj, okJ := order[out[j].Phase]
			if !okJ {
				oj = 99
			}
			if oj < oi || (oj == oi && out[j].Model < out[i].Model) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// formatModelLabel returns a short label for a model name.
// Recognises both Claude (opus/sonnet/haiku) and Codex/GPT families
// (gpt-5.5, gpt-4o, gpt-5.3-codex, o3, o4-mini, etc.).
func formatModelLabel(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return "Opus"
	case strings.Contains(m, "sonnet"):
		return "Sonnet"
	case strings.Contains(m, "haiku"):
		return "Haiku"
	case strings.Contains(m, "codex"):
		// e.g. gpt-5.3-codex
		return "Codex"
	case strings.Contains(m, "gpt-5.5"):
		return "GPT-5.5"
	case strings.Contains(m, "gpt-5.4"):
		return "GPT-5.4"
	case strings.Contains(m, "gpt-4o"):
		return "GPT-4o"
	case strings.Contains(m, "gpt-4.1"):
		return "GPT-4.1"
	case strings.Contains(m, "gpt-"):
		// Generic GPT fallback for unseen versions
		return "GPT"
	case strings.HasPrefix(m, "o3"):
		return "o3"
	case strings.HasPrefix(m, "o4"):
		return "o4-mini"
	case model == "":
		return "Unknown"
	default:
		return model
	}
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
