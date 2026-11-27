package app

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
)

// fleetAllowAll allows every command, for tests about which buttons a
// message offers before the task's state is taken into account.
func fleetAllowAll() fleetActionSet {
	return fleetActionSet{Allowed: []string{fleetOpCancel, fleetOpRetry, fleetOpReply, fleetOpApprove, fleetOpMerge, fleetOpPass, fleetOpDiscard}}
}

// acceptStateOf builds a structured acceptance state with one item per
// decision given.
func acceptStateOf(decisions ...string) acceptState {
	st := acceptState{Structured: true, ReportedRuns: map[int]bool{}}
	for i, d := range decisions {
		id := fmt.Sprintf("item-%d", i+1)
		st.Items = append(st.Items, acceptItem{ID: id, Decision: d})
	}
	return st
}

func TestFleetTaskActionsRules(t *testing.T) {
	ship := func(status fleet.Status, mode fleet.TrustMode, pr int) fleet.Task {
		return fleet.Task{Status: status, Shape: fleet.ShapeShip, TrustMode: mode, PRNumber: pr, PRURL: map[bool]string{true: "https://example.test/pull/1"}[pr > 0]}
	}
	approved := acceptStateOf(acceptDecisionApproved, acceptDecisionApproved)
	cases := []struct {
		name    string
		in      fleetActionInput
		allowed string
		blocked string // action:reason, space separated
	}{
		{"queued", fleetActionInput{Task: ship(fleet.StatusQueued, fleet.TrustLocalOnly, 0)}, "cancel", ""},
		{"running", fleetActionInput{Task: ship(fleet.StatusRunning, fleet.TrustLocalOnly, 0)}, "cancel", ""},
		{"local-only legacy task approves", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustLocalOnly, 0)}, "approve reply discard", ""},
		{"local-only every item approved", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustLocalOnly, 0), Accept: approved}, "approve reply discard", ""},
		{"local-only items open (goll #70)", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustLocalOnly, 0), Accept: acceptStateOf(acceptDecisionApproved, acceptDecisionPending, acceptDecisionExternal)},
			"reply discard", "approve:items_not_approved"},
		{"no-mistakes without a PR", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustNoMistakes, 0)}, "reply discard", "approve:no_pull_request"},
		{"no-mistakes with a PR", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustNoMistakes, 4)}, "approve reply discard", ""},
		{"direct-pr waved through without a PR", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustDirectPR, 0), Accept: approved}, "reply discard", "approve:no_pull_request"},
		{"direct-pr evidence without a PR (goll #71)", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustDirectPR, 0), Accept: approved, EvidenceOnly: true}, "finish_evidence reply discard", "approve:no_pull_request"},
		{"direct-pr evidence with an item open", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustDirectPR, 0), Accept: acceptStateOf(acceptDecisionApproved, acceptDecisionExternal), EvidenceOnly: true},
			"reply discard", "approve:no_pull_request finish_evidence:items_not_approved"},
		{"review direct-pr with an open PR and unchecked criteria", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustDirectPR, 4), Accept: acceptStateOf(acceptDecisionPending)},
			"reply discard", "approve:no_approval_step merge:items_not_approved"},
		{"review direct-pr with an approved PR", fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustDirectPR, 4), Accept: approved},
			"merge reply discard", "approve:no_approval_step"},
		{"scout", fleetActionInput{Task: fleet.Task{Status: fleet.StatusReviewReady, Shape: fleet.ShapeScout}}, "reply discard", "approve:not_ship"},
		{"needs decision", fleetActionInput{Task: ship(fleet.StatusNeedsDecision, fleet.TrustLocalOnly, 0)}, "reply retry pass discard", ""},
		{"needs decision awaiting external", fleetActionInput{Task: ship(fleet.StatusNeedsDecision, fleet.TrustLocalOnly, 0), Accept: acceptStateOf(acceptDecisionExternal)},
			"reply pass discard", "retry:awaiting_external"},
		{"failed awaiting external", fleetActionInput{Task: ship(fleet.StatusFailed, fleet.TrustLocalOnly, 0), Accept: acceptStateOf(acceptDecisionExternal)}, "reply discard", "retry:awaiting_external"},
		{"done direct-pr open", fleetActionInput{Task: ship(fleet.StatusDone, fleet.TrustDirectPR, 4)}, "merge", ""},
		{"done direct-pr items open", fleetActionInput{Task: ship(fleet.StatusDone, fleet.TrustDirectPR, 4), Accept: acceptStateOf(acceptDecisionPending)}, "", "merge:items_not_approved"},
		{"done direct-pr merged", fleetActionInput{Task: ship(fleet.StatusDone, fleet.TrustDirectPR, 4), Landed: true}, "", "merge:already_landed"},
		{"done local-only", fleetActionInput{Task: ship(fleet.StatusDone, fleet.TrustLocalOnly, 0)}, "", ""},
		{"cancelled", fleetActionInput{Task: ship(fleet.StatusCancelled, fleet.TrustLocalOnly, 0)}, "discard", ""},
	}
	for _, c := range cases {
		set := fleetTaskActions(c.in)
		if got := strings.Join(set.Allowed, " "); got != c.allowed {
			t.Errorf("%s: allowed = %q, want %q", c.name, got, c.allowed)
		}
		var blocked []string
		for _, b := range set.Blocked {
			blocked = append(blocked, b.Action+":"+b.Reason)
		}
		if got := strings.Join(blocked, " "); got != c.blocked {
			t.Errorf("%s: blocked = %q, want %q", c.name, got, c.blocked)
		}
	}
	// The open items are named.
	set := fleetTaskActions(fleetActionInput{Task: ship(fleet.StatusReviewReady, fleet.TrustLocalOnly, 0), Accept: acceptStateOf(acceptDecisionApproved, acceptDecisionPending, acceptDecisionNeeds)})
	if b := set.Why(fleetOpApprove); strings.Join(b.Items, ",") != "item-2,item-3" {
		t.Fatalf("open items = %v", b.Items)
	}
	// Anything the status never offers is blocked by the status.
	if b := set.Why(fleetOpMerge); b.Reason != fleetWhyStatus {
		t.Fatalf("merge on review_ready = %+v", b)
	}
}

