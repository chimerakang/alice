package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/hermes"

	"github.com/chimerakang/alice-core/fleet"
)

// fakeMessenger records the cards a manager sends and edits.
type fakeMessenger struct {
	mu      sync.Mutex
	nextID  int
	sent    []fakeCardMessage
	edits   []fakeCardMessage
	sendErr error
	editErr error
}

type fakeCardMessage struct {
	Key       chatKey
	MessageID int
	Text      string
	Buttons   [][]fleetButton
}

func (f *fakeMessenger) SendCard(key chatKey, text string, buttons [][]fleetButton) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return 0, f.sendErr
	}
	f.nextID++
	id := 1000 + f.nextID
	f.sent = append(f.sent, fakeCardMessage{key, id, text, buttons})
	return id, nil
}

func (f *fakeMessenger) EditCard(key chatKey, messageID int, text string, buttons [][]fleetButton) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editErr != nil {
		return f.editErr
	}
	f.edits = append(f.edits, fakeCardMessage{key, messageID, text, buttons})
	return nil
}

func (f *fakeMessenger) counts() (sent, edits int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent), len(f.edits)
}

func (f *fakeMessenger) lastEdit(t *testing.T) fakeCardMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.edits) == 0 {
		t.Fatal("no card edit recorded")
	}
	return f.edits[len(f.edits)-1]
}

// waitEdits waits until at least n edits have been recorded.
func (f *fakeMessenger) waitEdits(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, edits := f.counts(); edits >= n {
			return
		}
		if time.Now().After(deadline) {
			_, edits := f.counts()
			t.Fatalf("edits = %d, want at least %d", edits, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// cardEnv is a bot with a fleet that is built but not started: nothing
// schedules tasks, so a task only moves when the test moves it.
type cardEnv struct {
	t       *testing.T
	bot     *TelegramBot
	svc     *FleetService
	storage *SQLiteStorage
	msgr    *fakeMessenger
	cards   *fleetCardManager
	key     chatKey
	project fleet.Project
}

// testLocalize renders a message as its key followed by its variables, so
// tests can see both without depending on wording.
func testLocalize(_ int64, key string, vars map[string]string) string {
	if len(vars) == 0 {
		return key
	}
	raw, _ := json.Marshal(vars)
	return key + string(raw)
}

func newCardEnv(t *testing.T) *cardEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "alice.db")
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	prev := globalStorage
	globalStorage = storage
	t.Cleanup(func() {
		globalStorage = prev
		storage.Close()
	})
	svc, err := NewFleetService(FleetConfig{Enabled: true}, storage.db, dbPath, "sonnet")
	if err != nil {
		t.Fatalf("NewFleetService: %v", err)
	}
	// Tests must never start a real agent: point the runner at nothing.
	svc.Runner.Binary = filepath.Join(t.TempDir(), "no-such-claude")
	// A reply starts a run in the background, which fails at once for want
	// of an agent; let it finish before the test's directories are removed.
	t.Cleanup(svc.Scheduler.Wait)
	cardStore, err := newFleetCardStore(storage.db)
	if err != nil {
		t.Fatalf("newFleetCardStore: %v", err)
	}
	msgr := &fakeMessenger{}
	cards := &fleetCardManager{
		Fleet: svc, Store: cardStore, Messenger: msgr, Localize: testLocalize,
		MinInterval: time.Nanosecond, // effectively no throttle unless a test sets one
	}
	bot := &TelegramBot{
		agents:          map[chatKey]*Agent{},
		config:          &Config{DefaultProjectDir: t.TempDir()},
		messageQueue:    make(chan *TelegramMessage, 32),
		langPreferences: map[int64]string{},
		fleet:           svc,
		fleetCards:      cards,
	}
	key := chatKey{chatID: 7, threadID: 3}
	repo := newFleetTestRepo(t)
	bindTopic(t, storage, key, repo)
	project, _, err := svc.EnsureProject(context.Background(), fleetActor(42), repo)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	return &cardEnv{t, bot, svc, storage, msgr, cards, key, project}
}

// dispatch sends a /ship command and returns the task it created.
func (e *cardEnv) dispatch(text string) fleet.Task {
	e.t.Helper()
	if !e.bot.handleFleetCommand(e.key, 42, text) {
		e.t.Fatalf("%q was not handled", text)
	}
	tasks, err := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{})
	if err != nil || len(tasks) == 0 {
		e.t.Fatalf("no task after %q (err %v)", text, err)
	}
	return tasks[0]
}

