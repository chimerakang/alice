package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

type TargetKind string

const (
	TargetHermesTask   TargetKind = "hermes_task"
	TargetReviewResult TargetKind = "review_result"
	TargetGitHubIssue  TargetKind = "github_issue"
)

type TargetCandidate struct {
	Kind       TargetKind
	ID         string
	Title      string
	ProjectDir string
	Score      float64
	Reason     string
	Metadata   map[string]string
}

type ResolveTargetRequest struct {
	ChatID     int64
	ThreadID   int
	ProjectDir string
	Intent     string
	Query      string
	Kinds      []TargetKind
	Limit      int
}

type targetResolverSources struct {
	Tasks           []hermes.TaskState
	RetryCandidates []retryTaskCandidate
	Issues          []tasksGitHubIssue
	Now             time.Time
}

func resolveTargetCandidates(req ResolveTargetRequest, src targetResolverSources) []TargetCandidate {
	if req.Limit <= 0 {
		req.Limit = 3
	}
	if src.Now.IsZero() {
		src.Now = time.Now()
	}
	allowed := targetKindSet(req.Kinds)
	query := normalizeTargetQuery(req.Query)
	var out []TargetCandidate
	if allowed[TargetHermesTask] {
		out = append(out, resolveHermesTaskCandidates(req, src.Tasks, query, src.Now)...)
	}
	if allowed[TargetReviewResult] {
		out = append(out, resolveReviewCandidates(req, src.RetryCandidates, query, src.Now)...)
	}
	if allowed[TargetGitHubIssue] {
		out = append(out, resolveIssueCandidates(req, src.Issues, query)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out
}

func targetKindSet(kinds []TargetKind) map[TargetKind]bool {
	if len(kinds) == 0 {
		return map[TargetKind]bool{
			TargetHermesTask:   true,
			TargetReviewResult: true,
			TargetGitHubIssue:  true,
		}
	}
	set := make(map[TargetKind]bool, len(kinds))
	for _, kind := range kinds {
		set[kind] = true
	}
	return set
}

func resolveHermesTaskCandidates(req ResolveTargetRequest, tasks []hermes.TaskState, query string, now time.Time) []TargetCandidate {
	var out []TargetCandidate
	for _, task := range tasks {
		if task.ChatID != 0 && task.ChatID != req.ChatID {
			continue
		}
		if !hermesTaskMatchesSelectableScope(task, req.ThreadID, req.ProjectDir) {
			continue
		}
		rank := hermesContinuationRank(task)
		if rank < 0 {
			continue
		}
		score := 0.35
		reasons := []string{"可接續 Hermes 任務"}
		switch rank {
		case 0:
			score += 0.3
			reasons = append(reasons, "active")
		case 1:
			score += 0.25
			reasons = append(reasons, "interrupted")
		case 2:
			score += 0.18
			reasons = append(reasons, "failed with progress")
		}
		if task.ThreadID == req.ThreadID {
			score += 0.12
			reasons = append(reasons, "same topic")
		}
		if hermesTaskMatchesProject(task, req.ProjectDir) {
			score += 0.1
			reasons = append(reasons, "same project")
		}
		if !task.UpdatedAt.IsZero() {
			score += recencyScore(now.Sub(task.UpdatedAt), 0.12)
		}
		if query != "" {
			id := strings.ToLower(strings.TrimSpace(task.ID))
			if strings.HasPrefix(id, query) {
				score = 0.99
				reasons = append(reasons, "task id prefix")
			} else {
				sim := hermesGoalSimilarity(query, extractHermesActionableGoal(task.Goal))
				if sim < 0.2 {
					continue
				}
				score += sim * 0.28
				reasons = append(reasons, "goal match")
			}
		}
		out = append(out, TargetCandidate{
			Kind:       TargetHermesTask,
			ID:         task.ID,
			Title:      targetTitleFromTask(task),
			ProjectDir: task.ProjectDir,
			Score:      clampScore(score),
			Reason:     strings.Join(reasons, ", "),
			Metadata: map[string]string{
				"status":       string(task.Status),
				"thread_id":    fmt.Sprintf("%d", task.ThreadID),
				"github_issue": fmt.Sprintf("%d", task.GithubIssueNumber),
			},
		})
	}
	return out
}

func resolveReviewCandidates(req ResolveTargetRequest, candidates []retryTaskCandidate, query string, now time.Time) []TargetCandidate {
	out := make([]TargetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ID) == "" {
			continue
		}
		score := 0.55
		reasons := []string{"has failed review"}
		if candidate.FailedCount == 1 {
			score += 0.12
			reasons = append(reasons, "唯一低分 sub-task")
		} else if candidate.FailedCount > 1 {
			score += 0.08
			reasons = append(reasons, fmt.Sprintf("%d failed subtasks", candidate.FailedCount))
		}
		if !candidate.LatestReviewAt.IsZero() {
			score += recencyScore(now.Sub(candidate.LatestReviewAt), 0.18)
		}
		if query != "" {
			id := strings.ToLower(strings.TrimSpace(candidate.ID))
			issueRef := fmt.Sprintf("#%d", candidate.GithubIssueNumber)
			issueNumber := fmt.Sprintf("%d", candidate.GithubIssueNumber)
			switch {
			case strings.HasPrefix(id, query):
				score = 0.99
				reasons = append(reasons, "task id prefix")
			case candidate.GithubIssueNumber > 0 && (query == issueRef || query == issueNumber):
				score = 0.96
				reasons = append(reasons, "issue match")
			default:
				sim := hermesGoalSimilarity(query, candidate.Goal)
				if sim < 0.2 {
					continue
				}
				score += sim * 0.24
				reasons = append(reasons, "goal match")
			}
		}
		out = append(out, TargetCandidate{
			Kind:   TargetReviewResult,
			ID:     candidate.ID,
			Title:  strings.TrimSpace(candidate.Goal),
			Score:  clampScore(score),
			Reason: strings.Join(reasons, ", "),
			Metadata: map[string]string{
				"failed_count": fmt.Sprintf("%d", candidate.FailedCount),
				"github_issue": fmt.Sprintf("%d", candidate.GithubIssueNumber),
			},
		})
	}
	return out
}

func resolveIssueCandidates(req ResolveTargetRequest, issues []tasksGitHubIssue, query string) []TargetCandidate {
	query = strings.TrimPrefix(query, "#")
	var out []TargetCandidate
	for _, issue := range issues {
		if issue.Number <= 0 {
			continue
		}
		score := 0.25
		reasons := []string{"GitHub issue"}
		if query != "" {
			if fmt.Sprintf("%d", issue.Number) == query {
				score = 0.99
				reasons = append(reasons, "issue number")
			} else {
				text := issue.Title + " " + strings.Join(issue.Labels, " ") + " " + issue.Milestone
				sim := hermesGoalSimilarity(query, text)
				if sim < 0.12 && !strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
					continue
				}
				score += sim * 0.55
				if strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
					score += 0.25
				}
				reasons = append(reasons, "text match")
			}
		}
		out = append(out, TargetCandidate{
			Kind:   TargetGitHubIssue,
			ID:     fmt.Sprintf("%d", issue.Number),
			Title:  strings.TrimSpace(issue.Title),
			Score:  clampScore(score),
			Reason: strings.Join(reasons, ", "),
			Metadata: map[string]string{
				"labels": strings.Join(issue.Labels, ","),
				"url":    issue.URL,
			},
		})
	}
	return out
}

