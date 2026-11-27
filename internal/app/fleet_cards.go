package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// A task card is the one Telegram message that stands for a fleet task. It
// is sent once and then edited in place as the task moves, so a task never
// floods its topic.

// fleetCallbackPrefix starts the callback data of every fleet button.
const fleetCallbackPrefix = "fleet:"

// Card button actions, as they appear in callback data:
// "fleet:<action>:<task ID>".
const (
	fleetActCancel         = "cancel"
	fleetActRetry          = "retry"
	fleetActApprove        = "approve"
	fleetActMerge          = "merge"
	fleetActDiscard        = "discard"  // asks for confirmation
	fleetActDiscardConfirm = "discard!" // discards
	fleetActBack           = "back"     // leaves a confirmation
)

// telegramCallbackDataLimit is Telegram's cap on callback_data, in bytes.
const telegramCallbackDataLimit = 64

// DefaultFleetCardInterval is the least time between two edits of one card.
const DefaultFleetCardInterval = 5 * time.Second

// fleetButton is one inline keyboard button: a callback when Data is set, a
// link when URL is set.
type fleetButton struct {
	Text string
	Data string
	URL  string
}

// fleetCallbackData builds the callback data for an action on a task.
func fleetCallbackData(action, taskID string) string {
	return fleetCallbackPrefix + action + ":" + taskID
}

// parseFleetCallback splits callback data into its action and task ID.
func parseFleetCallback(data string) (action, taskID string, ok bool) {
	rest, found := strings.CutPrefix(data, fleetCallbackPrefix)
	if !found {
		return "", "", false
	}
	action, taskID, found = strings.Cut(rest, ":")
	if !found || action == "" || taskID == "" || strings.Contains(taskID, ":") {
		return "", "", false
	}
	return action, taskID, true
}

// fleetMessenger sends and edits card messages. The bot implements it on its
// synchronous, retrying send path; tests substitute a recorder.
type fleetMessenger interface {
	SendCard(key chatKey, text string, buttons [][]fleetButton) (messageID int, err error)
	EditCard(key chatKey, messageID int, text string, buttons [][]fleetButton) error
}

// fleetCard records where a task's card lives.
type fleetCard struct {
	TaskID    string
	Key       chatKey
	MessageID int
	// Landed is set once the task's work has been merged, so the merge
	// button is not offered again.
	Landed bool
}

// fleetCardStore persists cards, so they can still be edited after a
// restart.
type fleetCardStore struct{ db *sql.DB }

func newFleetCardStore(db *sql.DB) (*fleetCardStore, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS alice_fleet_cards (
		task_id    TEXT PRIMARY KEY,
		chat_id    INTEGER NOT NULL,
		thread_id  INTEGER NOT NULL DEFAULT 0,
		message_id INTEGER NOT NULL,
		landed     INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("fleet cards: prepare table: %w", err)
	}
	return &fleetCardStore{db: db}, nil
}

func (s *fleetCardStore) Save(card fleetCard) error {
	_, err := s.db.Exec(`INSERT INTO alice_fleet_cards (task_id, chat_id, thread_id, message_id, landed, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			chat_id = excluded.chat_id, thread_id = excluded.thread_id, message_id = excluded.message_id`,
		card.TaskID, card.Key.chatID, card.Key.threadID, card.MessageID, card.Landed, time.Now().UnixMilli())
	return err
}

// Get returns the card of a task; ok is false when the task has none.
func (s *fleetCardStore) Get(taskID string) (card fleetCard, ok bool, err error) {
	card.TaskID = taskID
	err = s.db.QueryRow(`SELECT chat_id, thread_id, message_id, landed FROM alice_fleet_cards WHERE task_id = ?`, taskID).
		Scan(&card.Key.chatID, &card.Key.threadID, &card.MessageID, &card.Landed)
	if errors.Is(err, sql.ErrNoRows) {
		return fleetCard{}, false, nil
	}
	return card, err == nil, err
}

// ByMessage returns the card shown in a given message.
func (s *fleetCardStore) ByMessage(key chatKey, messageID int) (card fleetCard, ok bool, err error) {
	card.Key, card.MessageID = key, messageID
	err = s.db.QueryRow(`SELECT task_id, landed FROM alice_fleet_cards WHERE chat_id = ? AND message_id = ?`,
		key.chatID, messageID).Scan(&card.TaskID, &card.Landed)
	if errors.Is(err, sql.ErrNoRows) {
		return fleetCard{}, false, nil
	}
	return card, err == nil, err
}