func (e *cardEnv) press(action, taskID string) string {
	e.t.Helper()
	e.bot.handleFleetCallback(e.key, 42, "q1", fleetCallbackData(action, taskID))
	return e.nextAnswer()
}

// nextAnswer returns the text of the next callback answer the bot queued.
func (e *cardEnv) nextAnswer() string {
	e.t.Helper()
	for {
		select {
		case msg := <-e.bot.messageQueue:
			if msg.Method == "answerCallbackQuery" {
				text, _ := msg.Params["text"].(string)
				return text
			}
		case <-time.After(2 * time.Second):
			e.t.Fatal("timed out waiting for a callback answer")
			return ""
		}
	}
}

func buttonData(rows [][]fleetButton) []string {
	var out []string
	for _, row := range rows {
		for _, b := range row {
			if b.URL != "" {
				out = append(out, "url:"+b.URL)
			} else {
				out = append(out, b.Data)
			}
		}
	}
	return out
}

func TestFleetShipPostsCardAndCarriesOptions(t *testing.T) {
	e := newCardEnv(t)
	task := e.dispatch("/ship 修好登入測試\n第二行說明 --model opus --budget $2.50")

	if task.Shape != fleet.ShapeShip || task.Model != "opus" || task.Budget.CostUSD != 2.5 {
		t.Fatalf("task = shape %s model %q budget %+v", task.Shape, task.Model, task.Budget)
	}
	if task.Goal != "修好登入測試\n第二行說明" {
		t.Fatalf("goal = %q", task.Goal)
	}
	sent, edits := e.msgr.counts()
	if sent != 1 || edits != 0 {
		t.Fatalf("sent %d, edits %d; want exactly one card", sent, edits)
	}
	card := e.msgr.sent[0]
	if card.Key != e.key {
		t.Fatalf("card sent to %+v, want %+v", card.Key, e.key)
	}
	for _, want := range []string{"fleet_status_queued", "fleet_shape_ship", task.ID, "修好登入測試", "fleet_card_action_queued"} {
		if !strings.Contains(card.Text, want) {
			t.Errorf("card text misses %q:\n%s", want, card.Text)
		}
	}
	if strings.Contains(card.Text, "第二行說明") {
		t.Errorf("card shows more than the goal's first line:\n%s", card.Text)
	}
	if got := buttonData(card.Buttons); len(got) != 1 || got[0] != "fleet:cancel:"+task.ID {
		t.Fatalf("buttons = %v, want only cancel", got)
	}
	stored, ok, err := e.cards.Store.Get(task.ID)
	if err != nil || !ok || stored.MessageID != card.MessageID || stored.Key != e.key {
		t.Fatalf("stored card = %+v, %v, %v", stored, ok, err)
	}
}

func TestFleetScoutDispatchesAScout(t *testing.T) {
	e := newCardEnv(t)
	task := e.dispatch("/scout@alice_bot 登入流程在哪裡驗證 token？")
	if task.Shape != fleet.ShapeScout || task.Goal != "登入流程在哪裡驗證 token？" {
		t.Fatalf("task = %s %q", task.Shape, task.Goal)
	}
	if !strings.Contains(e.msgr.sent[0].Text, "fleet_shape_scout") {
		t.Fatalf("card = %s", e.msgr.sent[0].Text)
	}
}

