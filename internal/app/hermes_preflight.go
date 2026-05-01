package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

type hermesPreflightResult struct {
	CompletionPercent int      `json:"completion_percent"`
	Verdict           string   `json:"verdict"`
	Completed         []string `json:"completed"`
	Remaining         []string `json:"remaining"`
	Evidence          []string `json:"evidence"`
	Risk              string   `json:"risk"`
}

func (r hermesPreflightResult) shouldSkip(threshold int) bool {
	verdict := strings.ToLower(strings.TrimSpace(r.Verdict))
	if verdict == "complete" || verdict == "done" || verdict == "already_done" {
		return len(r.Remaining) == 0
	}
	return threshold > 0 && r.CompletionPercent >= threshold && len(r.Remaining) == 0
}

func (r hermesPreflightResult) goalContext() string {
	var b strings.Builder
	b.WriteString("=== Hermes pre-flight completion check ===\n")
	fmt.Fprintf(&b, "completion_percent: %d\n", r.CompletionPercent)
	if r.Verdict != "" {
		fmt.Fprintf(&b, "verdict: %s\n", r.Verdict)
	}
	writePreflightList(&b, "completed", r.Completed)
	writePreflightList(&b, "remaining", r.Remaining)
	writePreflightList(&b, "evidence", r.Evidence)
	if strings.TrimSpace(r.Risk) != "" {
		fmt.Fprintf(&b, "risk: %s\n", strings.TrimSpace(r.Risk))
	}
	b.WriteString("Planner instruction: do not re-plan completed items; plan only the remaining items above plus any issue unchecked items that are still unsupported by repo state.\n")
	return b.String()
}

func writePreflightList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(label)
	b.WriteString(":\n")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		b.WriteString("  - ")
		b.WriteString(item)
		b.WriteString("\n")
	}
}

func (t *TelegramBot) runHermesIssuePreflight(ctx context.Context, issue *hermes.IssueContext, projectDir, model string, cfg HermesPreflightConfig) (hermesPreflightResult, error) {
	if issue == nil {
		return hermesPreflightResult{}, fmt.Errorf("missing issue")
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	preCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	snapshot := collectHermesPreflightSnapshot(preCtx, projectDir)
	prompt := buildHermesPreflightPrompt(issue, snapshot)
	var collected strings.Builder
	resp, err := t.client.CallPlan(preCtx, prompt, projectDir, "", model, func(contentType, text string) {
		if contentType == "text" {
			collected.WriteString(text)
		}
	})
	if err != nil {
		return hermesPreflightResult{}, err
	}
	text := strings.TrimSpace(collected.String())
	if text == "" && resp != nil {
		text = strings.TrimSpace(resp.TextContent)
	}
	if text == "" && resp != nil {
		text = strings.TrimSpace(resp.Result)
	}
	return parseHermesPreflightResult(text)
}

func collectHermesPreflightSnapshot(ctx context.Context, projectDir string) string {
	var b strings.Builder
	appendSnapshotCommand(ctx, &b, projectDir, "git status --short", "git", "status", "--short")
	appendSnapshotCommand(ctx, &b, projectDir, "git log --oneline -10", "git", "log", "--oneline", "-10")
	appendSnapshotCommand(ctx, &b, projectDir, "git diff --stat HEAD", "git", "diff", "--stat", "HEAD")
	return b.String()
}

func appendSnapshotCommand(ctx context.Context, b *strings.Builder, projectDir, title, cmd string, args ...string) {
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{
		Dir:         projectDir,
		Timeout:     5 * time.Second,
		OutputLimit: 16 * 1024,
	}, cmd, args...)
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n")
	if err != nil {
		fmt.Fprintf(b, "(unavailable: %v)\n\n", err)
		return
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "(empty)"
	}
	b.WriteString(text)
	b.WriteString("\n\n")
}

func buildHermesPreflightPrompt(issue *hermes.IssueContext, snapshot string) string {
	issueStateSnapshot := hermes.BuildIssueStateSnapshot(issue)
	return fmt.Sprintf(`You are a cheap pre-flight checker for a Hermes coding agent.

Decide whether this GitHub issue already appears completed from the issue state snapshot, issue body, and repository snapshot.
Do not execute tools. Be conservative: if meaningful work may remain, verdict must be "remaining".

Return ONLY a JSON object with this schema:
{
  "completion_percent": 0-100,
  "verdict": "complete" | "remaining" | "uncertain",
  "completed": ["short completed item"],
  "remaining": ["short remaining item"],
  "evidence": ["file/commit/status evidence"],
  "risk": "short risk note"
}

Skip guidance:
- Use "complete" only when the issue is effectively done and no concrete remaining work is visible.
- Checked checklist items count as completed.
- Unchecked checklist items count as remaining unless repo snapshot strongly shows they are already done.
- If uncertain, prefer "remaining".

Issue #%d: %s

Issue state snapshot:
%s

Issue body:
%s

Repository snapshot:
%s
`, issue.Number, issue.Title, issueStateSnapshot, issue.Body, snapshot)
}

