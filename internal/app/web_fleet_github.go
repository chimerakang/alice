package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// Testable vars for GitHub operations during Fleet dispatch, following the same
// pattern as tasksGitHubIssueListFunc.
var (
	fleetGitHubIssueCreateFunc      = createFleetGitHubIssue
	fleetGitHubMilestoneGetOrCreate = getOrCreateFleetGitHubMilestone
	fleetGitHubMilestoneListFunc    = listFleetGitHubMilestones
	fleetGitHubEpicUpdateFunc       = appendToFleetGitHubEpic
	// fleetDispatchFromIssueFunc wraps the Commander.Dispatch call for the
	// "from issue" path so tests can substitute it without needing real gh.
	fleetDispatchFromIssueFunc = defaultFleetDispatchFromIssue
)

type fleetCreatedIssue struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func createFleetGitHubIssue(ctx context.Context, repoPath, title, body string, milestoneNumber int) (fleetCreatedIssue, error) {
	args := []string{
		"api", "repos/:owner/:repo/issues", "-X", "POST",
		"-f", "title=" + title,
		"-f", "body=" + body,
	}
	if milestoneNumber > 0 {
		args = append(args, "-F", fmt.Sprintf("milestone=%d", milestoneNumber))
	}
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh", args...)
	if err != nil {
		return fleetCreatedIssue{}, ghWrapError(out, err, "create issue")
	}
	var result fleetCreatedIssue
	if err := json.Unmarshal(out, &result); err != nil {
		return fleetCreatedIssue{}, fmt.Errorf("parse issue creation response: %w", err)
	}
	return result, nil
}

type fleetMilestoneJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

func listFleetGitHubMilestones(ctx context.Context, repoPath string) ([]fleetMilestoneJSON, error) {
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", "repos/:owner/:repo/milestones?state=open&per_page=100",
	)
	if err != nil {
		return nil, ghWrapError(out, err, "list milestones")
	}
	var list []fleetMilestoneJSON
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse milestones: %w", err)
	}
	return list, nil
}

func getOrCreateFleetGitHubMilestone(ctx context.Context, repoPath, title string) (int, error) {
	milestones, err := listFleetGitHubMilestones(ctx, repoPath)
	if err != nil {
		return 0, err
	}
	want := strings.ToLower(strings.TrimSpace(title))
	for _, m := range milestones {
		if strings.ToLower(strings.TrimSpace(m.Title)) == want {
			return m.Number, nil
		}
	}
	// Milestone not found; create it.
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", "repos/:owner/:repo/milestones", "-X", "POST",
		"-f", "title="+strings.TrimSpace(title),
	)
	if err != nil {
		return 0, ghWrapError(out, err, "create milestone")
	}
	var created fleetMilestoneJSON
	if err := json.Unmarshal(out, &created); err != nil {
		return 0, fmt.Errorf("parse created milestone: %w", err)
	}
	return created.Number, nil
}

type fleetIssueBodyJSON struct {
	Body string `json:"body"`
}

func appendToFleetGitHubEpic(ctx context.Context, repoPath string, epicNumber, issueNumber int, issueTitle string) error {
	out, err := runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", fmt.Sprintf("repos/:owner/:repo/issues/%d", epicNumber),
	)
	if err != nil {
		return ghWrapError(out, err, "fetch epic body")
	}
	var epic fleetIssueBodyJSON
	if err := json.Unmarshal(out, &epic); err != nil {
		return fmt.Errorf("parse epic body: %w", err)
	}
	newItem := fmt.Sprintf("- [ ] #%d %s", issueNumber, issueTitle)
	body := strings.TrimRight(epic.Body, "\n")
	if body == "" {
		body = newItem
	} else {
		body = body + "\n" + newItem
	}
	_, err = runProcessCombinedOutput(ctx, ProcessOptions{Dir: repoPath, Timeout: 30 * time.Second}, "gh",
		"api", fmt.Sprintf("repos/:owner/:repo/issues/%d", epicNumber),
		"-X", "PATCH",
		"-f", "body="+body,
	)
	if err != nil {
		return ghWrapError(out, err, "update epic body")
	}
	return nil
}

// ghWrapError maps gh CLI errors onto the sentinel errors used by the fleet API.
func ghWrapError(out []byte, err error, op string) error {
	lower := strings.ToLower(string(out) + " " + err.Error())
	switch {
	case strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "not logged in"),
		strings.Contains(lower, "gh auth login"),
		strings.Contains(lower, "must authenticate"),
		strings.Contains(lower, "401 unauthorized"),
		strings.Contains(lower, "authentication failed"):
		return errTasksGitHubAuthRequired
	case strings.Contains(lower, "could not resolve to a repository"),
		strings.Contains(lower, "not a git repository"),
		strings.Contains(lower, "no remotes configured"),
		strings.Contains(lower, "no remotes found"),
		strings.Contains(lower, "no github remote found"):
		return errTasksNoGitHubRepo
	default:
		return fmt.Errorf("%s: %w (%s)", op, err, strings.TrimSpace(string(out)))
	}
}

// isEpicIssue reports whether the issue is an epic based on its labels and
// title, using the labels and pattern from the fleet config.
func isEpicIssue(labels []string, title string, epicLabels []string, epicPattern *regexp.Regexp) bool {
	for _, label := range labels {
		for _, epicLabel := range epicLabels {
			if strings.EqualFold(label, epicLabel) {
				return true
			}
		}
	}
	if epicPattern != nil && epicPattern.MatchString(title) {
		return true
	}
	return false
}

// compileEpicPattern compiles the fleet config's epic title regexp, or returns
// nil when the pattern is empty. Logs a warning and returns nil on bad syntax.
func compileEpicPattern(pattern string) *regexp.Regexp {
	if strings.TrimSpace(pattern) == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

// resolvedEpicLabels returns the epic labels from config, defaulting to ["epic"].
func resolvedEpicLabels(labels []string) []string {
	if len(labels) > 0 {
		return labels
	}
	return []string{"epic"}
}

// defaultFleetDispatchFromIssue is the real implementation: it calls
// Commander.Dispatch with the issue number, which triggers the alice-core
// IssueTracker to fetch the issue from GitHub.
func defaultFleetDispatchFromIssue(ctx context.Context, svc *FleetService, actor fleet.Actor, projectID string, issueNum int, opts fleet.DispatchRequest) (fleet.Task, error) {
	return svc.Commander.Dispatch(ctx, actor, opts)
}
