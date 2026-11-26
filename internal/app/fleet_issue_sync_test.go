package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/ghissue"
)

// fakeIssueHost stands in for GitHub: nothing here touches a real repository.
type fakeIssueHost struct {
	mu        sync.Mutex
	issues    map[int]*ghissue.IssueContext
	comments  map[string]string // marker -> body
	writes    int               // comment creations and edits
	closed    []int
	prs       map[int]FleetPullRequest
	failPR    error
	failComm  error
	failOnce  bool
	failClose error
}

func newFakeIssueHost() *fakeIssueHost {
	return &fakeIssueHost{issues: map[int]*ghissue.IssueContext{}, comments: map[string]string{}, prs: map[int]FleetPullRequest{}}
}

func (h *fakeIssueHost) Fetch(_ context.Context, _ string, n int) (*ghissue.IssueContext, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if is, ok := h.issues[n]; ok {
		cp := *is
		return &cp, nil
	}
	return nil, errors.New("not found")
}

func (h *fakeIssueHost) UpsertComment(_ context.Context, _ string, _ int, marker, body string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failComm != nil {
		err := h.failComm
		if h.failOnce {
			h.failComm = nil
		}
		return err
	}
	if h.comments[marker] != body {
		h.comments[marker] = body
		h.writes++
	}
	return nil
}

func (h *fakeIssueHost) CloseIssue(_ context.Context, _ string, n int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failClose != nil {
		return h.failClose
	}
	h.closed = append(h.closed, n)
	h.issues[n].State = "CLOSED"
	return nil
}

func (h *fakeIssueHost) PullRequest(_ context.Context, _ string, n int) (FleetPullRequest, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failPR != nil {
		return FleetPullRequest{}, h.failPR
	}
	return h.prs[n], nil
}

func (h *fakeIssueHost) LocalHead(context.Context, string, string) (string, string, error) {
	return "abcdef1234567890", "local change", nil
}

func (h *fakeIssueHost) commentFor(taskID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.comments[fleetIssueMarker(taskID)]
}

type issueSyncEnv struct {
	*cardEnv
	host *fakeIssueHost
	sync *FleetIssueSync
}

func newIssueSyncEnv(t *testing.T) *issueSyncEnv {
	t.Helper()
	e := newCardEnv(t)
	host := newFakeIssueHost()
	host.issues[213] = &ghissue.IssueContext{Number: 213, State: "OPEN", Checklist: []ghissue.ChecklistItem{
		{ID: "item-1", Text: "first", Section: "驗收條件"}, {ID: "item-2", Text: "second", Section: "驗收條件"},
	}}
	return &issueSyncEnv{e, host, &FleetIssueSync{Store: e.svc.Store, Host: host}}
}

// task creates an issue task and walks it to the status, with a passing
// verification round when passed is non-nil.
func (e *issueSyncEnv) task(issue int, mode fleet.TrustMode, passed *bool, statuses ...fleet.Status) fleet.Task {
	e.t.Helper()
	ctx := context.Background()
	task, err := e.svc.Store.CreateTask(ctx, fleet.NewTask{ProjectID: e.project.ID, Goal: "ship it\nmore", Source: fleet.Source{Kind: fleet.SourceIssue, IssueNumber: issue}})
	if err != nil {
		e.t.Fatal(err)
	}
	_, _ = e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventIssueLinked, map[string]any{"number": issue, "items": []fleet.IssueItem{{ID: "item-1", Text: "first"}, {ID: "item-2", Text: "second"}}})
	for _, s := range statuses {
		if s == fleet.StatusReviewReady && passed != nil {
			checks := []fleet.Verification{{Name: "t", Command: "go test", Passed: *passed}}
			attempt, err := e.svc.Store.RecordVerification(ctx, task.ID, checks)
			if err != nil {
				e.t.Fatal(err)
			}
			if _, err := e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventVerification, map[string]any{"attempt": attempt, "passed": *passed, "checks": checks}); err != nil {
				e.t.Fatal(err)
			}
		}
		if _, err := e.svc.Store.Transition(ctx, task.ID, s, "test"); err != nil {
			e.t.Fatalf("Transition %s: %v", s, err)
		}
		if s == fleet.StatusRunning {
			if _, err := e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventRunStarted, nil); err != nil {
				e.t.Fatal(err)
			}
		}
	}
	// A task takes its project's trust mode at dispatch; set it directly.
	if _, err := e.storage.db.Exec(`UPDATE fleet_tasks SET trust_mode = ? WHERE id = ?`, string(mode), task.ID); err != nil {
		e.t.Fatal(err)
	}
	got, _ := e.svc.Store.GetTask(ctx, task.ID)
	return got
}

