package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/ghissue"
)

func TestReconcileCriteriaKeepsIDs(t *testing.T) {
	cur := []acceptCriterion{{"item-1", "alpha"}, {"item-2", "beta"}, {"item-3", "gamma"}}
	fresh := []acceptCriterion{{"item-1", "beta"}, {"item-2", "ALPHA"}, {"item-9", "delta"}, {"item-4", "epsilon"}}
	merged, diff := reconcileCriteria(cur, fresh)
	if merged[0].ID != "item-2" || merged[1].ID != "item-1" {
		t.Fatalf("unchanged text keeps its ID: %+v", merged)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].ID != "item-3" || diff.Changed[0].Text != "delta" {
		t.Fatalf("diff = %+v", diff)
	}
	if len(diff.Added) != 1 || diff.Added[0].Text != "epsilon" || diff.Added[0].ID != "item-4" {
		t.Fatalf("added = %+v", diff.Added)
	}
	_, diff = reconcileCriteria(cur, cur[:2])
	if len(diff.Removed) != 1 || diff.Removed[0].ID != "item-3" {
		t.Fatalf("removed = %+v", diff.Removed)
	}
	if _, diff = reconcileCriteria(cur, cur); !diff.empty() {
		t.Fatalf("same list must have no diff: %+v", diff)
	}
}

func TestParseAcceptanceReportRejectsUnknownAndDuplicate(t *testing.T) {
	known := func(id string) bool { return id == "item-1" || id == "item-2" }
	wrap := func(body string) string { return "done\n```acceptance-report\n" + body + "\n```\n" }
	ev := `[{"type":"commit","ref":"abc","paths":["a.go"]}]`
	cases := map[string]string{
		"unknown":    `{"items":[{"id":"item-9","status":"evidence_submitted","evidence":` + ev + `}]}`,
		"duplicate":  `{"items":[{"id":"item-1","status":"evidence_submitted","evidence":` + ev + `},{"id":"item-1","status":"needs_evidence","note":"x"}]}`,
		"noevidence": `{"items":[{"id":"item-1","status":"evidence_submitted"}]}`,
		"noblocker":  `{"items":[{"id":"item-1","status":"awaiting_external","blocker":{"missing":"grade"}}]}`,
		"badjson":    `{"items":`,
	}
	for name, body := range cases {
		items, errs, found := parseAcceptanceReport(wrap(body), known)
		if !found || len(errs) == 0 || items != nil {
			t.Errorf("%s: items=%v errs=%v found=%v", name, items, errs, found)
		}
	}
	items, errs, found := parseAcceptanceReport(wrap(`{"items":[{"id":"ITEM-1","status":"evidence_submitted","evidence":`+ev+`},{"id":"item-2","status":"awaiting_external","blocker":{"missing":"grade","owner":"teacher"}}]}`), known)
	if !found || len(errs) != 0 || len(items) != 2 || items[0].ID != "item-1" {
		t.Fatalf("valid report: %v %v %v", items, errs, found)
	}
	if _, _, found := parseAcceptanceReport("Completed items: item-1", known); found {
		t.Fatal("a message without a block has no report")
	}
}

