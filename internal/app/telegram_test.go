package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
	tasksvc "claude-tg-agent/internal/app/task"
)

func writeTestExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestExtractHermesActionableGoal(t *testing.T) {
	goal := `[Previous conversation context]
User: 先分析 webrtc-server
Assistant: 找到四個待修問題

[Current request]
好，請幫忙修正`

	if got := extractHermesActionableGoal(goal); got != "好，請幫忙修正" {
		t.Fatalf("expected actionable goal, got %q", got)
	}
}

func TestExtractHermesActionableGoalFromContinuationPrompt(t *testing.T) {
	goal := `[Hermes continuation]
Mode: replan
Task ID: task-123
Task status: interrupted

Original goal:
修復登入流程

Current progress:
Accumulated summary:
已完成 cookie 修正

Instructions:
- Do not repeat completed work.`

	if got := extractHermesActionableGoal(goal); got != "修復登入流程" {
		t.Fatalf("expected original goal from continuation prompt, got %q", got)
	}
}

func TestExtractHermesActionableGoalFromNestedContinuationPrompt(t *testing.T) {
	goal := `[Hermes continuation]
Mode: continue
Task ID: task-456
Task status: failed

Original goal:
[Previous conversation context]
User: 先分析登入流程

[Current request]
補登入整合測試

Current progress:
Subtasks:
1. [done] 修正 cookie`

	if got := extractHermesActionableGoal(goal); got != "補登入整合測試" {
		t.Fatalf("expected nested actionable goal, got %q", got)
	}
}

func TestComposeHermesGoalWithContext(t *testing.T) {
	tasks := []hermes.TaskState{
		{
			Goal:        "先分析 webrtc-server",
			Accumulated: "列出 4 個待修問題：1. MediaSourceService 沒註冊到 HTTP gateway。2. JWK 環境變數名稱不一致。3. LOG_LEVEL 與 ENV 不同步。4. OBS 文件 API 路徑寫錯。",
			UpdatedAt:   time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
		},
	}
	recentMessages := []contextMessage{
		{Role: "user", Content: "先分析 webrtc-server"},
		{Role: "assistant", Content: "我找到 4 個待修問題：MediaSourceService、JWK、LOG_LEVEL、OBS 文件。"},
		{Role: "user", Content: "好，請幫忙修正"},
	}

	goal := composeHermesGoalWithContext("好，請幫忙修正", tasks, recentMessages)

	for _, want := range []string{
		hermesPreviousContextHeader,
		"MediaSourceService 沒註冊到 HTTP gateway",
		"JWK 環境變數名稱不一致",
		"LOG_LEVEL 與 ENV 不同步",
		"OBS 文件 API 路徑寫錯",
		"Persisted Hermes context:",
		"avoid broad rediscovery",
		"Assistant: 我找到 4 個待修問題：MediaSourceService、JWK、LOG_LEVEL、OBS 文件。",
		"User: 好，請幫忙修正",
		hermesCurrentRequestHeader,
		"好，請幫忙修正",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("expected composed goal to contain %q, got:\n%s", want, goal)
		}
	}
}

func TestComposeHermesGoalWithIssueMemory(t *testing.T) {
	tasks := []hermes.TaskState{
		{
			ID:                "task-issue-94",
			GithubIssueNumber: 94,
			Status:            hermes.TaskStatusDone,
			Goal:              "[GitHub #94] CLAUDE.md 文件分層",
			Plan: []hermes.SubTask{
				{
					Description: "拆出 alice-i18n 與 alice-add-tool skills",
					Status:      hermes.SubTaskDone,
					Result:      "已新增兩個 SKILL.md，並確認 .gitignore 例外放行。",
				},
				{
					Description: "補 model routing 文件",
					Status:      hermes.SubTaskDone,
					Result:      "docs/arch/model-routing.md 已包含四層優先順序與 session lifecycle。",
				},
			},
			Accumulated: "CLAUDE.md 已降到 77 行；剩下 review、commit、關 issue。",
			UpdatedAt:   time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
		},
	}

	goal := composeHermesGoalWithContext("好，那我們 #94 算是結束了嗎", tasks, nil)

	for _, want := range []string{
		"Persisted Hermes context:",
		"Task ID: task-issue-94",
		"GitHub issue: #94",
		"Status: done",
		"CLAUDE.md 已降到 77 行",
		"拆出 alice-i18n 與 alice-add-tool skills",
		"model-routing.md 已包含四層優先順序",
		"avoid broad rediscovery",
		"only re-read files or GitHub issue details when a specific uncertainty must be verified",
		"好，那我們 #94 算是結束了嗎",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("expected issue memory goal to contain %q, got:\n%s", want, goal)
		}
	}
}

func TestLoadHermesContextTasksPrioritizesMatchingIssue(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	now := time.Date(2026, 4, 29, 13, 0, 0, 0, time.UTC)
	for _, task := range []hermes.TaskState{
		{
			ID:                "issue-94",
			ChatID:            42,
			GithubIssueNumber: 94,
			Status:            hermes.TaskStatusDone,
			Goal:              "[GitHub #94] 文件分層",
			Accumulated:       "CLAUDE.md 已降到 77 行。",
			CreatedAt:         now.Add(-3 * time.Hour),
			UpdatedAt:         now.Add(-3 * time.Hour),
		},
		{
			ID:          "different-topic",
			ChatID:      42,
			Status:      hermes.TaskStatusExecuting,
			Goal:        "其他問題",
			Accumulated: "即使是 active task，也不應優先於明確指定的 issue。",
			CreatedAt:   now.Add(-time.Hour),
			UpdatedAt:   now.Add(-time.Hour),
		},
	} {
		if _, err := store.CreateTask(task); err != nil {
			t.Fatalf("CreateTask(%s): %v", task.ID, err)
		}
	}
	bot := &TelegramBot{taskSvc: tasksvc.New(store)}

	tasks := bot.loadHermesContextTasks(42, "好，那我們 #94 算是結束了嗎")
	if len(tasks) == 0 {
		t.Fatal("expected matching issue task context")
	}
	if tasks[0].ID != "issue-94" {
		t.Fatalf("first context task = %q, want issue-94; all tasks: %#v", tasks[0].ID, tasks)
	}
}

