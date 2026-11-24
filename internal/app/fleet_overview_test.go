package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// nextQueued returns the next message the bot queued, with its buttons'
// callback data.
func nextQueued(t *testing.T, bot *TelegramBot) (text string, callbacks []string) {
	t.Helper()
	select {
	case msg := <-bot.messageQueue:
		text, _ = msg.Params["text"].(string)
		if markup, ok := msg.Params["reply_markup"].(map[string]interface{}); ok {
			if rows, ok := markup["inline_keyboard"].([][]map[string]interface{}); ok {
				for _, row := range rows {
					for _, b := range row {
						data, _ := b["callback_data"].(string)
						callbacks = append(callbacks, data)
					}
				}
			}
		}
		return text, callbacks
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
		return "", nil
	}
}

// useLocale makes the bot answer a chat in real zh-TW text, for tests that
// check what a message says rather than which message it is.
func useLocale(t *testing.T, bot *TelegramBot, chatID int64) {
	t.Helper()
	i18n, err := NewI18nManager(filepath.Join("..", "..", "locales"), "en")
	if err != nil {
		t.Fatalf("NewI18nManager: %v", err)
	}
	bot.i18n = i18n
	bot.langPreferences[chatID] = "zh-TW"
}

func drainQueue(bot *TelegramBot) {
	for {
		select {
		case <-bot.messageQueue:
		default:
			return
		}
	}
}

func TestFleetOverviewCommand(t *testing.T) {
	e := newNotifyTestEnv(t)
	useLocale(t, e.bot, e.key.chatID)
	running := e.dispatch("/ship one")
	e.walk(running.ID, "slot free", fleet.StatusRunning)
	blocked := e.dispatch("/ship two")
	e.walk(blocked.ID, "checks kept failing", fleet.StatusRunning, fleet.StatusNeedsDecision)
	e.dispatch("/ship three") // stays queued

	if !e.bot.handleFleetCommand(e.key, 42, "/fleet") {
		t.Fatal("/fleet was not handled")
	}
	text, callbacks := nextQueued(t, e.bot)
	for _, want := range []string{
		"艦隊總覽", "📁 " + e.project.Name, "執行中 1", "排隊 1", "待決策 1", "待審 0", "💰 $0.00",
		"通知模式：一般",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/fleet misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "合計") {
		t.Errorf("a single project needs no total:\n%s", text)
	}
	if len(callbacks) != 1 || callbacks[0] != "fleet:tasks:all" {
		t.Fatalf("/fleet buttons = %v", callbacks)
	}

	// It works from a topic with no project too.
	if !e.bot.handleFleetCommand(chatKey{chatID: 7, threadID: 99}, 42, "/fleet") {
		t.Fatal("/fleet on an unbound topic was not handled")
	}
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "艦隊總覽") {
		t.Fatalf("unbound topic: %q", text)
	}

	e.bot.handleFleetCommand(e.key, 42, "/fleet nonsense")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "/fleet tasks") {
		t.Fatalf("/fleet nonsense: %q", text)
	}
}

func TestFleetTaskListOpensCards(t *testing.T) {
	e := newNotifyTestEnv(t)

	e.bot.handleFleetCommand(e.key, 42, "/fleet tasks")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "fleet_tasks_empty") {
		t.Fatalf("empty list: %q", text)
	}
	e.bot.handleFleetCommand(chatKey{chatID: 7, threadID: 99}, 42, "/fleet tasks")
	if text, _ := nextQueued(t, e.bot); text != "fleet_no_project" {
		t.Fatalf("unbound topic: %q", text)
	}

	first := e.dispatch("/ship one")
	second := e.dispatch("/ship two")
	e.bot.handleFleetCommand(e.key, 42, "/fleet tasks")
	text, callbacks := nextQueued(t, e.bot)
	if !strings.Contains(text, "fleet_tasks_title") || len(callbacks) != 2 {
		t.Fatalf("list = %q, %v", text, callbacks)
	}
	want := map[string]bool{"fleet:card:" + first.ID: true, "fleet:card:" + second.ID: true}
	for _, c := range callbacks {
		if !want[c] {
			t.Errorf("unexpected button %q", c)
		}
	}

	// Opening a card posts it here and makes this the card that is updated.
	other := chatKey{chatID: 7, threadID: 50}
	sentBefore, _ := e.msgr.counts()
	e.bot.handleFleetCallback(other, 42, "q1", "fleet:card:"+first.ID)
	if sent, _ := e.msgr.counts(); sent != sentBefore+1 {
		t.Fatalf("opening a card sent %d message(s)", sent-sentBefore)
	}
	card, ok, err := e.cards.Store.Get(first.ID)
	if err != nil || !ok || card.Key != other {
		t.Fatalf("card now at %+v (%v, %v), want %+v", card.Key, ok, err, other)
	}

	drainQueue(e.bot)
	e.bot.handleFleetCallback(e.key, 42, "q1", "fleet:card:nosuchtask00")
	if got := e.nextAnswer(); !strings.Contains(got, "fleet_error") {
		t.Fatalf("opening an unknown task: %q", got)
	}
}