func TestFleetDispatchRefusals(t *testing.T) {
	e := newCardEnv(t)
	cases := []struct {
		text string
		want []string
	}{
		{"/ship", []string{"fleet_ship_usage"}},
		{"/scout   ", []string{"fleet_scout_usage"}},
		{"/ship fix --budget lots", []string{"fleet_dispatch_err_budget", "fleet_ship_usage"}},
		{"/ship fix --model", []string{"fleet_dispatch_err_flag_value", "fleet_ship_usage"}},
	}
	for _, c := range cases {
		if !e.bot.handleFleetCommand(e.key, 42, c.text) {
			t.Fatalf("%q was not handled", c.text)
		}
		got := nextFleetMessage(t, e.bot)
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%q: message %q misses %q", c.text, got, want)
			}
		}
	}

	// A topic without a project is told to run /project first.
	other := chatKey{chatID: 7, threadID: 99}
	e.bot.handleFleetCommand(other, 42, "/ship fix it")
	if got := nextFleetMessage(t, e.bot); got != "fleet_no_project" {
		t.Fatalf("unbound topic: message = %q", got)
	}

	if tasks, _ := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{}); len(tasks) != 0 {
		t.Fatalf("refused commands created %d task(s)", len(tasks))
	}
	if sent, _ := e.msgr.counts(); sent != 0 {
		t.Fatalf("refused commands sent %d card(s)", sent)
	}
}

func TestFleetShipIssueGoesThroughTheQualityGate(t *testing.T) {
	e := newCardEnv(t)
	prev := fleetFetchIssue
	defer func() { fleetFetchIssue = prev }()

	var asked struct {
		dir    string
		number int
	}
	fleetFetchIssue = func(_ context.Context, dir string, number int) (*hermes.IssueContext, error) {
		asked.dir, asked.number = dir, number
		return &hermes.IssueContext{Number: number, Title: "t", State: "CLOSED", Body: strings.Repeat("x", 200)}, nil
	}
	e.bot.handleFleetCommand(e.key, 42, "/ship #12")
	if got := nextFleetMessage(t, e.bot); !strings.Contains(got, "fleet_issue_reading") {
		t.Fatalf("issue read acknowledgement: message = %q", got)
	}
	if got := nextFleetMessage(t, e.bot); !strings.Contains(got, "#12") || !strings.Contains(got, "closed") {
		t.Fatalf("closed issue: message = %q", got)
	}
	if asked.number != 12 || asked.dir != e.project.RepoPath {
		t.Fatalf("issue fetched as %+v, want #12 in %s", asked, e.project.RepoPath)
	}

	fleetFetchIssue = func(context.Context, string, int) (*hermes.IssueContext, error) {
		return &hermes.IssueContext{Number: 13, Title: "t", State: "OPEN", Body: "too short"}, nil
	}
	e.bot.handleFleetCommand(e.key, 42, "/ship #13")
	if got := nextFleetMessage(t, e.bot); !strings.Contains(got, "fleet_issue_reading") {
		t.Fatalf("issue read acknowledgement: message = %q", got)
	}
	if got := nextFleetMessage(t, e.bot); !strings.Contains(got, "#13") {
		t.Fatalf("vague issue: message = %q", got)
	}

	fleetFetchIssue = func(context.Context, string, int) (*hermes.IssueContext, error) {
		return nil, errors.New("gh: not found")
	}
	e.bot.handleFleetCommand(e.key, 42, "/ship #14")
	if got := nextFleetMessage(t, e.bot); !strings.Contains(got, "fleet_issue_reading") {
		t.Fatalf("issue read acknowledgement: message = %q", got)
	}
	if got := nextFleetMessage(t, e.bot); !strings.Contains(got, "fleet_issue_read_failed") {
		t.Fatalf("unreadable issue: message = %q", got)
	}

	if tasks, _ := e.svc.Store.ListTasks(context.Background(), fleet.TaskFilter{}); len(tasks) != 0 {
		t.Fatalf("gated issues created %d task(s)", len(tasks))
	}
}

