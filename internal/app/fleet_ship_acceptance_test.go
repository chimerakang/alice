package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/ghissue"
	"github.com/chimerakang/alice-core/issueops"
)

type shipAcceptanceIssueHost struct{ *fakeIssueHost }

func (h shipAcceptanceIssueHost) Comment(context.Context, string, int, string) error {
	return fmt.Errorf("unexpected legacy issue comment")
}

func (h shipAcceptanceIssueHost) SyncChecklist(context.Context, issueops.SyncChecklistRequest) (issueops.SyncChecklistResult, error) {
	return issueops.SyncChecklistResult{}, fmt.Errorf("unexpected checklist write")
}

func TestFleetShipFromIssueThroughLocalApproval(t *testing.T) {
	ctx := context.Background()
	e := newWebFleetEnv(t)
	host := newFakeIssueHost()
	e.svc.IssueSync.Host = host
	e.svc.Commander.Issues.Host = shipAcceptanceIssueHost{host}
	e.svc.Commander.Issues.NoChecklistSync = true
	const issue = 200
	host.issues[issue] = &ghissue.IssueContext{Number: issue, State: "OPEN", Title: "Produce a Ship acceptance deliverable", Body: "Create a deliverable file in the isolated worktree and verify that it lands on the project target branch only after owner approval."}

	for _, pair := range [][2]string{
		{"GIT_AUTHOR_NAME", "Fleet Test"}, {"GIT_AUTHOR_EMAIL", "fleet@example.test"},
		{"GIT_COMMITTER_NAME", "Fleet Test"}, {"GIT_COMMITTER_EMAIL", "fleet@example.test"},
	} {
		t.Setenv(pair[0], pair[1])
	}
	repo := e.project.RepoPath
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(repo, "branch", "develop")
	before := git(repo, "rev-parse", "develop")
	defaultHead := git(repo, "rev-parse", "HEAD")
	projectPath := "/api/fleet/projects/" + e.project.ID
	code, updated := e.call(http.MethodPut, projectPath,
		`{"default_branch":"develop","branch_prefix":"feat/","trust_mode":"local-only","max_concurrent":1,"verify_commands":["test -f deliverable.txt"]}`, true)
	if code != http.StatusOK {
		t.Fatalf("update project: %d %+v", code, updated)
	}

	agent := filepath.Join(t.TempDir(), "agent")
	script := `#!/bin/sh
set -eu
printf 'accepted\n' > deliverable.txt
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"Added the acceptance deliverable."}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"ship-test"}'
`
	if err := os.WriteFile(agent, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	e.svc.Runner.Binary = agent
	code, dispatched := e.call(http.MethodPost, "/api/fleet/tasks",
		fmt.Sprintf(`{"project_id":%q,"issue":%d,"shape":"ship"}`, e.project.ID, issue), true)
	if code != http.StatusOK {
		t.Fatalf("dispatch: %d %+v", code, dispatched)
	}
	taskID := dispatched["id"].(string)
	taskPath := "/api/fleet/tasks/" + taskID
	if _, err := e.svc.Scheduler.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	e.svc.Scheduler.Wait()

	task, err := e.svc.Store.GetTask(ctx, taskID)
	if err != nil || task.Status != fleet.StatusReviewReady || task.TargetBranch != "develop" || task.Branch != "feat/"+taskID {
		t.Fatalf("task before approval = %+v, err = %v", task, err)
	}
	if got := git(task.WorktreePath, "rev-parse", "HEAD^"); got != before {
		t.Fatalf("task started from %s, want %s", got, before)
	}
	summary := e.get(taskPath + "/delivery")
	if summary["verification"] != "passed" || summary["note"] != "Added the acceptance deliverable." {
		t.Fatalf("delivery summary before approval = %+v", summary)
	}
	if got := git(repo, "rev-parse", "develop"); got != before {
		t.Fatalf("develop changed before approval: %s", got)
	}

	code, approved := e.call(http.MethodPost, taskPath+"/approve", "{}", true)
	if code != http.StatusOK || approved["status"] != "done" {
		t.Fatalf("approve: %d %+v", code, approved)
	}
	if got := git(repo, "show", "develop:deliverable.txt"); got != "accepted" {
		t.Fatalf("develop deliverable = %q", got)
	}
	if got := git(repo, "rev-parse", "HEAD"); got != defaultHead {
		t.Fatalf("checked-out branch changed from %s to %s", defaultHead, got)
	}
	code, synced := e.call(http.MethodPost, taskPath+"/sync-issue", "{}", true)
	if code != http.StatusOK {
		t.Fatalf("sync issue: %d %+v", code, synced)
	}
	comment := host.commentFor(taskID)
	if !strings.Contains(comment, "本機") || !strings.Contains(comment, "develop") || len(host.closed) != 0 {
		t.Fatalf("local landing must be described and issue kept open: %q, closed=%v", comment, host.closed)
	}

	interruptedIssue := *host.issues[issue]
	interruptedIssue.Number++
	host.issues[interruptedIssue.Number] = &interruptedIssue
	stopScript := `#!/bin/sh
printf 'partial work\n' > partial.txt
printf '%s\n' '{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":1,"session_id":"interrupted-test"}'
`
	if err := os.WriteFile(agent, []byte(stopScript), 0700); err != nil {
		t.Fatal(err)
	}
	code, dispatched = e.call(http.MethodPost, "/api/fleet/tasks",
		fmt.Sprintf(`{"project_id":%q,"issue":%d,"shape":"ship"}`, e.project.ID, interruptedIssue.Number), true)
	if code != http.StatusOK {
		t.Fatalf("dispatch interrupted case: %d %+v", code, dispatched)
	}
	interruptedID := dispatched["id"].(string)
	if _, err := e.svc.Scheduler.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	e.svc.Scheduler.Wait()
	interruptedPath := "/api/fleet/tasks/" + interruptedID
	summary = e.get(interruptedPath + "/delivery")
	if summary["status"] != "needs_decision" || summary["verification"] != "not_run" ||
		!strings.Contains(fmt.Sprint(summary["stop_reason"]), "turn limit") {
		t.Fatalf("interrupted summary should show pending decision and no verification: %+v", summary)
	}
	code, synced = e.call(http.MethodPost, interruptedPath+"/sync-issue", "{}", true)
	if code != http.StatusOK || len(host.closed) != 0 {
		t.Fatalf("interrupted issue must stay open: %d %+v, closed=%v", code, synced, host.closed)
	}

}
