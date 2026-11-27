package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// The reason the fleet records when an agent stops to ask something.
const reasonAgentWaiting = "agent is waiting for an answer"

type fakeDocs struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeDocs) SendDocument(_ chatKey, path, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, path)
	return nil
}

// notifyEnv is a cardEnv with the notification layer attached. Nothing runs
// in the background: a test scans for wakes and processes them itself.
type notifyEnv struct {
	*cardEnv
	notices  *fleetNotices
	notifier *fleet.Notifier
	docs     *fakeDocs
}

func newNotifyTestEnv(t *testing.T) *notifyEnv {
	t.Helper()
	e := newCardEnv(t)
	if err := e.cards.Store.preparePrompts(); err != nil {
		t.Fatalf("preparePrompts: %v", err)
	}
	docs := &fakeDocs{}
	notices := &fleetNotices{
		Fleet: e.svc, Store: e.cards.Store, Messenger: e.msgr, Docs: docs,
		Localize: testLocalize, Owner: 42, Topics: e.storage,
	}
	notifier := &fleet.Notifier{
		Store: e.svc.Store, Commander: e.svc.Commander, User: "42",
		Deliver: notices.Deliver, MinInterval: -1,
	}
	e.bot.fleetNotices, e.bot.fleetNotifier = notices, notifier
	return &notifyEnv{e, notices, notifier, docs}
}

// walk moves a task through statuses; the last one takes the given reason.
func (e *notifyEnv) walk(taskID, reason string, statuses ...fleet.Status) fleet.Task {
	e.t.Helper()
	var task fleet.Task
	for i, to := range statuses {
		r := "test"
		if i == len(statuses)-1 {
			r = reason
		}
		var err error
		if task, err = e.svc.Store.Transition(context.Background(), taskID, to, r); err != nil {
			e.t.Fatalf("Transition to %s: %v", to, err)
		}
	}
	return task
}

// notify turns what happened into wakes and delivers them, returning the
// messages sent by this call.
func (e *notifyEnv) notify() []fakeCardMessage {
	e.t.Helper()
	ctx := context.Background()
	if _, err := e.svc.Watcher.Scan(ctx); err != nil {
		e.t.Fatalf("Scan: %v", err)
	}
	before, _ := e.msgr.counts()
	if _, err := e.notifier.Process(ctx); err != nil {
		e.t.Fatalf("Process: %v", err)
	}
	e.msgr.mu.Lock()
	defer e.msgr.mu.Unlock()
	return append([]fakeCardMessage(nil), e.msgr.sent[before:]...)
}