func TestFleetCardIsEditedInPlaceOnStatusChange(t *testing.T) {
	e := newCardEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.cards.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	time.Sleep(50 * time.Millisecond) // let the subscription start at the log's end

	task := e.dispatch("/ship fix it")
	messageID := e.msgr.sent[0].MessageID

	if got := e.press(fleetActCancel, task.ID); got != "fleet_cb_cancel" {
		t.Fatalf("answer = %q", got)
	}
	e.msgr.waitEdits(t, 1)
	edit := e.msgr.lastEdit(t)
	if edit.MessageID != messageID || edit.Key != e.key {
		t.Fatalf("edited message %d in %+v, want %d in %+v", edit.MessageID, edit.Key, messageID, e.key)
	}
	if !strings.Contains(edit.Text, "fleet_status_cancelled") {
		t.Fatalf("card after cancel:\n%s", edit.Text)
	}
	if got := strings.Join(buttonData(edit.Buttons), " "); got != fleetCallbackData(fleetActDiscard, task.ID) {
		t.Fatalf("cancelled task cleanup buttons = %s", got)
	}
	if sent, _ := e.msgr.counts(); sent != 1 {
		t.Fatalf("status change sent %d messages, want the one card", sent)
	}
}

func TestFleetCardEditsAreThrottledAndFolded(t *testing.T) {
	e := newCardEnv(t)
	e.cards.MinInterval = 200 * time.Millisecond
	ctx := context.Background()
	task := e.dispatch("/ship fix it")

	// The task changes right after its card was posted, and is refreshed
	// several times in a row.
	if _, err := e.svc.Store.Transition(ctx, task.ID, fleet.StatusCancelled, "stop"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		e.cards.Refresh(ctx, task.ID)
	}
	if _, edits := e.msgr.counts(); edits != 0 {
		t.Fatalf("edited %d time(s) inside the interval", edits)
	}
	e.msgr.waitEdits(t, 1)
	time.Sleep(300 * time.Millisecond)
	if _, edits := e.msgr.counts(); edits != 1 {
		t.Fatalf("edits = %d, want the five refreshes folded into one", edits)
	}
}

func TestFleetCardSkipsEditsThatChangeNothing(t *testing.T) {
	e := newCardEnv(t)
	e.cards.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	task := e.dispatch("/ship fix it")
	for i := 0; i < 3; i++ {
		e.cards.refreshNow(context.Background(), task.ID)
	}
	if _, edits := e.msgr.counts(); edits != 0 {
		t.Fatalf("edits = %d for an unchanged card", edits)
	}
}

func TestFleetCardStillUpdatesAfterRestart(t *testing.T) {
	e := newCardEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")
	messageID := e.msgr.sent[0].MessageID

	// A new manager, as after a restart: it remembers nothing in memory.
	store, err := newFleetCardStore(e.storage.db)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &fleetCardManager{Fleet: e.svc, Store: store, Messenger: e.msgr, Localize: testLocalize}
	if _, err := e.svc.Store.Transition(ctx, task.ID, fleet.StatusCancelled, "stop"); err != nil {
		t.Fatal(err)
	}
	restarted.Refresh(ctx, task.ID)

	edit := e.msgr.lastEdit(t)
	if edit.MessageID != messageID || !strings.Contains(edit.Text, "fleet_status_cancelled") {
		t.Fatalf("after restart edited %d:\n%s", edit.MessageID, edit.Text)
	}
}

func TestFleetCardFailedEditIsRetriedOnNextRefresh(t *testing.T) {
	e := newCardEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")
	if _, err := e.svc.Store.Transition(ctx, task.ID, fleet.StatusCancelled, "stop"); err != nil {
		t.Fatal(err)
	}
	e.msgr.editErr = errors.New("telegram down")
	e.cards.refreshNow(ctx, task.ID)
	e.msgr.editErr = nil
	e.cards.refreshNow(ctx, task.ID)
	if _, edits := e.msgr.counts(); edits != 1 {
		t.Fatalf("edits = %d, want the failed edit sent again", edits)
	}
}

