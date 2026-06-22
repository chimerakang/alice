package hermes

import (
	"fmt"
	"regexp"
	"strings"

	"claude-tg-agent/internal/app/engine/errorclass"
)

const (
	// cheapPathMaxBytes is the FIFO truncation limit for the cheap path.
	// Loosened from 1.5 KB to 3 KB after M1 added the prior-subtask Result
	// block to BuildExecutorPrompt: the prompt no longer relies on
	// accumulated as the only context source, so a slightly fatter rolling
	// summary buys back the "decision points" the conclusion-only extract
	// was throwing away without re-introducing the geometric bloat P0 fixed.
	cheapPathMaxBytes = 3 * 1024

	// expensivePathTriggerBytes triggers a compression call when accumulated
	// exceeds this. Kept at 6 KB (2× cheap cap) so compression fires before
	// the rolling buffer grows into "Prompt is too long" territory.
	expensivePathTriggerBytes = 6 * 1024

	// expensivePathTriggerCount triggers compression after this many completed
	// sub-tasks. Tightened from 10 → 5 so long plans collapse early rather
	// than collecting four-section reports verbatim.
	expensivePathTriggerCount = 5

	// perSubtaskConclusionMaxBytes caps each sub-task's contribution to
	// accumulated. extractConclusion keeps the "**結論**：…" line plus the
	// first 1-2 evidence bullets — enough to reconstruct what was decided
	// and why, without replaying the executor's "未驗證 / 下一步" sections
	// (those live in state.Plan[i].Result and surface via the prior-subtask
	// block in BuildExecutorPrompt). Per-subtask budget raised to 600 runes
	// to fit conclusion + evidence in roughly one paragraph.
	perSubtaskConclusionMaxBytes = 600
)

// (G) The AccumulatedConfig struct used to allow JSON overrides of the cheap
// truncation cap and expensive-compression triggers. The override knobs were
// never wired through config.json or surfaced to operators — every caller
// passed an empty struct, so the runtime always fell back to the constants
// above. The struct is removed; the constants are the single source of
// truth.

// conclusionPattern matches the executor's structured "**結論**：…" line.
// Both Chinese full-width and ASCII colons are tolerated.
var conclusionPattern = regexp.MustCompile(`\*\*結論\*\*[：:]\s*([^\n]+)`)

// evidenceSectionPattern captures the body of the structured "**證據**" block
// up to the next bold heading or the end of the message. Used by
// extractConclusion to lift the first 1-2 evidence bullets so accumulated
// preserves the key decision context, not just the conclusion line.
var evidenceSectionPattern = regexp.MustCompile(`(?s)\*\*證據\*\*[：:]\s*\n?(.*?)(?:\n\*\*[^*]+\*\*|$)`)

// extractConclusion returns a compact summary of a sub-task result for
// re-injection into the next sub-task's prompt. Output shape:
//
//	結論：<one-line conclusion>
//	證據：<first evidence bullet>; <second evidence bullet>
//
// Conclusion-only extraction (the previous behaviour) lost the "why" so
// downstream sub-tasks could not tell *which file at which line* the prior
// step had touched. M4 lifts the first 1-2 evidence bullets to give the next
// sub-task enough breadcrumbs to chain on. Falls back to the first non-empty
// line when the structured markers are missing.
func extractConclusion(subtaskResult string) string {
	trimmed := strings.TrimSpace(subtaskResult)
	if trimmed == "" {
		return ""
	}

	conclusion := ""
	if m := conclusionPattern.FindStringSubmatch(trimmed); len(m) >= 2 {
		conclusion = strings.TrimSpace(m[1])
	}

	evidence := ""
	if m := evidenceSectionPattern.FindStringSubmatch(trimmed); len(m) >= 2 {
		evidence = pickFirstEvidenceBullets(m[1], 2)
	}

	if conclusion == "" && evidence == "" {
		// No structured markers; first non-empty line as a last resort.
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return truncateRunesAccumulated(line, perSubtaskConclusionMaxBytes)
			}
		}
		return ""
	}

	var sb strings.Builder
	if conclusion != "" {
		sb.WriteString("結論：")
		sb.WriteString(conclusion)
	}
	if evidence != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("證據：")
		sb.WriteString(evidence)
	}
	return truncateRunesAccumulated(sb.String(), perSubtaskConclusionMaxBytes)
}

// pickFirstEvidenceBullets extracts up to n leading "- " bullet items from an
// evidence section body, joined by "; ". Stops at the first non-bullet line
// so we don't drag stray narration into accumulated.
func pickFirstEvidenceBullets(body string, n int) string {
	var picked []string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
			break
		}
		bullet := strings.TrimSpace(strings.TrimLeft(line, "-* "))
		if bullet == "" {
			continue
		}
		picked = append(picked, bullet)
		if len(picked) >= n {
			break
		}
	}
	return strings.Join(picked, "; ")
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

// ClassifyFailure returns the failure category for a sub-task error string.
// Returns FailureUnknown for empty input so callers can short-circuit.
//
// Pattern recognition is delegated to errorclass for a single source of
// truth across the codebase. The legacy envFailurePatterns list lived
// here; its full coverage is preserved and verified by
// errorclass.TestRegression_HermesEnvFailurePatterns.
func ClassifyFailure(errText string) FailureKind {
	if strings.TrimSpace(errText) == "" {
		return FailureUnknown
	}
	if errorclass.ClassifyText(errText).IsEnv() {
		return FailureEnv
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
func AppendResult(accumulated, subtaskResult string, completedCount int) (updated string, needsCompression bool) {
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
	if len(updated) > cheapPathMaxBytes {
		updated = updated[len(updated)-cheapPathMaxBytes:]
		// Trim to the next newline boundary to avoid cutting mid-line
		if idx := strings.Index(updated, "\n"); idx >= 0 {
			updated = updated[idx+1:]
		}
	}

	needsCompression = len(updated) > expensivePathTriggerBytes ||
		completedCount >= expensivePathTriggerCount

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