func TestFleetQuestionPromptAndReply(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship pick an approach")
	e.walk(task.ID, "", fleet.StatusRunning)
	if _, err := e.svc.Store.AppendEvent(ctx, task.ID, fleet.EventAgentText, map[string]string{"text": "要用方案 A 還是 B？"}); err != nil {
		t.Fatal(err)
	}
	e.walk(task.ID, reasonAgentWaiting, fleet.StatusNeedsDecision)

	sent := e.notify()
	if len(sent) != 1 {
		t.Fatalf("sent %d message(s), want one prompt", len(sent))
	}
	prompt := sent[0]
	if prompt.Key != e.key {
		t.Fatalf("prompt sent to %+v, want the card's topic %+v", prompt.Key, e.key)
	}
	for _, want := range []string{"fleet_wake_question", task.ID, "要用方案 A 還是 B？", "fleet_prompt_hint_question"} {
		if !strings.Contains(prompt.Text, want) {
			t.Errorf("prompt misses %q:\n%s", want, prompt.Text)
		}
	}
	if got := buttonData(prompt.Buttons); len(got) != 1 || got[0] != "fleet:pdiscard:"+task.ID {
		t.Fatalf("question buttons = %v", got)
	}

	// Nothing new has happened: a second pass sends nothing.
	if again := e.notify(); len(again) != 0 {
		t.Fatalf("second pass sent %d message(s)", len(again))
	}

	// A reply to the prompt goes to the task; a reply to anything else,
	// and a command typed as a reply, do not.
	if e.bot.fleetHandleReply(e.key, 42, prompt.MessageID+999, "用 B") {
		t.Fatal("a reply to an unrelated message was taken")
	}
	if e.bot.fleetHandleReply(e.key, 42, prompt.MessageID, "/status") {
		t.Fatal("a command typed as a reply was taken")
	}
	if !e.bot.fleetHandleReply(e.key, 42, prompt.MessageID, "用 B") {
		t.Fatal("the reply to the prompt was not taken")
	}
	audit, err := e.svc.Commander.Audit(ctx, fleet.AuditFilter{TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	replied := false
	for _, entry := range audit {
		replied = replied || (entry.Command == fleet.CmdReply && entry.Detail == "用 B" && entry.Actor == "42")
	}
	if !replied {
		t.Fatalf("no reply by 42 with the text in the audit log: %+v", audit)
	}
}

func TestFleetReplyToCardReachesTheTask(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/ship fix it")
	e.walk(task.ID, "checks kept failing", fleet.StatusRunning, fleet.StatusNeedsDecision)
	if !e.bot.fleetHandleReply(e.key, 42, e.msgr.sent[0].MessageID, "先跑 go mod tidy") {
		t.Fatal("the reply to the card was not taken")
	}
	audit, _ := e.svc.Commander.Audit(context.Background(), fleet.AuditFilter{TaskID: task.ID})
	if len(audit) == 0 || audit[0].Command != fleet.CmdReply {
		t.Fatalf("audit = %+v", audit)
	}
}

func TestFleetPromptButtonsByTrouble(t *testing.T) {
	e := newNotifyTestEnv(t)
	loc := func(key string, vars map[string]string) string { return testLocalize(0, key, vars) }
	task := fleet.Task{ID: "0123456789ab"}
	id := task.ID
	cases := []struct {
		kind fleet.WakeKind
		want []string
	}{
		{fleet.WakeQuestion, []string{"fleet:pdiscard:" + id}},
		{fleet.WakeNeedsDecision, []string{"fleet:retry:" + id, "fleet:hint:" + id, "fleet:pass:" + id, "fleet:pdiscard:" + id}},
		{fleet.WakeFailed, []string{"fleet:retry:" + id, "fleet:pdiscard:" + id}},
		{fleet.WakeOverBudget, []string{"fleet:goon:" + id, "fleet:pdiscard:" + id}},
		{fleet.WakeStuck, []string{"fleet:cancel:" + id}},
	}
	for _, c := range cases {
		got := buttonData(e.notices.promptButtons(loc, task, c.kind))
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: buttons = %v, want %v", c.kind, got, c.want)
		}
		for _, data := range got {
			if len(data) > telegramCallbackDataLimit {
				t.Errorf("%s is %d bytes", data, len(data))
			}
		}
	}
}

func TestFleetPromptKinds(t *testing.T) {
	e := newNotifyTestEnv(t)

	blocked := e.dispatch("/ship one")
	e.walk(blocked.ID, "verification failed 3 times", fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusNeedsDecision)
	sent := e.notify()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "fleet_wake_needs_decision") ||
		!strings.Contains(sent[0].Text, "verification failed 3 times") {
		t.Fatalf("blocked task: %+v", sent)
	}

	failed := e.dispatch("/ship two")
	e.walk(failed.ID, "agent crashed", fleet.StatusRunning, fleet.StatusFailed)
	sent = e.notify()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "fleet_wake_failed") || !strings.Contains(sent[0].Text, failed.ID) {
		t.Fatalf("failed task: %+v", sent)
	}
	if got := buttonData(sent[0].Buttons); got[0] != "fleet:retry:"+failed.ID {
		t.Fatalf("failed task buttons = %v", got)
	}
}

func TestFleetPromptIsNotRepeatedForTheSameReason(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")
	task = e.walk(task.ID, "verification failed 3 times", fleet.StatusRunning, fleet.StatusNeedsDecision)
	note := fleet.Notification{User: "42", TaskID: task.ID, ProjectID: task.ProjectID,
		Wakes: []fleet.Wake{{TaskID: task.ID, Kind: fleet.WakeNeedsDecision, Detail: task.StatusReason}}}

	for i := 0; i < 3; i++ {
		if err := e.notices.Deliver(ctx, note); err != nil {
			t.Fatalf("Deliver: %v", err)
		}
	}
	if sent, _ := e.msgr.counts(); sent != 2 { // the card and one prompt
		t.Fatalf("messages = %d, want the card and a single prompt", sent)
	}

	// The same trouble in a later run is asked about again.
	e.walk(task.ID, "verification failed 3 times", fleet.StatusQueued, fleet.StatusRunning, fleet.StatusNeedsDecision)
	if err := e.notices.Deliver(ctx, note); err != nil {
		t.Fatal(err)
	}
	if sent, _ := e.msgr.counts(); sent != 3 {
		t.Fatalf("messages = %d, want a new prompt for the new run", sent)
	}
}