func TestFoldAcceptanceReopensOnCriteriaAndEvidenceChange(t *testing.T) {
	var entries []acceptLogEntry
	add := func(kind, actor string, run int, payload any) {
		raw, _ := json.Marshal(payload)
		entries = append(entries, acceptLogEntry{ID: int64(len(entries) + 1), Kind: kind, Actor: actor, RunNo: run, Payload: raw})
	}
	ev := func(ref string) []acceptEvidence { return []acceptEvidence{{Type: acceptEvidenceCommit, Ref: ref}} }
	add(acceptKindCriteria, "system", 0, acceptCriteriaEntry{Version: 1, Items: []acceptCriterion{{"item-1", "a"}, {"item-2", "b"}}})
	add(acceptKindReport, "agent", 1, acceptReport{Run: 1, Source: "agent", Items: []acceptReportItem{{ID: "item-1", Status: acceptReportEvidence, Evidence: ev("c1")}}})
	add(acceptKindDecision, "me", 0, acceptDecision{ItemID: "item-1", Decision: acceptDecisionApproved})
	st := foldAcceptance(entries)
	if a, n := st.Counts(); a != 1 || n != 2 {
		t.Fatalf("counts = %d/%d", a, n)
	}
	if it, _ := st.item("item-2"); it.Report != acceptReportNone {
		t.Fatalf("an unreported item reads %q", it.Report)
	}
	// Same evidence again: the approval stands.
	add(acceptKindReport, "agent", 2, acceptReport{Run: 2, Source: "agent", Items: []acceptReportItem{{ID: "item-1", Status: acceptReportEvidence, Evidence: ev("c1")}}})
	if it, _ := foldAcceptance(entries).item("item-1"); it.Decision != acceptDecisionApproved {
		t.Fatal("identical evidence must not reopen")
	}
	// Different evidence reopens it.
	add(acceptKindReport, "agent", 3, acceptReport{Run: 3, Source: "agent", Items: []acceptReportItem{{ID: "item-1", Status: acceptReportEvidence, Evidence: ev("c2")}}})
	it, _ := foldAcceptance(entries).item("item-1")
	if it.Decision != acceptDecisionPending || it.Reopened != acceptReopenEvidence {
		t.Fatalf("changed evidence: %+v", it)
	}
	add(acceptKindDecision, "me", 0, acceptDecision{ItemID: "item-1", Decision: acceptDecisionApproved})
	add(acceptKindCriteria, "me", 0, acceptCriteriaEntry{Version: 2, Items: []acceptCriterion{{"item-1", "a changed"}, {"item-2", "b"}}})
	st = foldAcceptance(entries)
	it, _ = st.item("item-1")
	if it.Decision != acceptDecisionPending || it.Reopened != acceptReopenCriteria || st.Version != 2 || len(it.History) < 4 {
		t.Fatalf("changed criteria: %+v", it)
	}
	if g := evaluateAcceptGate(st, true); g.OK || g.Code != acceptGateItemsOpen {
		t.Fatalf("gate = %+v", g)
	}
	if g := evaluateAcceptGate(acceptState{}, false); !g.OK {
		t.Fatal("a task without criteria is not gated")
	}
}

// ── end to end ──────────────────────────────────────────────────────────────

type acceptEnv struct {
	*webFleetEnv
	host  *fakeIssueHost
	agent string
}

func newAcceptEnv(t *testing.T, verify string) *acceptEnv {
	t.Helper()
	e := newWebFleetEnv(t)
	host := newFakeIssueHost()
	e.svc.IssueSync.Host = host
	e.svc.Commander.Issues.Host = shipAcceptanceIssueHost{host}
	e.svc.Commander.Issues.NoChecklistSync = true
	for _, pair := range [][2]string{{"GIT_AUTHOR_NAME", "T"}, {"GIT_AUTHOR_EMAIL", "t@example.test"}, {"GIT_COMMITTER_NAME", "T"}, {"GIT_COMMITTER_EMAIL", "t@example.test"}} {
		t.Setenv(pair[0], pair[1])
	}
	body := `{"trust_mode":"local-only","max_concurrent":1,"verify_commands":[` + verify + `]}`
	if code, out := e.call(http.MethodPut, "/api/fleet/projects/"+e.project.ID, body, true); code != http.StatusOK {
		t.Fatalf("update project: %d %v", code, out)
	}
	return &acceptEnv{webFleetEnv: e, host: host, agent: filepath.Join(t.TempDir(), "agent")}
}

func (e *acceptEnv) setIssue(n int, items ...string) {
	var sb strings.Builder
	sb.WriteString("Intro\n\n## 驗收條件\n\n")
	for _, it := range items {
		sb.WriteString("- [ ] " + it + "\n")
	}
	e.host.issues[n] = &ghissue.IssueContext{Number: n, State: "OPEN", Title: "Issue", Body: sb.String(), Checklist: ghissue.ExtractChecklist(sb.String())}
}

