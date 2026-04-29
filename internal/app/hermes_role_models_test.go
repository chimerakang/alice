package app

import (
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
}