func TestFleetStatusAndHelpAdditions(t *testing.T) {
	e := newNotifyTestEnv(t)
	useLocale(t, e.bot, e.key.chatID)
	e.dispatch("/ship one")
	drainQueue(e.bot)

	e.bot.fleetAfterCommand(e.key, 42, "/status")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "🚢 Fleet") || !strings.Contains(text, "🕓 1") || !strings.Contains(text, "通知：一般") {
		t.Fatalf("/status addition: %q", text)
	}
	e.bot.fleetAfterCommand(e.key, 42, "/help")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "/ship") || !strings.Contains(text, "/afk") {
		t.Fatalf("/help addition: %q", text)
	}
}

func TestFleetCompatHermes(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()

	if !e.bot.handleFleetCommand(e.key, 42, "/hermes 修好登入測試") {
		t.Fatal("/hermes was not handled")
	}
	if text, _ := nextQueued(t, e.bot); text != "fleet_compat_hermes" {
		t.Fatalf("/hermes note: %q", text)
	}
	tasks, _ := e.svc.Store.ListTasks(ctx, fleet.TaskFilter{})
	if len(tasks) != 1 || tasks[0].Goal != "修好登入測試" || tasks[0].Shape != fleet.ShapeShip {
		t.Fatalf("tasks = %+v", tasks)
	}
	if sent, _ := e.msgr.counts(); sent != 1 {
		t.Fatalf("cards = %d, want the new task's", sent)
	}

	e.bot.handleFleetCommand(e.key, 42, "/ghermes another thing")
	if text, _ := nextQueued(t, e.bot); text != "fleet_compat_ghermes" {
		t.Fatalf("/ghermes note: %q", text)
	}

	// Bare /hermes explains itself and shows how to dispatch.
	e.bot.handleFleetCommand(e.key, 42, "/hermes")
	if text, _ := nextQueued(t, e.bot); text != "fleet_compat_hermes" {
		t.Fatalf("bare /hermes note: %q", text)
	}
	if text, _ := nextQueued(t, e.bot); text != "fleet_ship_usage" {
		t.Fatalf("bare /hermes usage: %q", text)
	}

	e.bot.handleFleetCommand(e.key, 42, "/hermes status")
	nextQueued(t, e.bot)
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "fleet_tasks_title") {
		t.Fatalf("/hermes status: %q", text)
	}

	// Listing GitHub issues is left to the old command.
	if e.bot.handleFleetCommand(e.key, 42, "/hermes issues") {
		t.Fatal("/hermes issues was taken over")
	}
}