func (s *fleetCardStore) MarkLanded(taskID string) error {
	_, err := s.db.Exec(`UPDATE alice_fleet_cards SET landed = 1 WHERE task_id = ?`, taskID)
	return err
}

// fleetCardManager posts task cards and keeps them current.
type fleetCardManager struct {
	Fleet     *FleetService
	Store     *fleetCardStore
	Messenger fleetMessenger
	// Localize returns a message in the chat's language.
	Localize func(chatID int64, key string, vars map[string]string) string
	// MinInterval is the least time between two edits of one card. Zero
	// means DefaultFleetCardInterval.
	MinInterval time.Duration
	// DashboardURL, when set, is the base of the "details" link.
	DashboardURL string

	now func() time.Time

	mu     sync.Mutex
	states map[string]*fleetCardState
	wg     sync.WaitGroup
}

// fleetCardState is what is remembered about one card between edits.
type fleetCardState struct {
	lastEdit time.Time
	// shown is what the card currently displays, to skip edits that would
	// change nothing (Telegram rejects those).
	shown string
	// pending is set while a delayed edit is scheduled.
	pending bool
	// action is the latest thing the agent was seen doing.
	action string
	// confirm is the action awaiting confirmation, if any.
	confirm string
}

func (m *fleetCardManager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *fleetCardManager) interval() time.Duration {
	if m.MinInterval > 0 {
		return m.MinInterval
	}
	return DefaultFleetCardInterval
}

// state returns the state of a task's card. The caller holds m.mu.
func (m *fleetCardManager) state(taskID string) *fleetCardState {
	if m.states == nil {
		m.states = map[string]*fleetCardState{}
	}
	st := m.states[taskID]
	if st == nil {
		st = &fleetCardState{}
		m.states[taskID] = st
	}
	return st
}

// Post sends the card for a task to a topic and remembers where it is.
func (m *fleetCardManager) Post(ctx context.Context, key chatKey, task fleet.Task) error {
	text, buttons := m.render(ctx, key.chatID, task, fleetCard{TaskID: task.ID}, "", "")
	messageID, err := m.Messenger.SendCard(key, text, buttons)
	if err != nil {
		return err
	}
	m.mu.Lock()
	st := m.state(task.ID)
	st.lastEdit, st.shown = m.clock(), fleetCardFingerprint(text, buttons)
	m.mu.Unlock()
	return m.Store.Save(fleetCard{TaskID: task.ID, Key: key, MessageID: messageID})
}

// Run follows the fleet's events and refreshes the affected cards until ctx
// ends. Cards of tasks that changed while alice was down are refreshed by
// the events recovery writes at startup.
func (m *fleetCardManager) Run(ctx context.Context) error {
	events, err := m.Fleet.Bus.Subscribe(ctx, fleet.EventFilter{})
	if err != nil {
		return err
	}
	for ev := range events {
		m.handleEvent(ctx, ev.Event)
	}
	m.wg.Wait()
	return nil
}

// cardEvents are the events that change what a card shows.
var cardEvents = map[string]bool{
	fleet.EventStatus:            true,
	fleet.EventVerification:      true,
	fleet.EventAgentToolUse:      true,
	fleet.EventAgentResult:       true,
	fleet.EventDeliveryCommitted: true,
	fleet.EventDeliveryPushed:    true,
	fleet.EventDeliveryPROpened:  true,
	fleet.EventDeliveryApproved:  true,
	fleet.EventDiscarded:         true,
}

func (m *fleetCardManager) handleEvent(ctx context.Context, ev fleet.Event) {
	if ev.TaskID == "" || !cardEvents[ev.Type] {
		return
	}
	switch ev.Type {
	case fleet.EventAgentToolUse:
		if action := fleetToolSummary(ev.Payload); action != "" {
			m.mu.Lock()
			m.state(ev.TaskID).action = action
			m.mu.Unlock()
		}
	case fleet.EventStatus:
		// A new status ends whatever the agent was doing and any pending
		// confirmation: the buttons are about to change.
		m.mu.Lock()
		st := m.state(ev.TaskID)
		st.action, st.confirm = "", ""
		m.mu.Unlock()
	case fleet.EventDeliveryApproved:
		if err := m.Store.MarkLanded(ev.TaskID); err != nil {
			log.Printf("[fleet] mark card landed for task %s: %v", ev.TaskID, err)
		}
	}
	m.Refresh(ctx, ev.TaskID)
}

// SetConfirm puts a card into, or with action "" out of, a confirmation for
// an action, and redraws it at once.
func (m *fleetCardManager) SetConfirm(ctx context.Context, taskID, action string) {
	m.mu.Lock()
	m.state(taskID).confirm = action
	m.mu.Unlock()
	m.refreshNow(ctx, taskID)
}