func TestMemoryResolverSkipsRecentMessagesForExplicitIssue(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	now := time.Date(2026, 4, 29, 13, 0, 0, 0, time.UTC)
	for _, task := range []hermes.TaskState{
		{
			ID:                "issue-143",
			ChatID:            42,
			GithubIssueNumber: 143,
			Status:            hermes.TaskStatusDone,
			Goal:              "[GitHub #143] Unified Memory Architecture",
			Accumulated:       "已決定先建立 MemoryResolver 與 MemoryBundle，再逐步接 Direct/file/multimedia。",
			CreatedAt:         now.Add(-2 * time.Hour),
			UpdatedAt:         now.Add(-2 * time.Hour),
		},
		{
			ID:                "issue-99",
			ChatID:            42,
			GithubIssueNumber: 99,
			Status:            hermes.TaskStatusExecuting,
			Goal:              "其他 active task",
			Accumulated:       "不應污染 #143 的 memory bundle。",
			CreatedAt:         now.Add(-time.Hour),
			UpdatedAt:         now.Add(-time.Hour),
		},
	} {
		if _, err := store.CreateTask(task); err != nil {
			t.Fatalf("CreateTask(%s): %v", task.ID, err)
		}
	}

	resolver := NewMemoryResolver(tasksvc.New(store))
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		UserMessage: "接下來我們來處理 #143 memory的架構重構工作",
		Mode:        "hermes",
		RecentMessages: []contextMessage{
			{Role: "user", Content: "剛剛在處理別的 issue"},
			{Role: "assistant", Content: "那是 #99 的內容"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bundle.Sections) != 1 {
		t.Fatalf("sections = %d, want only issue memory: %#v", len(bundle.Sections), bundle.Sections)
	}
	if bundle.Sections[0].Source != "issue_task" {
		t.Fatalf("first source = %q, want issue_task", bundle.Sections[0].Source)
	}
	rendered := bundle.Render()
	if !strings.Contains(rendered, "MemoryResolver 與 MemoryBundle") {
		t.Fatalf("expected issue memory in rendered bundle:\n%s", rendered)
	}
	if strings.Contains(rendered, "不應污染 #143") {
		t.Fatalf("different issue memory leaked into bundle:\n%s", rendered)
	}
	if strings.Contains(rendered, "那是 #99 的內容") {
		t.Fatalf("recent messages leaked into explicit issue bundle:\n%s", rendered)
	}
}

func TestMemoryResolverIncludesRecentMessagesWithoutExplicitIssue(t *testing.T) {
	resolver := NewMemoryResolver(tasksvc.New(hermes.NewMemoryTaskStore()))
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		UserMessage: "好，繼續",
		Mode:        "hermes",
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

func TestMemoryResolverSkipsHermesTaskMemoryForHermesWithoutExplicitIssue(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{
		ID:          "previous-hermes-task",
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		Status:      hermes.TaskStatusDone,
		Goal:        "舊 Hermes 任務",
		Accumulated: "這段 persisted Hermes context 不應進入沒有 issue anchor 的新 Hermes prompt。",
		CreatedAt:   time.Now().Add(-time.Hour),
		UpdatedAt:   time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	resolver := NewMemoryResolver(tasksvc.New(store))
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		UserMessage: "繼續處理",
		Mode:        "hermes",
		RecentMessages: []contextMessage{
			{Role: "user", Content: "剛剛討論目前任務"},
			{Role: "assistant", Content: "下一步是先確認 issue anchor。"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	rendered := bundle.Render()
	if strings.Contains(rendered, "Persisted Hermes context") || strings.Contains(rendered, "previous-hermes-task") {
		t.Fatalf("unanchored Hermes prompt leaked persisted task memory:\n%s", rendered)
	}
	if !strings.Contains(rendered, "下一步是先確認 issue anchor。") {
		t.Fatalf("expected recent messages to remain available:\n%s", rendered)
	}
}

func TestMemoryResolverSkipsHermesTaskMemoryForNonHermesWithoutExplicitIssue(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{
		ID:          "active-issue-143",
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		Status:      hermes.TaskStatusExecuting,
		Goal:        "[GitHub #143] Active Hermes work",
		Accumulated: "這段 active Hermes memory 不應進入一般文件分析 prompt。",
		CreatedAt:   time.Now().Add(-time.Hour),
		UpdatedAt:   time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	resolver := NewMemoryResolver(tasksvc.New(store))
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		UserMessage: "分析這份文件",
		Mode:        "document",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rendered := bundle.Render(); strings.Contains(rendered, "Persisted Hermes context") || strings.Contains(rendered, "active Hermes memory") {
		t.Fatalf("non-Hermes prompt leaked Hermes task memory:\n%s", rendered)
	}
}

func TestMemoryResolverKeepsIssueScopedHermesTaskMemoryForDocumentMode(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{
		ID:                "issue-143",
		ChatID:            42,
		ThreadID:          7,
		ProjectDir:        "/repo",
		GithubIssueNumber: 143,
		Status:            hermes.TaskStatusDone,
		Goal:              "[GitHub #143] Unified Memory Architecture",
		Accumulated:       "這段 #143 issue memory 可以支援明確 issue 文件分析。",
		CreatedAt:         time.Now().Add(-time.Hour),
		UpdatedAt:         time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	resolver := NewMemoryResolver(tasksvc.New(store))
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		UserMessage: "分析文件並延續 #143",
		Mode:        "document",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	rendered := bundle.Render()
	if !strings.Contains(rendered, "Persisted Hermes context") || !strings.Contains(rendered, "#143 issue memory") {
		t.Fatalf("issue-scoped document prompt missing Hermes task memory:\n%s", rendered)
	}
}

func TestMemoryResolverClampsLowPrioritySections(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{
		ID:                "issue-143",
		ChatID:            42,
		GithubIssueNumber: 143,
		Status:            hermes.TaskStatusDone,
		Goal:              "[GitHub #143] Unified Memory Architecture",
		Accumulated:       strings.Repeat("重要的 issue-scoped memory。", 20),
		CreatedAt:         time.Now().Add(-time.Hour),
		UpdatedAt:         time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	resolver := NewMemoryResolver(tasksvc.New(store))
	bundle, err := resolver.Resolve(context.Background(), MemoryRequest{
		ChatID:      42,
		UserMessage: "繼續 #143",
		BudgetChars: 160,
		RecentMessages: []contextMessage{
			{Role: "user", Content: strings.Repeat("recent ", 50)},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bundle.Sections) != 1 {
		t.Fatalf("sections = %d, want only high-priority issue memory: %#v", len(bundle.Sections), bundle.Sections)
	}
	if bundle.Sections[0].Source != "issue_task" {
		t.Fatalf("source = %q, want issue_task", bundle.Sections[0].Source)
	}
	if got := len([]rune(bundle.Render())); got > 160 {
		t.Fatalf("rendered length = %d, want <= 160", got)
	}
}

func TestSQLiteStorageListsGeneralMemoryCardsFromDecisionTasks(t *testing.T) {
	storage := newTestSQLiteStorage(t)
	now := time.Now().Add(-time.Hour)
	if err := storage.InsertDecisionLog(DecisionLog{
		Timestamp:     now,
		SessionID:     "direct-session",
		ProjectPath:   "/repo",
		ChatID:        42,
		ThreadID:      7,
		UserPrompt:    "請分析文件並整理 #143 memory 架構",
		AgentResponse: "已整理出 MemoryResolver 後續要讀 general task memory。",
		Outcome: ExecutionOutcome{
			Success:      true,
			TaskType:     "analysis",
			Summary:      "整理 memory 架構",
			FilesChanged: []string{"internal/app/memory_resolver.go", "docs/arch/memory.md"},
		},
		Model: "gpt-5.5",
	}); err != nil {
		t.Fatalf("InsertDecisionLog: %v", err)
	}
	if err := storage.InsertDecisionLog(DecisionLog{
		Timestamp:     now.Add(30 * time.Minute),
		SessionID:     "other-session",
		ProjectPath:   "/repo",
		ChatID:        42,
		ThreadID:      7,
		UserPrompt:    "處理 #99 unrelated",
		AgentResponse: "不應進入 #143 memory。",
		Outcome: ExecutionOutcome{
			Success:  true,
			TaskType: "analysis",
		},
		Model: "gpt-5.5",
	}); err != nil {
		t.Fatalf("InsertDecisionLog(other): %v", err)
	}

	cards, err := storage.ListGeneralMemoryCards(context.Background(), MemoryRequest{
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		UserMessage: "繼續 #143",
	}, 3)
	if err != nil {
		t.Fatalf("ListGeneralMemoryCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1: %#v", len(cards), cards)
	}
	if !strings.Contains(cards[0].Result, "general task memory") {
		t.Fatalf("card result = %q, want #143 memory", cards[0].Result)
	}
	if len(cards[0].Files) != 2 {
		t.Fatalf("card files = %#v, want touched files", cards[0].Files)
	}
	if !containsString(cards[0].Files, "internal/app/memory_resolver.go") {
		t.Fatalf("card files missing memory_resolver.go: %#v", cards[0].Files)
	}
	if len(cards[0].ContinuationHints) == 0 {
		t.Fatalf("expected continuation hints for card: %#v", cards[0])
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

	resolver := NewMemoryResolverWithSources(nil, storage)
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

	resolver := NewMemoryResolverWithAllSources(nil, nil, ProjectStaticHintSource{})
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
	resolver := NewMemoryResolverWithAllSources(nil, nil, ProjectStaticHintSource{})
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestComposeHermesGoalWithContextStripsNestedInjectedGoal(t *testing.T) {
	tasks := []hermes.TaskState{
		{
			Goal: `[Previous conversation context]
User: 先分析 webrtc-server
Assistant: 找到 4 個待修問題

[Current request]
好，請幫忙修正`,
			Accumulated: "已修正 4 個問題。",
		},
	}

	goal := composeHermesGoalWithContext("再幫我補測試", tasks, nil)
	if strings.Count(goal, "好，請幫忙修正") != 1 {
		t.Fatalf("expected prior actionable goal to appear once, got:\n%s", goal)
	}
	if !strings.Contains(goal, "再幫我補測試") {
		t.Fatalf("expected current request in goal, got:\n%s", goal)
	}
}

func TestSelectHermesContinuationTaskPrefersActiveSameProject(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "old-failed",
			ChatID:     42,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusFailed,
			Goal:       "修復舊問題",
			Plan: []hermes.SubTask{
				{Description: "已完成部分", Status: hermes.SubTaskDone},
			},
			UpdatedAt: now.Add(-time.Hour),
		},
		{
			ID:         "active-task",
			ChatID:     42,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "修復登入流程",
			UpdatedAt:  now.Add(-2 * time.Hour),
		},
		{
			ID:         "other-project",
			ChatID:     42,
			ProjectDir: "/tmp/other",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "其他專案",
			UpdatedAt:  now,
		},
	}

	got, ok := selectHermesContinuationTask(tasks, "/tmp/project")
	if !ok {
		t.Fatal("expected continuation candidate")
	}
	if got.ID != "active-task" {
		t.Fatalf("candidate = %q, want active-task", got.ID)
	}
}

func TestSelectHermesContinuationTaskSkipsLegacyEmptyProjectWhenCurrentProjectKnown(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:                "legacy-empty-project",
			ChatID:            42,
			ProjectDir:        "",
			GithubIssueNumber: 77,
			Status:            hermes.TaskStatusFailed,
			Goal:              "舊專案任務",
			Plan: []hermes.SubTask{
				{Description: "已完成部分", Status: hermes.SubTaskDone},
			},
			UpdatedAt: now,
		},
		{
			ID:         "same-project",
			ChatID:     42,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusInterrupted,
			Goal:       "目前專案任務",
			UpdatedAt:  now.Add(-time.Hour),
		},
	}

	got, ok := selectHermesContinuationTask(tasks, "/tmp/project")
	if !ok {
		t.Fatal("expected same-project continuation candidate")
	}
	if got.ID != "same-project" {
		t.Fatalf("candidate = %q, want same-project", got.ID)
	}
}

func TestSelectHermesContinuationTaskForScopeSkipsOtherThread(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "other-thread",
			ChatID:     42,
			ThreadID:   8,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "其他 topic 任務",
			UpdatedAt:  now,
		},
		{
			ID:         "same-thread",
			ChatID:     42,
			ThreadID:   7,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusInterrupted,
			Goal:       "目前 topic 任務",
			UpdatedAt:  now.Add(-time.Minute),
		},
	}

	got, ok := selectHermesContinuationTaskForScope(tasks, 7, "/tmp/project")
	if !ok {
		t.Fatal("expected same-thread continuation candidate")
	}
	if got.ID != "same-thread" {
		t.Fatalf("candidate = %q, want same-thread", got.ID)
	}
}

func TestSelectHermesContinuationTaskForScopeDoesNotAutoPickLegacyThread(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "legacy-thread",
			ChatID:     42,
			ThreadID:   0,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusInterrupted,
			Goal:       "舊版任務",
			UpdatedAt:  now,
		},
	}

	_, ok := selectHermesContinuationTaskForScope(tasks, 7, "/tmp/project")
	if ok {
		t.Fatal("legacy thread task should not be auto-selected for a topic")
	}
}

func TestSelectHermesContinuationTaskByIDForSelectableScopeAcceptsLegacyThread(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "legacy-thread",
			ChatID:     42,
			ThreadID:   0,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusInterrupted,
			Goal:       "舊版任務",
			UpdatedAt:  now,
		},
	}

	got, ok, ambiguous := selectHermesContinuationTaskByIDForSelectableScope(tasks, 7, "/tmp/project", "legacy")
	if !ok || ambiguous {
		t.Fatalf("expected explicit legacy selection, ok=%v ambiguous=%v", ok, ambiguous)
	}
	if got.ID != "legacy-thread" {
		t.Fatalf("task = %q, want legacy-thread", got.ID)
	}
}

func TestSelectHermesLegacyContinuationTasksForScope(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "legacy-thread",
			ThreadID:   0,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusInterrupted,
			Goal:       "舊版任務",
			UpdatedAt:  now,
		},
		{
			ID:         "other-thread",
			ThreadID:   8,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "其他 topic 任務",
			UpdatedAt:  now,
		},
	}

	got := selectHermesLegacyContinuationTasksForScope(tasks, 7, "/tmp/project", 3)
	if len(got) != 1 {
		t.Fatalf("legacy candidate count = %d, want 1", len(got))
	}
	if got[0].ID != "legacy-thread" {
		t.Fatalf("legacy candidate = %q, want legacy-thread", got[0].ID)
	}
	if got := selectHermesLegacyContinuationTasksForScope(tasks, 0, "/tmp/project", 3); len(got) != 0 {
		t.Fatalf("general topic should not use legacy fallback, got %d candidates", len(got))
	}
}

func TestSelectHermesContinuationTaskMatchesCleanProjectPath(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "same-project",
			ChatID:     42,
			ProjectDir: "/tmp/project/",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "目前專案任務",
			UpdatedAt:  now,
		},
	}

	got, ok := selectHermesContinuationTask(tasks, "/tmp/project")
	if !ok {
		t.Fatal("expected continuation candidate")
	}
	if got.ID != "same-project" {
		t.Fatalf("candidate = %q, want same-project", got.ID)
	}
}

func TestSelectHermesContinuationTasksRanksMultipleCandidates(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "done-recent",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusDone,
			Goal:       "已完成",
			Plan:       []hermes.SubTask{{Description: "done", Status: hermes.SubTaskDone}},
			UpdatedAt:  now,
		},
		{
			ID:         "interrupted-newer",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusInterrupted,
			Goal:       "中斷",
			UpdatedAt:  now.Add(-time.Minute),
		},
		{
			ID:         "executing-older",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "執行中",
			UpdatedAt:  now.Add(-time.Hour),
		},
	}

	got := selectHermesContinuationTasks(tasks, "/tmp/project", 3)
	if len(got) != 3 {
		t.Fatalf("candidate count = %d, want 3", len(got))
	}
	want := []string{"executing-older", "interrupted-newer", "done-recent"}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i].ID, want[i])
		}
	}
}

