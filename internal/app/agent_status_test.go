package app

import (
	"strings"
	"testing"
)

func TestProcessingStatusForCodexModelDoesNotSayClaude(t *testing.T) {
	status := processingStatusForModel("gpt-5.5")
	if !strings.Contains(status, "GPT/Codex") {
		t.Fatalf("expected GPT/Codex status, got %q", status)
	}
	if strings.Contains(status, "Claude") {
		t.Fatalf("codex status should not mention Claude, got %q", status)
	}
}

func TestProcessingStatusForClaudeModelNamesModel(t *testing.T) {
	status := processingStatusForModel("claude-sonnet-4-6")
	if !strings.Contains(status, "Claude") || !strings.Contains(status, "claude-sonnet-4-6") {
		t.Fatalf("expected Claude status with model name, got %q", status)
	}
}