// Refresh brings a task's card up to date, now if the card has not been
// edited within MinInterval, otherwise once that interval has passed.
// Refreshes that arrive while one is waiting are folded into it.
func (m *fleetCardManager) Refresh(ctx context.Context, taskID string) {
	m.mu.Lock()
	st := m.state(taskID)
	wait := m.interval() - m.clock().Sub(st.lastEdit)
	if wait <= 0 {
		m.mu.Unlock()
		m.refreshNow(ctx, taskID)
		return
	}
	if st.pending {
		m.mu.Unlock()
		return
	}
	st.pending = true
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		m.mu.Lock()
		st.pending = false
		m.mu.Unlock()
		m.refreshNow(ctx, taskID)
	}()
}

// refreshNow redraws a task's card if it has one and its content changed.
func (m *fleetCardManager) refreshNow(ctx context.Context, taskID string) {
	card, ok, err := m.Store.Get(taskID)
	if err != nil {
		log.Printf("[fleet] load card of task %s: %v", taskID, err)
		return
	}
	if !ok {
		return
	}
	task, err := m.Fleet.Store.GetTask(ctx, taskID)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[fleet] load task %s for its card: %v", taskID, err)
		}
		return
	}

	m.mu.Lock()
	st := m.state(taskID)
	action, confirm := st.action, st.confirm
	m.mu.Unlock()

	text, buttons := m.render(ctx, card.Key.chatID, task, card, action, confirm)
	fingerprint := fleetCardFingerprint(text, buttons)

	m.mu.Lock()
	if st.shown == fingerprint {
		m.mu.Unlock()
		return
	}
	// Claim the edit before sending it, so a concurrent refresh neither
	// repeats it nor sends inside the interval.
	previous := st.shown
	st.shown, st.lastEdit = fingerprint, m.clock()
	m.mu.Unlock()

	if err := m.Messenger.EditCard(card.Key, card.MessageID, text, buttons); err != nil {
		log.Printf("[fleet] edit card of task %s: %v", taskID, err)
		m.mu.Lock()
		if st.shown == fingerprint {
			st.shown = previous
		}
		m.mu.Unlock()
	}
}

func fleetCardFingerprint(text string, buttons [][]fleetButton) string {
	raw, _ := json.Marshal(buttons)
	return text + "\x00" + string(raw)
}

// fleetStatusEmoji marks each status on a card.
var fleetStatusEmoji = map[fleet.Status]string{
	fleet.StatusQueued:        "🕓",
	fleet.StatusRunning:       "🏃",
	fleet.StatusVerifying:     "🔎",
	fleet.StatusReviewReady:   "👀",
	fleet.StatusNeedsDecision: "❓",
	fleet.StatusDone:          "✅",
	fleet.StatusFailed:        "❌",
	fleet.StatusCancelled:     "🚫",
}

// render builds a card's text and buttons in the chat's language.
func (m *fleetCardManager) render(ctx context.Context, chatID int64, task fleet.Task, card fleetCard, action, confirm string) (string, [][]fleetButton) {
	loc := func(key string, vars map[string]string) string { return m.Localize(chatID, key, vars) }

	projectName := task.ProjectID
	if project, err := m.Fleet.Store.GetProject(ctx, task.ProjectID); err == nil && project.Name != "" {
		projectName = project.Name
	}
	shape := loc("fleet_shape_"+string(task.Shape), nil)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %s · %s · %s\n", fleetStatusEmoji[task.Status],
		loc("fleet_status_"+string(task.Status), nil), shape, projectName, task.ID)
	b.WriteString(fleetGoalLine(task))
	b.WriteString("\n")
	b.WriteString(loc("fleet_card_meter", map[string]string{
		"{elapsed}": fleetElapsed(task, m.clock()),
		"{cost}":    fmt.Sprintf("%.2f", task.CostUSD),
	}))
	if line := m.actionLine(loc, task, action); line != "" {
		b.WriteString("\n▸ ")
		b.WriteString(line)
	}
	if task.PRURL != "" {
		b.WriteString("\n🔗 ")
		b.WriteString(task.PRURL)
	}
	if task.Status == fleet.StatusNeedsDecision && task.SessionID != "" {
		b.WriteString("\n\n")
		b.WriteString(loc("fleet_card_retry_vs_reply", nil))
	}
	if confirm == fleetActDiscard {
		b.WriteString("\n\n")
		b.WriteString(loc("fleet_card_confirm_discard", nil))
	}
	return b.String(), m.buttons(loc, task, card, confirm)
}