func TestSelectHermesContinuationTaskByIDPrefix(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "6b1960ba-active",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusInterrupted,
			Goal:       "接續目標",
			UpdatedAt:  now,
		},
		{
			ID:         "aaaaaaaa-other",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "其他目標",
			UpdatedAt:  now,
		},
	}

	got, ok, ambiguous := selectHermesContinuationTaskByID(tasks, "/tmp/project", "6b1960ba")
	if !ok || ambiguous {
		t.Fatalf("expected unambiguous match, ok=%v ambiguous=%v", ok, ambiguous)
	}
	if got.ID != "6b1960ba-active" {
		t.Fatalf("task = %q, want 6b1960ba-active", got.ID)
	}
}

func TestSelectHermesContinuationTaskByIDPrefixDetectsAmbiguous(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{ID: "6b1960ba-one", ProjectDir: "/tmp/project", Status: hermes.TaskStatusExecuting, UpdatedAt: now},
		{ID: "6b1960ba-two", ProjectDir: "/tmp/project", Status: hermes.TaskStatusInterrupted, UpdatedAt: now},
	}

	_, ok, ambiguous := selectHermesContinuationTaskByID(tasks, "/tmp/project", "6b1960ba")
	if ok || !ambiguous {
		t.Fatalf("expected ambiguous prefix, ok=%v ambiguous=%v", ok, ambiguous)
	}
}

func TestSelectHermesContinuationTaskByIDPrefixSkipsNonContinuable(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{ID: "6b1960ba-done-old", ProjectDir: "/tmp/project", Status: hermes.TaskStatusDone, UpdatedAt: now.Add(-48 * time.Hour)},
	}

	_, ok, ambiguous := selectHermesContinuationTaskByID(tasks, "/tmp/project", "6b1960ba")
	if ok || ambiguous {
		t.Fatalf("old completed task should not match, ok=%v ambiguous=%v", ok, ambiguous)
	}
}

func TestHermesTaskIsContinuable(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		task hermes.TaskState
		want bool
	}{
		{
			name: "active",
			task: hermes.TaskState{Status: hermes.TaskStatusExecuting, UpdatedAt: now},
			want: true,
		},
		{
			name: "failed with progress",
			task: hermes.TaskState{Status: hermes.TaskStatusFailed, Accumulated: "已完成一部分", UpdatedAt: now},
			want: true,
		},
		{
			name: "recent done with progress",
			task: hermes.TaskState{Status: hermes.TaskStatusDone, Accumulated: "已完成", UpdatedAt: now.Add(-time.Hour)},
			want: true,
		},
		{
			name: "old done",
			task: hermes.TaskState{Status: hermes.TaskStatusDone, Accumulated: "已完成", UpdatedAt: now.Add(-48 * time.Hour)},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hermesTaskIsContinuable(tt.task); got != tt.want {
				t.Fatalf("hermesTaskIsContinuable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectSimilarHermesTaskContinuesActiveMatch(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "active-match",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "修復登入流程",
			UpdatedAt:  now.Add(-time.Hour),
		},
		{
			ID:         "other-goal",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "修復付款流程",
			UpdatedAt:  now,
		},
	}

	got, decision, ok := selectSimilarHermesTask(tasks, "/tmp/project", " 修復登入流程 ", now)
	if !ok {
		t.Fatal("expected similar task")
	}
	if got.ID != "active-match" || decision != hermesSimilarContinue {
		t.Fatalf("similar task = (%q, %v), want active-match continue", got.ID, decision)
	}
}

func TestSelectSimilarHermesTaskStopsRecentCompletedMatch(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "completed-match",
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusDone,
			Goal:       "補 retry 測試",
			Plan: []hermes.SubTask{
				{Description: "補測試", Status: hermes.SubTaskDone},
			},
			UpdatedAt: now.Add(-2 * time.Hour),
		},
	}

	got, decision, ok := selectSimilarHermesTask(tasks, "/tmp/project", "補 retry 測試", now)
	if !ok {
		t.Fatal("expected recent completed similar task")
	}
	if got.ID != "completed-match" || decision != hermesSimilarCompleted {
		t.Fatalf("similar task = (%q, %v), want completed-match completed", got.ID, decision)
	}
}

func TestSelectSimilarHermesTaskReturnsAmbiguousForMediumSimilarity(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:          "maybe-login",
			ProjectDir:  "/tmp/project",
			Status:      hermes.TaskStatusFailed,
			Goal:        "修復登入流程並補測試",
			Accumulated: "已完成 cookie 修正",
			UpdatedAt:   now,
		},
	}

	got, decision, ok := selectSimilarHermesTask(tasks, "/tmp/project", "修復登入流程", now)
	if !ok {
		t.Fatal("expected ambiguous similar task")
	}
	if got.ID != "maybe-login" || decision != hermesSimilarAmbiguous {
		t.Fatalf("similar task = (%q, %v), want maybe-login ambiguous", got.ID, decision)
	}
}

func TestParseHermesCallbackData(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantMode   string
		wantTaskID string
		wantOK     bool
	}{
		{name: "continue", data: "hermes:continue:task-123", wantMode: "continue", wantTaskID: "task-123", wantOK: true},
		{name: "replan", data: "hermes:replan:task-456", wantMode: "replan", wantTaskID: "task-456", wantOK: true},
		{name: "cancel", data: "hermes:cancel", wantMode: "cancel", wantOK: true},
		{name: "issue restart", data: "hermes:issue-restart:182:codex", wantMode: "issue-restart:codex", wantTaskID: "182", wantOK: true},
		{name: "missing task", data: "hermes:continue:", wantOK: false},
		{name: "unknown mode", data: "hermes:restart:task-123", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMode, gotTaskID, gotOK := parseHermesCallbackData(tt.data)
			if gotMode != tt.wantMode || gotTaskID != tt.wantTaskID || gotOK != tt.wantOK {
				t.Fatalf("parseHermesCallbackData(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.data, gotMode, gotTaskID, gotOK, tt.wantMode, tt.wantTaskID, tt.wantOK)
			}
		})
	}
}

func TestSendHermesRecentCompletedIssueActionsQueuesRestartAndReplan(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		i18n:            newTestI18nManager(t),
		langPreferences: map[int64]string{},
		messageQueue:    make(chan *TelegramMessage, 1),
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.sendHermesRecentCompletedIssueActions(key, 182, hermes.TaskState{ID: "09570b36-1111-2222-3333-444444444444"}, "codex")

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "不代表 GitHub issue 必須停止") {
			t.Fatalf("recent completed issue message should explain non-terminal issue state:\n%s", text)
		}
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 2 || len(rows[0]) != 2 || len(rows[1]) != 1 {
			t.Fatalf("unexpected keyboard shape: %#v", rows)
		}
		if rows[0][0]["callback_data"] != "hermes:issue-restart:182:codex" {
			t.Fatalf("unexpected restart button: %#v", rows[0][0])
		}
		if rows[0][1]["callback_data"] != "hermes:replan:09570b36-1111-2222-3333-444444444444" {
			t.Fatalf("unexpected replan button: %#v", rows[0][1])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recent completed issue actions message")
	}
}

func TestCountUncheckedIssueChecklistItems(t *testing.T) {
	issue := &hermes.IssueContext{
		Checklist: []hermes.ChecklistItem{
			{Text: "done", Checked: true},
			{Text: "remaining", Checked: false},
			{Text: "also remaining", Checked: false},
		},
	}
	if got := countUncheckedIssueChecklistItems(issue); got != 2 {
		t.Fatalf("countUncheckedIssueChecklistItems = %d, want 2", got)
	}
	if got := countUncheckedIssueChecklistItems(nil); got != 0 {
		t.Fatalf("countUncheckedIssueChecklistItems(nil) = %d, want 0", got)
	}
}

func TestFormatHermesIssueReconciliationMessage(t *testing.T) {
	rec := hermes.ReconcileIssueCompletion(&hermes.IssueContext{
		Number: 153,
		Checklist: []hermes.ChecklistItem{
			{Text: "done", Checked: true},
			{Text: "remaining", Checked: false},
		},
	})
	text := formatHermesIssueReconciliationMessage(rec)
	if !strings.Contains(text, "本輪已完成") || !strings.Contains(text, "Issue #153 尚未完成") || !strings.Contains(text, "remaining") {
		t.Fatalf("unexpected reconciliation text:\n%s", text)
	}
}

