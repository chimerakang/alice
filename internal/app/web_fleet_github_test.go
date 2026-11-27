package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
)

// stubDispatchFromIssue replaces fleetDispatchFromIssueFunc with a stub that
// creates the task directly in the store (bypassing gh issue view).
func stubDispatchFromIssue(t *testing.T, svc *FleetService) {
	t.Helper()
	prev := fleetDispatchFromIssueFunc
	t.Cleanup(func() { fleetDispatchFromIssueFunc = prev })
	fleetDispatchFromIssueFunc = func(ctx context.Context, s *FleetService, actor fleet.Actor, projectID string, issueNum int, req fleet.DispatchRequest) (fleet.Task, error) {
		task, err := s.Store.CreateTask(ctx, fleet.NewTask{
			ProjectID: projectID,
			Shape:     req.Shape,
			Goal:      req.Goal,
			Source:    fleet.Source{Kind: fleet.SourceIssue, IssueNumber: issueNum},
			Priority:  req.Priority,
		})
		return task, err
	}
	_ = svc
}

// TestFleetDispatchCreateIssue tests the create_issue dispatch path.
func TestFleetDispatchCreateIssue(t *testing.T) {
	e := newWebFleetEnv(t)

	// Stubs – restore after each sub-test.
	prevCreate := fleetGitHubIssueCreateFunc
	prevMilestoneGetOrCreate := fleetGitHubMilestoneGetOrCreate
	prevMilestoneList := fleetGitHubMilestoneListFunc
	prevEpicUpdate := fleetGitHubEpicUpdateFunc
	t.Cleanup(func() {
		fleetGitHubIssueCreateFunc = prevCreate
		fleetGitHubMilestoneGetOrCreate = prevMilestoneGetOrCreate
		fleetGitHubMilestoneListFunc = prevMilestoneList
		fleetGitHubEpicUpdateFunc = prevEpicUpdate
	})

	// Stub the dispatch-from-issue call for all subtests.
	stubDispatchFromIssue(t, e.svc)

	t.Run("success: creates issue and dispatches task", func(t *testing.T) {
		var createCalled struct {
			repoPath  string
			title     string
			body      string
			milestone int
		}
		fleetGitHubIssueCreateFunc = func(_ context.Context, repoPath, title, body string, milestone int) (fleetCreatedIssue, error) {
			createCalled.repoPath = repoPath
			createCalled.title = title
			createCalled.body = body
			createCalled.milestone = milestone
			return fleetCreatedIssue{Number: 42, HTMLURL: "https://github.com/test/repo/issues/42"}, nil
		}
		fleetGitHubEpicUpdateFunc = func(context.Context, string, int, int, string) error { return nil }
		fleetGitHubMilestoneGetOrCreate = func(context.Context, string, string) (int, error) { return 0, nil }

		status, body := e.call(http.MethodPost, "/api/fleet/tasks",
			`{"project_id":"`+e.project.ID+`","goal":"Fix the login bug\n\nDetails here","create_issue":true}`,
			true)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %v", status, body)
		}
		// Issue title should be the first non-empty line.
		if createCalled.title != "Fix the login bug" {
			t.Errorf("title = %q, want %q", createCalled.title, "Fix the login bug")
		}
		if createCalled.repoPath != e.project.RepoPath {
			t.Errorf("repoPath = %q", createCalled.repoPath)
		}
		// The task should be dispatched from the created issue.
		if body["issue_number"] != float64(42) {
			t.Errorf("issue_number = %v, want 42", body["issue_number"])
		}
		if body["issue_url"] != "https://github.com/test/repo/issues/42" {
			t.Errorf("issue_url = %v", body["issue_url"])
		}
		if body["source_kind"] != "issue" {
			t.Errorf("source_kind = %v, want issue", body["source_kind"])
		}
	})

	t.Run("success: creates milestone when given a name", func(t *testing.T) {
		var msName string
		fleetGitHubMilestoneGetOrCreate = func(_ context.Context, _ string, name string) (int, error) {
			msName = name
			return 7, nil
		}
		var issueMilestone int
		fleetGitHubIssueCreateFunc = func(_ context.Context, _, _, _ string, milestone int) (fleetCreatedIssue, error) {
			issueMilestone = milestone
			return fleetCreatedIssue{Number: 43, HTMLURL: "https://github.com/test/repo/issues/43"}, nil
		}
		fleetGitHubEpicUpdateFunc = func(context.Context, string, int, int, string) error { return nil }

		status, body := e.call(http.MethodPost, "/api/fleet/tasks",
			`{"project_id":"`+e.project.ID+`","goal":"Add dark mode","create_issue":true,"milestone":"v2.0"}`,
			true)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %v", status, body)
		}
		if msName != "v2.0" {
			t.Errorf("milestone name passed to getOrCreate = %q", msName)
		}
		if issueMilestone != 7 {
			t.Errorf("issue milestone number = %d, want 7", issueMilestone)
		}
	})

	t.Run("success: appends to epic, no warning on success", func(t *testing.T) {
		var epicCalled struct {
			epicNum, issueNum int
			issueTitle        string
		}
		fleetGitHubIssueCreateFunc = func(context.Context, string, string, string, int) (fleetCreatedIssue, error) {
			return fleetCreatedIssue{Number: 44, HTMLURL: "https://github.com/test/repo/issues/44"}, nil
		}
		fleetGitHubEpicUpdateFunc = func(_ context.Context, _ string, epicNum, issueNum int, title string) error {
			epicCalled.epicNum, epicCalled.issueNum, epicCalled.issueTitle = epicNum, issueNum, title
			return nil
		}
		fleetGitHubMilestoneGetOrCreate = func(context.Context, string, string) (int, error) { return 0, nil }

		status, body := e.call(http.MethodPost, "/api/fleet/tasks",
			`{"project_id":"`+e.project.ID+`","goal":"Refactor auth","create_issue":true,"epic_issue":10}`,
			true)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %v", status, body)
		}
		if epicCalled.epicNum != 10 || epicCalled.issueNum != 44 {
			t.Errorf("epic update called with (%d, %d), want (10, 44)", epicCalled.epicNum, epicCalled.issueNum)
		}
		if body["warning"] != "" {
			t.Errorf("warning should be empty on success, got %q", body["warning"])
		}
	})

	t.Run("epic update failure: task dispatched with warning", func(t *testing.T) {
		fleetGitHubIssueCreateFunc = func(context.Context, string, string, string, int) (fleetCreatedIssue, error) {
			return fleetCreatedIssue{Number: 45, HTMLURL: "https://github.com/test/repo/issues/45"}, nil
		}
		fleetGitHubEpicUpdateFunc = func(context.Context, string, int, int, string) error {
			return errors.New("not found")
		}
		fleetGitHubMilestoneGetOrCreate = func(context.Context, string, string) (int, error) { return 0, nil }

		status, body := e.call(http.MethodPost, "/api/fleet/tasks",
			`{"project_id":"`+e.project.ID+`","goal":"Improve caching","create_issue":true,"epic_issue":99}`,
			true)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %v (task should be dispatched despite epic failure)", status, body)
		}
		if body["issue_number"] != float64(45) {
			t.Errorf("issue_number = %v, want 45", body["issue_number"])
		}
		warning, _ := body["warning"].(string)
		if warning == "" {
			t.Error("warning should be non-empty when epic update fails")
		}
	})

	t.Run("issue creation failure: request rejected, no task created", func(t *testing.T) {
		fleetGitHubIssueCreateFunc = func(context.Context, string, string, string, int) (fleetCreatedIssue, error) {
			return fleetCreatedIssue{}, errTasksGitHubAuthRequired
		}
		existing, _ := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{})
		before := len(existing)
		fleetGitHubMilestoneGetOrCreate = func(context.Context, string, string) (int, error) { return 0, nil }

		status, body := e.call(http.MethodPost, "/api/fleet/tasks",
			`{"project_id":"`+e.project.ID+`","goal":"Something","create_issue":true}`,
			true)
		if status != http.StatusBadGateway {
			t.Fatalf("expected 502 on auth failure, got %d %v", status, body)
		}
		if body["code"] != "github_auth" {
			t.Errorf("code = %v, want github_auth", body["code"])
		}
		if tasks, _ := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{}); len(tasks) != before {
			t.Errorf("a failed issue creation left %d task(s), want %d", len(tasks), before)
		}
	})

	t.Run("rejected when both issue and create_issue are set", func(t *testing.T) {
		status, body := e.call(http.MethodPost, "/api/fleet/tasks",
			`{"project_id":"`+e.project.ID+`","goal":"x","issue":5,"create_issue":true}`,
			true)
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d %v", status, body)
		}
	})

	t.Run("without create_issue, existing dispatch flow unchanged", func(t *testing.T) {
		// Ensure the stubs are not called in the normal path.
		fleetGitHubIssueCreateFunc = func(context.Context, string, string, string, int) (fleetCreatedIssue, error) {
			t.Error("createFleetGitHubIssue should not be called without create_issue=true")
			return fleetCreatedIssue{}, nil
		}

		status, body := e.call(http.MethodPost, "/api/fleet/tasks",
			`{"project_id":"`+e.project.ID+`","goal":"plain task"}`,
			true)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %v", status, body)
		}
		if body["source_kind"] != "manual" {
			t.Errorf("source_kind = %v, want manual", body["source_kind"])
		}
	})
}

