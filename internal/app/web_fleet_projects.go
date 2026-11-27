package app

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	alicev1 "claude-tg-agent/gen/go/alice/v1"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/verify"
	"github.com/chimerakang/alice-core/worktree"
	"google.golang.org/protobuf/proto"
)

// Endpoints behind the dashboard's dispatch editor, project page and cost
// view.

const (
	// fleetVerifyRunTimeout bounds a trial run of a project's verify
	// commands; each command also has its own, shorter limit.
	fleetVerifyRunTimeout  = 10 * time.Minute
	fleetVerifyStepTimeout = 5 * time.Minute
	// fleetVerifyOutputTail is how many lines of each command's output a
	// trial run returns.
	fleetVerifyOutputTail = 80

	fleetIssueListLimit = 100
	fleetCostTaskLimit  = 20
	fleetCostMaxDays    = 90
)

// fleetOverBudgetPrefix starts the status reason of a task stopped for
// having reached a budget.
const fleetOverBudgetPrefix = "over the "

func (wi *WebInterface) registerFleetProjectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fleet/projects/{id}/issues", wi.fleetHandler(false, wi.fleetProjectIssues))
	mux.HandleFunc("GET /api/fleet/projects/{id}/milestones", wi.fleetHandler(false, wi.fleetProjectMilestones))
	mux.HandleFunc("GET /api/fleet/projects/{id}/github", wi.fleetHandler(false, wi.fleetProjectGitHub))
	mux.HandleFunc("GET /api/fleet/projects/{id}/worktrees", wi.fleetHandler(false, wi.fleetProjectWorktrees))
	mux.HandleFunc("POST /api/fleet/projects/{id}/verify", wi.fleetHandlerWithin(true, fleetVerifyRunTimeout, wi.fleetProjectVerify))
	mux.HandleFunc("GET /api/fleet/usage/backends", wi.fleetHandler(false, wi.fleetBackendUsage))
	mux.HandleFunc("GET /api/fleet/costs", wi.fleetHandler(false, wi.fleetCosts))
	// The same view under the existing cost endpoints, where the Performance
	// page reads the rest of its numbers.
	mux.HandleFunc("GET /api/costs/fleet", wi.fleetHandler(false, wi.fleetCosts))
}