func TestFleetStaleWakeSendsNothing(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/ship fix it")
	e.walk(task.ID, "agent crashed", fleet.StatusRunning, fleet.StatusFailed)
	// The owner retried before the notifier got to it.
	e.walk(task.ID, "retry", fleet.StatusQueued)
	if sent := e.notify(); len(sent) != 0 {
		t.Fatalf("sent %d message(s) about a task that moved on", len(sent))
	}
}

func TestFleetFailedDeliveryIsTriedAgain(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/ship fix it")
	e.walk(task.ID, "agent crashed", fleet.StatusRunning, fleet.StatusFailed)

	e.msgr.mu.Lock()
	e.msgr.sendErr = errors.New("telegram down")
	e.msgr.mu.Unlock()
	if sent := e.notify(); len(sent) != 0 {
		t.Fatalf("sent %d during the outage", len(sent))
	}
	e.msgr.mu.Lock()
	e.msgr.sendErr = nil
	e.msgr.mu.Unlock()
	if sent := e.notify(); len(sent) != 1 {
		t.Fatalf("sent %d after the outage, want the prompt", len(sent))
	}
}

func TestFleetResultMessage(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/ship add the feature")
	e.walk(task.ID, "no verification commands configured", fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady)

	sent := e.notify()
	if len(sent) != 1 {
		t.Fatalf("sent %d message(s), want the result", len(sent))
	}
	for _, want := range []string{"fleet_status_review_ready", task.ID, "add the feature", "fleet_dv_verify_not_configured", "fleet_result_next_local", "fleet_card_meter"} {
		if !strings.Contains(sent[0].Text, want) {
			t.Errorf("result misses %q:\n%s", want, sent[0].Text)
		}
	}
	if got := buttonData(sent[0].Buttons); len(got) != 2 || got[0] != "fleet:approve:"+task.ID || got[1] != "fleet:pdiscard:"+task.ID {
		t.Fatalf("local-only result buttons = %v", got)
	}
}

func TestFleetResultButtonsByTrustMode(t *testing.T) {
	e := newNotifyTestEnv(t)
	loc := func(key string, vars map[string]string) string { return testLocalize(0, key, vars) }
	id := "0123456789ab"
	cases := []struct {
		name string
		task fleet.Task
		want []string
		next string
	}{
		{"local-only awaiting review", fleet.Task{Status: fleet.StatusReviewReady, TrustMode: fleet.TrustLocalOnly},
			[]string{"fleet:approve:" + id, "fleet:pdiscard:" + id}, "fleet_result_next_local"},
		{"no-mistakes offers the one approval", fleet.Task{Status: fleet.StatusReviewReady, TrustMode: fleet.TrustNoMistakes, PRNumber: 9},
			[]string{"fleet:approve:" + id}, "fleet_result_next_no_mistakes"},
		{"direct-pr done with its PR open", fleet.Task{Status: fleet.StatusDone, TrustMode: fleet.TrustDirectPR, PRNumber: 9},
			[]string{"fleet:merge:" + id}, "fleet_result_next_direct_pr"},
		{"done and landed", fleet.Task{Status: fleet.StatusDone, TrustMode: fleet.TrustLocalOnly}, nil, ""},
	}
	for _, c := range cases {
		c.task.ID = id
		buttons := e.notices.resultButtons(loc, c.task)
		if got := buttonData(buttons); strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: buttons = %v, want %v", c.name, got, c.want)
		}
		if got := fleetResultNextKey(c.task); got != c.next {
			t.Errorf("%s: next = %q, want %q", c.name, got, c.next)
		}
	}
	// The no-mistakes button says what it does.
	b := e.notices.resultButtons(loc, fleet.Task{ID: id, Status: fleet.StatusReviewReady, TrustMode: fleet.TrustNoMistakes})
	if b[0][0].Text != "fleet_btn_approve_merge" {
		t.Errorf("no-mistakes button text = %q", b[0][0].Text)
	}
}