// TestFleetProjectMilestones tests the new milestones endpoint.
func TestFleetProjectMilestones(t *testing.T) {
	e := newWebFleetEnv(t)
	prev := fleetGitHubMilestoneListFunc
	t.Cleanup(func() { fleetGitHubMilestoneListFunc = prev })

	fleetGitHubMilestoneListFunc = func(_ context.Context, dir string) ([]fleetMilestoneJSON, error) {
		if dir != e.project.RepoPath {
			t.Errorf("dir = %q, want %q", dir, e.project.RepoPath)
		}
		return []fleetMilestoneJSON{
			{Number: 1, Title: "v1.0"},
			{Number: 2, Title: "v2.0"},
		}, nil
	}

	milestones := list(t, e.get("/api/fleet/projects/"+e.project.ID+"/milestones"), "milestones")
	if len(milestones) != 2 {
		t.Fatalf("milestones = %v", milestones)
	}
	if milestones[0]["number"] != float64(1) || milestones[0]["title"] != "v1.0" {
		t.Errorf("first milestone = %v", milestones[0])
	}
	if milestones[1]["title"] != "v2.0" {
		t.Errorf("second milestone = %v", milestones[1])
	}

	// Auth failure is surfaced correctly.
	fleetGitHubMilestoneListFunc = func(context.Context, string) ([]fleetMilestoneJSON, error) {
		return nil, errTasksGitHubAuthRequired
	}
	if status, body := e.call(http.MethodGet, "/api/fleet/projects/"+e.project.ID+"/milestones", "", false); status != http.StatusBadGateway || body["code"] != "github_auth" {
		t.Fatalf("auth required: %d %v", status, body)
	}

	// Unknown project returns 404.
	if status, _ := e.call(http.MethodGet, "/api/fleet/projects/nope/milestones", "", false); status != http.StatusNotFound {
		t.Fatalf("unknown project: %d", status)
	}
}