func (e *issueSyncEnv) setPR(task fleet.Task, n int, state string) {
	e.t.Helper()
	_ = e.svc.Store.SetPullRequest(context.Background(), task.ID, "https://github.com/o/r/pull/9", n)
	e.host.prs[n] = FleetPullRequest{State: state, Title: "the change", BaseBranch: "main", MergeCommit: "0123456789abcdef", Files: []string{"b.go", "a.go"}}
}

func (e *issueSyncEnv) tick(texts ...string) {
	for i := range e.host.issues[213].Checklist {
		for _, t := range texts {
			if e.host.issues[213].Checklist[i].Text == t {
				e.host.issues[213].Checklist[i].Checked = true
			}
		}
	}
}

func (e *issueSyncEnv) run(task fleet.Task, opts IssueSyncOptions) *IssueSyncResult {
	e.t.Helper()
	res, err := e.sync.Sync(context.Background(), task.ID, opts)
	if err != nil {
		e.t.Fatalf("Sync: %v", err)
	}
	return res
}

var (
	yes = true
	no  = false
)

var toReview = []fleet.Status{fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady}

func contains(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("comment lacks %q:\n%s", w, body)
		}
	}
}

func TestIssueSyncReviewReadyKeepsIssueOpen(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustLocalOnly, &yes, toReview...)
	res := e.run(task, IssueSyncOptions{})
	body := e.host.commentFor(task.ID)
	contains(t, body, "待審查", "`go test`", "維持開啟")
	if strings.Contains(body, "Alice 完成") || res.Closed || len(e.host.closed) != 0 {
		t.Fatalf("review stage must not claim completion or close: %s", body)
	}
}

func TestIssueSyncDirectPRReportsOnlyPRReady(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustDirectPR, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(task, 9, "OPEN")
	res := e.run(task, IssueSyncOptions{AuthorizeClose: true})
	body := e.host.commentFor(task.ID)
	contains(t, body, "PR 就緒", "pull/9", "維持開啟")
	if strings.Contains(body, "仍未完成的項目") || res.Closed {
		t.Fatalf("an open PR must not touch the checklist or close the issue: %s", body)
	}
}

func TestIssueSyncMergedWithoutAuthorizationStaysOpen(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustNoMistakes, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(task, 9, "MERGED")
	e.tick("first", "second")
	res := e.run(task, IssueSyncOptions{})
	body := e.host.commentFor(task.ID)
	contains(t, body, "已合併", "目標分支: `main`", "01234567", "a.go", "pull/9", "尚未授權自動關單")
	if res.Closed || len(e.host.closed) != 0 {
		t.Fatal("closed without authorization")
	}
}

func TestIssueSyncClosesWhenAuthorizedAndComplete(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustNoMistakes, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(task, 9, "MERGED")
	e.tick("first", "second")
	if err := e.svc.Store.SetBinding(context.Background(), e.project.ID, FleetProjectBindingIssueAutoClose, "true"); err != nil {
		t.Fatal(err)
	}
	res := e.run(task, IssueSyncOptions{})
	if !res.Closed || len(e.host.closed) != 1 {
		t.Fatalf("expected the issue to close: %+v", res)
	}
	// Idempotent: a second sync neither rewrites the comment nor closes again.
	writes := e.host.writes
	e.run(task, IssueSyncOptions{})
	if e.host.writes != writes || len(e.host.closed) != 1 {
		t.Fatalf("second sync wrote again: writes %d->%d closed %v", writes, e.host.writes, e.host.closed)
	}
}

func TestIssueSyncOperatorAuthorizationCloses(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustDirectPR, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(task, 9, "MERGED")
	e.tick("first", "second")
	if res := e.run(task, IssueSyncOptions{AuthorizeClose: true}); !res.Closed {
		t.Fatalf("operator authorization should close: %+v", res)
	}
}

