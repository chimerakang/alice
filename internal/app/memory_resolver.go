package app

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

const defaultMemoryBudgetChars = 6000

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

type UnifiedMemoryResolver struct {
	tasks hermesMemoryTaskSource
}

func NewMemoryResolver(tasks hermesMemoryTaskSource) *UnifiedMemoryResolver {
	return &UnifiedMemoryResolver{tasks: tasks}
}

func (r *UnifiedMemoryResolver) Resolve(ctx context.Context, req MemoryRequest) (MemoryBundle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	budget := req.BudgetChars
	if budget <= 0 {
		budget = defaultMemoryBudgetChars
	}

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
	if bridge := strings.TrimSpace(buildContextBridge(req.RecentMessages)); bridge != "" {
		sections = append(sections, MemorySection{
			Source:   "recent_messages",
			Scope:    memoryScopeForRequest(req),
			Priority: 20,
			Text:     clampHermesContext(bridge, hermesContextMaxChars),
		})
	}

	bundle := MemoryBundle{Sections: sections}
	bundle.Sections = clampMemorySections(bundle.Sections, budget)
	log.Printf("[memory] resolved sections=%d mode=%s chat=%d thread=%d issue=%d budget=%d",
		len(bundle.Sections), req.Mode, req.ChatID, req.ThreadID, normalizedMemoryIssueNumber(req), budget)
	return bundle, nil
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