// agentSays installs an agent that writes files and ends with the given text.
func (e *acceptEnv) agentSays(files map[string]string, text string) {
	e.t.Helper()
	line, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}})
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\nset -eu\n")
	for name, content := range files {
		sb.WriteString(fmt.Sprintf("printf '%%s\\n' %q > %s\n", content, name))
	}
	sb.WriteString("cat <<'JSONEOF'\n" + string(line) + "\nJSONEOF\n")
	sb.WriteString(`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s1"}'` + "\n")
	if err := os.WriteFile(e.agent, []byte(sb.String()), 0o700); err != nil {
		e.t.Fatal(err)
	}
	e.svc.Runner.Binary = e.agent
}

func (e *acceptEnv) run() {
	e.t.Helper()
	if _, err := e.svc.Scheduler.Tick(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	e.svc.Scheduler.Wait()
}

func (e *acceptEnv) dispatch(issue int) string {
	e.t.Helper()
	code, out := e.call(http.MethodPost, "/api/fleet/tasks", fmt.Sprintf(`{"project_id":%q,"issue":%d,"shape":"ship"}`, e.project.ID, issue), true)
	if code != http.StatusOK {
		e.t.Fatalf("dispatch: %d %v", code, out)
	}
	return out["id"].(string)
}

func (e *acceptEnv) post(path, body string, want int) map[string]any {
	e.t.Helper()
	code, out := e.call(http.MethodPost, path, body, true)
	if code != want {
		e.t.Fatalf("POST %s %s: %d %v, want %d", path, body, code, out, want)
	}
	return out
}

func acceptItemOf(t *testing.T, d map[string]any, id string) map[string]any {
	t.Helper()
	for _, it := range list(t, d, "acceptance_items") {
		if it["id"] == id {
			return it
		}
	}
	t.Fatalf("no item %s in %v", id, d["acceptance_items"])
	return nil
}

func reportBlock(body string) string {
	return "Done.\n\n```acceptance-report\n" + body + "\n```\n"
}

// A partly evidenced task with an item waiting on a person outside, as in
// goll #70: nothing closes until each item is approved, evidence is asked
// for on the same task, and the outside answer is recorded.
func TestAcceptancePartialEvidenceExternalBlockerAndFollowUp(t *testing.T) {
	e := newAcceptEnv(t, `"test -f out.txt"`)
	e.setIssue(300, "Write the output file", "Grade the essays by hand", "Explain the method")
	issue := e.host.issues[300]
	a, b, c := issue.Checklist[0].ID, issue.Checklist[1].ID, issue.Checklist[2].ID

	e.agentSays(map[string]string{"out.txt": "x"}, reportBlock(fmt.Sprintf(`{"items":[
	 {"id":%q,"status":"evidence_submitted","evidence":[{"type":"file","ref":"out.txt","paths":["out.txt"]},{"type":"test","ref":"test -f out.txt","note":"ok"}]},
	 {"id":%q,"status":"awaiting_external","blocker":{"missing":"human grade of the essays","owner":"course teacher"}}]}`, a, b)))
	id := e.dispatch(300)
	task, _ := e.svc.Store.GetTask(context.Background(), id)
	if !strings.Contains(task.Goal, "acceptance-report") {
		t.Fatal("the agent must be told how to report")
	}
	e.run()
	path := "/api/fleet/tasks/" + id
	d := e.get(path + "/delivery")
	if d["status"] != "review_ready" {
		t.Fatalf("status = %v (%v)", d["status"], d["stop_reason"])
	}
	sum := d["acceptance"].(map[string]any)
	if sum["structured"] != true || sum["total"].(float64) != 3 || sum["approved"].(float64) != 0 || sum["gate_ok"] != false {
		t.Fatalf("summary = %v", sum)
	}
	if it := acceptItemOf(t, d, a); it["report"] != "evidence_submitted" || len(it["evidence"].([]any)) != 2 {
		t.Fatalf("item a = %v", it)
	}
	if it := acceptItemOf(t, d, b); it["report"] != "awaiting_external" || it["blocker"].(map[string]any)["owner"] != "course teacher" {
		t.Fatalf("item b = %v", it)
	}
	if it := acceptItemOf(t, d, c); it["report"] != "not_reported" {
		t.Fatalf("an item left out reads not_reported: %v", it)
	}

	// Closing is refused; approving an item with no evidence is too.
	e.post(path+"/approve", "{}", http.StatusConflict)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved"}`, c), http.StatusBadRequest)
	e.post(path+"/acceptance/decide", `{"item_id":"item-99","decision":"approved"}`, http.StatusBadRequest)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"needs_evidence"}`, c), http.StatusBadRequest) // note required

	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved","note":"ok"}`, a), http.StatusOK)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"awaiting_external","note":"wait for the teacher"}`, b), http.StatusOK)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"needs_evidence","note":"add the method write-up"}`, c), http.StatusOK)
	d = e.get(path + "/delivery")
	if d["acceptance"].(map[string]any)["external_wait"] != true || d["acceptance"].(map[string]any)["approved"].(float64) != 1 {
		t.Fatalf("summary = %v", d["acceptance"])
	}
	if !e.svc.AutoRerunPaused(context.Background(), id) {
		t.Fatal("automatic reruns must pause while waiting for an outside answer")
	}

	// Ask for evidence: same task, only item c, and a is left alone.
	e.agentSays(map[string]string{"method.md": "m"}, reportBlock(fmt.Sprintf(`{"items":[{"id":%q,"status":"evidence_submitted","evidence":[{"type":"report","ref":"method.md"}]}]}`, c)))
	e.post(path+"/acceptance/request-evidence", "{}", http.StatusOK)
	e.svc.Scheduler.Wait()
	d = e.get(path + "/delivery")
	if d["status"] != "review_ready" {
		t.Fatalf("after follow-up: %v %v", d["status"], d["stop_reason"])
	}
	if it := acceptItemOf(t, d, c); it["report"] != "evidence_submitted" || it["decision"] != "pending" || it["round"].(float64) != 2 {
		t.Fatalf("item c after follow-up = %v", it)
	}
	if it := acceptItemOf(t, d, a); it["decision"] != "approved" {
		t.Fatalf("approved item must stay approved: %v", it)
	}
	if it := acceptItemOf(t, d, b); it["decision"] != "awaiting_external" {
		t.Fatalf("item b = %v", it)
	}
	if list(t, d, "runs") == nil || len(list(t, d, "runs")) != 2 {
		t.Fatalf("runs = %v", d["runs"])
	}

	// The outside reply is filled in by hand, then reviewed.
	e.post(path+"/acceptance/entry", fmt.Sprintf(`{"items":[{"id":%q,"status":"evidence_submitted","evidence":[{"type":"report","ref":"teacher-grades.pdf","note":"graded by the teacher"}]}]}`, b), http.StatusOK)
	e.post(path+"/acceptance/entry", fmt.Sprintf(`{"items":[{"id":%q,"status":"evidence_submitted"}]}`, b), http.StatusBadRequest)
	if e.svc.AutoRerunPaused(context.Background(), id) {
		t.Fatal("the reply is in; reruns may resume")
	}
	e.post(path+"/approve", "{}", http.StatusConflict)
	for _, it := range []string{b, c} {
		e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved"}`, it), http.StatusOK)
	}
	d = e.get(path + "/delivery")
	if d["acceptance"].(map[string]any)["gate_ok"] != true {
		t.Fatalf("gate = %v", d["acceptance"])
	}
	out := e.post(path+"/approve", "{}", http.StatusOK)
	if out["status"] != "done" {
		t.Fatalf("approve = %v", out)
	}
}

