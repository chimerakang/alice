package app

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
)

func TestFleetCreateEpic(t *testing.T) {
	e := newWebFleetEnv(t)
	prevLabel, prevIssue, prevMS := fleetGitHubLabelEnsureFunc, fleetGitHubLabeledIssueFunc, fleetGitHubMilestoneGetOrCreate
	t.Cleanup(func() {
		fleetGitHubLabelEnsureFunc, fleetGitHubLabeledIssueFunc, fleetGitHubMilestoneGetOrCreate = prevLabel, prevIssue, prevMS
	})
	e.svc.Config.EpicChildrenHeading = "Children"

	type created struct {
		title, body string
		milestone   int
		labels      []string
	}
	var got created
	stub := func(labelCreated bool) {
		fleetGitHubLabelEnsureFunc = func(context.Context, string, string) (bool, error) { return labelCreated, nil }
		fleetGitHubLabeledIssueFunc = func(_ context.Context, _, title, body string, ms int, labels []string) (fleetCreatedIssue, error) {
			got = created{title, body, ms, labels}
			return fleetCreatedIssue{Number: 60, HTMLURL: "https://github.com/test/repo/issues/60"}, nil
		}
		fleetGitHubMilestoneGetOrCreate = func(context.Context, string, string) (int, bool, error) { return 0, false, nil }
	}
	tasksBefore := func() int {
		ts, _ := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{})
		return len(ts)
	}
	path := "/api/fleet/epics"

	t.Run("requires the api token", func(t *testing.T) {
		stub(false)
		status, _ := e.call(http.MethodPost, path, `{"project_id":"`+e.project.ID+`","description":"x"}`, false)
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", status)
		}
	})

	t.Run("success creates a labelled epic and no task", func(t *testing.T) {
		stub(false)
		before := tasksBefore()
		status, body := e.call(http.MethodPost, path,
			`{"project_id":"`+e.project.ID+`","description":"\n\nBig plan\n\nDetails"}`, true)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %v", status, body)
		}
		if got.title != "Big plan" || len(got.labels) != 1 || got.labels[0] != "epic" {
			t.Errorf("created = %+v", got)
		}
		if !strings.HasSuffix(got.body, "Details\n\n## Children\n") {
			t.Errorf("body = %q, want the description then an empty Children section", got.body)
		}
		if body["number"] != float64(60) || body["url"] == "" || body["label_created"] != false {
			t.Errorf("reply = %v", body)
		}
		if tasksBefore() != before {
			t.Error("creating an epic must not create a task")
		}
	})

	t.Run("a child appended later lands in the section", func(t *testing.T) {
		in := buildEpicBody("Big plan", "Children")
		out := appendEpicChild(in, "- [ ] #61 First", "Children")
		if out != "Big plan\n\n## Children\n\n- [ ] #61 First" {
			t.Errorf("got %q", out)
		}
		out = appendEpicChild(out, "- [ ] #62 Second", "Children")
		if !strings.HasSuffix(out, "- [ ] #61 First\n- [ ] #62 Second") {
			t.Errorf("got %q", out)
		}
	})

	t.Run("label that is missing is reported as created", func(t *testing.T) {
		stub(true)
		e.svc.Config.EpicLabels = []string{"initiative", "epic"}
		t.Cleanup(func() { e.svc.Config.EpicLabels = nil })
		status, body := e.call(http.MethodPost, path, `{"project_id":"`+e.project.ID+`","description":"Plan"}`, true)
		if status != http.StatusOK || body["label_created"] != true || body["label"] != "initiative" {
			t.Fatalf("status = %d, body = %v", status, body)
		}
		if got.labels[0] != "initiative" {
			t.Errorf("labels = %v", got.labels)
		}
	})

	t.Run("milestone is created by name", func(t *testing.T) {
		stub(false)
		var name string
		fleetGitHubMilestoneGetOrCreate = func(_ context.Context, _ string, n string) (int, bool, error) { name = n; return 9, true, nil }
		status, body := e.call(http.MethodPost, path, `{"project_id":"`+e.project.ID+`","description":"Plan","milestone":"v3"}`, true)
		if status != http.StatusOK || name != "v3" || got.milestone != 9 {
			t.Fatalf("status = %d, body = %v, name = %q, milestone = %d", status, body, name, got.milestone)
		}
	})

	t.Run("title pattern mismatch is flagged, title kept", func(t *testing.T) {
		stub(false)
		e.svc.Config.EpicTitlePattern = `^\[E\]`
		t.Cleanup(func() { e.svc.Config.EpicTitlePattern = "" })
		_, body := e.call(http.MethodPost, path, `{"project_id":"`+e.project.ID+`","description":"Plain title"}`, true)
		if body["title_pattern_mismatch"] != true || got.title != "Plain title" {
			t.Errorf("body = %v, title = %q", body, got.title)
		}
		_, body = e.call(http.MethodPost, path, `{"project_id":"`+e.project.ID+`","description":"[E] ok"}`, true)
		if body["title_pattern_mismatch"] == true {
			t.Errorf("matching title flagged: %v", body)
		}
	})

	t.Run("creation failure returns an error", func(t *testing.T) {
		stub(false)
		fleetGitHubLabeledIssueFunc = func(context.Context, string, string, string, int, []string) (fleetCreatedIssue, error) {
			return fleetCreatedIssue{}, errors.New("boom")
		}
		before := tasksBefore()
		status, _ := e.call(http.MethodPost, path, `{"project_id":"`+e.project.ID+`","description":"Plan"}`, true)
		if status < 400 || tasksBefore() != before {
			t.Errorf("status = %d", status)
		}
	})

	t.Run("blank description is rejected", func(t *testing.T) {
		stub(false)
		status, _ := e.call(http.MethodPost, path, `{"project_id":"`+e.project.ID+`","description":"  "}`, true)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d", status)
		}
	})
}