func TestFleetDiscardTakesTwoPresses(t *testing.T) {
	e := newCardEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")
	// A failed task offers retry and discard.
	for _, to := range []fleet.Status{fleet.StatusRunning, fleet.StatusFailed} {
		if _, err := e.svc.Store.Transition(ctx, task.ID, to, "test"); err != nil {
			t.Fatal(err)
		}
	}
	e.cards.refreshNow(ctx, task.ID)
	if got := buttonData(e.msgr.lastEdit(t).Buttons); len(got) != 2 || got[0] != "fleet:retry:"+task.ID || got[1] != "fleet:discard:"+task.ID {
		t.Fatalf("failed task buttons = %v", got)
	}

	if got := e.press(fleetActDiscard, task.ID); got != "fleet_cb_confirm" {
		t.Fatalf("first press answer = %q", got)
	}
	confirm := e.msgr.lastEdit(t)
	if got := buttonData(confirm.Buttons); len(got) != 2 || got[0] != "fleet:discard!:"+task.ID || got[1] != "fleet:back:"+task.ID {
		t.Fatalf("confirmation buttons = %v", got)
	}
	if !strings.Contains(confirm.Text, "fleet_card_confirm_discard") {
		t.Fatalf("confirmation text:\n%s", confirm.Text)
	}
	if got, _ := e.svc.Store.GetTask(ctx, task.ID); got.Status != fleet.StatusFailed {
		t.Fatalf("one press changed the task to %s", got.Status)
	}

	// Backing out restores the normal buttons and changes nothing.
	e.press(fleetActBack, task.ID)
	if got := buttonData(e.msgr.lastEdit(t).Buttons); len(got) != 2 || got[0] != "fleet:retry:"+task.ID {
		t.Fatalf("buttons after back = %v", got)
	}

	e.press(fleetActDiscard, task.ID)
	if got := e.press(fleetActDiscardConfirm, task.ID); got != "fleet_cb_discard" {
		t.Fatalf("second press answer = %q", got)
	}
	events, err := e.svc.Store.ListEvents(ctx, task.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	discarded := false
	for _, ev := range events {
		discarded = discarded || ev.Type == fleet.EventDiscarded
	}
	if !discarded {
		t.Fatalf("no %s event after the confirmed discard", fleet.EventDiscarded)
	}
}

func TestFleetCallbackRefusals(t *testing.T) {
	e := newCardEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")

	// Malformed data, an unknown action, a task with no card here.
	for _, data := range []string{"fleet:", "fleet:cancel", "fleet:cancel:", "fleet:explode:" + task.ID, "fleet:cancel:nosuchtask00"} {
		e.bot.handleFleetCallback(e.key, 42, "q1", data)
		if got := e.nextAnswer(); got != "fleet_cb_unknown" {
			t.Errorf("%q: answer = %q, want fleet_cb_unknown", data, got)
		}
	}
	// The same task's button pressed from another chat.
	e.bot.handleFleetCallback(chatKey{chatID: 8}, 42, "q1", fleetCallbackData(fleetActCancel, task.ID))
	if got := e.nextAnswer(); got != "fleet_cb_unknown" {
		t.Errorf("other chat: answer = %q", got)
	}
	if got, _ := e.svc.Store.GetTask(ctx, task.ID); got.Status != fleet.StatusQueued {
		t.Fatalf("refused presses changed the task to %s", got.Status)
	}

	// An action the task's status does not allow says why in the user's language.
	var err error
	e.bot.i18n, err = NewI18nManager("../../locales", "en")
	if err != nil {
		t.Fatal(err)
	}
	if got := e.press(fleetActApprove, task.ID); !strings.Contains(got, e.bot.fleetMsg(e.key.chatID, "fleet_why_status", map[string]string{"{status}": e.bot.fleetMsg(e.key.chatID, "fleet_status_queued", nil)})) {
		t.Fatalf("approve on a queued task: answer=%q", got)
	}
	e.bot.i18n = nil

	// With the fleet off, buttons from an earlier run say so.
	e.bot.fleet, e.bot.fleetCards = nil, nil
	e.bot.handleFleetCallback(e.key, 42, "q1", fleetCallbackData(fleetActCancel, task.ID))
	if got := e.nextAnswer(); got != "fleet_disabled" {
		t.Fatalf("disabled: answer = %q", got)
	}
}

func TestFleetCardButtonsByStatus(t *testing.T) {
	e := newCardEnv(t)
	e.cards.DashboardURL = "http://localhost:3939/"
	loc := func(key string, vars map[string]string) string { return testLocalize(0, key, vars) }
	id := "0123456789ab"
	details := "url:http://localhost:3939/fleet/tasks/" + id
	cases := []struct {
		name string
		task fleet.Task
		card fleetCard
		want []string
	}{
		{"queued", fleet.Task{Status: fleet.StatusQueued}, fleetCard{}, []string{"fleet:cancel:" + id, details}},
		{"running", fleet.Task{Status: fleet.StatusRunning}, fleetCard{}, []string{"fleet:cancel:" + id, details}},
		{"verifying", fleet.Task{Status: fleet.StatusVerifying}, fleetCard{}, []string{"fleet:cancel:" + id, details}},
		{"review ready", fleet.Task{Status: fleet.StatusReviewReady, Shape: fleet.ShapeShip, TrustMode: fleet.TrustLocalOnly}, fleetCard{}, []string{"fleet:approve:" + id, "fleet:discard:" + id, details}},
		{"needs decision", fleet.Task{Status: fleet.StatusNeedsDecision}, fleetCard{}, []string{"fleet:retry:" + id, "fleet:discard:" + id, details}},
		{"failed", fleet.Task{Status: fleet.StatusFailed}, fleetCard{}, []string{"fleet:retry:" + id, "fleet:discard:" + id, details}},
		{"cancelled", fleet.Task{Status: fleet.StatusCancelled}, fleetCard{}, []string{"fleet:discard:" + id, details}},
		{"done local", fleet.Task{Status: fleet.StatusDone, TrustMode: fleet.TrustLocalOnly}, fleetCard{}, []string{details}},
		{"done direct-pr with an open PR", fleet.Task{Status: fleet.StatusDone, TrustMode: fleet.TrustDirectPR, PRNumber: 5}, fleetCard{}, []string{"fleet:merge:" + id, details}},
		{"done direct-pr already merged", fleet.Task{Status: fleet.StatusDone, TrustMode: fleet.TrustDirectPR, PRNumber: 5}, fleetCard{Landed: true}, []string{details}},
		{"done no-mistakes", fleet.Task{Status: fleet.StatusDone, TrustMode: fleet.TrustNoMistakes, PRNumber: 5}, fleetCard{}, []string{details}},
	}
	for _, c := range cases {
		c.task.ID = id
		got := buttonData(e.cards.buttons(loc, c.task, c.card, "", fleetTaskActions(fleetActionInput{Task: c.task})))
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: buttons = %v, want %v", c.name, got, c.want)
		}
	}

	// Without a dashboard address there is no details link.
	e.cards.DashboardURL = ""
	if got := buttonData(e.cards.buttons(loc, fleet.Task{ID: id, Status: fleet.StatusCancelled}, fleetCard{}, "", fleetActionSet{})); len(got) != 0 {
		t.Errorf("no dashboard: buttons = %v", got)
	}
}