// fleetProjectIssues lists a project's open GitHub issues, marking the ones
// an unfinished task is already working on, and tagging epics from config.
func (wi *WebInterface) fleetProjectIssues(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	project, err := svc.Commander.GetProject(ctx, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	issues, err := tasksGitHubIssueListFunc(ctx, project.RepoPath, "open", fleetIssueListLimit)
	if err != nil {
		return nil, err
	}
	tasks, err := svc.Commander.ListTasks(ctx, fleet.TaskFilter{ProjectID: project.ID})
	if err != nil {
		return nil, err
	}
	busy := map[int]string{}
	for _, task := range tasks {
		if task.Source.Kind == fleet.SourceIssue && !task.Status.IsTerminal() {
			busy[task.Source.IssueNumber] = task.ID
		}
	}
	epicLabels := resolvedEpicLabels(svc.Config.EpicLabels)
	epicPattern := compileEpicPattern(svc.Config.EpicTitlePattern)
	out := &alicev1.FleetIssueList{}
	for _, issue := range issues {
		out.Issues = append(out.Issues, &alicev1.FleetIssue{
			Number: int32(issue.Number), Title: issue.Title, Labels: append([]string{}, issue.Labels...),
			Milestone: issue.Milestone, Url: issue.URL, TaskId: busy[issue.Number],
			IsEpic: isEpicIssue(issue.Labels, issue.Title, epicLabels, epicPattern),
		})
	}
	return out, nil
}

// fleetProjectMilestones lists the open milestones of a project's GitHub repo.
func (wi *WebInterface) fleetProjectMilestones(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	project, err := svc.Commander.GetProject(ctx, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	milestones, err := fleetGitHubMilestoneListFunc(ctx, project.RepoPath)
	if err != nil {
		return nil, err
	}
	out := &alicev1.FleetMilestoneList{}
	for _, m := range milestones {
		out.Milestones = append(out.Milestones, &alicev1.FleetMilestone{
			Number: int32(m.Number), Title: m.Title,
		})
	}
	return out, nil
}

// fleetProjectGitHub says whether GitHub can be written for the project now,
// or why not, so the dispatch form can say so before anything is submitted.
func (wi *WebInterface) fleetProjectGitHub(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	project, err := svc.Commander.GetProject(ctx, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if project.Remote == "" {
		return &alicev1.FleetGitHubStatus{Reason: "no_remote"}, nil
	}
	switch err := fleetGitHubStatusFunc(ctx, project.RepoPath); {
	case err == nil:
		return &alicev1.FleetGitHubStatus{Available: true}, nil
	case errors.Is(err, errTasksGitHubAuthRequired):
		return &alicev1.FleetGitHubStatus{Reason: "auth_required"}, nil
	case errors.Is(err, errTasksNoGitHubRepo):
		return &alicev1.FleetGitHubStatus{Reason: "no_remote"}, nil
	default:
		return nil, err
	}
}

// fleetProjectWorktrees lists the task checkouts of a project. One with
// uncommitted work is dirty; one that no unfinished task owns is an orphan.
// Nothing is removed here: an orphan may still hold work worth keeping.
func (wi *WebInterface) fleetProjectWorktrees(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	project, err := svc.Commander.GetProject(ctx, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	trees, err := worktree.List(ctx, project.RepoPath)
	if err != nil {
		return nil, err
	}
	tasks, err := svc.Commander.ListTasks(ctx, fleet.TaskFilter{ProjectID: project.ID})
	if err != nil {
		return nil, err
	}
	byPath := map[string]fleet.Task{}
	for _, task := range tasks {
		if task.WorktreePath != "" {
			byPath[fleetCanonicalPath(task.WorktreePath)] = task
		}
	}
	out := &alicev1.FleetWorktreeList{}
	for _, tree := range trees {
		if tree.Main {
			continue
		}
		wt := &alicev1.FleetWorktree{Path: tree.Path, Branch: tree.Branch, Missing: tree.Prunable, Orphan: true}
		if task, ok := byPath[fleetCanonicalPath(tree.Path)]; ok {
			wt.TaskId, wt.TaskStatus = task.ID, string(task.Status)
			wt.Orphan = task.Status.IsTerminal()
		}
		if !wt.Missing {
			if dirty, err := worktree.IsDirty(ctx, tree.Path); err == nil {
				wt.Dirty = dirty
			} else {
				wt.Missing = true
			}
		}
		out.Worktrees = append(out.Worktrees, wt)
	}
	sort.SliceStable(out.Worktrees, func(i, j int) bool { return out.Worktrees[i].Path < out.Worktrees[j].Path })
	return out, nil
}

// fleetProjectVerify runs a project's saved verify commands once, in the
// repository's own checkout, and returns each one's outcome. Only the saved
// commands run: this endpoint never executes text from the request.
func (wi *WebInterface) fleetProjectVerify(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	project, err := svc.Commander.GetProject(ctx, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if len(project.VerifyCommands) == 0 {
		return nil, fleetBadRequest("the project has no verify commands; save some first")
	}
	steps := make([]verify.Step, 0, len(project.VerifyCommands))
	for _, command := range project.VerifyCommands {
		steps = append(steps, verify.Shell(command).WithTimeout(fleetVerifyStepTimeout))
	}
	// Every command runs, so one failure does not hide the others.
	report := verify.RunAll(ctx, project.RepoPath, steps...)
	out := &alicev1.FleetVerificationList{}
	for i, res := range report.Results {
		out.Verifications = append(out.Verifications, &alicev1.FleetVerification{
			StepIndex: int32(i), Name: res.Name, Command: res.Command, Passed: res.Passed,
			ExitCode: int32(res.ExitCode), OutputTail: verify.TrimLines(res.Output, fleetVerifyOutputTail),
			DurationMs: float64(res.Duration.Milliseconds()), TimedOut: res.TimedOut, Error: res.Err,
			CreatedAt: fleetTime(time.Now()),
		})
	}
	return out, nil
}

// fleetCosts reports spending by project and by task, the limits it is
// measured against, and the tasks a budget has stopped.
func (wi *WebInterface) fleetCosts(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	days, err := fleetIntParam(r, "days", 7, fleetCostMaxDays)
	if err != nil {
		return nil, err
	}
	if days < 1 {
		days = 1
	}
	digest, err := svc.Commander.Digest(ctx)
	if err != nil {
		return nil, err
	}
	// The period ends today and covers whole days.
	periodStart := digest.DayStart.AddDate(0, 0, -(days - 1))
	period, err := svc.Store.UsageSince(ctx, "", periodStart)
	if err != nil {
		return nil, err
	}
	budget := svc.Runner.Budget
	if budget == nil {
		budget = &fleet.Budget{}
	}
	out := &alicev1.FleetCosts{
		DayStart:      fleetTime(digest.DayStart),
		PeriodStart:   fleetTime(periodStart),
		Today:         fleetUsageProto(digest.Today),
		Period:        fleetUsageProto(period),
		DailyLimitUsd: budget.Daily.CostUSD,
	}

	projects, err := svc.Commander.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	today := map[string]fleet.Usage{}
	for _, p := range digest.Projects {
		today[p.ProjectID] = p.Today
	}
	names := map[string]string{}
	for _, p := range projects {
		names[p.ID] = p.Name
		used, err := svc.Store.UsageSince(ctx, p.ID, periodStart)
		if err != nil {
			return nil, err
		}
		cost := &alicev1.FleetProjectCost{
			ProjectId: p.ID, Name: p.Name, Today: fleetUsageProto(today[p.ID]), Period: fleetUsageProto(used),
			DailyLimitUsd: budget.ProjectDaily.CostUSD, TaskLimitUsd: budget.Task.CostUSD,
		}
		// A project's own limits override the fleet's defaults.
		if p.Budget.Daily.CostUSD > 0 {
			cost.DailyLimitUsd = p.Budget.Daily.CostUSD
		}
		if p.Budget.Task.CostUSD > 0 {
			cost.TaskLimitUsd = p.Budget.Task.CostUSD
		}
		out.Projects = append(out.Projects, cost)
	}

	tasks, err := svc.Commander.ListTasks(ctx, fleet.TaskFilter{})
	if err != nil {
		return nil, err
	}
	var costly, overruns []fleet.Task
	for _, task := range tasks {
		if !task.CreatedAt.Before(periodStart) && task.CostUSD > 0 {
			costly = append(costly, task)
		}
		if strings.HasPrefix(task.StatusReason, fleetOverBudgetPrefix) {
			overruns = append(overruns, task)
		}
	}
	sort.SliceStable(costly, func(i, j int) bool { return costly[i].CostUSD > costly[j].CostUSD })
	sort.SliceStable(overruns, func(i, j int) bool { return overruns[i].UpdatedAt.After(overruns[j].UpdatedAt) })
	for i, task := range costly {
		if i == fleetCostTaskLimit {
			break
		}
		out.Tasks = append(out.Tasks, fleetTaskProto(task, names[task.ProjectID]))
	}
	for i, task := range overruns {
		if i == fleetCostTaskLimit {
			break
		}
		out.Overruns = append(out.Overruns, fleetTaskProto(task, names[task.ProjectID]))
	}
	return out, nil
}