func TestSendHermesIssueReconciliationQueuesActionsWhenUnchecked(t *testing.T) {
	oldFetch := hermesFetchIssue
	defer func() { hermesFetchIssue = oldFetch }()
	hermesFetchIssue = func(ctx context.Context, projectDir string, issueNumber int) (*hermes.IssueContext, error) {
		if projectDir != "/repo" || issueNumber != 153 {
			t.Fatalf("unexpected fetch args: %q #%d", projectDir, issueNumber)
		}
		return &hermes.IssueContext{
			Number: 153,
			Checklist: []hermes.ChecklistItem{
				{Text: "done", Checked: true},
				{Text: "remaining", Checked: false},
			},
		}, nil
	}
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		messageQueue: make(chan *TelegramMessage, 1),
	}
	bot.sendHermesIssueReconciliation(context.Background(), key, hermes.TaskState{
		ID:                "6b1960ba-1111-2222-3333-444444444444",
		ProjectDir:        "/repo",
		GithubIssueNumber: 153,
	})
	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "sendMessage" {
			t.Fatalf("unexpected method: %s", msg.Method)
		}
		if !strings.Contains(fmt.Sprint(msg.Params["text"]), "Issue #153 尚未完成") {
			t.Fatalf("unexpected text: %#v", msg.Params["text"])
		}
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok || len(rows) < 2 {
			t.Fatalf("inline_keyboard missing or too small: %#v", markup["inline_keyboard"])
		}
		if rows[0][0]["callback_data"] != "hermes:continue:6b1960ba-1111-2222-3333-444444444444" {
			t.Fatalf("unexpected continue button: %#v", rows[0][0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reconciliation actions message")
	}
}

func TestSendHermesCandidateActionsQueuesInlineKeyboard(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		messageQueue: make(chan *TelegramMessage, 1),
	}

	bot.sendHermesCandidateActions(key, "找到可能相關任務", hermes.TaskState{ID: "6b1960ba-1111-2222-3333-444444444444"})

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "sendMessage" {
			t.Fatalf("unexpected method: %s", msg.Method)
		}
		if msg.Params["chat_id"] != "42" || msg.Params["message_thread_id"] != "7" {
			t.Fatalf("unexpected chat/thread params: %#v", msg.Params)
		}
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 2 || len(rows[0]) != 2 || len(rows[1]) != 1 {
			t.Fatalf("unexpected keyboard shape: %#v", rows)
		}
		if rows[0][0]["callback_data"] != "hermes:continue:6b1960ba-1111-2222-3333-444444444444" {
			t.Fatalf("unexpected continue button: %#v", rows[0][0])
		}
		if rows[0][1]["callback_data"] != "hermes:replan:6b1960ba-1111-2222-3333-444444444444" {
			t.Fatalf("unexpected replan button: %#v", rows[0][1])
		}
		if rows[1][0]["callback_data"] != "hermes:cancel" {
			t.Fatalf("unexpected cancel button: %#v", rows[1][0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Hermes candidate actions message")
	}
}

func TestSendMenuQueuesInlineKeyboard(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/tmp"},
		agents:       map[chatKey]*Agent{},
		chatContexts: map[chatKey]*ChatContext{},
		hermesCoords: map[chatKey]*hermesCoord{},
		messageQueue: make(chan *TelegramMessage, 1),
	}

	bot.sendMenu(key)

	select {
	case msg := <-bot.messageQueue:
		if msg.Method != "sendMessage" {
			t.Fatalf("unexpected method: %s", msg.Method)
		}
		if msg.Params["chat_id"] != "42" || msg.Params["message_thread_id"] != "7" {
			t.Fatalf("unexpected chat/thread params: %#v", msg.Params)
		}
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 5 {
			t.Fatalf("expected 5 menu rows, got %d", len(rows))
		}
		if rows[1][0]["callback_data"] != "menu:tasks" {
			t.Fatalf("unexpected tasks button: %#v", rows[1][0])
		}
		if rows[1][1]["callback_data"] != "menu:hermes_status" {
			t.Fatalf("unexpected Hermes button: %#v", rows[1][1])
		}
		if rows[2][0]["callback_data"] != "retry:menu" {
			t.Fatalf("unexpected retry button: %#v", rows[2][0])
		}
		if rows[2][1]["callback_data"] != "model:menu" {
			t.Fatalf("unexpected model button: %#v", rows[2][1])
		}
		if rows[4][1]["callback_data"] != "menu:abort_confirm" {
			t.Fatalf("unexpected abort button: %#v", rows[4][1])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for menu message")
	}
}

func TestSendTasksSelectorQueuesOpenClosedRefresh(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendTasksSelector(key)

	select {
	case msg := <-bot.messageQueue:
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if rows[0][0]["callback_data"] != "tasks:view:open" || rows[0][1]["callback_data"] != "tasks:view:closed" {
			t.Fatalf("unexpected task view row: %#v", rows[0])
		}
		if rows[1][0]["callback_data"] != "tasks:refresh:open" || rows[1][1]["callback_data"] != "tasks:refresh:closed" {
			t.Fatalf("unexpected task refresh row: %#v", rows[1])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tasks selector message")
	}
}

func TestSendModelMenuQueuesSelector(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		chatContexts: map[chatKey]*ChatContext{key: NewChatContext(42, 7, "/repo")},
		messageQueue: make(chan *TelegramMessage, 1),
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")

	bot.sendModelMenu(key)

	select {
	case msg := <-bot.messageQueue:
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 5 {
			t.Fatalf("expected 5 model rows, got %d", len(rows))
		}
		if rows[0][0]["callback_data"] != "model:set:fast" || rows[3][0]["callback_data"] != "model:set:gpt-deep" {
			t.Fatalf("unexpected model rows: %#v", rows)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for model menu message")
	}
}

func TestSendModelMenuIncludesHermesTierSwitchWhenEnabled(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			Hermes:            HermesConfig{Enabled: true},
		},
		chatContexts: map[chatKey]*ChatContext{key: NewChatContext(42, 7, "/repo")},
		messageQueue: make(chan *TelegramMessage, 1),
	}
	bot.chatContexts[key].Pref = ModelPreference("auto")
	bot.hermesCoords = map[chatKey]*hermesCoord{
		key: &hermesCoord{enabled: true, tier: "codex"},
	}

	bot.sendModelMenu(key)

	select {
	case msg := <-bot.messageQueue:
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 6 {
			t.Fatalf("expected 6 model rows with Hermes enabled, got %d", len(rows))
		}
		if rows[4][0]["callback_data"] != "model:hermes-tier:claude" || rows[4][1]["callback_data"] != "model:hermes-tier:codex" {
			t.Fatalf("unexpected Hermes tier row: %#v", rows[4])
		}
		if rows[4][1]["text"] != "✅ Hermes: GPT/Codex" {
			t.Fatalf("unexpected Hermes tier label: %#v", rows[4][1]["text"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for model menu message")
	}
}

func TestSendRetryConfirmationQueuesRunButton(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 1)}

	bot.sendRetryConfirmation(key, "all", "task-123", 0)

	select {
	case msg := <-bot.messageQueue:
		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if rows[0][0]["callback_data"] != "retry:run:all:task-123" {
			t.Fatalf("unexpected retry run button: %#v", rows[0][0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for retry confirmation message")
	}
}

func TestHermesCandidateActionRowsSupportsMultipleTasks(t *testing.T) {
	rows := hermesCandidateActionRows("zh-TW", nil, []hermes.TaskState{
		{ID: "task-one-abcdef"},
		{ID: "task-two-abcdef"},
	})

	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3", len(rows))
	}
	if rows[0][0]["callback_data"] != "hermes:continue:task-one-abcdef" {
		t.Fatalf("unexpected first continue button: %#v", rows[0][0])
	}
	if rows[1][1]["callback_data"] != "hermes:replan:task-two-abcdef" {
		t.Fatalf("unexpected second replan button: %#v", rows[1][1])
	}
	if rows[2][0]["callback_data"] != "hermes:cancel" {
		t.Fatalf("unexpected cancel row: %#v", rows[2])
	}
}

func TestSelectSimilarHermesTaskSkipsEmptyProjectWhenCurrentProjectKnown(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:          "legacy-empty-project",
			ProjectDir:  "",
			Status:      hermes.TaskStatusFailed,
			Goal:        "修復登入流程",
			Accumulated: "已完成部分",
			UpdatedAt:   now,
		},
	}

	_, _, ok := selectSimilarHermesTask(tasks, "/tmp/project", "修復登入流程", now)
	if ok {
		t.Fatal("legacy empty-project task should not match a known current project")
	}
}

func TestSelectSimilarHermesTaskForScopeSkipsOtherThread(t *testing.T) {
	now := time.Now()
	tasks := []hermes.TaskState{
		{
			ID:         "other-thread",
			ThreadID:   8,
			ProjectDir: "/tmp/project",
			Status:     hermes.TaskStatusExecuting,
			Goal:       "修復登入流程",
			UpdatedAt:  now,
		},
	}

	_, _, ok := selectSimilarHermesTaskForScope(tasks, 7, "/tmp/project", "修復登入流程", now)
	if ok {
		t.Fatal("other-thread task should not match current topic")
	}
}

func TestHermesContinuationProjectDirPrefersStoredTaskProject(t *testing.T) {
	task := hermes.TaskState{ProjectDir: "/tmp/original"}
	if got := hermesContinuationProjectDir(task, "/tmp/current"); got != "/tmp/original" {
		t.Fatalf("project dir = %q, want stored task project", got)
	}
}

func TestBuildHermesContinuationGoalIncludesProgressInstructions(t *testing.T) {
	task := hermes.TaskState{
		ID:          "task-1234567890",
		Status:      hermes.TaskStatusInterrupted,
		Goal:        "修復登入流程",
		Accumulated: "已完成 session cookie 修正。",
		Plan: []hermes.SubTask{
			{Description: "修正 cookie", Status: hermes.SubTaskDone, Result: "PASS"},
			{Description: "補登入測試", Status: hermes.SubTaskFailed, Result: "缺少 integration test"},
		},
	}

	goal := buildHermesContinuationGoal(task, "replan")
	for _, want := range []string{
		"[Hermes continuation]",
		"Mode: replan",
		"Original goal:",
		"修復登入流程",
		"已完成 session cookie 修正",
		"[done] 修正 cookie",
		"[failed] 補登入測試",
		"Do not repeat completed work",
		"Re-plan only the remaining",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("continuation goal missing %q:\n%s", want, goal)
		}
	}
}

func TestIsHermesContinuationRequest(t *testing.T) {
	for _, text := range []string{"繼續處理", "接續上一個", "重新規劃剩下的工作", "continue", "resume task", "replan task"} {
		if !isHermesContinuationRequest(text) {
			t.Fatalf("expected continuation request for %q", text)
		}
	}
	if isHermesContinuationRequest("重新開始新的任務") {
		t.Fatal("restart text should not be treated as continuation")
	}
}

func TestHermesContinuationModeFromRequest(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "繼續處理", want: "continue"},
		{text: "接續上一個", want: "continue"},
		{text: "重新規劃剩下的工作", want: "replan"},
		{text: "重規 #139", want: "replan"},
		{text: "replan task", want: "replan"},
		{text: "resume task", want: "continue"},
	}

	for _, tt := range tests {
		if got := hermesContinuationModeFromRequest(tt.text); got != tt.want {
			t.Fatalf("hermesContinuationModeFromRequest(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestIsHermesIssueReferenceRequest(t *testing.T) {
	for _, text := range []string{"繼續處理＃１３７", "好，繼續處理 #137", "接續 #137", "請處理 #137", "start #137"} {
		if !isHermesIssueReferenceRequest(text) {
			t.Fatalf("expected issue reference request for %q", text)
		}
	}
	for _, text := range []string{
		"剛剛 #137 是什麼狀態？",
		"#184 要繼續處理嗎",
		"所以目前 #182 還需要處理嗎",
		"請問 #225 的子項目處理完畢了嗎",
	} {
		if isHermesIssueReferenceRequest(text) {
			t.Fatalf("status-style issue mention should not be treated as an issue launch request: %q", text)
		}
	}
}

func TestIsHermesIssueRestartRequest(t *testing.T) {
	for _, text := range []string{"重新處理#293", "重新開始 #293", "重做 ＃２９３", "restart #293", "rerun #293"} {
		if !isHermesIssueRestartRequest(text) {
			t.Fatalf("expected issue restart request for %q", text)
		}
	}
	if isHermesIssueRestartRequest("繼續處理 #293") {
		t.Fatal("continue issue mention should not be treated as restart")
	}
	if isHermesIssueRestartRequest("重新處理 #293 嗎") {
		t.Fatal("restart question should not be treated as restart")
	}
}

func TestParseHermesRestartIssue(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  int
		ok    bool
	}{
		{name: "plain issue", parts: []string{"/ghermes", "restart", "#293"}, want: 293, ok: true},
		{name: "issue in generated hint", parts: []string{"/hermes", "restart", "[GitHub", "#293]", "title"}, want: 293, ok: true},
		{name: "non restart", parts: []string{"/ghermes", "處理", "#293"}, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseHermesRestartIssue(tt.parts)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseHermesRestartIssue(%v) = (%d, %v), want (%d, %v)", tt.parts, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestChatContextSharesRecentMessagesAcrossAgentAndHermes(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		chatContexts: map[chatKey]*ChatContext{},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
	}
	chatCtx := bot.getChatContext(key, "/tmp/alice-project")
	agent := NewAgentWithContext(&mockClient{}, chatCtx)

	agent.AddRecentMessage("用 /gsmart 分析", "找到一個設定問題")
	recent := bot.getChatContext(key, "").RecentMessagesSnapshot()

	if len(recent) != 2 {
		t.Fatalf("expected shared recent messages, got %d", len(recent))
	}
	if recent[0].Content != "用 /gsmart 分析" || recent[1].Content != "找到一個設定問題" {
		t.Fatalf("unexpected recent messages: %#v", recent)
	}
}

func TestChatContextKeepsBackendSessionsIndependent(t *testing.T) {
	ctx := NewChatContext(42, 7, "/tmp/alice-project")
	ctx.SetSession(BackendClaude, "claude-session")
	ctx.SetSession(BackendCodex, "codex-thread")

	if got := ctx.Session(BackendClaude); got != "claude-session" {
		t.Fatalf("claude session: got %q", got)
	}
	if got := ctx.Session(BackendCodex); got != "codex-thread" {
		t.Fatalf("codex session: got %q", got)
	}

	ctx.ClearSession(BackendCodex)
	if got := ctx.Session(BackendClaude); got != "claude-session" {
		t.Fatalf("clearing codex should not clear claude session, got %q", got)
	}
	if got := ctx.Session(BackendCodex); got != "" {
		t.Fatalf("codex session should be cleared, got %q", got)
	}
}

func TestManualModelSwitchCommandsWarnContextReset(t *testing.T) {
	tests := []struct {
		command       string
		lastUsedModel string
		want          string
	}{
		{command: "/fast", lastUsedModel: "claude-sonnet-4-6", want: "快速模式"},
		{command: "/smart", lastUsedModel: "claude-haiku-4-5", want: "智能模式"},
		{command: "/deep", lastUsedModel: "claude-sonnet-4-6", want: "深度模式"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			key := chatKey{chatID: 42, threadID: 7}
			agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
			agent.current().ctx.SetSession(BackendClaude, "claude-session")
			agent.lastUsedModel = tt.lastUsedModel

			bot := &TelegramBot{
				agents: map[chatKey]*Agent{
					key: agent,
				},
				config: &Config{
					DefaultProjectDir: "/tmp/alice-project",
					ModelRouting: ModelRoutingConfig{
						EnableDynamicRouting: true,
						FastModel:            "claude-haiku-4-5",
						SmartModel:           "claude-sonnet-4-6",
						DeepModel:            "claude-opus-4-5",
					},
				},
				i18n:            newTestI18nManager(t),
				messageQueue:    make(chan *TelegramMessage, 1),
				langPreferences: map[int64]string{},
			}
			bot.setChatlanguage(key.chatID, "zh-TW")

			bot.handleCommand(key, tt.command)

			select {
			case msg := <-bot.messageQueue:
				text, _ := msg.Params["text"].(string)
				if !strings.Contains(text, tt.want) {
					t.Fatalf("message %q does not contain mode text %q", text, tt.want)
				}
				if !strings.Contains(text, "切換模型將在下一則訊息時開始新的 backend session") {
					t.Fatalf("message %q does not contain context reset warning", text)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for model switch response")
			}

			if got := agent.current().ctx.Session(BackendClaude); got != "" {
				t.Fatalf("claude session after %s = %q, want empty", tt.command, got)
			}
		})
	}
}

func TestClearAndResetCommandsResetSession(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "/clear", want: "Session 上下文已清除"},
		{command: "/reset", want: "對話已清除"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			key := chatKey{chatID: 42, threadID: 7}
			agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
			agent.current().ctx.SetSession(BackendClaude, "claude-session")
			agent.current().ctx.SetSession(BackendCodex, "codex-thread")
			agent.current().ctx.RecentMsgs = []contextMessage{{Role: "user", Content: "前一則訊息"}}
			agent.lastUsedModel = "claude-sonnet-4-6"

			bot := &TelegramBot{
				agents: map[chatKey]*Agent{
					key: agent,
				},
				config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
				i18n:            newTestI18nManager(t),
				messageQueue:    make(chan *TelegramMessage, 1),
				langPreferences: map[int64]string{},
			}
			bot.setChatlanguage(key.chatID, "zh-TW")

			bot.handleCommand(key, tt.command)

			assertQueuedMessageContains(t, bot.messageQueue, tt.want)
			if got := agent.current().ctx.Session(BackendClaude); got != "" {
				t.Fatalf("claude session after %s = %q, want empty", tt.command, got)
			}
			if got := agent.current().ctx.Session(BackendCodex); got != "" {
				t.Fatalf("codex session after %s = %q, want empty", tt.command, got)
			}
			if got := len(agent.current().ctx.RecentMsgs); got != 0 {
				t.Fatalf("recent messages after %s = %d, want 0", tt.command, got)
			}
		})
	}
}