// A task that changed nothing, as in goll #71: the evidence is an existing
// PR and a research report; no empty commit or PR is needed to review it.
func TestAcceptanceZeroChangeEvidenceDelivery(t *testing.T) {
	e := newAcceptEnv(t, "")
	e.setIssue(301, "Confirm the existing PR covers the change", "Confirm the external terms")
	cl := e.host.issues[301].Checklist
	a, b := cl[0].ID, cl[1].ID
	e.agentSays(nil, reportBlock(fmt.Sprintf(`{"items":[
	 {"id":%q,"status":"evidence_submitted","evidence":[{"type":"pr","ref":"https://example.test/pull/12"},{"type":"commit","ref":"deadbeef","paths":["a.go"]}]},
	 {"id":%q,"status":"awaiting_external","blocker":{"missing":"written confirmation of the terms","owner":"legal"}}]}`, a, b)))
	id := e.dispatch(301)
	e.run()
	path := "/api/fleet/tasks/" + id
	d := e.get(path + "/delivery")
	if d["status"] != "needs_decision" {
		t.Fatalf("nothing to deliver stops at needs_decision: %v %v", d["status"], d["stop_reason"])
	}
	head := func() string {
		out, _ := exec.Command("git", "-C", e.project.RepoPath, "rev-parse", "HEAD").Output()
		return string(out)
	}
	before := head()

	// Sending it to review skips verification and approves nothing.
	out := e.post(path+"/pass", "{}", http.StatusOK)
	if out["status"] != "review_ready" {
		t.Fatalf("pass = %v", out)
	}
	d = e.get(path + "/delivery")
	sum := d["acceptance"].(map[string]any)
	if sum["verification_skipped"] != true || sum["approved"].(float64) != 0 || sum["evidence_only"] != true {
		t.Fatalf("summary = %v", sum)
	}
	e.post(path+"/approve", "{}", http.StatusConflict)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved"}`, a), http.StatusOK)
	e.post(path+"/approve", "{}", http.StatusConflict)

	// The terms are confirmed outside; record it and close on evidence alone.
	e.post(path+"/acceptance/entry", fmt.Sprintf(`{"items":[{"id":%q,"status":"evidence_submitted","evidence":[{"type":"report","ref":"terms-confirmation.md","note":"legal replied"}]}]}`, b), http.StatusOK)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved"}`, b), http.StatusOK)
	if out := e.post(path+"/approve", "{}", http.StatusOK); out["status"] != "done" {
		t.Fatalf("approve = %v", out)
	}
	if head() != before {
		t.Fatal("closing on evidence must not create a commit")
	}
}