func normalizeTargetQuery(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	query = strings.TrimPrefix(query, "/hermes")
	query = strings.TrimPrefix(query, "/ghermes")
	query = strings.TrimPrefix(query, "/retry")
	query = strings.TrimSpace(query)
	query = strings.TrimPrefix(query, "#")
	for _, phrase := range []string{"處理", "那個", "issue", "任務", "繼續", "continue", "resume", "all-failed"} {
		query = strings.ReplaceAll(query, phrase, " ")
	}
	return strings.Join(strings.Fields(query), " ")
}

func targetTitleFromTask(task hermes.TaskState) string {
	title := strings.TrimSpace(extractHermesActionableGoal(task.Goal))
	if title == "" {
		title = strings.TrimSpace(task.Goal)
	}
	return title
}

func recencyScore(age time.Duration, max float64) float64 {
	if age < 0 {
		return max
	}
	switch {
	case age <= time.Hour:
		return max
	case age <= 6*time.Hour:
		return max * 0.8
	case age <= 24*time.Hour:
		return max * 0.55
	case age <= 72*time.Hour:
		return max * 0.25
	default:
		return 0
	}
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func (t *TelegramBot) resolveTargets(ctx context.Context, req ResolveTargetRequest) ([]TargetCandidate, error) {
	src := targetResolverSources{Now: time.Now()}
	allowed := targetKindSet(req.Kinds)
	if allowed[TargetHermesTask] && t.taskSvc != nil {
		tasks, err := t.taskSvc.ListForChat(req.ChatID, 20)
		if err != nil {
			return nil, err
		}
		src.Tasks = tasks
	}
	if allowed[TargetReviewResult] {
		store, ok := globalStorage.(*SQLiteStorage)
		if globalStorage != nil && ok {
			candidates, err := store.selectRetryTaskCandidates(ctx, chatKey{chatID: req.ChatID, threadID: req.ThreadID}, 10)
			if err != nil {
				return nil, err
			}
			src.RetryCandidates = candidates
		}
	}
	if allowed[TargetGitHubIssue] {
		openIssues, err := tasksGitHubIssueListFunc(ctx, req.ProjectDir, "open", 30)
		if err != nil {
			return nil, err
		}
		closedIssues, _ := tasksGitHubIssueListFunc(ctx, req.ProjectDir, "closed", 20)
		src.Issues = append(openIssues, closedIssues...)
	}
	return resolveTargetCandidates(req, src), nil
}