func TestCloseCommandClosesFetchedIssue(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: agent,
		},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue:    make(chan *TelegramMessage, 2),
		langPreferences: map[int64]string{},
	}

	oldFetch := hermesFetchIssue
	oldClose := hermesCloseIssue
	defer func() {
		hermesFetchIssue = oldFetch
		hermesCloseIssue = oldClose
	}()
	var closedProject string
	var closedIssue int
	hermesFetchIssue = func(ctx context.Context, projectDir string, issueNumber int) (*hermes.IssueContext, error) {
		if projectDir != "/tmp/alice-project" {
			t.Fatalf("projectDir = %q, want /tmp/alice-project", projectDir)
		}
		if issueNumber != 57 {
			t.Fatalf("issueNumber = %d, want 57", issueNumber)
		}
		return &hermes.IssueContext{Number: issueNumber, Title: "Chat history", State: "OPEN"}, nil
	}
	hermesCloseIssue = func(ctx context.Context, projectDir string, issueNumber int) error {
		closedProject = projectDir
		closedIssue = issueNumber
		return nil
	}

	bot.handleCommand(key, "/close 57")

	if closedProject != "/tmp/alice-project" || closedIssue != 57 {
		t.Fatalf("closed issue = (%q, %d), want (/tmp/alice-project, 57)", closedProject, closedIssue)
	}
	assertQueuedMessageContains(t, bot.messageQueue, "已關閉 Issue #57")
}

func TestCloseCommandSkipsAlreadyClosedIssue(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	agent := NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)
	bot := &TelegramBot{
		agents:       map[chatKey]*Agent{key: agent},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	oldFetch := hermesFetchIssue
	oldClose := hermesCloseIssue
	defer func() {
		hermesFetchIssue = oldFetch
		hermesCloseIssue = oldClose
	}()
	hermesFetchIssue = func(ctx context.Context, projectDir string, issueNumber int) (*hermes.IssueContext, error) {
		return &hermes.IssueContext{Number: issueNumber, Title: "Already done", State: "CLOSED"}, nil
	}
	hermesCloseIssue = func(ctx context.Context, projectDir string, issueNumber int) error {
		t.Fatalf("CloseIssue should not be called for closed issue")
		return nil
	}

	bot.handleCommand(key, "/close https://github.com/chimerakang/dumbledore/issues/57")

	assertQueuedMessageContains(t, bot.messageQueue, "已經是 closed")
}

func TestParseCloseIssueNumber(t *testing.T) {
	cases := []struct {
		text string
		want int
		ok   bool
	}{
		{text: "/close #57", want: 57, ok: true},
		{text: "/close 57", want: 57, ok: true},
		{text: "/close https://github.com/chimerakang/dumbledore/issues/57", want: 57, ok: true},
		{text: "/close nope", ok: false},
	}
	for _, tc := range cases {
		got, ok := parseCloseIssueNumber(strings.Fields(tc.text), tc.text)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseCloseIssueNumber(%q) = (%d, %v), want (%d, %v)", tc.text, got, ok, tc.want, tc.ok)
		}
	}
}

func TestChatContextRestoresModelPreferenceFromStorage(t *testing.T) {
	storage := newTestSQLiteStorage(t)
	if err := storage.SaveTopicModelPreference(42, 7, "/tmp/alice-project", "gpt-5.5"); err != nil {
		t.Fatalf("SaveTopicModelPreference: %v", err)
	}

	oldStorage := globalStorage
	globalStorage = storage
	t.Cleanup(func() { globalStorage = oldStorage })

	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		chatContexts: map[chatKey]*ChatContext{},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
	}

	ctx := bot.getChatContext(key, "/tmp/alice-project")
	if got := string(ctx.Pref); got != "gpt-5.5" {
		t.Fatalf("restored model preference = %q, want gpt-5.5", got)
	}
}

func TestStrictCommandDispatchAndToggle(t *testing.T) {
	key := chatKey{chatID: 7, threadID: 11}
	bot := &TelegramBot{
		config:       &Config{},
		hermesCoords: map[chatKey]*hermesCoord{},
		messageQueue: make(chan *TelegramMessage, 4),
	}

	bot.handleCommand(key, "/strict")
	assertQueuedMessageContains(t, bot.messageQueue, "strict review mode 已啟用")
	if !bot.strictModeEnabled(key, "") {
		t.Fatal("expected strict mode override to enable after /strict toggle")
	}

	bot.handleCommand(key, "/strict off")
	assertQueuedMessageContains(t, bot.messageQueue, "strict review mode 已停用")
	if bot.strictModeEnabled(key, "") {
		t.Fatal("expected strict mode override to disable after /strict off")
	}

	bot.handleCommand(key, "/strict status")
	assertQueuedMessageContains(t, bot.messageQueue, "strict review mode：已停用")
}

func TestStrictModeAutoEnableFromRiskVerbs(t *testing.T) {
	cases := []struct {
		name string
		goal string
		want bool
	}{
		{name: "commit", goal: "請 commit 這次修改", want: true},
		{name: "push", goal: "先整理後 push 到 main", want: true},
		{name: "deploy", goal: "完成部署到 production", want: true},
		{name: "ssh", goal: "ssh 到主機確認服務", want: true},
		{name: "release", goal: "release v1.2.3", want: true},
		{name: "non-risk", goal: "請幫我整理文件", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoEnableStrict(tc.goal); got != tc.want {
				t.Fatalf("shouldAutoEnableStrict(%q) = %v, want %v", tc.goal, got, tc.want)
			}
		})
	}
}

func TestResolveStrictModeConfigAutoEnablesFromGoal(t *testing.T) {
	key := chatKey{chatID: 99, threadID: 3}
	bot := &TelegramBot{
		config: &Config{},
		hermesCoords: map[chatKey]*hermesCoord{
			key: &hermesCoord{},
		},
	}

	cfg := bot.resolveStrictModeConfig(key, "請 commit 並 push 變更")
	if !cfg.Enabled {
		t.Fatalf("expected strict config to auto-enable for risk verb goal, got %+v", cfg)
	}

	cfg = bot.resolveStrictModeConfig(key, "只是說明文件內容")
	if cfg.Enabled {
		t.Fatalf("expected strict config to stay disabled for non-risk goal, got %+v", cfg)
	}
}