func TestIssueSyncLocalOnlyNeverCloses(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustLocalOnly, &yes, append(toReview, fleet.StatusDone)...)
	_, _ = e.svc.Store.AppendEvent(context.Background(), task.ID, fleet.EventDeliveryApproved, map[string]any{"landed": "fast-forwarded main"})
	e.tick("first", "second")
	res := e.run(task, IssueSyncOptions{AuthorizeClose: true})
	contains(t, e.host.commentFor(task.ID), "僅合併至本機", "沒有推送到遠端", "abcdef12", "只合併到本機")
	if res.Closed {
		t.Fatal("local-only merge closed the issue")
	}
}

func TestIssueSyncKeepsOpenWhenNotEligible(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(e *issueSyncEnv, task fleet.Task)
		passed *bool
		reason string
	}{
		{"partial delivery", func(e *issueSyncEnv, _ fleet.Task) { e.tick("first") }, &yes, "未完成"},
		{"not verified", func(e *issueSyncEnv, _ fleet.Task) { e.tick("first", "second") }, nil, "沒有驗收紀錄"},
		{"verification failed", func(e *issueSyncEnv, _ fleet.Task) { e.tick("first", "second") }, &no, "驗收未通過"},
		{"merge cannot be confirmed", func(e *issueSyncEnv, _ fleet.Task) {
			e.tick("first", "second")
			e.host.failPR = errors.New("gh: 502")
		}, &yes, "無法確認"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newIssueSyncEnv(t)
			task := e.task(213, fleet.TrustNoMistakes, c.passed, append(toReview, fleet.StatusDone)...)
			e.setPR(task, 9, "MERGED")
			c.setup(e, task)
			_ = e.svc.Store.SetBinding(context.Background(), e.project.ID, FleetProjectBindingIssueAutoClose, "true")
			res, _ := e.sync.Sync(context.Background(), task.ID, IssueSyncOptions{})
			if res == nil || res.Closed || len(e.host.closed) != 0 {
				t.Fatalf("must stay open: %+v", res)
			}
			contains(t, e.host.commentFor(task.ID), "維持開啟", c.reason)
		})
	}
}

