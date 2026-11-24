package app

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/worktree"
)

func TestFleetAPIProjectIssues(t *testing.T) {
	e := newWebFleetEnv(t)
	prev := tasksGitHubIssueListFunc
	defer func() { tasksGitHubIssueListFunc = prev }()

	var asked struct {
		dir, state string
		limit      int
	}
	tasksGitHubIssueListFunc = func(_ context.Context, dir, state string, limit int) ([]tasksGitHubIssue, error) {
		asked.dir, asked.state, asked.limit = dir, state, limit
		return []tasksGitHubIssue{
			{Number: 12, Title: "Fix login", Labels: []string{"bug"}, Milestone: "v1", URL: "https://example.test/12"},
			{Number: 13, Title: "Add export"},
		}, nil
	}
	// Issue 12 already has an unfinished task; a finished one does not count.
	busy, err := e.svc.Store.CreateTask(context.Background(), fleet.NewTask{
		ProjectID: e.project.ID, Goal: "x", Source: fleet.Source{Kind: fleet.SourceIssue, IssueNumber: 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	done, _ := e.svc.Store.CreateTask(context.Background(), fleet.NewTask{
		ProjectID: e.project.ID, Goal: "x", Source: fleet.Source{Kind: fleet.SourceIssue, IssueNumber: 13},
	})
	e.walk(done.ID, "withdrawn", fleet.StatusCancelled)

	issues := list(t, e.get("/api/fleet/projects/"+e.project.ID+"/issues"), "issues")
	if len(issues) != 2 {
		t.Fatalf("issues = %v", issues)
	}
	if asked.dir != e.project.RepoPath || asked.state != "open" || asked.limit != fleetIssueListLimit {
		t.Fatalf("listed with %+v", asked)
	}
	if issues[0]["number"] != float64(12) || issues[0]["title"] != "Fix login" || issues[0]["task_id"] != busy.ID || issues[0]["milestone"] != "v1" {
		t.Fatalf("issue 12 = %v", issues[0])
	}
	if issues[1]["task_id"] != "" {
		t.Fatalf("issue 13 should be free: %v", issues[1])
	}

	tasksGitHubIssueListFunc = func(context.Context, string, string, int) ([]tasksGitHubIssue, error) {
		return nil, errTasksNoGitHubRepo
	}
	if status, body := e.call(http.MethodGet, "/api/fleet/projects/"+e.project.ID+"/issues", "", false); status != http.StatusConflict || body["code"] != "no_github_repo" {
		t.Fatalf("no GitHub repo: %d %v", status, body)
	}
	tasksGitHubIssueListFunc = func(context.Context, string, string, int) ([]tasksGitHubIssue, error) {
		return nil, errTasksGitHubAuthRequired
	}
	if status, body := e.call(http.MethodGet, "/api/fleet/projects/"+e.project.ID+"/issues", "", false); status != http.StatusBadGateway || body["code"] != "github_auth" {
		t.Fatalf("gh not signed in: %d %v", status, body)
	}
	if status, _ := e.call(http.MethodGet, "/api/fleet/projects/nope/issues", "", false); status != http.StatusNotFound {
		t.Fatalf("unknown project: %d", status)
	}
}

func TestFleetAPIProjectWorktrees(t *testing.T) {
	e := newWebFleetEnv(t)
	ctx := context.Background()
	path := "/api/fleet/projects/" + e.project.ID + "/worktrees"

	if got := list(t, e.get(path), "worktrees"); len(got) != 0 {
		t.Fatalf("a project with no task checkouts lists %v", got)
	}

	// One checkout owned by a running task, with uncommitted work; one whose
	// task is finished; one no task knows about.
	checkout := func(taskID string) string {
		dir, err := worktree.Layout{Root: e.svc.Config.WorktreeRoot}.Path(e.project.ID, taskID)
		if err != nil {
			t.Fatal(err)
		}
		branch := "alice/" + taskID
		if _, err := worktree.Create(ctx, e.project.RepoPath, dir, branch, e.project.DefaultBranch); err != nil {
			t.Fatalf("worktree.Create: %v", err)
		}
		return dir
	}
	live := e.dispatch("/ship live")
	liveDir := checkout(live.ID)
	if err := e.svc.Store.SetWorkspace(ctx, live.ID, "alice/"+live.ID, liveDir); err != nil {
		t.Fatal(err)
	}
	e.walk(live.ID, "slot free", fleet.StatusRunning)
	if err := os.WriteFile(filepath.Join(liveDir, "wip.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}

	finished := e.dispatch("/ship finished")
	finishedDir := checkout(finished.ID)
	if err := e.svc.Store.SetWorkspace(ctx, finished.ID, "alice/"+finished.ID, finishedDir); err != nil {
		t.Fatal(err)
	}
	e.walk(finished.ID, "withdrawn", fleet.StatusCancelled)

	strayDir := checkout("strayaaaaaaa")

	trees := list(t, e.get(path), "worktrees")
	if len(trees) != 3 {
		t.Fatalf("worktrees = %v", trees)
	}
	byPath := map[string]map[string]any{}
	for _, tree := range trees {
		byPath[filepath.Base(tree["path"].(string))] = tree
		if strings.Contains(tree["path"].(string), e.project.RepoPath+"/.git") {
			t.Errorf("the main checkout is listed: %v", tree)
		}
	}
	if got := byPath[live.ID]; got["task_id"] != live.ID || got["task_status"] != "running" || got["dirty"] != true || got["orphan"] != false {
		t.Errorf("live checkout = %v", got)
	}
	if got := byPath[finished.ID]; got["task_id"] != finished.ID || got["dirty"] != false || got["orphan"] != true {
		t.Errorf("finished task's checkout = %v", got)
	}
	if got := byPath[filepath.Base(strayDir)]; got["task_id"] != "" || got["orphan"] != true || got["branch"] != "alice/strayaaaaaaa" {
		t.Errorf("stray checkout = %v", got)
	}
	if status, _ := e.call(http.MethodGet, "/api/fleet/projects/nope/worktrees", "", false); status != http.StatusNotFound {
		t.Fatalf("unknown project: %d", status)
	}
}

func TestFleetAPIProjectVerifyRun(t *testing.T) {
	e := newWebFleetEnv(t)
	path := "/api/fleet/projects/" + e.project.ID + "/verify"

	// It is a command: it runs things.
	if status, body := e.call(http.MethodPost, path, "", false); status != http.StatusUnauthorized {
		t.Fatalf("without the token: %d %v", status, body)
	}
	// With nothing saved there is nothing to run.
	if status, body := e.call(http.MethodPost, path, "", true); status != http.StatusBadRequest {
		t.Fatalf("no commands saved: %d %v", status, body)
	}

	if _, err := e.svc.Store.UpdateProject(context.Background(), e.project.ID, func(p *fleet.Project) error {
		p.VerifyCommands = []string{"test -f README.md", "echo out; echo err >&2; exit 3", "echo still-runs"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Text in the request is not run; only the saved commands are.
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	status, body := e.call(http.MethodPost, path, "", true)
	if status != http.StatusOK {
		t.Fatalf("verify run: %d %v", status, body)
	}
	results := list(t, body, "verifications")
	if len(results) != 3 {
		t.Fatalf("results = %v", results)
	}
	if results[0]["passed"] != true || results[0]["exit_code"] != float64(0) || results[0]["command"] != "test -f README.md" {
		t.Errorf("first command = %v", results[0])
	}
	second := results[1]
	if second["passed"] != false || second["exit_code"] != float64(3) {
		t.Errorf("failing command = %v", second)
	}
	if out, _ := second["output_tail"].(string); !strings.Contains(out, "out") || !strings.Contains(out, "err") {
		t.Errorf("failing command's output = %q, want both streams", out)
	}
	// A failure does not stop the commands after it.
	if results[2]["passed"] != true || !strings.Contains(results[2]["output_tail"].(string), "still-runs") {
		t.Errorf("third command = %v", results[2])
	}

	if status, _ := e.call(http.MethodPost, path, `{"commands":["touch `+marker+`"]}`, true); status == http.StatusOK {
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("a command from the request body was run")
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a command from the request body was run")
	}
	// The run happens in the repository, and leaves it as it was.
	out, err := exec.Command("git", "-C", e.project.RepoPath, "status", "--porcelain").Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("repository after the trial run: %q, %v", out, err)
	}
}

func TestFleetAPICosts(t *testing.T) {
	e := newWebFleetEnv(t)
	ctx := context.Background()
	e.svc.Runner.Budget = &fleet.Budget{Daily: fleet.Limit{CostUSD: 20}, Task: fleet.Limit{CostUSD: 5}, ProjectDaily: fleet.Limit{CostUSD: 8}}

	cheap := e.dispatch("/ship cheap")
	dear := e.dispatch("/ship dear")
	free := e.dispatch("/ship free")
	if err := e.svc.Store.AddUsage(ctx, cheap.ID, fleet.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.25}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Store.AddUsage(ctx, dear.ID, fleet.Usage{InputTokens: 1000, OutputTokens: 500, CostUSD: 6}); err != nil {
		t.Fatal(err)
	}
	e.walk(dear.ID, "over the task budget: $6.00 spent of a $5.00 limit", fleet.StatusRunning, fleet.StatusNeedsDecision)

	costs := e.get("/api/fleet/costs")
	today, _ := costs["today"].(map[string]any)
	period, _ := costs["period"].(map[string]any)
	if today["cost_usd"] != 6.25 || period["cost_usd"] != 6.25 || today["input_tokens"] != float64(1100) {
		t.Fatalf("today = %v, period = %v", today, period)
	}
	if costs["daily_limit_usd"] != float64(20) {
		t.Errorf("daily_limit_usd = %v", costs["daily_limit_usd"])
	}
	projects := list(t, costs, "projects")
	if len(projects) != 1 || projects[0]["daily_limit_usd"] != float64(8) || projects[0]["task_limit_usd"] != float64(5) {
		t.Fatalf("projects = %v", projects)
	}
	if got, _ := projects[0]["today"].(map[string]any); got["cost_usd"] != 6.25 {
		t.Errorf("project today = %v", got)
	}

	// Costliest first; a task that has spent nothing is left out.
	tasks := list(t, costs, "tasks")
	if len(tasks) != 2 || tasks[0]["id"] != dear.ID || tasks[1]["id"] != cheap.ID {
		t.Fatalf("tasks by cost = %v (free task %s must be absent)", tasks, free.ID)
	}
	overruns := list(t, costs, "overruns")
	if len(overruns) != 1 || overruns[0]["id"] != dear.ID || !strings.HasPrefix(overruns[0]["status_reason"].(string), "over the task budget") {
		t.Fatalf("overruns = %v", overruns)
	}

	// A project's own limits replace the defaults.
	if _, err := e.svc.Store.UpdateProject(ctx, e.project.ID, func(p *fleet.Project) error {
		p.Budget.Daily.CostUSD, p.Budget.Task.CostUSD = 3, 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	projects = list(t, e.get("/api/fleet/costs?days=30"), "projects")
	if projects[0]["daily_limit_usd"] != float64(3) || projects[0]["task_limit_usd"] != float64(1) {
		t.Fatalf("project limits = %v", projects[0])
	}
	if status, _ := e.call(http.MethodGet, "/api/fleet/costs?days=x", "", false); status != http.StatusBadRequest {
		t.Fatalf("days=x: %d", status)
	}
}