// Issue #147: HermesConfig.ReviewTimeoutSeconds must override the engine default
// so operators can tune review timeout from config.json without recompiling.
func TestResolveStrictModeConfigUsesHermesReviewTimeoutOverride(t *testing.T) {
	key := chatKey{chatID: 99, threadID: 3}
	bot := &TelegramBot{
		config: &Config{
			Hermes: HermesConfig{ReviewTimeoutSeconds: 300},
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: &hermesCoord{},
		},
	}

	cfg := bot.resolveStrictModeConfig(key, "any goal")
	if cfg.ReviewTimeout != 300*time.Second {
		t.Fatalf("ReviewTimeout = %s, want 300s (from HermesConfig override)", cfg.ReviewTimeout)
	}

	bot.config.Hermes.ReviewTimeoutSeconds = 0
	cfg = bot.resolveStrictModeConfig(key, "any goal")
	if cfg.ReviewTimeout != 120*time.Second {
		t.Fatalf("ReviewTimeout fallback = %s, want 120s (engine default)", cfg.ReviewTimeout)
	}
}

func assertQueuedMessageContains(t *testing.T, queue <-chan *TelegramMessage, want string) {
	t.Helper()

	select {
	case msg := <-queue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, want) {
			t.Fatalf("queued message %q does not contain %q", text, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for queued message containing %q", want)
	}
}

func TestHandleMessageHermesEnabledIssueRefFetchesIssue(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	const userID int64 = 123
	const projectDir = "/tmp/alice-project"

	called := make(chan struct{}, 1)
	oldFetchIssue := hermesFetchIssue
	hermesFetchIssue = func(ctx context.Context, gotProjectDir string, gotIssueNumber int) (*hermes.IssueContext, error) {
		if gotProjectDir != projectDir {
			t.Errorf("project dir: want %q, got %q", projectDir, gotProjectDir)
		}
		if gotIssueNumber != 109 {
			t.Errorf("issue number: want 109, got %d", gotIssueNumber)
		}
		called <- struct{}{}
		return nil, errors.New("stop after fetch")
	}
	defer func() { hermesFetchIssue = oldFetchIssue }()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		allowIDs: map[int64]bool{userID: true},
		config: &Config{
			Hermes: HermesConfig{Enabled: true},
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true},
		},
		messageQueue: make(chan *TelegramMessage, 10),
	}

	bot.handleMessage(key, userID, "接下來請處理 #109", "", nil, nil, nil, "", 1)

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("expected Hermes issue reference to fetch GitHub issue")
	}
}

func TestHandleMessageHermesContinuationWithIssueRefFetchesIssue(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	const userID int64 = 123
	const projectDir = "/tmp/alice-project"

	called := make(chan struct{}, 1)
	oldFetchIssue := hermesFetchIssue
	hermesFetchIssue = func(ctx context.Context, gotProjectDir string, gotIssueNumber int) (*hermes.IssueContext, error) {
		if gotProjectDir != projectDir {
			t.Errorf("project dir: want %q, got %q", projectDir, gotProjectDir)
		}
		if gotIssueNumber != 137 {
			t.Errorf("issue number: want 137, got %d", gotIssueNumber)
		}
		called <- struct{}{}
		return nil, errors.New("stop after fetch")
	}
	defer func() { hermesFetchIssue = oldFetchIssue }()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		allowIDs: map[int64]bool{userID: true},
		config: &Config{
			Hermes: HermesConfig{Enabled: true},
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true},
		},
		messageQueue: make(chan *TelegramMessage, 10),
	}

	bot.handleMessage(key, userID, "繼續處理＃１３７", "", nil, nil, nil, "", 1)

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("expected issue-specific continuation to fetch GitHub issue")
	}
}

func TestHandleMessageIssueStatusQuestionDoesNotFetchIssueInHermesMode(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	const userID int64 = 123
	const projectDir = "/tmp/alice-project"

	called := make(chan struct{}, 1)
	oldFetchIssue := hermesFetchIssue
	hermesFetchIssue = func(ctx context.Context, gotProjectDir string, gotIssueNumber int) (*hermes.IssueContext, error) {
		called <- struct{}{}
		return nil, errors.New("unexpected fetch")
	}
	defer func() { hermesFetchIssue = oldFetchIssue }()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		allowIDs: map[int64]bool{userID: true},
		config: &Config{
			Hermes: HermesConfig{Enabled: true},
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true},
		},
		messageQueue: make(chan *TelegramMessage, 10),
	}

	bot.handleMessage(key, userID, "#184 要繼續處理嗎", "", nil, nil, nil, "", 1)

	select {
	case <-called:
		t.Fatal("status question should not fetch GitHub issue through Hermes")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHandleMessageBareContinuationUsesNormalAgent(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	const userID int64 = 123
	const projectDir = "/tmp/alice-project"

	store := hermes.NewMemoryTaskStore()
	taskService := tasksvc.New(store)
	if _, err := taskService.CreateTask(hermes.TaskState{
		ID:          "continuable-task",
		ChatID:      key.chatID,
		ThreadID:    key.threadID,
		ProjectDir:  projectDir,
		Status:      hermes.TaskStatusInterrupted,
		Goal:        "修復登入流程",
		Accumulated: "已完成 cookie 修正",
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		allowIDs: map[int64]bool{userID: true},
		config: &Config{
			Hermes: HermesConfig{Enabled: true},
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true},
		},
		taskSvc:      taskService,
		messageQueue: make(chan *TelegramMessage, 20),
	}

	bot.handleMessage(key, userID, "繼續處理", "", nil, nil, nil, "", 1)

	for {
		select {
		case msg := <-bot.messageQueue:
			text, _ := msg.Params["text"].(string)
			if strings.Contains(text, "Hermes 任務") || strings.Contains(text, "偵測到你想") {
				t.Fatalf("bare continuation should stay on normal agent path, got queued text %q", text)
			}
		default:
			return
		}
	}
}

func TestHandleMessageHermesIssueRefTakesPrecedenceOverContinuation(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	const userID int64 = 123
	const projectDir = "/tmp/alice-project"

	storage := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = storage
	t.Cleanup(func() { globalStorage = oldStorage })

	store := buildHermesTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{
		ID:         "continuable-task",
		ChatID:     key.chatID,
		ProjectDir: projectDir,
		Status:     hermes.TaskStatusInterrupted,
		Goal:       "修復登入流程",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	fetchedIssue := make(chan struct{}, 1)
	oldFetchIssue := hermesFetchIssue
	hermesFetchIssue = func(ctx context.Context, gotProjectDir string, gotIssueNumber int) (*hermes.IssueContext, error) {
		if gotProjectDir != projectDir {
			t.Errorf("project dir: want %q, got %q", projectDir, gotProjectDir)
		}
		if gotIssueNumber != 137 {
			t.Errorf("issue number: want 137, got %d", gotIssueNumber)
		}
		fetchedIssue <- struct{}{}
		return nil, errors.New("stop after fetch")
	}
	defer func() { hermesFetchIssue = oldFetchIssue }()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		allowIDs: map[int64]bool{userID: true},
		config: &Config{
			Hermes: HermesConfig{Enabled: true},
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true},
		},
		messageQueue: make(chan *TelegramMessage, 20),
	}

	bot.handleMessage(key, userID, "繼續處理 #137", "", nil, nil, nil, "", 1)

	select {
	case <-fetchedIssue:
	case <-time.After(time.Second):
		t.Fatal("expected issue reference to fetch GitHub issue before generic continuation")
	}

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if strings.Contains(text, "偵測到你想接續") && strings.Contains(text, "Hermes 任務") {
			t.Fatalf("generic continuation should not run for issue-specific request, got %q", text)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHermesPlannerSessionCacheReusesSameTier(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true, tier: "codex"},
		},
	}

	bot.recordPlannerSession(key, "codex", "sess-123")
	if got := bot.plannerSessionForTier(key, "codex"); got != "sess-123" {
		t.Fatalf("expected cached planner session, got %q", got)
	}
}

func TestHermesTierForUsesGPTDeepPreferenceWhenNoCoordinator(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")

	if got := bot.hermesTierFor(key); got != "codex" {
		t.Fatalf("hermesTierFor = %q, want codex", got)
	}
	if got := bot.modelForUserPreference(key); got != "gpt-5.5" {
		t.Fatalf("modelForUserPreference = %q, want gpt-5.5", got)
	}
}

func TestBuildTaskSyncHookInvokesTaskSyncSlashCommand(t *testing.T) {
	called := make(chan struct{}, 1)
	oldRunTaskSyncCommand := runTaskSyncCommand
	runTaskSyncCommand = func(ctx context.Context, projectDir string) ([]byte, error) {
		if projectDir != "/repo" {
			t.Fatalf("projectDir = %q, want /repo", projectDir)
		}
		called <- struct{}{}
		return []byte("ok"), nil
	}
	t.Cleanup(func() { runTaskSyncCommand = oldRunTaskSyncCommand })

	bot := &TelegramBot{}
	hook := bot.buildTaskSyncHook(true, "/repo")
	if hook == nil {
		t.Fatal("expected task-sync hook when enabled")
	}

	hook(context.Background())

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("expected task-sync hook to call runTaskSyncCommand")
	}
}