func TestIssueSyncMultipleTasks(t *testing.T) {
	e := newIssueSyncEnv(t)
	_ = e.svc.Store.SetBinding(context.Background(), e.project.ID, FleetProjectBindingIssueAutoClose, "true")
	e.tick("first", "second")

	first := e.task(213, fleet.TrustNoMistakes, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(first, 9, "MERGED")
	other := e.task(213, fleet.TrustNoMistakes, &yes, fleet.StatusRunning)

	res := e.run(first, IssueSyncOptions{})
	if res.Closed {
		t.Fatal("closed while another task for the issue is still running")
	}
	contains(t, e.host.commentFor(first.ID), "關聯任務 `"+other.ID+"`")
	// Each task has its own comment.
	e.run(other, IssueSyncOptions{})
	if e.host.commentFor(other.ID) == "" || e.host.commentFor(other.ID) == e.host.commentFor(first.ID) {
		t.Fatal("tasks should have separate comments")
	}

	// Once the other task is cancelled, the issue can close.
	if _, err := e.svc.Store.Transition(context.Background(), other.ID, fleet.StatusCancelled, "test"); err != nil {
		t.Fatal(err)
	}
	if res := e.run(first, IssueSyncOptions{}); !res.Closed {
		t.Fatalf("expected close: %+v", res)
	}
}

func TestIssueSyncAPIFailureIsRecordedAndRetried(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustLocalOnly, &yes, toReview...)
	e.host.failComm, e.host.failOnce = errors.New("gh: 500"), true
	if _, err := e.sync.Sync(context.Background(), task.ID, IssueSyncOptions{}); err == nil {
		t.Fatal("expected the failure to be returned")
	}
	if _, ok, _ := e.svc.Store.LastEvent(context.Background(), task.ID, fleetEventIssueSyncError); !ok {
		t.Fatal("failure not recorded on the task")
	}
	// A repeated identical failure does not add history.
	e.host.failComm, e.host.failOnce = errors.New("gh: 500"), false
	_, _ = e.sync.Sync(context.Background(), task.ID, IssueSyncOptions{})
	_, _ = e.sync.Sync(context.Background(), task.ID, IssueSyncOptions{})
	evs, _ := e.svc.Store.ListEvents(context.Background(), task.ID, 0, 1000)
	n := 0
	for _, ev := range evs {
		if ev.Type == fleetEventIssueSyncError {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("error events = %d, want 1", n)
	}
	// The timer retries it, and the comment lands.
	e.host.failComm = nil
	e.sync.Reconcile(context.Background())
	contains(t, e.host.commentFor(task.ID), "待審查")
	if _, ok, _ := e.svc.Store.LastEvent(context.Background(), task.ID, fleetEventIssueSyncResult); !ok {
		t.Fatal("retry did not record a result")
	}
}

func TestIssueSyncCloseFailureRetries(t *testing.T) {
	e := newIssueSyncEnv(t)
	task := e.task(213, fleet.TrustNoMistakes, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(task, 9, "MERGED")
	e.tick("first", "second")
	_ = e.svc.Store.SetBinding(context.Background(), e.project.ID, FleetProjectBindingIssueAutoClose, "true")
	e.host.failClose = errors.New("gh: 500")
	if _, err := e.sync.Sync(context.Background(), task.ID, IssueSyncOptions{}); err == nil {
		t.Fatal("expected close failure")
	}
	e.host.failClose = nil
	e.sync.Reconcile(context.Background())
	if len(e.host.closed) != 1 {
		t.Fatalf("retry should close once, closed=%v", e.host.closed)
	}
}

func TestIssueSyncPRNotYetMergedThenMerged(t *testing.T) {
	e := newIssueSyncEnv(t)
	_ = e.svc.Store.SetBinding(context.Background(), e.project.ID, FleetProjectBindingIssueAutoClose, "true")
	e.tick("first", "second")
	task := e.task(213, fleet.TrustDirectPR, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(task, 9, "OPEN")
	e.sync.Reconcile(context.Background())
	contains(t, e.host.commentFor(task.ID), "PR 就緒")
	// Merged on the forge later: the timer notices, updates the same comment, closes.
	e.setPR(task, 9, "MERGED")
	e.sync.Reconcile(context.Background())
	contains(t, e.host.commentFor(task.ID), "已合併")
	if len(e.host.closed) != 1 || len(e.host.comments) != 1 {
		t.Fatalf("closed=%v comments=%d", e.host.closed, len(e.host.comments))
	}
}

func TestIssueSyncNeverEditsIssueBody(t *testing.T) {
	e := newIssueSyncEnv(t)
	before := *e.host.issues[213]
	task := e.task(213, fleet.TrustNoMistakes, &yes, append(toReview, fleet.StatusDone)...)
	e.setPR(task, 9, "MERGED")
	e.run(task, IssueSyncOptions{})
	if after := e.host.issues[213]; after.Body != before.Body || len(after.Checklist) != len(before.Checklist) {
		t.Fatal("issue body or checklist changed")
	}
}

func TestIssueSyncDoesNotCloseUsingPreviousRunChecks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		emptyRound bool
	}{
		{name: "no verification"},
		{name: "no configured checks", emptyRound: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newIssueSyncEnv(t)
			ctx := context.Background()
			task := e.task(213, fleet.TrustDirectPR, &yes, toReview...)
			e.tick("first", "second")
			for _, status := range []fleet.Status{fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady, fleet.StatusDone} {
				if status == fleet.StatusReviewReady && tc.emptyRound {
					attempt, err := e.svc.Store.RecordVerification(ctx, task.ID, nil)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventVerification, map[string]any{"attempt": attempt, "passed": true, "checks": []any{}}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := e.svc.Store.Transition(ctx, task.ID, status, "acceptance fixture"); err != nil {
					t.Fatal(err)
				}
				if status == fleet.StatusRunning {
					if _, err := e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventRunStarted, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			e.setPR(task, 9, "MERGED")
			res := e.run(task, IssueSyncOptions{AuthorizeClose: true})
			if res.Closed || len(e.host.closed) != 0 || !strings.Contains(res.KeptOpen, "未驗收") {
				t.Fatalf("issue should stay open without checks from the current run: %+v", res)
			}
		})
	}
}