func TestFleetCallbackDataFitsTelegram(t *testing.T) {
	// Task IDs are 12 hex characters; allow twice that for headroom.
	id := strings.Repeat("f", 24)
	for _, action := range []string{fleetActCancel, fleetActRetry, fleetActApprove, fleetActMerge, fleetActDiscard, fleetActDiscardConfirm, fleetActBack} {
		data := fleetCallbackData(action, id)
		if len(data) > telegramCallbackDataLimit {
			t.Errorf("%s: %d bytes, over Telegram's %d", data, len(data), telegramCallbackDataLimit)
		}
		gotAction, gotID, ok := parseFleetCallback(data)
		if !ok || gotAction != action || gotID != id {
			t.Errorf("round trip of %q = %q, %q, %v", data, gotAction, gotID, ok)
		}
	}
	for _, bad := range []string{"", "hermes:fail:x", "fleet:", "fleet:cancel", "fleet::id", "fleet:cancel:", "fleet:cancel:a:b"} {
		if _, _, ok := parseFleetCallback(bad); ok {
			t.Errorf("parseFleetCallback(%q) accepted", bad)
		}
	}
}

func TestFleetCardShowsLatestToolAndClearsItOnStatusChange(t *testing.T) {
	e := newCardEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")
	if _, err := e.svc.Store.Transition(ctx, task.ID, fleet.StatusRunning, "slot free"); err != nil {
		t.Fatal(err)
	}
	e.cards.handleEvent(ctx, fleet.Event{TaskID: task.ID, Type: fleet.EventAgentToolUse,
		Payload: json.RawMessage(`{"tool":"Bash","input":{"command":"go test ./...\necho done"}}`)})
	if got := e.msgr.lastEdit(t).Text; !strings.Contains(got, "▸ Bash go test ./...") || strings.Contains(got, "echo done") {
		t.Fatalf("running card:\n%s", got)
	}
	if _, err := e.svc.Store.Transition(ctx, task.ID, fleet.StatusVerifying, "agent finished"); err != nil {
		t.Fatal(err)
	}
	e.cards.handleEvent(ctx, fleet.Event{TaskID: task.ID, Type: fleet.EventStatus})
	if got := e.msgr.lastEdit(t).Text; strings.Contains(got, "Bash") || !strings.Contains(got, "fleet_card_action_verifying") {
		t.Fatalf("verifying card:\n%s", got)
	}
}

