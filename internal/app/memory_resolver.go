package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

const defaultMemoryBudgetChars = 6000
const staticHintBudgetChars = 1800

type MemoryResolver interface {
	Resolve(ctx context.Context, req MemoryRequest) (MemoryBundle, error)
}

type MemoryRequest struct {
	ChatID         int64
	ThreadID       int
	ProjectDir     string
	UserMessage    string
	IssueNumber    int
	Mode           string
	BudgetChars    int
	RecentMessages []contextMessage
}

type MemoryBundle struct {
	Sections []MemorySection
}

type MemorySection struct {
	Source   string
	Scope    string
	Priority int
	Text     string
}

func (b MemoryBundle) Render() string {
	if len(b.Sections) == 0 {
		return ""
	}
	sections := make([]MemorySection, 0, len(b.Sections))
	for _, section := range b.Sections {
		if strings.TrimSpace(section.Text) == "" {
			continue
		}
		sections = append(sections, section)
	}
	sort.SliceStable(sections, func(i, j int) bool {
		return sections[i].Priority > sections[j].Priority
	})

	var parts []string
	for _, section := range sections {
		parts = append(parts, strings.TrimSpace(section.Text))
	}
	return strings.Join(parts, "\n\n")
}

func (b MemoryBundle) RenderForPrompt(currentRequest string) string {
	currentRequest = strings.TrimSpace(currentRequest)
	if currentRequest == "" {
		return ""
	}
	rendered := strings.TrimSpace(b.Render())
	if rendered == "" {
		return currentRequest
	}

	var sb strings.Builder
	sb.WriteString(hermesPreviousContextHeader)
	sb.WriteString("\n")
	sb.WriteString(rendered)
	sb.WriteString("\n\n")
	sb.WriteString(hermesCurrentRequestHeader)
	sb.WriteString("\n")
	sb.WriteString(currentRequest)
	return sb.String()
}

type hermesMemoryTaskSource interface {
	GetActiveForChat(chatID int64) (hermes.TaskState, error)
	ListForChat(chatID int64, limit int) ([]hermes.TaskState, error)
}

type generalMemorySource interface {
	ListGeneralMemoryCards(ctx context.Context, req MemoryRequest, limit int) ([]GeneralMemoryCard, error)
}

type staticHintSource interface {
	ListStaticMemoryHints(ctx context.Context, req MemoryRequest, limit int) ([]StaticMemoryHint, error)
}

type GeneralMemoryCard struct {
	ID                string
	ChatID            int64
	ThreadID          int
	ProjectDir        string
	GithubIssueNumber int
	Goal              string
	Result            string
	Engine            string
	Backend           string
	Model             string
	Files             []string
	ContinuationHints []string
	UpdatedAt         time.Time
}

type StaticMemoryHint struct {
	Path string
	Text string
}

type UnifiedMemoryResolver struct {
	tasks   hermesMemoryTaskSource
	general generalMemorySource
	static  staticHintSource
}

func NewMemoryResolver(tasks hermesMemoryTaskSource) *UnifiedMemoryResolver {
	return &UnifiedMemoryResolver{tasks: tasks}
}

func NewMemoryResolverWithSources(tasks hermesMemoryTaskSource, general generalMemorySource) *UnifiedMemoryResolver {
	return &UnifiedMemoryResolver{tasks: tasks, general: general}
}

func NewMemoryResolverWithAllSources(tasks hermesMemoryTaskSource, general generalMemorySource, static staticHintSource) *UnifiedMemoryResolver {
	return &UnifiedMemoryResolver{tasks: tasks, general: general, static: static}
}

func defaultStaticHintSourceForProject(projectDir string) staticHintSource {
	if strings.TrimSpace(projectDir) == "" {
		return nil
	}
	return ProjectStaticHintSource{}
}

func globalGeneralMemorySource() generalMemorySource {
	if globalStorage == nil {
		return nil
	}
	source, ok := globalStorage.(generalMemorySource)
	if !ok {
		return nil
	}
	return source
}