func TestAcceptanceRejectedReportAndCriteriaUpdate(t *testing.T) {
	e := newAcceptEnv(t, "")
	e.setIssue(302, "First thing", "Second thing")
	cl := e.host.issues[302].Checklist
	a, b := cl[0].ID, cl[1].ID
	// A report naming an unknown ID and repeating one takes no effect.
	e.agentSays(map[string]string{"f.txt": "1"}, reportBlock(fmt.Sprintf(`{"items":[
	 {"id":%q,"status":"evidence_submitted","evidence":[{"type":"file","ref":"f.txt"}]},
	 {"id":%q,"status":"needs_evidence","note":"x"},{"id":%q,"status":"needs_evidence","note":"y"},{"id":"item-77","status":"needs_evidence","note":"z"}]}`, a, b, b)))
	id := e.dispatch(302)
	e.run()
	path := "/api/fleet/tasks/" + id
	d := e.get(path + "/delivery")
	if d["status"] != "review_ready" && d["status"] != "needs_decision" {
		t.Fatalf("status = %v", d["status"])
	}
	sum := d["acceptance"].(map[string]any)
	if len(sum["rejected_reports"].([]any)) != 1 {
		t.Fatalf("rejected = %v", sum["rejected_reports"])
	}
	if it := acceptItemOf(t, d, a); it["report"] != "not_reported" {
		t.Fatalf("a rejected report reports nothing: %v", it)
	}

	// The reviewer records evidence by hand and approves item a.
	if d["status"] == "needs_decision" {
		e.post(path+"/pass", "{}", http.StatusOK)
	}
	e.post(path+"/acceptance/entry", fmt.Sprintf(`{"items":[{"id":%q,"status":"evidence_submitted","evidence":[{"type":"file","ref":"f.txt"}]}]}`, a), http.StatusOK)
	e.post(path+"/acceptance/decide", fmt.Sprintf(`{"item_id":%q,"decision":"approved"}`, a), http.StatusOK)

	// The issue is edited: nothing changes until the reviewer applies it.
	e.setIssue(302, "First thing, now stricter", "Second thing", "A third thing")
	out := e.post(path+"/acceptance/criteria", `{"action":"refresh"}`, http.StatusOK)
	prop := out["acceptance"].(map[string]any)["proposal"].(map[string]any)
	if len(prop["changed"].([]any)) != 1 || len(prop["added"].([]any)) != 1 || prop["version"].(float64) != 2 {
		t.Fatalf("proposal = %v", prop)
	}
	if it := acceptItemOf(t, out, a); it["decision"] != "approved" || out["acceptance"].(map[string]any)["version"].(float64) != 1 {
		t.Fatalf("an unapplied update changes nothing: %v", it)
	}
	out = e.post(path+"/acceptance/criteria", `{"action":"apply"}`, http.StatusOK)
	sum = out["acceptance"].(map[string]any)
	if sum["version"].(float64) != 2 || sum["total"].(float64) != 3 || sum["proposal"] != nil {
		t.Fatalf("after apply = %v", sum)
	}
	if it := acceptItemOf(t, out, a); it["decision"] != "pending" || it["reopened"] != "criteria_changed" {
		t.Fatalf("an approved item whose criterion changed goes back to review: %v", it)
	}
	e.post(path+"/acceptance/criteria", `{"action":"apply"}`, http.StatusConflict)

	e.setIssue(302, "First thing, now stricter", "Second thing", "A third thing", "Fourth")
	e.post(path+"/acceptance/criteria", `{"action":"refresh"}`, http.StatusOK)
	out = e.post(path+"/acceptance/criteria", `{"action":"dismiss"}`, http.StatusOK)
	if out["acceptance"].(map[string]any)["total"].(float64) != 3 || out["acceptance"].(map[string]any)["proposal"] != nil {
		t.Fatalf("dismiss = %v", out["acceptance"])
	}
}

