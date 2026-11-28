package app

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/ghissue"
)

type fakeMilestones struct {
	mu     sync.Mutex
	issues map[int]milestoneIssue
	err    error
}

func (f *fakeMilestones) Issue(_ context.Context, _ string, n int) (milestoneIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return milestoneIssue{}, f.err
	}
	issue, ok := f.issues[n]
	if !ok {
		return milestoneIssue{}, errors.New("no such issue")
	}
	return issue, nil
}

func (f *fakeMilestones) MilestoneIssues(_ context.Context, _ string, milestone int) ([]milestoneIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []milestoneIssue
	for _, issue := range f.issues {
		if issue.Milestone == milestone {
			out = append(out, issue)
		}
	}
	return out, nil
}

type milestoneEnv struct {
	*cardEnv
	host   *fakeMilestones
	bodies *fakeIssueHost
}

const testMilestone = 9

// newMilestoneEnv has no running scheduler: queued tasks stay queued until a
// test moves them.
func newMilestoneEnv(t *testing.T, concurrency int) *milestoneEnv {
	t.Helper()
	e := &milestoneEnv{cardEnv: newCardEnv(t), host: &fakeMilestones{issues: map[int]milestoneIssue{}}, bodies: newFakeIssueHost()}
	e.bot.fleet = e.svc
	e.bot.fleetCards = e.cards
	e.svc.Milestones.Host = e.host
	e.svc.Commander.Issues.Host = shipAcceptanceIssueHost{e.bodies}
	previous := fleetFetchIssue
	fleetFetchIssue = func(_ context.Context, _ string, n int) (*ghissue.IssueContext, error) {
		issue, err := e.host.Issue(context.Background(), "", n)
		if err != nil {
			return nil, err
		}
		return &ghissue.IssueContext{Number: n, State: issue.State, Body: issue.Body, Checklist: []ghissue.ChecklistItem{{Text: "x"}}}, nil
	}
	t.Cleanup(func() { fleetFetchIssue = previous })
	e.bot.attachFleetMilestones(e.svc)
	project, err := e.svc.Commander.UpdateProject(context.Background(), fleetActor(1), e.project.ID, "test",
		func(p *fleet.Project) error { p.MaxConcurrent = concurrency; return nil })
	if err != nil {
		t.Fatal(err)
	}
	e.project = project
	return e
}

func (e *milestoneEnv) issue(n int, body string, change ...func(*milestoneIssue)) {
	issue := milestoneIssue{Number: n, Title: "issue " + strconv.Itoa(n), Body: body, State: "open", Milestone: testMilestone, MilestoneTitle: "M8"}
	for _, c := range change {
		c(&issue)
	}
	e.host.mu.Lock()
	e.host.issues[n] = issue
	e.host.mu.Unlock()
	e.bodies.issues[n] = &ghissue.IssueContext{Number: n, State: strings.ToUpper(issue.State), Title: issue.Title, Body: body}
}

func closedAs(reason string) func(*milestoneIssue) {
	return func(i *milestoneIssue) { i.State, i.StateReason = "closed", reason }
}

func labelled(labels ...string) func(*milestoneIssue) {
	return func(i *milestoneIssue) { i.Labels = labels }
}

func (e *milestoneEnv) auto() {
	e.t.Helper()
	project, err := e.svc.Commander.UpdateProject(context.Background(), fleetActor(1), e.project.ID, "auto",
		func(p *fleet.Project) error { p.Bindings[bindingMilestoneAuto] = bindingOn; return nil })
	if err != nil {
		e.t.Fatal(err)
	}
	e.project = project
}

func (e *milestoneEnv) ship(n int) bool {
	return e.bot.fleetMilestoneShip(context.Background(), e.key, 1, e.project, fleetDispatchArgs{Issue: n})
}

func (e *milestoneEnv) batch() milestoneBatch {
	e.t.Helper()
	b, err := e.svc.Milestones.ByMilestone(context.Background(), e.project.ID, testMilestone)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *milestoneEnv) reconcile() map[int]milestoneItem {
	e.t.Helper()
	b := e.batch()
	if _, err := e.svc.Milestones.Reconcile(context.Background(), b.ID); err != nil {
		e.t.Fatalf("Reconcile: %v", err)
	}
	return e.items()
}