func (r *UnifiedMemoryResolver) Resolve(ctx context.Context, req MemoryRequest) (MemoryBundle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	budget := req.BudgetChars
	if budget <= 0 {
		budget = defaultMemoryBudgetChars
	}
	issueNumber := normalizedMemoryIssueNumber(req)

	var sections []MemorySection
	if r != nil && r.tasks != nil {
		taskSection, err := r.resolveHermesTaskSection(ctx, req)
		if err != nil {
			return MemoryBundle{}, err
		}
		if strings.TrimSpace(taskSection.Text) != "" {
			sections = append(sections, taskSection)
		}
	}
	if r != nil && r.general != nil {
		generalSection, err := r.resolveGeneralMemorySection(ctx, req)
		if err != nil {
			return MemoryBundle{}, err
		}
		if strings.TrimSpace(generalSection.Text) != "" {
			sections = append(sections, generalSection)
		}
	}
	if r != nil && r.static != nil {
		staticSection, err := r.resolveStaticHintSection(ctx, req)
		if err != nil {
			return MemoryBundle{}, err
		}
		if strings.TrimSpace(staticSection.Text) != "" {
			sections = append(sections, staticSection)
		}
	}
	if issueNumber == 0 {
		bridge := strings.TrimSpace(buildContextBridge(req.RecentMessages))
		if bridge != "" {
			sections = append(sections, MemorySection{
				Source:   "recent_messages",
				Scope:    memoryScopeForRequest(req),
				Priority: 20,
				Text:     clampHermesContext(bridge, hermesContextMaxChars),
			})
		}
	} else if len(req.RecentMessages) > 0 {
		log.Printf("[memory] skipped recent_messages for explicit issue scope issue=%d chat=%d thread=%d",
			issueNumber, req.ChatID, req.ThreadID)
	}

	bundle := MemoryBundle{Sections: sections}
	bundle.Sections = clampMemorySections(bundle.Sections, budget)
	log.Printf("[memory] resolved sections=%d mode=%s chat=%d thread=%d issue=%d budget=%d sources=%s",
		len(bundle.Sections), req.Mode, req.ChatID, req.ThreadID, issueNumber, budget, memorySectionSummary(bundle.Sections))
	return bundle, nil
}

func memorySectionSummary(sections []MemorySection) string {
	if len(sections) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		parts = append(parts, fmt.Sprintf("%s/%s/%d", section.Source, section.Scope, len([]rune(section.Text))))
	}
	return strings.Join(parts, ",")
}

// recursiveCardMarkers identifies cards whose stored Goal or Result contains
// content from prior Hermes Executor invocations. Even after the engine-label
// filter (see general_memory_store.go), legacy data may still hold these; if
// re-injected as "Persisted general work memory" the next prompt nests Hermes
// rules inside Hermes rules, growing geometrically. Drop the card defensively.
var recursiveCardMarkers = []string{
	"# Hermes Executor Rules",
	"Hermes Executor Rules (Codex)",
	"[Hermes continuation]",
	"=== Hermes Executor 上下文 ===",
}

func cardHasRecursiveHermesContent(card GeneralMemoryCard) bool {
	for _, marker := range recursiveCardMarkers {
		if strings.Contains(card.Goal, marker) || strings.Contains(card.Result, marker) {
			return true
		}
	}
	return false
}

func buildGeneralMemoryContextSection(cards []GeneralMemoryCard) string {
	if len(cards) == 0 {
		return ""
	}
	sections := make([]string, 0, len(cards))
	for _, card := range cards {
		if cardHasRecursiveHermesContent(card) {
			continue
		}
		goal := strings.TrimSpace(stripLanguageDirective(card.Goal))
		result := strings.TrimSpace(card.Result)
		if goal == "" && result == "" {
			continue
		}
		var sb strings.Builder
		sb.WriteString("Persisted general work memory")
		if !card.UpdatedAt.IsZero() {
			sb.WriteString(" (")
			sb.WriteString(card.UpdatedAt.Format(time.RFC3339))
			sb.WriteString(")")
		}
		sb.WriteString(":\n")
		if card.ID != "" {
			sb.WriteString("- Task ID: ")
			sb.WriteString(card.ID)
			sb.WriteString("\n")
		}
		if card.Engine != "" {
			sb.WriteString("- Engine: ")
			sb.WriteString(card.Engine)
			sb.WriteString("\n")
		}
		if card.Model != "" {
			sb.WriteString("- Model: ")
			sb.WriteString(card.Model)
			sb.WriteString("\n")
		}
		if card.GithubIssueNumber > 0 {
			sb.WriteString(fmt.Sprintf("- GitHub issue: #%d\n", card.GithubIssueNumber))
		}
		if len(card.Files) > 0 {
			sb.WriteString("- Touched files:\n")
			for _, path := range card.Files {
				sb.WriteString("  - ")
				sb.WriteString(clampHermesContext(path, 240))
				sb.WriteString("\n")
			}
		}
		if len(card.ContinuationHints) > 0 {
			sb.WriteString("- Continuation hints:\n")
			for _, hint := range card.ContinuationHints {
				sb.WriteString("  - ")
				sb.WriteString(clampHermesContext(hint, 320))
				sb.WriteString("\n")
			}
		}
		if goal != "" {
			sb.WriteString("- Request: ")
			sb.WriteString(clampHermesContext(goal, 800))
			sb.WriteString("\n")
		}
		if result != "" {
			sb.WriteString("- Result:\n")
			sb.WriteString(indentHermesContext(clampHermesContext(result, 1200), "  "))
		}
		sections = append(sections, strings.TrimSpace(sb.String()))
	}
	if len(sections) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Persisted general task context:\n")
	sb.WriteString("Use this as continuity for related direct/file/media work. Treat it as a summary, and verify concrete details before editing or making high-impact decisions.\n\n")
	sb.WriteString(strings.Join(sections, "\n\n"))
	return sb.String()
}