func TestFleetScoutResultAttachesTheReport(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/scout where is the token validated?")
	reportPath, err := e.svc.Runner.ReportPath(task.ProjectID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	report := "# Findings\n\nThe token is validated in auth/middleware.go:42.\n\n" + strings.Repeat("More detail. ", 200)
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	e.walk(task.ID, "report written", fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady)

	sent := e.notify()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "auth/middleware.go:42") {
		t.Fatalf("scout result: %+v", sent)
	}
	if strings.Contains(sent[0].Text, "fleet_result_verify") || strings.Contains(sent[0].Text, "fleet_result_changes") {
		t.Errorf("a scout result talks about changes or checks:\n%s", sent[0].Text)
	}
	if n := len([]rune(sent[0].Text)); n > 1400 {
		t.Errorf("scout result is %d runes; the report belongs in the attachment", n)
	}
	if len(e.docs.sent) != 1 || e.docs.sent[0] != reportPath {
		t.Fatalf("documents sent = %v, want %s", e.docs.sent, reportPath)
	}
}

func TestFleetNoticeDestination(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()

	// A task with no card goes to a topic bound to its project.
	task, err := e.svc.Store.CreateTask(ctx, fleet.NewTask{ProjectID: e.project.ID, Goal: "from the web"})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.notices.destination(ctx, task.ID, task.ProjectID); got != e.key {
		t.Fatalf("destination = %+v, want the project's topic %+v", got, e.key)
	}

	// With no topic for the project either, it goes to the owner.
	e.notices.Topics = nil
	if got := e.notices.destination(ctx, task.ID, task.ProjectID); got != (chatKey{chatID: 42}) {
		t.Fatalf("destination = %+v, want the owner's chat", got)
	}
	// Or to wherever the owner last set a notification mode.
	home := chatKey{chatID: -100, threadID: 5}
	e.notices.SetHome(home)
	if got := e.notices.destination(ctx, task.ID, task.ProjectID); got != home {
		t.Fatalf("destination = %+v, want home %+v", got, home)
	}

	// A task with a card always goes where its card is.
	carded := e.dispatch("/ship fix it")
	if got := e.notices.destination(ctx, carded.ID, carded.ProjectID); got != e.key {
		t.Fatalf("destination = %+v, want the card's topic", got)
	}
}

func TestFleetLandingActionsAreOwnerOnly(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/scout look around")
	e.walk(task.ID, "checks kept failing", fleet.StatusRunning, fleet.StatusNeedsDecision)

	for _, action := range []string{fleetActApprove, fleetActMerge, fleetActPass} {
		e.bot.handleFleetCallback(e.key, 99, "q1", fleetCallbackData(action, task.ID))
		if got := e.nextAnswer(); got != "fleet_cb_owner_only" {
			t.Errorf("%s by another user: answer = %q", action, got)
		}
	}
	if got, _ := e.svc.Store.GetTask(context.Background(), task.ID); got.Status != fleet.StatusNeedsDecision {
		t.Fatalf("a non-owner moved the task to %s", got.Status)
	}

	// The owner waves it through.
	if got := e.press(fleetActPass, task.ID); got != "fleet_cb_pass" {
		t.Fatalf("owner's pass: answer = %q", got)
	}
	if got, _ := e.svc.Store.GetTask(context.Background(), task.ID); got.Status != fleet.StatusReviewReady {
		t.Fatalf("after pass the task is %s", got.Status)
	}
}

func TestFleetPromptDiscardTakesTwoPresses(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")
	e.walk(task.ID, "agent crashed", fleet.StatusRunning, fleet.StatusFailed)

	if got := e.press(fleetActPromptDiscard, task.ID); got != "fleet_cb_confirm" {
		t.Fatalf("first press: answer = %q", got)
	}
	events, _ := e.svc.Store.ListEvents(ctx, task.ID, 0, 100)
	for _, ev := range events {
		if ev.Type == fleet.EventDiscarded {
			t.Fatal("one press discarded the task")
		}
	}
	if got := e.press(fleetActPromptDiscard, task.ID); got != "fleet_cb_discard" {
		t.Fatalf("second press: answer = %q", got)
	}

	// The window closes: a press long after the first only arms it again.
	n := &fleetNotices{}
	start := time.Now()
	if n.confirmDiscard("t1", start) || n.confirmDiscard("t1", start.Add(2*fleetDiscardConfirmWindow)) {
		t.Fatal("a press outside the window confirmed")
	}
	if !n.confirmDiscard("t1", start.Add(2*fleetDiscardConfirmWindow+time.Second)) {
		t.Fatal("a second press inside the window did not confirm")
	}
}