// TestIsEpicIssue tests the epic detection helper.
func TestIsEpicIssue(t *testing.T) {
	epicLabels := []string{"epic", "type:epic"}

	// Label match.
	if !isEpicIssue([]string{"bug", "epic"}, "Fix login", epicLabels, nil) {
		t.Error("label 'epic' should match")
	}
	if !isEpicIssue([]string{"type:epic"}, "Fix login", epicLabels, nil) {
		t.Error("label 'type:epic' should match")
	}
	// Case-insensitive label match.
	if !isEpicIssue([]string{"EPIC"}, "Fix login", epicLabels, nil) {
		t.Error("label match should be case-insensitive")
	}

	// Pattern match.
	re := compileEpicPattern(`^\[M\d+\] Epic:`)
	if !isEpicIssue(nil, "[M8] Epic: Auth refactor", epicLabels, re) {
		t.Error("title pattern should match")
	}
	if isEpicIssue(nil, "Fix login", epicLabels, re) {
		t.Error("non-matching title should not match")
	}

	// Neither label nor pattern.
	if isEpicIssue([]string{"bug"}, "Fix login", epicLabels, nil) {
		t.Error("non-epic label should not match")
	}

	// Default label "epic" when no config.
	defaultLabels := resolvedEpicLabels(nil)
	if !isEpicIssue([]string{"epic"}, "x", defaultLabels, nil) {
		t.Error("default epic label should work")
	}

	// compileEpicPattern with empty string returns nil.
	if compileEpicPattern("") != nil {
		t.Error("empty pattern should return nil")
	}
}

// TestFleetProjectIssuesEpicDetection tests that is_epic is set in the issues endpoint.
func TestFleetProjectIssuesEpicDetection(t *testing.T) {
	e := newWebFleetEnv(t)
	// Set up config with epic labels.
	e.svc.Config.EpicLabels = []string{"epic"}
	e.svc.Config.EpicTitlePattern = `^\[M\d+\]`

	prev := tasksGitHubIssueListFunc
	t.Cleanup(func() { tasksGitHubIssueListFunc = prev })

	tasksGitHubIssueListFunc = func(context.Context, string, string, int) ([]tasksGitHubIssue, error) {
		return []tasksGitHubIssue{
			{Number: 1, Title: "[M1] Big feature", Labels: []string{"bug"}},
			{Number: 2, Title: "Small fix", Labels: []string{"epic"}},
			{Number: 3, Title: "Normal issue", Labels: []string{"bug"}},
		}, nil
	}

	issues := list(t, e.get("/api/fleet/projects/"+e.project.ID+"/issues"), "issues")
	if len(issues) != 3 {
		t.Fatalf("issues = %v", issues)
	}
	if issues[0]["is_epic"] != true {
		t.Errorf("issue 1 (title pattern) is_epic = %v, want true", issues[0]["is_epic"])
	}
	if issues[1]["is_epic"] != true {
		t.Errorf("issue 2 (label) is_epic = %v, want true", issues[1]["is_epic"])
	}
	if issues[2]["is_epic"] != false {
		t.Errorf("issue 3 (normal) is_epic = %v, want false", issues[2]["is_epic"])
	}
}