func TestCheckHermesCleanWorktreeReturnsDirtyPorcelainLines(t *testing.T) {
	projectDir := t.TempDir()
	if _, err := runProcessOutput(context.Background(), ProcessOptions{Dir: projectDir}, "git", "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}

	changes, err := checkHermesCleanWorktree(context.Background(), projectDir)
	if err != nil {
		t.Fatalf("clean worktree check: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected clean worktree, got %v", changes)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "leftover.txt"), []byte("stale change"), 0o644); err != nil {
		t.Fatalf("write leftover: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "tracked.txt"), []byte("initial"), 0o644); err != nil {
		t.Fatalf("write tracked: %v", err)
	}
	if _, err := runProcessOutput(context.Background(), ProcessOptions{Dir: projectDir}, "git", "add", "tracked.txt"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := runProcessOutput(context.Background(), ProcessOptions{Dir: projectDir}, "git", "-c", "user.email=test@example.com", "-c", "user.name=Test User", "commit", "-m", "initial"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "tracked.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatalf("modify tracked: %v", err)
	}

	changes, err = checkHermesCleanWorktree(context.Background(), projectDir)
	if err != nil {
		t.Fatalf("dirty worktree check: %v", err)
	}
	if len(changes) != 2 || changes[0] != " M tracked.txt" || changes[1] != "?? leftover.txt" {
		t.Fatalf("unexpected dirty changes: %#v", changes)
	}
}

func TestFormatHermesDirtyWorktreeWarningTreatsChangesAsBaseline(t *testing.T) {
	msg := formatHermesDirtyWorktreeWarning(296, []string{" M apps/cm-web/example.tsx"})
	if !strings.Contains(msg, "Issue #296") ||
		!strings.Contains(msg, "啟動 baseline") ||
		!strings.Contains(msg, " M apps/cm-web/example.tsx") {
		t.Fatalf("unexpected warning message: %q", msg)
	}
}

func TestResolveHermesRoleModelsKeepsCodexPlannerAndExecutorSeparate(t *testing.T) {
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

func TestApplyExplicitUserModelPreferenceUsesGPTDeepForVoice(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")
	agent := NewAgent(nil, "/repo", key.chatID, key.threadID)
	agent.SetModelOverride("claude-haiku-4-5-20251001")

	if !bot.applyExplicitUserModelPreference(key, agent, "voice") {
		t.Fatalf("applyExplicitUserModelPreference returned false")
	}
	if agent.currentModelOverride != "gpt-5.5" {
		t.Fatalf("currentModelOverride = %q, want gpt-5.5", agent.currentModelOverride)
	}
	if agent.enablePlanMode {
		t.Fatalf("enablePlanMode = true, want false")
	}
}

func TestRunAgentForStopButtonUsesGPTDeepPreferenceForDocumentAnalysis(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	client := &modelRecordingClient{}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")
	agent := NewAgent(client, "/repo", key.chatID, key.threadID)
	agent.SetModelOverride("claude-haiku-4-5-20251001")

	got, err := bot.runAgentForStopButton(key, agent, "分析文件 temp/feedback.docx", nil)
	if err != nil {
		t.Fatalf("runAgentForStopButton: %v", err)
	}
	if got != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}
	if len(client.calls) != 1 || client.calls[0] != "gpt-5.5" {
		t.Fatalf("model calls = %#v, want [gpt-5.5]", client.calls)
	}
	if agent.currentModelOverride != "gpt-5.5" {
		t.Fatalf("currentModelOverride = %q, want gpt-5.5", agent.currentModelOverride)
	}
}

func TestRunAgentForStopButtonUsesMemoryResolverForDocumentAnalysis(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	store := hermes.NewMemoryTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{
		ID:                "issue-143",
		ChatID:            key.chatID,
		ThreadID:          key.threadID,
		ProjectDir:        "/repo",
		GithubIssueNumber: 143,
		Status:            hermes.TaskStatusDone,
		Goal:              "[GitHub #143] Unified Memory Architecture",
		Accumulated:       "MemoryResolver 已成為文件 runner 需要共用的 memory 入口。",
		CreatedAt:         time.Now().Add(-time.Hour),
		UpdatedAt:         time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := store.CreateTask(hermes.TaskState{
		ID:                "issue-99",
		ChatID:            key.chatID,
		ThreadID:          key.threadID,
		ProjectDir:        "/repo",
		GithubIssueNumber: 99,
		Status:            hermes.TaskStatusExecuting,
		Goal:              "Unrelated work",
		Accumulated:       "這段 #99 active task 不應污染 #143 文件分析。",
		CreatedAt:         time.Now().Add(-30 * time.Minute),
		UpdatedAt:         time.Now().Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	client := &modelRecordingClient{}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
		taskSvc: tasksvc.New(store),
	}
	agent := NewAgent(client, "/repo", key.chatID, key.threadID)
	agent.current().ctx.RecentMsgs = []contextMessage{
		{Role: "user", Content: "剛剛在處理 #99"},
		{Role: "assistant", Content: "這是 unrelated recent bridge"},
	}

	got, err := bot.runAgentForStopButtonMode(key, agent, "分析文件 temp/notes.md，延續 #143", "document", nil)
	if err != nil {
		t.Fatalf("runAgentForStopButtonMode: %v", err)
	}
	if got != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}
	if len(client.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(client.messages))
	}
	prompt := client.messages[0]
	if !strings.Contains(prompt, "MemoryResolver 已成為文件 runner") {
		t.Fatalf("prompt did not include issue memory:\n%s", prompt)
	}
	if strings.Contains(prompt, "unrelated recent bridge") {
		t.Fatalf("prompt leaked generic recent bridge for explicit issue:\n%s", prompt)
	}
	if strings.Contains(prompt, "不應污染 #143") {
		t.Fatalf("prompt leaked different issue task memory:\n%s", prompt)
	}
	if !strings.Contains(prompt, "分析文件 temp/notes.md，延續 #143") {
		t.Fatalf("prompt dropped current document request:\n%s", prompt)
	}
}

func TestApplyExplicitUserModelPreferenceUsesPlanMode(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				PlanModel:            "claude-opus-4-5-20251101",
				ExecuteModel:         "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("plan")
	agent := NewAgent(nil, "/repo", key.chatID, key.threadID)

	if !bot.applyExplicitUserModelPreference(key, agent, "voice") {
		t.Fatalf("applyExplicitUserModelPreference returned false")
	}
	if !agent.enablePlanMode {
		t.Fatalf("enablePlanMode = false, want true")
	}
	if agent.planModel != "claude-opus-4-5-20251101" || agent.executeModel != "gpt-5.5" {
		t.Fatalf("plan/execute = %q/%q, want claude-opus-4-5-20251101/gpt-5.5", agent.planModel, agent.executeModel)
	}
}

func TestHermesTierForCoordinatorOverridesModelPreference(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true, tier: "claude"},
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")

	if got := bot.hermesTierFor(key); got != "claude" {
		t.Fatalf("hermesTierFor = %q, want existing coordinator tier claude", got)
	}
}

func TestHermesTierForIgnoresDisabledCoordinatorWhenGPTDeepPreferred(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config: &Config{
			DefaultProjectDir: "/repo",
			ModelRouting: ModelRoutingConfig{
				EnableDynamicRouting: true,
				CodexDeepModel:       "gpt-5.5",
			},
		},
		chatContexts: map[chatKey]*ChatContext{
			key: NewChatContext(key.chatID, key.threadID, "/repo"),
		},
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: false, tier: "claude"},
		},
	}
	bot.chatContexts[key].Pref = ModelPreference("gpt-deep")

	if got := bot.hermesTierFor(key); got != "codex" {
		t.Fatalf("hermesTierFor = %q, want codex from current gpt-deep preference", got)
	}
}

func TestCodexModelOrFallbackRejectsClaudeModel(t *testing.T) {
	if got := codexModelOrFallback("claude-haiku-4-5-20251001", "gpt-5.4-mini"); got != "gpt-5.4-mini" {
		t.Fatalf("codexModelOrFallback returned %q, want fallback gpt-5.4-mini", got)
	}
	if got := codexModelOrFallback("gpt-5.5", "gpt-5.4-mini"); got != "gpt-5.5" {
		t.Fatalf("codexModelOrFallback returned %q, want gpt-5.5", got)
	}
}

func TestHermesTierChangeClearsPlannerSessionCache(t *testing.T) {
	key := chatKey{chatID: 99, threadID: 3}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {
				enabled:            true,
				tier:               "claude",
				plannerSessionID:   "sess-old",
				plannerSessionTier: "claude",
			},
		},
	}

	bot.setHermesTier(key, "codex")
	hc := bot.hermesCoords[key]
	if hc == nil {
		t.Fatal("expected hermes coord to exist")
	}
	if hc.plannerSessionID != "" || hc.plannerSessionTier != "" {
		t.Fatalf("expected planner session cache to be cleared, got %+v", hc)
	}

	bot.recordPlannerSession(key, "claude", "sess-should-not-stick")
	if got := bot.plannerSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no planner session for codex tier after switch, got %q", got)
	}
}

func TestHermesExecutorSessionCacheReusesSameTier(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {enabled: true, tier: "codex"},
		},
	}

	bot.recordExecutorSession(key, "codex", "exec-sess-123")
	if got := bot.executorSessionForTier(key, "codex"); got != "exec-sess-123" {
		t.Fatalf("expected cached executor session, got %q", got)
	}
}

func TestHermesTierChangeClearsExecutorSessionCache(t *testing.T) {
	key := chatKey{chatID: 99, threadID: 3}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {
				enabled:             true,
				tier:                "claude",
				executorSessionID:   "exec-old",
				executorSessionTier: "claude",
			},
		},
	}

	bot.setHermesTier(key, "codex")
	hc := bot.hermesCoords[key]
	if hc == nil {
		t.Fatal("expected hermes coord to exist")
	}
	if hc.executorSessionID != "" || hc.executorSessionTier != "" {
		t.Fatalf("expected executor session cache to be cleared, got %+v", hc)
	}

	// A stale session recorded for the old tier must not be accepted
	bot.recordExecutorSession(key, "claude", "exec-should-not-stick")
	if got := bot.executorSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no executor session for codex tier after switch, got %q", got)
	}
}

func TestHandleHermesStatsCommandWeekQueuesWeeklyReviewReport(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = s
	defer func() { globalStorage = oldStorage }()

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertUnifiedTask(UnifiedTask{
		ID:        "task-hermes-week",
		Goal:      "weekly stats",
		Engine:    "plan_execute",
		Backend:   "codex",
		Status:    "done",
		StartedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertUnifiedTask: %v", err)
	}
	if err := s.UpsertUnifiedSubTask(UnifiedSubTask{
		ID:          "task-hermes-week:s1",
		TaskID:      "task-hermes-week",
		Idx:         0,
		Description: "step",
		Status:      "done",
		StartedAt:   now.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUnifiedSubTask: %v", err)
	}
	reviewID, err := s.InsertUnifiedReviewResult(UnifiedReviewResult{
		TaskID:        "task-hermes-week",
		ReviewerModel: "gpt-5.5",
		Verdict:       "partial",
		OverallScore:  66,
		FeedbackText:  "more validation needed",
		IssueTags:     []string{"missing_validation"},
		CreatedAt:     now.Add(-20 * time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertUnifiedReviewResult: %v", err)
	}
	if err := s.InsertUnifiedReviewSubTaskResult(UnifiedReviewSubTaskResult{
		ReviewID:  reviewID,
		SubTaskID: "task-hermes-week:s1",
		Score:     60,
		Feedback:  "run tests",
		IssueTags: []string{"missing_validation"},
	}); err != nil {
		t.Fatalf("InsertUnifiedReviewSubTaskResult: %v", err)
	}

	bot := &TelegramBot{
		config:          &Config{Hermes: HermesConfig{Enabled: true}},
		i18n:            newTestI18nManager(t),
		messageQueue:    make(chan *TelegramMessage, 10),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")
	bot.handleHermesStatsCommand(key, []string{"/hermes-stats", "week"})

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "Hermes Review 週報") || !strings.Contains(text, "missing_validation") || !strings.Contains(text, "Planner 建議") {
			t.Fatalf("unexpected message text:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("expected weekly report message")
	}
}

func TestHandleHermesCommandUsesLocalizedMessages(t *testing.T) {
	key := chatKey{chatID: 42, threadID: 7}
	bot := &TelegramBot{
		config:          &Config{Hermes: HermesConfig{Enabled: true}},
		i18n:            newTestI18nManager(t),
		messageQueue:    make(chan *TelegramMessage, 2),
		langPreferences: map[int64]string{},
		hermesCoords:    map[chatKey]*hermesCoord{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleHermesCommand(key, []string{"/hermes", "restart"}, "")

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		want := "請提供要重新開始的任務說明，例如：`/hermes restart 修復登入流程`"
		if text != want {
			t.Fatalf("unexpected localized restart usage text:\nwant: %s\ngot:  %s", want, text)
		}
	case <-time.After(time.Second):
		t.Fatal("expected localized restart usage message")
	}
}

func TestHandleTasksRendersGitHubIssuesWithKeyboard(t *testing.T) {
	key := chatKey{chatID: 88, threadID: 3}
	oldList := tasksGitHubIssueListFunc
	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubIssueListFunc = func(ctx context.Context, projectDir, state string, limit int) ([]tasksGitHubIssue, error) {
		if projectDir != "/tmp/alice-project" {
			t.Fatalf("unexpected projectDir: %s", projectDir)
		}
		if state != "open" {
			t.Fatalf("unexpected state: %s", state)
		}
		if limit != 20 {
			t.Fatalf("unexpected limit: %d", limit)
		}
		return []tasksGitHubIssue{
			{
				Number:    12,
				Title:     "Fix parser regression",
				Labels:    []string{"bug", "p1"},
				Milestone: "Sprint 1",
			},
			{
				Number: 13,
				Title:  "Add topic filter",
				Labels: []string{"enhancement"},
			},
		}, nil
	}
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "https://github.com/acme/alice", nil
	}
	defer func() {
		tasksGitHubIssueListFunc = oldList
		tasksGitHubRepoURLFunc = oldRepo
	}()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID),
		},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		i18n:            newTestI18nManager(t),
		messageQueue:    make(chan *TelegramMessage, 4),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "Alice 待辦工作清單") {
			t.Fatalf("unexpected title:\n%s", text)
		}
		if !strings.Contains(text, "顯示開放 Issues") {
			t.Fatalf("unexpected state line:\n%s", text)
		}
		if !strings.Contains(text, "Milestone: Sprint 1") || !strings.Contains(text, "#12 Fix parser regression") {
			t.Fatalf("missing milestone group:\n%s", text)
		}
		if !strings.Contains(text, "標籤: bug, p1") || !strings.Contains(text, "未指定 Milestone") {
			t.Fatalf("missing issue details:\n%s", text)
		}

		markup, ok := msg.Params["reply_markup"].(map[string]interface{})
		if !ok {
			t.Fatalf("reply_markup missing or wrong type: %#v", msg.Params["reply_markup"])
		}
		rows, ok := markup["inline_keyboard"].([][]map[string]interface{})
		if !ok {
			t.Fatalf("inline_keyboard missing or wrong type: %#v", markup["inline_keyboard"])
		}
		if len(rows) != 2 {
			t.Fatalf("expected 2 keyboard rows, got %d", len(rows))
		}
		if rows[0][0]["text"] != "🔄 重新整理" || rows[0][1]["text"] != "📋 已關閉" {
			t.Fatalf("unexpected first row buttons: %#v", rows[0])
		}
		if rows[1][0]["url"] != "https://github.com/acme/alice/issues" {
			t.Fatalf("unexpected GitHub URL button: %#v", rows[1][0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for /tasks response")
	}
}

func TestHandleTasksRendersEmptyIssueListMessage(t *testing.T) {
	key := chatKey{chatID: 89, threadID: 3}
	oldList := tasksGitHubIssueListFunc
	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubIssueListFunc = func(ctx context.Context, projectDir, state string, limit int) ([]tasksGitHubIssue, error) {
		if state != "open" {
			t.Fatalf("unexpected state: %s", state)
		}
		return []tasksGitHubIssue{}, nil
	}
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "https://github.com/acme/alice", nil
	}
	defer func() {
		tasksGitHubIssueListFunc = oldList
		tasksGitHubRepoURLFunc = oldRepo
	}()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID),
		},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		i18n:            newTestI18nManager(t),
		messageQueue:    make(chan *TelegramMessage, 4),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "目前沒有符合條件的 GitHub Issues") {
			t.Fatalf("expected empty-issue message, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for empty issue response")
	}
}