func TestFleetHintButtonOnlyAsksForAReply(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/ship fix it")
	e.walk(task.ID, "checks kept failing", fleet.StatusRunning, fleet.StatusNeedsDecision)
	if got := e.press(fleetActHint, task.ID); got != "fleet_cb_hint" {
		t.Fatalf("answer = %q", got)
	}
	if got, _ := e.svc.Store.GetTask(context.Background(), task.ID); got.Status != fleet.StatusNeedsDecision {
		t.Fatalf("the hint button moved the task to %s", got.Status)
	}
}

func TestParseDiffStat(t *testing.T) {
	cases := []struct {
		stat            string
		files, ins, del int
		ok              bool
	}{
		{" a.go | 3 ++-\n b.go | 9 +++++++++\n 2 files changed, 11 insertions(+), 1 deletion(-)", 2, 11, 1, true},
		{" 1 file changed, 1 insertion(+)", 1, 1, 0, true},
		{" 3 files changed, 7 deletions(-)", 3, 0, 7, true},
		{"", 0, 0, 0, false},
		{"nothing to see", 0, 0, 0, false},
	}
	for _, c := range cases {
		files, ins, del, ok := parseDiffStat(c.stat)
		if files != c.files || ins != c.ins || del != c.del || ok != c.ok {
			t.Errorf("parseDiffStat(%q) = %d, %d, %d, %v", c.stat, files, ins, del, ok)
		}
	}
}