func TestFleetCompatAbortRetryResumeClose(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()
	status := func(id string) fleet.Status {
		task, err := e.svc.Store.GetTask(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return task.Status
	}

	// With nothing in the fleet to act on, the old commands run.
	for _, text := range []string{"/abort", "/retry", "/resume", "/close", "/close #12", "/retry 3", "/resume abc retry"} {
		if e.bot.handleFleetCommand(e.key, 42, text) {
			t.Errorf("%q was taken over with no fleet task to act on", text)
		}
	}

	one := e.dispatch("/ship one")
	drainQueue(e.bot)
	if !e.bot.handleFleetCommand(e.key, 42, "/abort") {
		t.Fatal("/abort with one active task was not handled")
	}
	if text, _ := nextQueued(t, e.bot); text != "fleet_compat_cancelled" {
		t.Fatalf("/abort: %q", text)
	}
	if status(one.ID) != fleet.StatusCancelled {
		t.Fatalf("after /abort the task is %s", status(one.ID))
	}

	// /retry acts on the one failed task.
	two := e.dispatch("/ship two")
	e.walk(two.ID, "agent crashed", fleet.StatusRunning, fleet.StatusFailed)
	drainQueue(e.bot)
	e.bot.handleFleetCommand(e.key, 42, "/retry")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "fleet_compat_retried") {
		t.Fatalf("/retry: %q", text)
	}
	if status(two.ID) != fleet.StatusQueued {
		t.Fatalf("after /retry the task is %s", status(two.ID))
	}

	// Several candidates: a list to pick from, and nothing is changed.
	three := e.dispatch("/ship three")
	drainQueue(e.bot)
	e.bot.handleFleetCommand(e.key, 42, "/abort")
	text, callbacks := nextQueued(t, e.bot)
	if !strings.Contains(text, "fleet_compat_pick") || len(callbacks) != 2 {
		t.Fatalf("/abort with two active tasks: %q %v", text, callbacks)
	}
	if status(two.ID) != fleet.StatusQueued || status(three.ID) != fleet.StatusQueued {
		t.Fatal("an ambiguous /abort cancelled something")
	}

	// Naming the task removes the ambiguity.
	e.bot.handleFleetCommand(e.key, 42, "/abort "+three.ID)
	nextQueued(t, e.bot)
	if status(three.ID) != fleet.StatusCancelled || status(two.ID) != fleet.StatusQueued {
		t.Fatalf("/abort <id>: two=%s three=%s", status(two.ID), status(three.ID))
	}

	// /resume carries on a blocked task, with instructions if given.
	e.walk(two.ID, "checks kept failing", fleet.StatusRunning, fleet.StatusNeedsDecision)
	drainQueue(e.bot)
	e.bot.handleFleetCommand(e.key, 42, "/resume "+two.ID+" 先跑 go mod tidy")
	nextQueued(t, e.bot)
	audit, _ := e.svc.Commander.Audit(ctx, fleet.AuditFilter{TaskID: two.ID})
	found := false
	for _, entry := range audit {
		found = found || (entry.Command == fleet.CmdReply && entry.Detail == "先跑 go mod tidy")
	}
	if !found {
		t.Fatalf("/resume <id> <text> did not reach the task: %+v", audit)
	}

	// /close <task id> discards; /close #<n> is still the issue command.
	if e.bot.handleFleetCommand(e.key, 42, "/close #12") {
		t.Fatal("/close #12 was taken over")
	}
	if !e.bot.handleFleetCommand(e.key, 42, "/close "+one.ID) {
		t.Fatal("/close <task id> was not handled")
	}
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "fleet_compat_discarded") {
		t.Fatalf("/close <task id>: %q", text)
	}
}

func TestFleetCompatAbortLeavesABusyChatAgentToTheOldCommand(t *testing.T) {
	e := newNotifyTestEnv(t)
	task := e.dispatch("/ship one")
	agent := NewAgent(&mockClient{}, e.project.RepoPath, e.key.chatID, e.key.threadID)
	e.bot.agents[e.key] = agent
	if agent.IsProcessing() {
		t.Skip("a fresh agent reports itself busy; cannot set up the idle case")
	}
	if !e.bot.handleFleetCommand(e.key, 42, "/abort") {
		t.Fatal("with an idle chat agent, /abort should cancel the fleet task")
	}
	if got, _ := e.svc.Store.GetTask(context.Background(), task.ID); got.Status != fleet.StatusCancelled {
		t.Fatalf("task is %s", got.Status)
	}
}

