package app

import (
	"strings"
	"testing"

	appengine "claude-tg-agent/internal/app/engine"
)

func TestResolveHermesRoleModelsKeepsCodexPlannerAndExecutorSeparateConfig(t *testing.T) {
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
				CodexFastModel:       "gpt-5.4-mini",
				DeepModel:            "claude-opus-4-5-20251101",
			},
		},
	}

	models := bot.resolveHermesRoleModels("codex", HermesConfig{}, appengine.StrictModeConfig{})

	if models.planner != "gpt-5.5" {
		t.Fatalf("planner = %q, want gpt-5.5", models.planner)
	}
	if models.executor != "gpt-5.4-mini" {
		t.Fatalf("executor = %q, want gpt-5.4-mini", models.executor)
	}
	if models.heavyExecutor != "" {
		t.Fatalf("heavyExecutor = %q, want empty when codex heavy model is unset", models.heavyExecutor)
	}
	if models.reviewer != "gpt-5.5" {
		t.Fatalf("reviewer = %q, want gpt-5.5", models.reviewer)
	}
	if models.reviewerNote != "" {
		t.Fatalf("reviewerNote = %q, want empty for normal task-level review", models.reviewerNote)
	}
}

func TestResolveHermesRoleModelsStrictAndOpponentReviewNotes(t *testing.T) {
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
				CodexFastModel:       "gpt-5.4-mini",
				DeepModel:            "claude-opus-4-5-20251101",
				FastModel:            "claude-haiku-4-5-20251001",
			},
		},
	}

	strictCodex := bot.resolveHermesRoleModels("codex", HermesConfig{}, appengine.StrictModeConfig{Enabled: true}.WithDefaults())
	if strictCodex.reviewer != "claude-opus-4-5-20251101" {
		t.Fatalf("strict codex reviewer = %q, want claude-opus-4-5-20251101", strictCodex.reviewer)
	}
	if !strings.Contains(strictCodex.reviewerNote, "strict review mode") || !strings.Contains(strictCodex.reviewerNote, "Claude/Opus") {
		t.Fatalf("strict codex reviewerNote = %q, want strict Claude/Opus note", strictCodex.reviewerNote)
	}

	opponentCodex := bot.resolveHermesRoleModels("claude", HermesConfig{}, appengine.StrictModeConfig{
		Enabled:         true,
		OpponentBackend: appengine.BackendCodex,
	}.WithDefaults())
	if opponentCodex.reviewer != "gpt-5.5" {
		t.Fatalf("opponent codex reviewer = %q, want gpt-5.5", opponentCodex.reviewer)
	}
	if !strings.Contains(opponentCodex.reviewerNote, "Codex/GPT") || !strings.Contains(opponentCodex.reviewerNote, "opponent backend") {
		t.Fatalf("opponent codex reviewerNote = %q, want Codex/GPT opponent note", opponentCodex.reviewerNote)
	}
}
