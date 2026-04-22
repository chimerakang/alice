package hermes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPromptBuilder_NonEmpty(t *testing.T) {
	pb := DefaultPromptBuilder()
	if pb.ForRole(RolePlanner) == "" {
		t.Error("planner rules should not be empty")
	}
	if pb.ForRole(RoleExecutor) == "" {
		t.Error("executor rules should not be empty")
	}
}

func TestDefaultPromptBuilder_RoleSwitch(t *testing.T) {
	pb := DefaultPromptBuilder()
	planner := pb.ForRole(RolePlanner)
	executor := pb.ForRole(RoleExecutor)
	if planner == executor {
		t.Error("planner and executor rules should differ")
	}
}

func TestDefaultPromptBuilder_UnknownRole(t *testing.T) {
	pb := DefaultPromptBuilder()
	if got := pb.ForRole(Role(99)); got != "" {
		t.Errorf("unknown role should return empty string, got %q", got)
	}
}

func TestLoadPromptBuilder_FromFiles(t *testing.T) {
	dir := t.TempDir()

	plannerContent := "# Custom Planner Rules\nRule A"
	executorContent := "# Custom Executor Rules\nRule B"

	if err := os.WriteFile(filepath.Join(dir, "planner_rules.md"), []byte(plannerContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executor_rules.md"), []byte(executorContent), 0644); err != nil {
		t.Fatal(err)
	}

	pb := LoadPromptBuilder(dir)
	if got := pb.ForRole(RolePlanner); got != plannerContent {
		t.Errorf("planner rules = %q, want %q", got, plannerContent)
	}
	if got := pb.ForRole(RoleExecutor); got != executorContent {
		t.Errorf("executor rules = %q, want %q", got, executorContent)
	}
}

func TestLoadPromptBuilder_MissingFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	// Only planner file exists
	if err := os.WriteFile(filepath.Join(dir, "planner_rules.md"), []byte("custom planner"), 0644); err != nil {
		t.Fatal(err)
	}

	pb := LoadPromptBuilder(dir)
	if pb.ForRole(RolePlanner) != "custom planner" {
		t.Error("should use file content for planner")
	}
	// executor falls back to embedded default
	if !strings.Contains(pb.ForRole(RoleExecutor), "Executor") {
		t.Error("executor fallback should contain 'Executor'")
	}
}

func TestLoadPromptBuilder_MissingDirFallsBack(t *testing.T) {
	pb := LoadPromptBuilder("/nonexistent/dir")
	// Both fall back to embedded defaults
	if pb.ForRole(RolePlanner) == "" {
		t.Error("planner should fall back to default")
	}
	if pb.ForRole(RoleExecutor) == "" {
		t.Error("executor should fall back to default")
	}
}

func TestRole_String(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{RolePlanner, "planner"},
		{RoleExecutor, "executor"},
		{Role(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.role.String(); got != tt.want {
			t.Errorf("Role(%d).String() = %q, want %q", tt.role, got, tt.want)
		}
	}
}

func TestDefaultRulesContainKeyTerms(t *testing.T) {
	pb := DefaultPromptBuilder()

	planner := pb.ForRole(RolePlanner)
	if !strings.Contains(planner, "JSON") {
		t.Error("planner rules should mention JSON format requirement")
	}
	if !strings.Contains(planner, "config.json") {
		t.Error("planner rules should mention config.json protection")
	}

	executor := pb.ForRole(RoleExecutor)
	if !strings.Contains(executor, "2 行") {
		t.Error("executor rules should mention 2-line summary limit")
	}
	if !strings.Contains(executor, "config.json") {
		t.Error("executor rules should mention config.json protection")
	}
}