// actionLine is the card's one-line account of what is happening.
func (m *fleetCardManager) actionLine(loc func(string, map[string]string) string, task fleet.Task, action string) string {
	switch task.Status {
	case fleet.StatusQueued:
		return loc("fleet_card_action_queued", nil)
	case fleet.StatusRunning:
		if action != "" {
			return action
		}
		return loc("fleet_card_action_running", nil)
	case fleet.StatusVerifying:
		return loc("fleet_card_action_verifying", nil)
	}
	return fleetClip(firstLineOf(task.StatusReason), 160)
}

// buttons returns the actions a task's status allows.
func (m *fleetCardManager) buttons(loc func(string, map[string]string) string, task fleet.Task, card fleetCard, confirm string) [][]fleetButton {
	act := func(action string) fleetButton {
		return fleetButton{Text: loc("fleet_btn_"+strings.TrimSuffix(action, "!"), nil), Data: fleetCallbackData(action, task.ID)}
	}
	if confirm == fleetActDiscard {
		return [][]fleetButton{{
			{Text: loc("fleet_btn_discard_confirm", nil), Data: fleetCallbackData(fleetActDiscardConfirm, task.ID)},
			act(fleetActBack),
		}}
	}

	var row []fleetButton
	switch task.Status {
	case fleet.StatusQueued, fleet.StatusRunning, fleet.StatusVerifying:
		row = append(row, act(fleetActCancel))
	case fleet.StatusReviewReady:
		row = append(row, act(fleetActApprove), act(fleetActDiscard))
	case fleet.StatusNeedsDecision:
		row = append(row, act(fleetActRetry), act(fleetActDiscard))
	case fleet.StatusFailed:
		row = append(row, act(fleetActRetry), act(fleetActDiscard))
	case fleet.StatusDone:
		// A direct-pr task is done once its pull request is open; merging
		// it is still the owner's call.
		if task.TrustMode == fleet.TrustDirectPR && task.PRNumber > 0 && !card.Landed {
			row = append(row, act(fleetActMerge))
		}
	}
	var rows [][]fleetButton
	if len(row) > 0 {
		rows = append(rows, row)
	}
	if url := m.detailsURL(task.ID); url != "" {
		rows = append(rows, []fleetButton{{Text: loc("fleet_btn_details", nil), URL: url}})
	}
	return rows
}

func (m *fleetCardManager) detailsURL(taskID string) string {
	base := strings.TrimRight(strings.TrimSpace(m.DashboardURL), "/")
	if base == "" {
		return ""
	}
	return base + "/fleet/tasks/" + taskID
}

// fleetGoalLine is the first line of a task's goal, with its issue number.
func fleetGoalLine(task fleet.Task) string {
	goal := fleetClip(firstLineOf(task.Goal), 140)
	if task.Source.Kind == fleet.SourceIssue && task.Source.IssueNumber > 0 {
		prefix := "#" + strconv.Itoa(task.Source.IssueNumber)
		if !strings.Contains(goal, prefix) {
			goal = prefix + " " + goal
		}
	}
	return goal
}

// fleetElapsed is how long the task's current run has taken, or took.
func fleetElapsed(task fleet.Task, now time.Time) string {
	if task.StartedAt == nil {
		return "0s"
	}
	end := now
	switch {
	case task.FinishedAt != nil:
		end = *task.FinishedAt
	case !task.Status.IsActive():
		end = task.UpdatedAt
	}
	d := end.Sub(*task.StartedAt)
	if d < 0 {
		d = 0
	}
	return fleetDuration(d)
}

func fleetDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h, mnt, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, mnt)
	case mnt > 0:
		return fmt.Sprintf("%dm%02ds", mnt, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// fleetToolSummary describes a tool call in one short line, e.g.
// "Edit internal/app/main.go" or "Bash go test ./...".
func fleetToolSummary(payload json.RawMessage) string {
	var use struct {
		Tool  string          `json:"tool"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(payload, &use); err != nil || use.Tool == "" {
		return ""
	}
	var input map[string]any
	// The stored input may be clipped mid-way and so not parse; the tool
	// name alone is still worth showing.
	_ = json.Unmarshal(use.Input, &input)
	for _, field := range []string{"file_path", "path", "command", "pattern", "description", "query", "url"} {
		if value, ok := input[field].(string); ok && strings.TrimSpace(value) != "" {
			return fleetClip(use.Tool+" "+firstLineOf(value), 120)
		}
	}
	return use.Tool
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// fleetClip shortens s to at most max runes.
func fleetClip(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