func TestFleetCompatParallel(t *testing.T) {
	e := newNotifyTestEnv(t)
	ctx := context.Background()

	e.bot.handleFleetCommand(e.key, 42, "/parallel\n1. only one")
	if text, _ := nextQueued(t, e.bot); text != "parallel_min_tasks" {
		t.Fatalf("one task: %q", text)
	}

	if !e.bot.handleFleetCommand(e.key, 42, "/parallel\n1. 更新 README\n2. 補上單元測試\n3. 修正 lint") {
		t.Fatal("/parallel was not handled")
	}
	if text, _ := nextQueued(t, e.bot); text != "fleet_compat_parallel" {
		t.Fatalf("/parallel note: %q", text)
	}
	tasks, _ := e.svc.Store.ListTasks(ctx, fleet.TaskFilter{})
	if len(tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(tasks))
	}
	if sent, _ := e.msgr.counts(); sent != 3 {
		t.Fatalf("cards = %d, want one per task", sent)
	}
}

func TestFleetCompatStrictAndMR(t *testing.T) {
	e := newNotifyTestEnv(t)
	useLocale(t, e.bot, e.key.chatID)

	e.bot.handleFleetCommand(e.key, 42, "/strict")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "/strict on") || !strings.Contains(text, "目前模式：local-only") {
		t.Fatalf("/strict: %q", text)
	}
	// The test repository has no remote, so the strict mode cannot be set.
	e.bot.handleFleetCommand(e.key, 42, "/strict on")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "git remote") {
		t.Fatalf("/strict on without a remote: %q", text)
	}
	e.bot.handleFleetCommand(e.key, 42, "/strict off")
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, e.project.Name+" 的信任模式已設為 local-only") {
		t.Fatalf("/strict off: %q", text)
	}

	if !e.bot.handleFleetCommand(e.key, 42, "/mr 12") {
		t.Fatal("/mr was not handled")
	}
	if text, _ := nextQueued(t, e.bot); !strings.Contains(text, "/mr") || !strings.Contains(text, "/scout") {
		t.Fatalf("/mr: %q", text)
	}
}

func TestFleetCompatDoesNothingWhenDisabled(t *testing.T) {
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 4), langPreferences: map[int64]string{}}
	key := chatKey{chatID: 7, threadID: 3}
	for _, text := range []string{"/hermes fix it", "/abort", "/retry", "/resume", "/close x", "/parallel\n1. a\n2. b", "/strict on", "/mr 1", "/ship x", "/fleet", "/quiet"} {
		if bot.handleFleetCommand(key, 42, text) {
			t.Errorf("%q handled with the fleet disabled", text)
		}
	}
	expectNoFleetMessage(t, bot)
}

// With the real locale files, a card and a prompt come out with every
// placeholder filled in.
func TestFleetMessagesFillTheirPlaceholders(t *testing.T) {
	e := newNotifyTestEnv(t)
	useLocale(t, e.bot, e.key.chatID)
	e.cards.Localize, e.notices.Localize = e.bot.fleetMsg, e.bot.fleetMsg

	task := e.dispatch("/ship 修好登入測試 --budget 2")
	card := e.msgr.sent[0].Text
	for _, want := range []string{"排隊中", "交付", task.ID, "修好登入測試", "💰 $0.00", "等待空位"} {
		if !strings.Contains(card, want) {
			t.Errorf("card misses %q:\n%s", want, card)
		}
	}
	e.walk(task.ID, "verification failed 3 times", fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusNeedsDecision)
	sent := e.notify()
	if len(sent) != 1 {
		t.Fatalf("sent %d message(s), want the prompt", len(sent))
	}
	e.bot.handleFleetCommand(e.key, 42, "/afk 2h approve "+e.project.Name)
	afk, _ := nextQueued(t, e.bot)
	e.bot.handleFleetCommand(e.key, 42, "/back")
	back, _ := nextQueued(t, e.bot)

	for name, text := range map[string]string{"card": card, "prompt": sent[0].Text, "afk": afk, "back": back} {
		if strings.Contains(text, "{") || strings.Contains(text, "fleet_") {
			t.Errorf("%s has an unfilled placeholder or a raw key:\n%s", name, text)
		}
	}
	if !strings.Contains(sent[0].Text, "需要你決定") || !strings.Contains(afk, "自動核准") || !strings.Contains(afk, e.project.ID) {
		t.Errorf("unexpected wording:\nprompt: %s\nafk: %s", sent[0].Text, afk)
	}
}
