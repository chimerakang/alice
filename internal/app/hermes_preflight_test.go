package app

import (
	"context"
	"claude-tg-agent/internal/app/hermes"
	"strings"
	"testing"
	"time"
)

func TestParseHermesPreflightResultFencedJSON(t *testing.T) {
	result, err := parseHermesPreflightResult("```json\n{\"completion_percent\":95,\"verdict\":\"complete\",\"remaining\":[],\"evidence\":[\"tests pass\"]}\n```")
	if err != nil {
		t.Fatalf("parseHermesPreflightResult: %v", err)
	}
	if !result.shouldSkip(90) {
		t.Fatalf("expected complete result to skip: %+v", result)
	}
	if !strings.Contains(result.goalContext(), "completion_percent: 95") {
		t.Fatalf("goal context missing completion: %s", result.goalContext())
	}
}

func TestHermesPreflightRemainingDoesNotSkip(t *testing.T) {
	result := hermesPreflightResult{
		CompletionPercent: 92,
		Verdict:           "remaining",
		Remaining:         []string{"add missing e2e test"},
	}
	if result.shouldSkip(90) {
		t.Fatalf("remaining work should not skip: %+v", result)
	}
}

func TestBuildHermesPreflightPromptIncludesIssueStateSnapshot(t *testing.T) {
	issue := &hermes.IssueContext{
		Number: 12,
		Title:  "Close after verification",
		State:  "OPEN",
		Body:   "Body text\n- [x] Verified\n",
		Checklist: []hermes.ChecklistItem{
			{Text: "Verified", Checked: true},
		},
		Comments: []hermes.IssueComment{
			{Author: "alice", Body: "✅ **Hermes 完成** 1/1 SubTasks\n\n**結論**：全部 PASS。"},
		},
	}
	prompt := buildHermesPreflightPrompt(issue, "## git status --short\n(empty)\n")
	for _, want := range []string{
		"Issue state snapshot",
		"State: OPEN",
		"0 unchecked, 1 checked",
		"全部 PASS",
		"Repository snapshot",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildHermesQualityContextFiltersSuccessAndLimitsWarnings(t *testing.T) {
	context := buildHermesQualityContext([]QualityInsight{
		{Name: "quality_stable", Severity: "success", Message: "stable"},
		{Name: "high_partial_rate", Severity: "warning", Message: "partial 率 43% 高於目標", Suggestion: "檢查 Planner rules"},
		{Name: "top_issue_tag", Severity: "info", Message: "最高頻失分標籤是 incomplete_traceability", Suggestion: "加入 traceability 規則"},
		{Name: "missing_validation", Severity: "warning", Message: "validation gap", Suggestion: "require tests"},
		{Name: "extra", Severity: "warning", Message: "should not appear"},
	})
	for _, want := range []string{
		"Hermes quality feedback",
		"high_partial_rate",
		"incomplete_traceability",
		"missing_validation",
		"ensure traceability and validation",
	} {
		if !strings.Contains(context, want) {
			t.Fatalf("quality context missing %q:\n%s", want, context)
		}
	}
	if strings.Contains(context, "quality_stable") || strings.Contains(context, "should not appear") {
		t.Fatalf("quality context should filter success and limit entries:\n%s", context)
	}
}

// Phase 2.6.A: when preflight is disabled or client is nil, the async path
// returns immediately with a non-skip verdict so the caller knows the Planner
// can proceed unguarded.
func TestRunHermesIssuePreflightAsyncDisabledReturnsImmediately(t *testing.T) {
	bot := &TelegramBot{}
	issue := &hermes.IssueContext{Number: 200, Title: "x"}
	pfCh, cancel := bot.runHermesIssuePreflightAsync(context.Background(), issue, "/repo", "goal", HermesConfig{})
	defer cancel()
	select {
	case v := <-pfCh:
		if v.skip {
			t.Fatalf("disabled preflight must not tripwire: %+v", v)
		}
		if v.err != nil {
			t.Fatalf("disabled preflight must not error: %v", v.err)
		}
	case <-time.After(time.Second):
		t.Fatal("async preflight did not return when disabled")
	}
}

// When preflight is enabled but the bot has no Claude client wired in, the
// async path must not panic — it should report an empty verdict so the
// Planner continues without interruption.
func TestRunHermesIssuePreflightAsyncMissingClientFallsThrough(t *testing.T) {
	bot := &TelegramBot{}
	issue := &hermes.IssueContext{Number: 201, Title: "x"}
	cfg := HermesConfig{Preflight: HermesPreflightConfig{Enabled: true, Parallel: true}}
	pfCh, cancel := bot.runHermesIssuePreflightAsync(context.Background(), issue, "/repo", "goal", cfg)
	defer cancel()
	select {
	case v := <-pfCh:
		if v.skip || v.err != nil {
			t.Fatalf("missing client should not trip wire or error: %+v", v)
		}
	case <-time.After(time.Second):
		t.Fatal("async preflight did not return when client is nil")
	}
}

func TestMaybeRunHermesIssuePreflightSkipsRecentCompletionWhenPreflightDisabled(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		messageQueue: make(chan *TelegramMessage, 1),
	}
	issue := &hermes.IssueContext{
		Number: 119,
		Title:  "Add VWAP",
		State:  "OPEN",
		Comments: []hermes.IssueComment{
			{
				Author:    "alice",
				CreatedAt: time.Date(2026, 5, 1, 7, 2, 38, 0, time.UTC),
				Body:      "✅ **Hermes 完成** 3/3 SubTasks\n\n- Token 用量: 1424963",
			},
		},
	}
	gotGoal, skip := bot.maybeRunHermesIssuePreflight(nil, key, issue, "/repo", "goal text", HermesConfig{})
	if !skip {
		t.Fatal("recent successful Hermes completion should skip even when preflight is disabled")
	}
	if gotGoal != "goal text" {
		t.Fatalf("goal changed: %q", gotGoal)
	}
	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		for _, want := range []string{"最新 Hermes run 已完成", "目前仍是 OPEN", "/close #119"} {
			if !strings.Contains(text, want) {
				t.Fatalf("queued message missing %q:\n%s", want, text)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recent successful Hermes completion message")
	}
}
