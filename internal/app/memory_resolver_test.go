package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryResolverIncludesRecentMessagesWithoutExplicitIssue(t *testing.T) {
	resolver := NewMemoryResolverWithAllSources(nil, nil)
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		UserMessage: "好，繼續",
		Mode:        "direct",
		RecentMessages: []contextMessage{
			{Role: "user", Content: "先分析 context bridge"},
			{Role: "assistant", Content: "找到 recent bridge 脈絡"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bundle.Sections) != 1 {
		t.Fatalf("sections = %d, want recent bridge only: %#v", len(bundle.Sections), bundle.Sections)
	}
	if bundle.Sections[0].Source != "recent_messages" {
		t.Fatalf("source = %q, want recent_messages", bundle.Sections[0].Source)
	}
	if !strings.Contains(bundle.Render(), "找到 recent bridge 脈絡") {
		t.Fatalf("expected recent bridge content, got:\n%s", bundle.Render())
	}
}

func TestMemoryResolverIncludesGeneralTaskMemory(t *testing.T) {
	storage := newTestSQLiteStorage(t)
	if err := storage.InsertDecisionLog(DecisionLog{
		Timestamp:     time.Now().Add(-time.Hour),
		SessionID:     "direct-session",
		ProjectPath:   "/repo",
		ChatID:        42,
		ThreadID:      7,
		UserPrompt:    "分析文件並規劃 #143 general memory",
		AgentResponse: "決定先沿用 unified tasks 作為 general memory card。",
		Outcome: ExecutionOutcome{
			Success:      true,
			TaskType:     "analysis",
			FilesChanged: []string{"internal/app/general_memory_store.go"},
		},
		Model: "gpt-5.5",
	}); err != nil {
		t.Fatalf("InsertDecisionLog: %v", err)
	}

	resolver := NewMemoryResolverWithAllSources(storage, nil)
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		UserMessage: "繼續處理 #143",
		Mode:        "document",
		RecentMessages: []contextMessage{
			{Role: "assistant", Content: "這段 recent 應因 explicit issue 被跳過"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	rendered := bundle.Render()
	if !strings.Contains(rendered, "unified tasks 作為 general memory card") {
		t.Fatalf("rendered bundle missing general task memory:\n%s", rendered)
	}
	if !strings.Contains(rendered, "internal/app/general_memory_store.go") {
		t.Fatalf("rendered bundle missing touched file metadata:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Continuation hints") {
		t.Fatalf("rendered bundle missing continuation hints:\n%s", rendered)
	}
	if strings.Contains(rendered, "recent 應因 explicit issue") {
		t.Fatalf("rendered bundle leaked generic recent messages:\n%s", rendered)
	}
	if len(bundle.Sections) != 1 || bundle.Sections[0].Source != "general_task" {
		t.Fatalf("sections = %#v, want only general_task", bundle.Sections)
	}
}

func TestMemoryResolverIncludesStaticProjectHints(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "CLAUDE.md"), []byte("Static root guidance for Alice memory."), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	docsDir := filepath.Join(projectDir, "docs", "arch")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "memory.md"), []byte("Static memory architecture hint."), 0o644); err != nil {
		t.Fatalf("write memory.md: %v", err)
	}

	resolver := NewMemoryResolverWithAllSources(nil, ProjectStaticHintSource{})
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  projectDir,
		UserMessage: "請繼續處理 memory",
		Mode:        "direct_resume_fallback",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bundle.Sections) != 1 {
		t.Fatalf("sections = %#v, want one static hint section", bundle.Sections)
	}
	section := bundle.Sections[0]
	if section.Source != "static_hint" {
		t.Fatalf("source = %q, want static_hint", section.Source)
	}
	if section.Scope != "project:"+projectDir {
		t.Fatalf("scope = %q, want project scope", section.Scope)
	}
	rendered := bundle.Render()
	for _, want := range []string{"CLAUDE.md", "docs/arch/memory.md", "Static root guidance", "Static memory architecture"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered static hints missing %q:\n%s", want, rendered)
		}
	}
}

func TestProjectStaticHintSourceIgnoresMissingFiles(t *testing.T) {
	resolver := NewMemoryResolverWithAllSources(nil, ProjectStaticHintSource{})
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:     42,
		ProjectDir: t.TempDir(),
		Mode:       "preview",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bundle.Sections) != 0 {
		t.Fatalf("sections = %#v, want none for missing static files", bundle.Sections)
	}
}