func currentHermesQualityContext(now time.Time) string {
	if globalStorage == nil {
		return ""
	}
	window, err := ResolveQualityWindow("30d", now)
	if err != nil {
		return ""
	}
	insights, err := globalStorage.GetQualityInsights(window)
	if err != nil {
		log.Printf("[hermes.quality] load insights failed: %v", err)
		return ""
	}
	return buildHermesQualityContext(insights)
}

func buildHermesQualityContext(insights []QualityInsight) string {
	if len(insights) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("=== Hermes quality feedback from recent reviews ===\n")
	written := 0
	for _, insight := range insights {
		if insight.Severity == "success" {
			continue
		}
		name := strings.TrimSpace(insight.Name)
		msg := strings.TrimSpace(insight.Message)
		suggestion := strings.TrimSpace(insight.Suggestion)
		if name == "" && msg == "" {
			continue
		}
		if name != "" {
			fmt.Fprintf(&b, "- %s", name)
		} else {
			b.WriteString("- quality_signal")
		}
		if msg != "" {
			fmt.Fprintf(&b, ": %s", msg)
		}
		if suggestion != "" {
			fmt.Fprintf(&b, " -> %s", suggestion)
		}
		b.WriteString("\n")
		written++
		if written >= 3 {
			break
		}
	}
	if written == 0 {
		return ""
	}
	b.WriteString("Planner instruction: use these recent failure patterns to avoid low-quality decomposition; especially ensure traceability and validation before execution.\n")
	return b.String()
}

func parseHermesPreflightResult(text string) (hermesPreflightResult, error) {
	raw := extractJSONObject(text)
	if raw == "" {
		return hermesPreflightResult{}, fmt.Errorf("preflight returned no JSON object: %s", truncStderr(text))
	}
	var result hermesPreflightResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return hermesPreflightResult{}, fmt.Errorf("parse preflight JSON: %w", err)
	}
	if result.CompletionPercent < 0 {
		result.CompletionPercent = 0
	}
	if result.CompletionPercent > 100 {
		result.CompletionPercent = 100
	}
	return result, nil
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if start := strings.Index(text, "{"); start >= 0 {
			if end := strings.LastIndex(text, "}"); end > start {
				return text[start : end+1]
			}
		}
	}
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			return text[start : end+1]
		}
	}
	return ""
}

func (t *TelegramBot) maybeRunHermesIssuePreflight(ctx context.Context, key chatKey, issue *hermes.IssueContext, projectDir, goal string, cfg HermesConfig) (string, bool) {
	if issue == nil {
		return goal, false
	}
	if signal, ok := hermes.RecentSuccessfulHermesCompletion(issue); ok {
		t.send(key, formatHermesRecentCompletionSkipMessage(issue, signal))
		return goal, true
	}
	pf := cfg.Preflight
	if !pf.Enabled || t.client == nil {
		return goal, false
	}
	model := strings.TrimSpace(pf.Model)
	if model == "" {
		model = strings.TrimSpace(t.config.ModelRouting.FastModel)
	}
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	result, err := t.runHermesIssuePreflight(ctx, issue, projectDir, model, pf)
	if err != nil {
		log.Printf("[hermes.preflight] issue #%d failed: %v", issue.Number, err)
		t.send(key, "⚠️ Hermes pre-flight 檢查失敗，將照常啟動："+err.Error())
		return goal, false
	}
	threshold := pf.CompletionThreshold
	if threshold <= 0 {
		threshold = 90
	}
	if result.shouldSkip(threshold) {
		t.send(key, fmt.Sprintf("ℹ️ Hermes pre-flight 判定 Issue #%d 已接近完成（%d%%，verdict=%s），已停止本輪以避免重複消耗 token。\n\n%s", issue.Number, result.CompletionPercent, result.Verdict, strings.TrimSpace(result.goalContext())))
		return goal, true
	}
	t.send(key, fmt.Sprintf("🔎 Hermes pre-flight：Issue #%d 完成度約 %d%%（%s），只規劃剩餘項目。", issue.Number, result.CompletionPercent, strings.TrimSpace(result.Verdict)))
	return strings.TrimSpace(goal) + "\n\n" + result.goalContext(), false
}

func formatHermesRecentCompletionSkipMessage(issue *hermes.IssueContext, signal hermes.HermesCompletionSignal) string {
	when := ""
	if !signal.CreatedAt.IsZero() {
		when = " at " + signal.CreatedAt.Format(time.RFC3339)
	}
	state := strings.ToUpper(strings.TrimSpace(issue.State))
	if state == "" {
		state = "UNKNOWN"
	}
	msg := fmt.Sprintf("ℹ️ Hermes issue-state snapshot 判定 Issue #%d 最新 Hermes run 已完成（%d/%d SubTasks%s），已停止本輪以避免重複消耗 token。", issue.Number, signal.Done, signal.Total, when)
	if state != "CLOSED" {
		msg += fmt.Sprintf("\n\n注意：GitHub Issue #%d 目前仍是 %s。這只代表上一輪 Hermes job 已完成，不代表 issue 已關閉或必須停止；若確認驗收已完成，請使用 `/close #%d` 關閉，若還有缺口請使用 fresh/restart 流程重跑。", issue.Number, state, issue.Number)
		return msg
	}
	msg += "\n\nIssue 已是 CLOSED；若要強制重跑，請使用 fresh/restart 流程。"
	return msg
}