func TestFleetFilterButtonsKeepsLinksAndDropsEmptyRows(t *testing.T) {
	id := "0123456789ab"
	rows := [][]fleetButton{
		{{Data: fleetCallbackData(fleetActApprove, id)}, {Data: fleetCallbackData(fleetActPromptDiscard, id)}},
		{{Data: fleetCallbackData(fleetActRetry, id)}, {Data: fleetCallbackData(fleetActHint, id)}},
		{{URL: "http://localhost/x"}},
	}
	set := fleetActionSet{Allowed: []string{fleetOpDiscard}}
	got := buttonData(fleetFilterButtons(rows, set))
	if strings.Join(got, " ") != "fleet:pdiscard:"+id+" url:http://localhost/x" {
		t.Fatalf("buttons = %v", got)
	}
}

// A direct-pr task waved through by hand that never got a pull request, as
// with goll #70 and #71: neither the dashboard nor the API offers an
// approval that cannot happen, the evidence can still be reviewed and more
// asked for, and the refusal says why.
func TestFleetDirectPRWithoutPullRequestOffersNoApproval(t *testing.T) {
	e := newAcceptEnv(t, `"false"`)
	setFleetTrustMode(t, e.svc, e.project.ID, fleet.TrustDirectPR)
	e.setIssue(310, "Write the output file", "Grade the essays by hand")
	cl := e.host.issues[310].Checklist
	a, b := cl[0].ID, cl[1].ID
	e.agentSays(map[string]string{"out.txt": "x"}, reportBlock(fmt.Sprintf(`{"items":[
	 {"id":%q,"status":"evidence_submitted","evidence":[{"type":"file","ref":"out.txt","paths":["out.txt"]}]},
	 {"id":%q,"status":"awaiting_external","blocker":{"missing":"human grade","owner":"course teacher"}}]}`, a, b)))
	id := e.dispatch(310)
	e.run()
	ctx := context.Background()
	path := "/api/fleet/tasks/" + id
	task := e.get(path)
	if task["status"] != "needs_decision" || task["trust_mode"] != "direct-pr" {
		t.Fatalf("task = %v %v (%v)", task["status"], task["trust_mode"], task["status_reason"])
	}
	// Waiting on the teacher holds back a rerun, and the API says so.
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"awaiting_external","note":"teacher"}`, b), http.StatusOK)
	task = e.get(path)
	if strings.Contains(fmt.Sprint(task["actions"]), "retry") {
		t.Fatalf("actions while awaiting external = %v", task["actions"])
	}
	out := e.post(path+"/retry", "{}", http.StatusConflict)
	if out["code"] != "action_unavailable" || out["blocked"].(map[string]any)["reason"] != fleetWhyAwaitingExternal {
		t.Fatalf("retry refusal = %v", out)
	}

	// Waved through by hand; the pull request was never opened.
	if _, err := e.svc.Store.Transition(ctx, id, fleet.StatusReviewReady, "waved through by test"); err != nil {
		t.Fatal(err)
	}
	task = e.get(path)
	if got := fmt.Sprint(task["actions"]); got != "[reply discard]" {
		t.Fatalf("actions = %v", got)
	}
	blocked := list(t, task, "blocked_actions")
	if len(blocked) != 1 || blocked[0]["action"] != "approve" || blocked[0]["reason"] != fleetWhyNoPullRequest {
		t.Fatalf("blocked = %v", blocked)
	}
	// The listing carries the same.
	listed := list(t, e.get("/api/fleet/tasks"), "tasks")
	if len(listed) == 0 || fmt.Sprint(listed[0]["actions"]) != "[reply discard]" {
		t.Fatalf("listed = %v", listed)
	}

	// Approving and merging are refused with the reason, and change nothing.
	out = e.post(path+"/approve", "{}", http.StatusConflict)
	if out["code"] != "action_unavailable" || out["blocked"].(map[string]any)["reason"] != fleetWhyNoPullRequest {
		t.Fatalf("approve refusal = %v", out)
	}
	e.post(path+"/merge", "{}", http.StatusConflict)
	if got, _ := e.svc.Store.GetTask(ctx, id); got.Status != fleet.StatusReviewReady {
		t.Fatalf("status = %s", got.Status)
	}
	// Evidence is still reviewed item by item.
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved","note":"ok"}`, a), http.StatusOK)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"needs_evidence","note":"attach the grades"}`, b), http.StatusOK)
	if task = e.get(path); fmt.Sprint(task["actions"]) != "[reply discard]" {
		t.Fatalf("actions after review = %v", task["actions"])
	}
}

// The lawful path still approves: a local-only task whose items are all
// approved shows the button and lands.
func TestFleetApproveOfferedOnceItemsApproved(t *testing.T) {
	e := newAcceptEnv(t, `"test -f out.txt"`)
	e.setIssue(311, "Write the output file")
	a := e.host.issues[311].Checklist[0].ID
	e.agentSays(map[string]string{"out.txt": "x"}, reportBlock(fmt.Sprintf(`{"items":[{"id":%q,"status":"evidence_submitted","evidence":[{"type":"file","ref":"out.txt","paths":["out.txt"]}]}]}`, a)))
	id := e.dispatch(311)
	e.run()
	path := "/api/fleet/tasks/" + id
	task := e.get(path)
	if fmt.Sprint(task["actions"]) != "[reply discard]" {
		t.Fatalf("before approval: %v %v", task["status"], task["actions"])
	}
	if b := list(t, task, "blocked_actions"); len(b) != 1 || b[0]["reason"] != fleetWhyItemsOpen || fmt.Sprint(b[0]["items"]) != "["+a+"]" {
		t.Fatalf("blocked = %v", b)
	}
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved"}`, a), http.StatusOK)
	if task = e.get(path); fmt.Sprint(task["actions"]) != "[approve reply discard]" {
		t.Fatalf("after approval: %v", task["actions"])
	}
	if out := e.post(path+"/approve", "{}", http.StatusOK); out["status"] != "done" {
		t.Fatalf("approve = %v", out)
	}
}