func (e *milestoneEnv) items() map[int]milestoneItem {
	e.t.Helper()
	list, err := e.svc.Milestones.Items(context.Background(), e.batch().ID)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[int]milestoneItem{}
	for _, it := range list {
		out[it.Issue] = it
	}
	return out
}

// dispatched returns the issues that have tasks, one entry per task.
func (e *milestoneEnv) dispatched() []int {
	e.t.Helper()
	tasks, err := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{})
	if err != nil {
		e.t.Fatal(err)
	}
	var out []int
	for _, task := range tasks {
		out = append(out, task.Source.IssueNumber)
	}
	slices.Sort(out)
	return out
}

func (e *milestoneEnv) move(taskID string, statuses ...fleet.Status) {
	e.t.Helper()
	for _, s := range statuses {
		if _, err := e.svc.Store.Transition(context.Background(), taskID, s, "test"); err != nil {
			e.t.Fatal(err)
		}
	}
}

func wantItem(t *testing.T, items map[int]milestoneItem, issue int, state, reason string) {
	t.Helper()
	if it := items[issue]; it.State != state || it.Reason != reason {
		t.Errorf("#%d = %q/%q (%s), want %q/%q", issue, it.State, it.Reason, it.Detail, state, reason)
	}
}

func TestParseDependencies(t *testing.T) {
	cases := map[string][]int{
		"## 參考\n- 依賴:#58\n":             {58},
		"- 依賴：#72、#65":                  {72, 65},
		"依賴:#72、#66、#58":                {72, 66, 58},
		"**Depends on:** #3, #4 and #3": {3, 4},
		"Blocked by #12":                {12},
		"- [ ] 依賴:#7":                   {7},
		"see #9 for context":            nil,
		"這裡不依賴 #9":                      nil,
		"依賴: other/repo#4":              nil,
		"依賴:無":                          nil,
	}
	for body, want := range cases {
		if got := parseDependencies(body, defaultMilestoneDependencyMarkers); !slices.Equal(got, want) {
			t.Errorf("parseDependencies(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestParseMilestoneIssuePages(t *testing.T) {
	out := `[{"number":1,"state":"open","labels":[{"name":"a"}],"milestone":{"number":9,"title":"M8"}},{"number":2,"pull_request":{}}][{"number":3,"state":"closed","state_reason":"not_planned"}]`
	issues, err := parseMilestoneIssuePages([]byte(out))
	if err != nil || len(issues) != 2 || issues[0].Milestone != 9 || issues[0].Labels[0] != "a" || issues[1].StateReason != "not_planned" {
		t.Fatalf("issues = %+v, err %v", issues, err)
	}
	if _, err := parseMilestoneIssuePages([]byte(`[{"number":1}][{`)); err == nil {
		t.Fatal("a truncated page must be an error, not a shorter list")
	}
}

func TestMilestoneShipAsksOnceThenRunsTheBatch(t *testing.T) {
	e := newMilestoneEnv(t, 2)
	e.issue(70, "evaluate")
	e.issue(71, "privacy")

	if !e.ship(70) {
		t.Fatal("the command was not taken over")
	}
	batch := e.batch()
	if batch.Status != batchProposed || len(e.dispatched()) != 0 {
		t.Fatalf("batch = %+v, tasks %v; want a proposal and nothing queued", batch, e.dispatched())
	}
	last := e.msgr.sent[len(e.msgr.sent)-1]
	if len(last.Buttons) != 2 || last.Buttons[0][0].Data != fleetCallbackData(fleetActBatchGo, batch.ID) {
		t.Fatalf("confirmation = %+v", last)
	}
	// Sending the command again asks about the same batch.
	e.ship(71)
	if again := e.batch(); again.ID != batch.ID {
		t.Fatalf("a second batch appeared: %s, %s", batch.ID, again.ID)
	}

	e.bot.handleFleetBatchCallback(e.key, 1, "q", fleetActBatchGo, batch.ID)
	e.bot.handleFleetBatchCallback(e.key, 1, "q", fleetActBatchGo, batch.ID) // pressed twice
	if got := e.dispatched(); !slices.Equal(got, []int{70, 71}) {
		t.Fatalf("tasks for %v, want one each for 70 and 71", got)
	}
}

func TestMilestoneShipOnlyThisOne(t *testing.T) {
	e := newMilestoneEnv(t, 2)
	e.issue(70, "evaluate")
	e.issue(71, "privacy")
	e.ship(70)

	e.bot.handleFleetBatchCallback(e.key, 1, "q", fleetActBatchOne, e.batch().ID)
	if got := e.dispatched(); !slices.Equal(got, []int{70}) {
		t.Fatalf("tasks for %v, want only 70", got)
	}
	if e.ship(71) {
		t.Fatal("after declining, /ship must dispatch single issues without asking again")
	}
	e.auto()
	if !e.ship(71) || e.batch().Status != batchActive {
		t.Fatal("authorising the project must take the milestone up again")
	}
	if got := e.dispatched(); !slices.Equal(got, []int{70, 71}) {
		t.Fatalf("tasks for %v, want 70 reused and 71 queued", got)
	}
}

func TestMilestoneShipLeavesOrdinaryIssuesAlone(t *testing.T) {
	e := newMilestoneEnv(t, 1)
	e.issue(1, "alone", func(i *milestoneIssue) { i.Milestone = 0 })
	e.issue(2, "last one of its milestone")
	e.issue(3, "epic", labelled("epic"))
	for _, n := range []int{1, 2} {
		if e.ship(n) {
			t.Errorf("/ship #%d was taken over", n)
		}
	}
	e.host.err = errors.New("github is down")
	if e.ship(1) {
		t.Error("with GitHub unreachable the single dispatch must report the failure")
	}
}

func TestMilestoneDispatchOrderAndDispositions(t *testing.T) {
	e := newMilestoneEnv(t, 5)
	e.auto()
	e.issue(56, "done", closedAs("completed"))
	e.issue(57, "dropped", closedAs("not_planned"))
	e.issue(58, "dup", closedAs("duplicate"))
	e.issue(69, "deploy\n- 依賴:#56")
	e.issue(72, "research")
	e.issue(73, "asr\n- 依賴:#72、#56")
	e.issue(74, "tts\n- 依賴:#57")
	e.issue(75, "overview", labelled("epic"))
	e.issue(76, "waits on someone", labelled("blocked"))
	e.issue(77, "a\n依賴:#78")
	e.issue(78, "b\n依賴:#77")
	e.issue(79, "outside\n依賴:#5")
	e.issue(5, "elsewhere", func(i *milestoneIssue) { i.Milestone = 0 })

	e.ship(72)
	items := e.items()
	wantItem(t, items, 56, itemDone, "")
	wantItem(t, items, 57, itemSkipped, "not_planned")
	wantItem(t, items, 58, itemSkipped, "duplicate")
	wantItem(t, items, 69, itemActive, "")
	wantItem(t, items, 72, itemActive, "")
	wantItem(t, items, 73, itemWaiting, reasonDependency)
	wantItem(t, items, 74, itemHeld, reasonDependencyClosed)
	wantItem(t, items, 75, itemExcluded, reasonEpic)
	wantItem(t, items, 76, itemBlocked, reasonLabel)
	wantItem(t, items, 77, itemHeld, reasonCycle)
	wantItem(t, items, 78, itemHeld, reasonCycle)
	wantItem(t, items, 79, itemWaiting, reasonDependency)
	if got := e.dispatched(); !slices.Equal(got, []int{69, 72}) {
		t.Fatalf("dispatched %v, want 69 and 72", got)
	}

	// A done task does not release its dependents until the issue closes.
	e.move(items[72].TaskID, fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusDone)
	items = e.reconcile()
	wantItem(t, items, 72, itemLanded, "")
	wantItem(t, items, 73, itemWaiting, reasonDependency)

	// Merged and closed by hand: the batch catches up and moves on.
	e.issue(72, "research", closedAs("completed"))
	items = e.reconcile()
	wantItem(t, items, 72, itemDone, "")
	wantItem(t, items, 73, itemActive, "")
}

func TestMilestoneConcurrencyAndReuse(t *testing.T) {
	e := newMilestoneEnv(t, 1)
	e.auto()
	e.issue(70, "evaluate")
	e.issue(71, "privacy")
	e.issue(72, "research")
	existing, err := e.svc.Store.CreateTask(context.Background(), fleet.NewTask{ProjectID: e.project.ID, Goal: "g",
		Source: fleet.Source{Kind: fleet.SourceIssue, IssueNumber: 70}})
	if err != nil {
		t.Fatal(err)
	}

	e.ship(70)
	items := e.items()
	if items[70].TaskID != existing.ID {
		t.Fatalf("#70 task = %s, want the existing %s", items[70].TaskID, existing.ID)
	}
	wantItem(t, items, 70, itemActive, "")
	wantItem(t, items, 71, itemReady, "")
	if got := e.dispatched(); !slices.Equal(got, []int{70}) {
		t.Fatalf("dispatched %v with one slot taken", got)
	}

	// A task that waits for the owner does not stop the rest.
	e.move(existing.ID, fleet.StatusRunning, fleet.StatusNeedsDecision)
	items = e.reconcile()
	wantItem(t, items, 70, itemAttention, string(fleet.StatusNeedsDecision))
	wantItem(t, items, 71, itemActive, "")
	wantItem(t, items, 72, itemReady, "")

	// Resending and reconciling again queue nothing new.
	e.ship(70)
	e.reconcile()
	if got := e.dispatched(); !slices.Equal(got, []int{70, 71}) {
		t.Fatalf("dispatched %v, want no duplicates", got)
	}

	// A new service on the same database picks up the same batch.
	before := e.batch().ID
	restarted, err := newMilestoneBatches(e.storage.db, e.svc.Store, e.svc.Config.Milestone)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Host, restarted.dispatch, restarted.gate, restarted.isEpic = e.host, e.svc.Milestones.dispatch, e.svc.Milestones.gate, e.svc.Milestones.isEpic
	restarted.reconcileActive(context.Background())
	if got := e.dispatched(); !slices.Equal(got, []int{70, 71}) || e.batch().ID != before {
		t.Fatalf("after restart: tasks %v, batch %s (was %s)", got, e.batch().ID, before)
	}
}

func TestMilestoneCancelledTaskIsNotRedispatched(t *testing.T) {
	e := newMilestoneEnv(t, 5)
	e.auto()
	e.issue(70, "a")
	e.issue(71, "b")
	e.ship(70)
	e.move(e.items()[71].TaskID, fleet.StatusCancelled)

	items := e.reconcile()
	wantItem(t, items, 71, itemHeld, reasonCancelled)
	e.reconcile()
	if got := e.dispatched(); !slices.Equal(got, []int{70, 71}) {
		t.Fatalf("dispatched %v, want the cancelled task left alone", got)
	}
	if e.ship(71) {
		t.Fatal("an explicit /ship of a held issue must fall through to a single dispatch")
	}
}

func TestMilestoneStopsWhenTargetBranchOrReadFails(t *testing.T) {
	e := newMilestoneEnv(t, 1)
	e.auto()
	e.issue(70, "a")
	e.issue(71, "b")
	e.ship(70)

	// A failed read keeps what was known.
	e.host.err = errors.New("rate limited")
	if _, err := e.svc.Milestones.Reconcile(context.Background(), e.batch().ID); err == nil {
		t.Fatal("want the read error")
	}
	if got := e.batch().LastError; !strings.Contains(got, "rate limited") {
		t.Errorf("LastError = %q", got)
	}
	wantItem(t, e.items(), 70, itemActive, "")
	wantItem(t, e.items(), 71, itemReady, "")
	e.host.err = nil

	// The batch's target is fixed: a changed project target stops dispatch.
	first := e.items()[70].TaskID
	runGit := newGitRunner(t, e.project.RepoPath)
	runGit("branch", "other")
	if _, err := e.svc.Commander.UpdateProject(context.Background(), fleetActor(1), e.project.ID, "retarget",
		func(p *fleet.Project) error { p.DefaultBranch = "other"; return nil }); err != nil {
		t.Fatal(err)
	}
	e.move(first, fleet.StatusCancelled)
	items := e.reconcile()
	wantItem(t, items, 71, itemReady, reasonTargetChanged)

	// An issue moved out of the milestone is noted, not forgotten.
	e.issue(70, "a", func(i *milestoneIssue) { i.Milestone = 0 })
	wantItem(t, e.reconcile(), 70, itemRemoved, "")
	if e.ship(70) {
		t.Fatal("an issue that left the milestone is an ordinary issue again")
	}
}

func TestMilestoneSummaryAndLocales(t *testing.T) {
	var keys []string
	msg := func(k string, _ map[string]string) string { keys = append(keys, k); return k }
	text := fleetBatchText(msg, milestoneBatch{MilestoneTitle: "M8"}, []milestoneItem{{Issue: 7, State: itemWaiting, Reason: reasonDependency, Waiting: []int{6}}}, nil)
	if !strings.Contains(text, "#7 fleet_batch_state_waiting") || !strings.Contains(text, "fleet_batch_reason_dependency") {
		t.Fatalf("text = %q", text)
	}

	i18n, err := NewI18nManager("../../locales", "zh-TW")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fleet_batch_confirm", "fleet_batch_btn_all", "fleet_batch_btn_one", "fleet_batch_already_answered",
		"fleet_batch_header", "fleet_batch_incomplete", "fleet_project_milestone_usage", "fleet_project_milestone_auto", "fleet_project_milestone_ask"}
	for _, s := range []string{itemReady, itemWaiting, itemActive, itemAttention, itemLanded, itemDone, itemSkipped, itemExcluded, itemBlocked, itemHeld, itemRemoved} {
		want = append(want, "fleet_batch_state_"+s)
	}
	for _, r := range []string{reasonEpic, reasonLabel, reasonDependency, reasonDependencyClosed, reasonCycle, reasonCancelled, reasonAwaitingMerge,
		reasonTargetChanged, reasonTargetMismatch, reasonNeedsDetail, reasonDispatchFailed, "not_planned", "duplicate",
		string(fleet.StatusReviewReady), string(fleet.StatusNeedsDecision), string(fleet.StatusFailed)} {
		want = append(want, "fleet_batch_reason_"+r)
	}
	for _, lang := range []string{"zh-TW", "en"} {
		bundle := i18n.languages[lang]
		for _, key := range want {
			if bundle == nil || bundle.Messages[key] == "" {
				t.Errorf("%s lacks %s", lang, key)
			}
		}
	}
}

func newGitRunner(t *testing.T, dir string) func(args ...string) {
	return func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestMilestoneReconcilesManuallyMergedPR(t *testing.T) {
	e := newMilestoneEnv(t, 1)
	e.auto()
	e.issue(70, "evaluate")
	e.issue(71, "translates\n- 依賴:#70")
	e.ship(70)
	taskID := e.items()[70].TaskID
	ctx := context.Background()
	if _, err := e.storage.db.Exec("UPDATE fleet_tasks SET trust_mode = ? WHERE id = ?", fleet.TrustDirectPR, taskID); err != nil {
		t.Fatal(err)
	}
	e.move(taskID, fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady, fleet.StatusDone)
	if err := e.svc.Store.SetPullRequest(ctx, taskID, "https://example.test/pull/8", 8); err != nil {
		t.Fatal(err)
	}
	e.svc.IssueSync.Host = e.bodies
	e.bodies.prs[8] = FleetPullRequest{State: "MERGED", BaseBranch: "wrong"}
	wantItem(t, e.reconcile(), 70, itemAttention, reasonTargetMismatch)

	e.bodies.prs[8] = FleetPullRequest{State: "MERGED", BaseBranch: e.batch().TargetBranch}
	items := e.reconcile()
	wantItem(t, items, 70, itemLanded, "")
	wantItem(t, items, 71, itemWaiting, reasonDependency)
	if got := e.dispatched(); !slices.Equal(got, []int{70}) {
		t.Fatalf("dispatched %v, want only #70 until it closes", got)
	}
}