// A task from before per-item records keeps its note, shows no items, is not
// gated, and can be filled in by the reviewer.
func TestAcceptanceLegacyTaskCompatibility(t *testing.T) {
	e := newAcceptEnv(t, "")
	ctx := context.Background()
	task, err := e.svc.Store.CreateTask(ctx, fleet.NewTask{ProjectID: e.project.ID, Shape: fleet.ShapeShip, Goal: "old task"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/fleet/tasks/" + task.ID
	d := e.get(path + "/delivery")
	sum := d["acceptance"].(map[string]any)
	if sum["structured"] != false || len(list(t, d, "acceptance_items")) != 0 || d["note_missing"] != true {
		t.Fatalf("legacy delivery = %v", d)
	}
	if g, _, err := e.svc.CheckClose(ctx, task.ID); err != nil || !g.OK {
		t.Fatalf("legacy tasks are not gated: %+v %v", g, err)
	}
	e.post(path+"/acceptance/decide", `{"item_id":"item-1","decision":"approved"}`, http.StatusBadRequest)
	e.post(path+"/acceptance/entry", `{"items":[{"id":"item-1","status":"needs_evidence","note":"n"}]}`, http.StatusBadRequest)
	out := e.post(path+"/acceptance/entry", `{"criteria":[{"id":"item-1","text":"Do the old thing"}],"items":[{"id":"item-1","status":"evidence_submitted","evidence":[{"type":"commit","ref":"abc123"}]}]}`, http.StatusOK)
	if out["acceptance"].(map[string]any)["structured"] != true || acceptItemOf(t, out, "item-1")["report"] != "evidence_submitted" {
		t.Fatalf("manual entry = %v", out)
	}
}