func TestFleetCardExplainsRetryVersusReplyWhenASessionExists(t *testing.T) {
	e := newCardEnv(t)
	ctx := context.Background()
	task := e.dispatch("/ship fix it")
	if _, err := e.svc.Store.Transition(ctx, task.ID, fleet.StatusRunning, "slot free"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Store.Transition(ctx, task.ID, fleet.StatusNeedsDecision, "runner stopped: context canceled"); err != nil {
		t.Fatal(err)
	}
	e.cards.handleEvent(ctx, fleet.Event{TaskID: task.ID, Type: fleet.EventStatus})
	if got := e.msgr.lastEdit(t).Text; strings.Contains(got, "fleet_card_retry_vs_reply") {
		t.Fatalf("no session yet, but the card explains Retry vs Reply:\n%s", got)
	}
	if err := e.svc.Store.SetSession(ctx, task.ID, "sess-1"); err != nil {
		t.Fatal(err)
	}
	e.cards.handleEvent(ctx, fleet.Event{TaskID: task.ID, Type: fleet.EventStatus})
	if got := e.msgr.lastEdit(t).Text; !strings.Contains(got, "fleet_card_retry_vs_reply") {
		t.Fatalf("card with a session lacks the Retry vs Reply note:\n%s", got)
	}
}

func TestFleetToolSummary(t *testing.T) {
	cases := []struct{ payload, want string }{
		{`{"tool":"Edit","input":{"file_path":"internal/app/main.go","old_string":"a"}}`, "Edit internal/app/main.go"},
		{`{"tool":"Grep","input":{"pattern":"TODO"}}`, "Grep TODO"},
		{`{"tool":"Read","input":"{\"file_path\":\"clipped mid-w"}`, "Read"},
		{`{"tool":"TodoWrite","input":{}}`, "TodoWrite"},
		{`{"input":{}}`, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		if got := fleetToolSummary(json.RawMessage(c.payload)); got != c.want {
			t.Errorf("fleetToolSummary(%s) = %q, want %q", c.payload, got, c.want)
		}
	}
	long := fleetToolSummary(json.RawMessage(`{"tool":"Bash","input":{"command":"` + strings.Repeat("x", 500) + `"}}`))
	if len([]rune(long)) > 120 {
		t.Errorf("summary is %d runes, want at most 120", len([]rune(long)))
	}
}

func TestFleetElapsed(t *testing.T) {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := start.Add(d); return &v }
	now := start.Add(2*time.Hour + 5*time.Minute)
	cases := []struct {
		name string
		task fleet.Task
		want string
	}{
		{"not started", fleet.Task{Status: fleet.StatusQueued}, "0s"},
		{"running counts up to now", fleet.Task{Status: fleet.StatusRunning, StartedAt: at(0)}, "2h05m"},
		{"finished stops at the finish", fleet.Task{Status: fleet.StatusDone, StartedAt: at(0), FinishedAt: at(95 * time.Second)}, "1m35s"},
		{"waiting stops at the last update", fleet.Task{Status: fleet.StatusReviewReady, StartedAt: at(0), UpdatedAt: start.Add(42 * time.Second)}, "42s"},
	}
	for _, c := range cases {
		if got := fleetElapsed(c.task, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