func buildStaticHintContextSection(hints []StaticMemoryHint) string {
	if len(hints) == 0 {
		return ""
	}
	sections := make([]string, 0, len(hints))
	for _, hint := range hints {
		path := strings.TrimSpace(hint.Path)
		text := strings.TrimSpace(hint.Text)
		if path == "" || text == "" {
			continue
		}
		var sb strings.Builder
		sb.WriteString("Static project hint from ")
		sb.WriteString(path)
		sb.WriteString(":\n")
		sb.WriteString(clampHermesContext(text, staticHintBudgetChars))
		sections = append(sections, strings.TrimSpace(sb.String()))
	}
	if len(sections) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Static project context:\n")
	sb.WriteString("Use these attributed project hints as low-priority orientation. Prefer live code and task memory for concrete implementation details.\n\n")
	sb.WriteString(strings.Join(sections, "\n\n"))
	return sb.String()
}

type ProjectStaticHintSource struct{}

func (ProjectStaticHintSource) ListStaticMemoryHints(ctx context.Context, req MemoryRequest, limit int) ([]StaticMemoryHint, error) {
	projectDir := strings.TrimSpace(req.ProjectDir)
	if projectDir == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 3
	}
	paths := []string{
		"CLAUDE.md",
		filepath.Join("docs", "arch", "memory.md"),
	}
	capacity := limit
	if len(paths) < capacity {
		capacity = len(paths)
	}
	hints := make([]StaticMemoryHint, 0, capacity)
	for _, relPath := range paths {
		if len(hints) >= limit {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		fullPath := filepath.Join(projectDir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() || info.Size() > 256*1024 {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		hints = append(hints, StaticMemoryHint{
			Path: filepath.ToSlash(relPath),
			Text: text,
		})
	}
	return hints, nil
}

func (r *UnifiedMemoryResolver) resolveHermesTaskSection(ctx context.Context, req MemoryRequest) (MemorySection, error) {
	select {
	case <-ctx.Done():
		return MemorySection{}, ctx.Err()
	default:
	}

	tasks, err := r.loadHermesMemoryTasks(req)
	if err != nil {
		return MemorySection{}, err
	}
	if len(tasks) == 0 {
		return MemorySection{}, nil
	}

	issueNumber := normalizedMemoryIssueNumber(req)
	scope := "chat"
	source := "hermes_task"
	priority := 80
	if issueNumber > 0 && tasks[0].GithubIssueNumber == issueNumber {
		scope = fmt.Sprintf("issue:%d", issueNumber)
		source = "issue_task"
		priority = 100
	}
	return MemorySection{
		Source:   source,
		Scope:    scope,
		Priority: priority,
		Text:     buildHermesTaskContextSection(tasks),
	}, nil
}

func (r *UnifiedMemoryResolver) resolveGeneralMemorySection(ctx context.Context, req MemoryRequest) (MemorySection, error) {
	select {
	case <-ctx.Done():
		return MemorySection{}, ctx.Err()
	default:
	}
	cards, err := r.general.ListGeneralMemoryCards(ctx, req, 3)
	if err != nil {
		return MemorySection{}, fmt.Errorf("list general memory: %w", err)
	}
	if len(cards) == 0 {
		return MemorySection{}, nil
	}

	issueNumber := normalizedMemoryIssueNumber(req)
	scope := memoryScopeForRequest(req)
	priority := 60
	if issueNumber > 0 {
		scope = fmt.Sprintf("issue:%d", issueNumber)
		priority = 90
	}
	return MemorySection{
		Source:   "general_task",
		Scope:    scope,
		Priority: priority,
		Text:     buildGeneralMemoryContextSection(cards),
	}, nil
}

func (r *UnifiedMemoryResolver) resolveStaticHintSection(ctx context.Context, req MemoryRequest) (MemorySection, error) {
	select {
	case <-ctx.Done():
		return MemorySection{}, ctx.Err()
	default:
	}
	hints, err := r.static.ListStaticMemoryHints(ctx, req, 3)
	if err != nil {
		return MemorySection{}, fmt.Errorf("list static hints: %w", err)
	}
	text := buildStaticHintContextSection(hints)
	if strings.TrimSpace(text) == "" {
		return MemorySection{}, nil
	}
	return MemorySection{
		Source:   "static_hint",
		Scope:    projectMemoryScope(req.ProjectDir),
		Priority: 35,
		Text:     text,
	}, nil
}

func (r *UnifiedMemoryResolver) loadHermesMemoryTasks(req MemoryRequest) ([]hermes.TaskState, error) {
	var tasks []hermes.TaskState
	issueNumber := normalizedMemoryIssueNumber(req)
	hasIssueNumber := issueNumber > 0

	active, err := r.tasks.GetActiveForChat(req.ChatID)
	switch {
	case err == nil:
		if !hasIssueNumber || active.GithubIssueNumber == issueNumber {
			tasks = append(tasks, active)
		}
	case err != nil && err != hermes.ErrNoTask:
		return nil, fmt.Errorf("load active hermes memory: %w", err)
	}

	historyLimit := 3
	if hasIssueNumber {
		historyLimit = 10
	}
	history, err := r.tasks.ListForChat(req.ChatID, historyLimit)
	if err != nil {
		return nil, fmt.Errorf("list hermes memory: %w", err)
	}

	currentNorm := normalizeHermesGoal(req.UserMessage)
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		seen[task.ID] = struct{}{}
	}
	if hasIssueNumber {
		for _, task := range history {
			if task.GithubIssueNumber != issueNumber {
				continue
			}
			if _, ok := seen[task.ID]; ok {
				continue
			}
			tasks = append(tasks, task)
			seen[task.ID] = struct{}{}
			if len(tasks) >= 3 {
				return tasks, nil
			}
		}
		return tasks, nil
	}
	for _, task := range history {
		if _, ok := seen[task.ID]; ok {
			continue
		}
		if normalizeHermesGoal(extractHermesActionableGoal(task.Goal)) == currentNorm {
			continue
		}
		tasks = append(tasks, task)
		if len(tasks) >= 2 {
			break
		}
	}
	return tasks, nil
}

func normalizedMemoryIssueNumber(req MemoryRequest) int {
	if req.IssueNumber > 0 {
		return req.IssueNumber
	}
	if issueNumber, ok := ParseIssueNumber(req.UserMessage); ok {
		return issueNumber
	}
	return 0
}

func memoryScopeForRequest(req MemoryRequest) string {
	if issueNumber := normalizedMemoryIssueNumber(req); issueNumber > 0 {
		return fmt.Sprintf("issue:%d", issueNumber)
	}
	if req.ThreadID != 0 {
		return fmt.Sprintf("chat:%d/thread:%d", req.ChatID, req.ThreadID)
	}
	return fmt.Sprintf("chat:%d", req.ChatID)
}

func projectMemoryScope(projectDir string) string {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return "project"
	}
	return "project:" + projectDir
}

func clampMemorySections(sections []MemorySection, budget int) []MemorySection {
	if budget <= 0 || len(sections) == 0 {
		return sections
	}
	sort.SliceStable(sections, func(i, j int) bool {
		return sections[i].Priority > sections[j].Priority
	})

	out := make([]MemorySection, 0, len(sections))
	remaining := budget
	for _, section := range sections {
		text := strings.TrimSpace(section.Text)
		if text == "" || remaining <= 0 {
			continue
		}
		if len([]rune(text)) > remaining {
			text = clampMemoryText(text, remaining)
		}
		section.Text = text
		out = append(out, section)
		remaining -= len([]rune(text))
	}
	return out
}

func clampMemoryText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return strings.TrimSpace(string(runes[:maxRunes-3])) + "..."
}