func TestFleetProjectGitHubStatus(t *testing.T) {
	e := newWebFleetEnv(t)
	prev := fleetGitHubStatusFunc
	t.Cleanup(func() { fleetGitHubStatusFunc = prev })
	url := "/api/fleet/projects/" + e.project.ID + "/github"
	ctx := context.Background()

	fleetGitHubStatusFunc = func(context.Context, string) error { t.Error("gh must not run without a remote"); return nil }
	if _, err := e.svc.Store.UpdateProject(ctx, e.project.ID, func(p *fleet.Project) error { p.Remote = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	if body := e.get(url); body["available"] == true || body["reason"] != "no_remote" {
		t.Errorf("no remote: %v", body)
	}

	if out, err := exec.Command("git", "-C", e.project.RepoPath, "remote", "add", "origin", "https://github.com/o/r.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v %s", err, out)
	}
	if _, err := e.svc.Store.UpdateProject(ctx, e.project.ID, func(p *fleet.Project) error { p.Remote = "origin"; return nil }); err != nil {
		t.Fatal(err)
	}
	fleetGitHubStatusFunc = func(context.Context, string) error { return errTasksGitHubAuthRequired }
	if body := e.get(url); body["available"] == true || body["reason"] != "auth_required" {
		t.Errorf("not logged in: %v", body)
	}
	fleetGitHubStatusFunc = func(context.Context, string) error { return nil }
	if body := e.get(url); body["available"] != true {
		t.Errorf("logged in: %v", body)
	}
}

// A child joins the end of the epic's list of children, not the end of the body.
func TestAppendEpicChildAfterLastChild(t *testing.T) {
	const item = "- [ ] #9 New"
	for name, tc := range map[string]struct{ body, want string }{
		"sections follow the list": {
			"## Tasks\n- [x] #1 A\n- [ ] #2 B\n  detail\n\n## Notes\ntext",
			"## Tasks\n- [x] #1 A\n- [ ] #2 B\n  detail\n- [ ] #9 New\n\n## Notes\ntext",
		},
		"another checklist follows": {
			"- [ ] #1 A\n\n## Done when\n- [ ] it works",
			"- [ ] #1 A\n- [ ] #9 New\n\n## Done when\n- [ ] it works",
		},
		"fenced example is not the list": {
			"- [ ] #1 A\n```\n- [ ] #2 example\n```",
			"- [ ] #1 A\n- [ ] #9 New\n```\n- [ ] #2 example\n```",
		},
		"CRLF body": {
			"- [ ] #1 A\r\n\r\nend",
			"- [ ] #1 A\r\n- [ ] #9 New\r\n\r\nend",
		},
		"no children yet": {"intro", "intro\n- [ ] #9 New"},
	} {
		if got := appendEpicChild(tc.body, item, ""); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestAppendEpicChildUsesConfiguredSection(t *testing.T) {
	const body = "## 驗收\n- [ ] #12 Evidence\n\n## 子議題\n- [ ] #1 First\n\n## 備註\n- [ ] #13 Follow up"
	const want = "## 驗收\n- [ ] #12 Evidence\n\n## 子議題\n- [ ] #1 First\n- [ ] #9 New\n\n## 備註\n- [ ] #13 Follow up"
	if got := appendEpicChild(body, "- [ ] #9 New", "子議題"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