func TestParseFleetDuration(t *testing.T) {
	good := map[string]time.Duration{
		"8h": 8 * time.Hour, "90m": 90 * time.Minute, "1d": 24 * time.Hour,
		"1d12h": 36 * time.Hour, "2h30m": 150 * time.Minute, "7D": 7 * 24 * time.Hour,
	}
	for in, want := range good {
		if got, err := parseFleetDuration(in); err != nil || got != want {
			t.Errorf("parseFleetDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "0h", "8", "h", "8 hours", "-3h", "8d", "1h1d", "30s", "abc"} {
		if _, err := parseFleetDuration(bad); !errors.Is(err, errFleetAFKDuration) {
			t.Errorf("parseFleetDuration(%q) err = %v, want a duration error", bad, err)
		}
	}
}

func TestParseFleetAFK(t *testing.T) {
	cases := []struct {
		in       string
		actions  []fleet.AFKAction
		projects []string
		err      error
	}{
		{"8h", nil, nil, nil},
		{"8h approve", []fleet.AFKAction{fleet.AFKApprove}, nil, nil},
		{"8h approve,resume", []fleet.AFKAction{fleet.AFKApprove, fleet.AFKResume}, nil, nil},
		{"8h all", []fleet.AFKAction{fleet.AFKApprove, fleet.AFKResume}, nil, nil},
		{"8h APPROVE approve", []fleet.AFKAction{fleet.AFKApprove}, nil, nil},
		{"2h resume alice web", []fleet.AFKAction{fleet.AFKResume}, []string{"alice", "web"}, nil},
		{"", nil, nil, errFleetAFKUsage},
		{"approve 8h", nil, nil, errFleetAFKDuration},
		{"forever", nil, nil, errFleetAFKDuration},
	}
	for _, c := range cases {
		got, err := parseFleetAFK(c.in)
		if !errors.Is(err, c.err) {
			t.Errorf("%q: err = %v, want %v", c.in, err, c.err)
			continue
		}
		if err != nil {
			continue
		}
		if len(got.Actions) != len(c.actions) || strings.Join(got.Projects, ",") != strings.Join(c.projects, ",") {
			t.Errorf("%q: got %+v", c.in, got)
			continue
		}
		for i := range c.actions {
			if got.Actions[i] != c.actions[i] {
				t.Errorf("%q: actions = %v, want %v", c.in, got.Actions, c.actions)
			}
		}
	}
}

func TestFleetNotificationModes(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()
	say := func(text string) string {
		t.Helper()
		if !e.bot.handleFleetCommand(e.key, 42, text) {
			t.Fatalf("%q was not handled", text)
		}
		return nextFleetMessage(t, e.bot)
	}

	if got := say("/back"); got != "fleet_back_already" {
		t.Fatalf("/back in normal mode: %q", got)
	}

	if got := say("/quiet"); got != "fleet_quiet_on" {
		t.Fatalf("/quiet: %q", got)
	}
	// The mode is stored, not held in memory: a fresh notifier sees it.
	fresh := &fleet.Notifier{Store: e.svc.Store, User: "42", Deliver: e.notices.Deliver}
	if prefs, err := fresh.Prefs(ctx); err != nil || prefs.Mode != fleet.NotifyQuiet {
		t.Fatalf("after a restart: %+v, %v", prefs, err)
	}
	if got := e.notices.home(); got != e.key {
		t.Fatalf("home = %+v, want where /quiet was typed", got)
	}

	// In quiet mode a finished task is held, and reported at /back.
	task := e.dispatch("/ship add the feature")
	e.walk(task.ID, "verification passed", fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusReviewReady, fleet.StatusDone)
	_ = e.notify()
	summary := say("/back")
	for _, want := range []string{"fleet_away_header", "fleet_mode_quiet", "fleet_away_held", task.ID} {
		if !strings.Contains(summary, want) {
			t.Errorf("/back summary misses %q:\n%s", want, summary)
		}
	}
	if prefs, _ := e.notifier.Prefs(ctx); prefs.Mode != fleet.NotifyNormal {
		t.Fatalf("after /back the mode is %s", prefs.Mode)
	}

	// /afk needs a duration, and only grants to projects that exist.
	if got := say("/afk"); !strings.Contains(got, "fleet_afk_usage") {
		t.Fatalf("/afk: %q", got)
	}
	if got := say("/afk soon approve"); !strings.Contains(got, "fleet_afk_err_duration") {
		t.Fatalf("/afk soon: %q", got)
	}
	if got := say("/afk 8h approve nosuchproject"); !strings.Contains(got, "fleet_afk_err_project") {
		t.Fatalf("/afk with an unknown project: %q", got)
	}
	if prefs, _ := e.notifier.Prefs(ctx); prefs.Mode != fleet.NotifyNormal {
		t.Fatalf("a refused /afk left the mode at %s", prefs.Mode)
	}

	if got := say("/afk 8h approve " + e.project.Name); !strings.Contains(got, "fleet_afk_on") {
		t.Fatalf("/afk 8h approve: %q", got)
	}
	prefs, err := e.notifier.Prefs(ctx)
	if err != nil || prefs.Mode != fleet.NotifyAFK {
		t.Fatalf("prefs = %+v, %v", prefs, err)
	}
	if len(prefs.Grant.Actions) != 1 || prefs.Grant.Actions[0] != fleet.AFKApprove ||
		len(prefs.Grant.Projects) != 1 || prefs.Grant.Projects[0] != e.project.ID {
		t.Fatalf("grant = %+v", prefs.Grant)
	}
	if left := time.Until(prefs.Grant.ExpiresAt); left < 7*time.Hour+59*time.Minute || left > 8*time.Hour+time.Minute {
		t.Fatalf("grant expires in %v, want about 8h", left)
	}
	if got := say("/back"); !strings.Contains(got, "fleet_mode_afk") {
		t.Fatalf("/back from afk: %q", got)
	}
}

func TestFleetExpiredGrantSummaryGoesHome(t *testing.T) {
	e := newNotifyTestEnv(t)
	home := chatKey{chatID: -100, threadID: 5}
	e.notices.SetHome(home)
	summary := fleet.AwaySummary{Mode: fleet.NotifyAFK, Since: time.Now().Add(-time.Hour), Until: time.Now(),
		Handled: []fleet.NotifyRecord{{TaskID: "aaaaaaaaaaaa", Kind: fleet.WakeReviewReady, Disposition: fleet.AutoApproved, Detail: "verification passed"}},
		Held:    []fleet.NotifyRecord{{TaskID: "bbbbbbbbbbbb", Kind: fleet.WakeQuestion, Disposition: fleet.Held, Note: "an agent's question is never answered for you"}},
	}
	if err := e.notices.Deliver(context.Background(), fleet.Notification{User: "42", Summary: &summary}); err != nil {
		t.Fatal(err)
	}
	msg := e.msgr.sent[len(e.msgr.sent)-1]
	if msg.Key != home {
		t.Fatalf("summary sent to %+v, want home %+v", msg.Key, home)
	}
	for _, want := range []string{"fleet_away_handled", "fleet_disp_auto_approved", "aaaaaaaaaaaa", "fleet_away_held", "fleet_wake_question", "bbbbbbbbbbbb", "never answered"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("summary misses %q:\n%s", want, msg.Text)
		}
	}
}