func TestHandleTasksGeneralTopicShowsNoRepoMessage(t *testing.T) {
	key := chatKey{chatID: 90, threadID: 0}
	bot := &TelegramBot{
		allowIDs:        map[int64]bool{123: true},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue:    make(chan *TelegramMessage, 2),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleMessage(key, 123, "/tasks", "", nil, nil, nil, "", 1)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "具體 project topic") {
			t.Fatalf("expected general-topic guidance, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for general topic response")
	}
}

func TestHandleTasksUnboundTopicShowsNoRepoMessage(t *testing.T) {
	key := chatKey{chatID: 91, threadID: 4}
	s := newTestSQLiteStorage(t)
	oldStorage := globalStorage
	globalStorage = s
	defer func() { globalStorage = oldStorage }()

	bot := &TelegramBot{
		agents:          map[chatKey]*Agent{key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID)},
		config:          &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue:    make(chan *TelegramMessage, 2),
		langPreferences: map[int64]string{},
	}
	bot.setChatlanguage(key.chatID, "zh-TW")

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "具體 project topic") {
			t.Fatalf("expected unbound-topic guidance, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unbound topic response")
	}
}

func TestHandleTasksFallsBackToLegacyPhaseOverview(t *testing.T) {
	key := chatKey{chatID: 91, threadID: 4}
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "docs"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "preamble\n\n## Phase Overview\nlegacy content\n\n## Next\nmore"
	if err := os.WriteFile(filepath.Join(projectDir, "docs", "MASTER_TASKS.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "", errTasksNoGitHubRepo
	}
	defer func() { tasksGitHubRepoURLFunc = oldRepo }()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, projectDir, key.chatID, key.threadID),
		},
		config:       &Config{DefaultProjectDir: projectDir},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "Phase Overview") || !strings.Contains(text, "legacy content") {
			t.Fatalf("expected legacy fallback text, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for legacy fallback")
	}
}

func TestHandleTasksAuthFailurePromptsLogin(t *testing.T) {
	key := chatKey{chatID: 92, threadID: 5}
	oldList := tasksGitHubIssueListFunc
	oldRepo := tasksGitHubRepoURLFunc
	tasksGitHubRepoURLFunc = func(projectDir string) (string, error) {
		return "https://github.com/acme/alice", nil
	}
	tasksGitHubIssueListFunc = func(ctx context.Context, projectDir, state string, limit int) ([]tasksGitHubIssue, error) {
		return nil, errTasksGitHubAuthRequired
	}
	defer func() {
		tasksGitHubIssueListFunc = oldList
		tasksGitHubRepoURLFunc = oldRepo
	}()

	bot := &TelegramBot{
		agents: map[chatKey]*Agent{
			key: NewAgent(&mockClient{}, "/tmp/alice-project", key.chatID, key.threadID),
		},
		config:       &Config{DefaultProjectDir: "/tmp/alice-project"},
		messageQueue: make(chan *TelegramMessage, 2),
	}

	bot.handleTasks(key)

	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		if !strings.Contains(text, "gh auth login") {
			t.Fatalf("expected auth prompt, got:\n%s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for auth prompt")
	}
}

func TestHermesExecutorSessionReusesSameTierAndClearsOnSwitch(t *testing.T) {
	key := chatKey{chatID: 55, threadID: 2}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{},
	}

	bot.setHermesTier(key, "codex")
	bot.recordExecutorSession(key, "codex", "exec-codex-1")
	if got := bot.executorSessionForTier(key, "codex"); got != "exec-codex-1" {
		t.Fatalf("expected cached codex executor session, got %q", got)
	}

	// Same tier re-selection must keep cached session
	bot.setHermesTier(key, "codex")
	if got := bot.executorSessionForTier(key, "codex"); got != "exec-codex-1" {
		t.Fatalf("expected same-tier executor session to persist, got %q", got)
	}

	// Tier switch clears executor session cache
	bot.setHermesTier(key, "claude")
	if got := bot.executorSessionForTier(key, "claude"); got != "" {
		t.Fatalf("expected cleared executor session after tier switch, got %q", got)
	}

	// Stale codex session must not be accepted after switch
	bot.recordExecutorSession(key, "codex", "exec-codex-stale")
	if got := bot.executorSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no codex executor session after tier switch, got %q", got)
	}
}

func TestHermesPlannerSessionReusesSameTierAndClearsOnSwitch(t *testing.T) {
	key := chatKey{chatID: 123, threadID: 9}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{},
	}

	bot.setHermesTier(key, "codex")
	bot.recordPlannerSession(key, "codex", "sess-codex-1")
	if got := bot.plannerSessionForTier(key, "codex"); got != "sess-codex-1" {
		t.Fatalf("expected cached codex planner session, got %q", got)
	}

	// Re-selecting the same backend must keep the cached session intact.
	bot.setHermesTier(key, "codex")
	if got := bot.plannerSessionForTier(key, "codex"); got != "sess-codex-1" {
		t.Fatalf("expected same-tier planner session to persist, got %q", got)
	}

	// Switching back to Claude clears the Codex planner session cache.
	bot.setHermesTier(key, "claude")
	if got := bot.plannerSessionForTier(key, "claude"); got != "" {
		t.Fatalf("expected cleared planner session after tier switch, got %q", got)
	}

	// A stale Codex resume ID must not be accepted after the tier flip.
	bot.recordPlannerSession(key, "codex", "sess-codex-stale")
	if got := bot.plannerSessionForTier(key, "codex"); got != "" {
		t.Fatalf("expected no codex planner session after tier switch, got %q", got)
	}
}

type fakeInterruptibleCoord struct {
	running       bool
	interrupted   bool
	interruptFrom int64
}

func (f *fakeInterruptibleCoord) TaskID() string { return "task-1" }

func (f *fakeInterruptibleCoord) IsRunning() bool { return f.running }

func (f *fakeInterruptibleCoord) InterruptWith(messageID int64) {
	f.interrupted = true
	f.interruptFrom = messageID
}

func TestAbortActiveTaskInterruptsRunningHermesCoordinator(t *testing.T) {
	key := chatKey{chatID: 77, threadID: 3}
	coord := &fakeInterruptibleCoord{running: true}
	bot := &TelegramBot{
		hermesCoords: map[chatKey]*hermesCoord{
			key: {coord: coord, enabled: true},
		},
	}

	if got := bot.abortActiveTask(key, 12345); got != abortTaskAborted {
		t.Fatalf("abortActiveTask result = %v, want %v", got, abortTaskAborted)
	}
	if !coord.interrupted {
		t.Fatal("expected Hermes coordinator to be interrupted")
	}
	if coord.interruptFrom != 12345 {
		t.Fatalf("interrupt message id = %d, want 12345", coord.interruptFrom)
	}
	if got := bot.getChatContext(key, "").StateSnapshot().State; got != string(appengine.ChatStateInterrupting) {
		t.Fatalf("chat state = %q, want %q", got, appengine.ChatStateInterrupting)
	}
}

func TestAbortActiveTaskMarksAgentInterrupting(t *testing.T) {
	key := chatKey{chatID: 77, threadID: 3}
	agent := NewAgent(&mockClient{}, "/repo", key.chatID, key.threadID)
	agent.transitionExecution(appengine.ExecutionStateStarting, "test_busy")
	bot := &TelegramBot{
		config:       &Config{DefaultProjectDir: "/repo"},
		agents:       map[chatKey]*Agent{key: agent},
		chatContexts: map[chatKey]*ChatContext{key: agent.current().ctx},
		hermesCoords: map[chatKey]*hermesCoord{},
	}

	if got := bot.abortActiveTask(key, 0); got != abortTaskFinished {
		t.Fatalf("abortActiveTask result = %v, want %v", got, abortTaskFinished)
	}
	if got := agent.current().ctx.StateSnapshot().State; got != string(appengine.ChatStateInterrupting) {
		t.Fatalf("chat state = %q, want %q", got, appengine.ChatStateInterrupting)
	}
}