// A button left on an old card for a command the task no longer allows is
// answered with the reason, and the card is redrawn without it.
func TestFleetStaleTelegramButtonSaysWhyAndRedrawsCard(t *testing.T) {
	e := newCardEnv(t)
	var err error
	e.bot.i18n, err = NewI18nManager("../../locales", "en")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	setFleetTrustMode(t, e.svc, e.project.ID, fleet.TrustNoMistakes)
	task := e.dispatch("/ship fix it")
	// The card offered approval for a no-mistakes task that never got its
	// pull request, so approving cannot happen.
	for _, s := range []fleet.Status{fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady} {
		if _, err := e.svc.Store.Transition(ctx, task.ID, s, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := e.svc.Store.GetTask(ctx, task.ID); got.TrustMode != fleet.TrustNoMistakes {
		t.Fatalf("trust mode = %s", got.TrustMode)
	}
	_, before := e.msgr.counts()
	got := e.press(fleetActApprove, task.ID)
	if !strings.Contains(got, e.bot.fleetMsg(e.key.chatID, "fleet_why_no_pull_request", nil)) {
		t.Fatalf("answer = %q", got)
	}
	audit, err := e.svc.Commander.Audit(ctx, fleet.AuditFilter{TaskID: task.ID})
	if err != nil || len(audit) == 0 || audit[0].Command != fleet.CmdApprove || audit[0].OK || audit[0].Via != "telegram" {
		t.Fatalf("stale Telegram refusal audit=%+v err=%v", audit, err)
	}
	_, after := e.msgr.counts()
	if after <= before {
		t.Fatal("the card must be redrawn")
	}
	e.msgr.mu.Lock()
	last := e.msgr.edits[len(e.msgr.edits)-1]
	e.msgr.mu.Unlock()
	if !strings.Contains(last.Text, "fleet_card_blocked") {
		t.Fatalf("the card must say why it cannot be approved: %s", last.Text)
	}
	if data := strings.Join(buttonData(last.Buttons), " "); strings.Contains(data, "fleet:approve:") || !strings.Contains(data, "fleet:discard:") {
		t.Fatalf("redrawn buttons = %s", data)
	}
	if got, _ := e.svc.Store.GetTask(ctx, task.ID); got.Status != fleet.StatusReviewReady {
		t.Fatalf("status = %s", got.Status)
	}
}

func setFleetTrustMode(t *testing.T, svc *FleetService, projectID string, mode fleet.TrustMode) {
	t.Helper()
	project, err := svc.Store.GetProject(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	// A local bare remote satisfies trust-mode validation without real GitHub.
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init remote: %s %v", out, err)
	}
	if out, err := exec.Command("git", "-C", project.RepoPath, "remote", "add", "origin", remote).CombinedOutput(); err != nil {
		t.Fatalf("add remote: %s %v", out, err)
	}
	if _, err := svc.Store.UpdateProject(context.Background(), projectID, func(p *fleet.Project) error {
		p.TrustMode = mode
		p.Remote = "origin"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A zero-change direct-pr task can finish evidence review without a PR.
func TestFleetEvidenceOnlyDirectPRFinishesWithoutPR(t *testing.T) {
	e := newAcceptEnv(t, `"false"`)
	setFleetTrustMode(t, e.svc, e.project.ID, fleet.TrustDirectPR)
	e.setIssue(312, "Confirm existing results")
	item := e.host.issues[312].Checklist[0].ID
	e.agentSays(nil, reportBlock(fmt.Sprintf(`{"items":[{"id":%q,"status":"evidence_submitted","evidence":[{"type":"report","ref":"existing-report.md"}]}]}`, item)))
	id := e.dispatch(312)
	e.run()
	path := "/api/fleet/tasks/" + id
	e.post(path+"/pass", "{}", http.StatusOK)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved"}`, item), http.StatusOK)
	task := e.get(path)
	if strings.Contains(fmt.Sprint(task["actions"]), "approve") {
		t.Fatalf("invalid approval offered: %v", task["actions"])
	}
	e.post(path+"/approve", "{}", http.StatusConflict)
	gate, evidenceOnly, err := e.svc.CheckClose(context.Background(), id)
	if err != nil || !evidenceOnly || !gate.OK {
		t.Fatalf("close gate=%+v evidenceOnly=%v err=%v", gate, evidenceOnly, err)
	}
	e.post(path+"/finish_evidence", "{}", http.StatusOK)
	if task := e.get(path); task["status"] != string(fleet.StatusDone) {
		t.Fatalf("evidence-only task did not finish: %+v", task)
	}
}
