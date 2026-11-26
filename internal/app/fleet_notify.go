package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// The fleet tells its owner about tasks through one Notifier. This file
// turns what the Notifier decides to say into Telegram messages:
//
//   - a decision prompt when a task cannot move without the owner;
//   - a delivery message when a task's work is ready or finished;
//   - a summary after a quiet or away period.

// Prompt button actions, beside the card's own.
const (
	fleetActPass          = "pass"     // wave a blocked task through to review
	fleetActHint          = "hint"     // ask for a hint to retry with
	fleetActGoOn          = "goon"     // continue past a reached budget
	fleetActPromptDiscard = "pdiscard" // discard from a prompt; takes two presses
	fleetActCard          = "card"     // post the task's card here
)

// fleetDiscardConfirmWindow is how long a first press on a prompt's discard
// button waits for the second.
const fleetDiscardConfirmWindow = time.Minute

// fleetPrompt records a message the fleet sent about a task, so that a
// reply to it reaches the task and a repeat of it can be recognised.
type fleetPrompt struct {
	Key       chatKey
	MessageID int
	TaskID    string
	// Reason identifies what the message was about. Two messages with the
	// same reason for the same task say the same thing.
	Reason string
}

func (s *fleetCardStore) preparePrompts() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS alice_fleet_prompts (
			chat_id    INTEGER NOT NULL,
			message_id INTEGER NOT NULL,
			thread_id  INTEGER NOT NULL DEFAULT 0,
			task_id    TEXT NOT NULL,
			reason     TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			PRIMARY KEY (chat_id, message_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alice_fleet_prompts_task ON alice_fleet_prompts (task_id)`,
		`CREATE TABLE IF NOT EXISTS alice_fleet_settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("fleet prompts: prepare tables: %w", err)
		}
	}
	return nil
}

func (s *fleetCardStore) SavePrompt(p fleetPrompt) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO alice_fleet_prompts
		(chat_id, message_id, thread_id, task_id, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Key.chatID, p.MessageID, p.Key.threadID, p.TaskID, p.Reason, time.Now().UnixMilli())
	return err
}

// PromptByMessage returns the task a fleet message is about.
func (s *fleetCardStore) PromptByMessage(chatID int64, messageID int) (taskID string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT task_id FROM alice_fleet_prompts WHERE chat_id = ? AND message_id = ?`,
		chatID, messageID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return taskID, err == nil, err
}

// HasPrompt reports whether a task has been written about in a chat, and
// with reason set, whether for that reason.
func (s *fleetCardStore) HasPrompt(chatID int64, taskID, reason string) (bool, error) {
	query := `SELECT COUNT(*) FROM alice_fleet_prompts WHERE task_id = ?`
	args := []any{taskID}
	if chatID != 0 {
		query += ` AND chat_id = ?`
		args = append(args, chatID)
	}
	if reason != "" {
		query += ` AND reason = ?`
		args = append(args, reason)
	}
	var n int
	err := s.db.QueryRow(query, args...).Scan(&n)
	return n > 0, err
}

func (s *fleetCardStore) Setting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM alice_fleet_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *fleetCardStore) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO alice_fleet_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// fleetDocSender sends a file as a Telegram document.
type fleetDocSender interface {
	SendDocument(key chatKey, path, caption string) error
}

// topicBinding is one row of topic_settings.
type topicBinding struct {
	Key chatKey
	Dir string
}

// topicBindingLister is the part of storage used to find a project's topic.
type topicBindingLister interface {
	ListTopicBindings() ([]topicBinding, error)
}

// fleetNotices turns the Notifier's messages into Telegram messages.
type fleetNotices struct {
	Fleet     *FleetService
	Store     *fleetCardStore
	Messenger fleetMessenger
	Docs      fleetDocSender
	Localize  func(chatID int64, key string, vars map[string]string) string
	// Owner is the Telegram user the fleet answers to. Messages with no
	// better destination go to the owner's private chat.
	Owner int64
	// Topics finds the topic a project is bound to; nil when unavailable.
	Topics topicBindingLister

	mu sync.Mutex
	// discardPressed is when a prompt's discard button was first pressed,
	// per task.
	discardPressed map[string]time.Time
}

const fleetHomeChatSetting = "home_chat"

// SetHome remembers the chat the owner last set a notification mode from;
// summaries are sent there.
func (n *fleetNotices) SetHome(key chatKey) {
	if err := n.Store.SetSetting(fleetHomeChatSetting, fmt.Sprintf("%d:%d", key.chatID, key.threadID)); err != nil {
		log.Printf("[fleet] remember home chat: %v", err)
	}
}

// home is where messages that concern no particular topic are sent.
func (n *fleetNotices) home() chatKey {
	if raw, err := n.Store.Setting(fleetHomeChatSetting); err == nil && raw != "" {
		chat, thread, _ := strings.Cut(raw, ":")
		chatID, err1 := strconv.ParseInt(chat, 10, 64)
		threadID, err2 := strconv.Atoi(thread)
		if err1 == nil && err2 == nil && chatID != 0 {
			return chatKey{chatID: chatID, threadID: threadID}
		}
	}
	return chatKey{chatID: n.Owner}
}

// destination picks where to write about a task: where its card is, else a
// topic bound to its project, else the owner's home chat.
func (n *fleetNotices) destination(ctx context.Context, taskID, projectID string) chatKey {
	if taskID != "" {
		if card, ok, err := n.Store.Get(taskID); err == nil && ok {
			return card.Key
		}
	}
	if projectID != "" && n.Topics != nil {
		if project, err := n.Fleet.Store.GetProject(ctx, projectID); err == nil {
			if bindings, err := n.Topics.ListTopicBindings(); err == nil {
				for _, b := range bindings {
					if fleetPathWithin(b.Dir, project.RepoPath) {
						return b.Key
					}
				}
			}
		}
	}
	return n.home()
}

// Deliver sends one notification. It is the Notifier's Deliver function: an
// error leaves the wakes pending, to be tried again.
func (n *fleetNotices) Deliver(ctx context.Context, note fleet.Notification) error {
	if note.Summary != nil {
		key := n.home()
		_, err := n.Messenger.SendCard(key, n.renderSummary(key.chatID, *note.Summary), nil)
		return err
	}
	if len(note.Wakes) == 0 {
		return nil
	}
	if note.TaskID == "" {
		return n.deliverGeneral(ctx, note)
	}
	task, err := n.Fleet.Store.GetTask(ctx, note.TaskID)
	if errors.Is(err, fleet.ErrTaskNotFound) {
		return nil // nothing left to say about a task that is gone
	}
	if err != nil {
		return err
	}
	key := n.destination(ctx, task.ID, task.ProjectID)

	switch {
	case task.Status == fleet.StatusReviewReady || task.Status == fleet.StatusDone:
		return n.deliverResult(ctx, key, task)
	case task.Status == fleet.StatusNeedsDecision || task.Status == fleet.StatusFailed,
		task.Status == fleet.StatusRunning && fleetHasWake(note.Wakes, fleet.WakeStuck):
		return n.deliverPrompt(ctx, key, task, note.Wakes)
	}
	// The task has moved on since the wake was raised (resumed, cancelled):
	// what it said is no longer true, so nothing is sent.
	return nil
}

func fleetHasWake(wakes []fleet.Wake, kinds ...fleet.WakeKind) bool {
	for _, w := range wakes {
		for _, k := range kinds {
			if w.Kind == k {
				return true
			}
		}
	}
	return false
}

// deliverGeneral sends a wake that is about a project or the whole fleet,
// such as a daily budget being reached.
func (n *fleetNotices) deliverGeneral(ctx context.Context, note fleet.Notification) error {
	key := n.destination(ctx, "", note.ProjectID)
	var b strings.Builder
	for i, w := range note.Wakes {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "⚠️ %s", n.Localize(key.chatID, "fleet_wake_"+string(w.Kind), nil))
		if note.ProjectID != "" {
			b.WriteString(" · " + n.projectName(ctx, note.ProjectID))
		}
		if w.Detail != "" {
			b.WriteString("\n" + w.Detail)
		}
	}
	_, err := n.Messenger.SendCard(key, b.String(), nil)
	return err
}

func (n *fleetNotices) projectName(ctx context.Context, projectID string) string {
	if project, err := n.Fleet.Store.GetProject(ctx, projectID); err == nil && project.Name != "" {
		return project.Name
	}
	return projectID
}

// promptReason identifies what a prompt is about: the kind of trouble, its
// explanation, and the run it happened in. The same trouble in a later run
// is a new reason.
func promptReason(task fleet.Task, kind fleet.WakeKind) string {
	run := int64(0)
	if task.StartedAt != nil {
		run = task.StartedAt.UnixMilli()
	}
	return fmt.Sprintf("%s|%s|%d", kind, task.StatusReason, run)
}

// promptKind picks the one kind a prompt is about when several wakes are
// pending for a task: the one that best describes where the task stands.
func promptKind(task fleet.Task, wakes []fleet.Wake) fleet.WakeKind {
	switch task.Status {
	case fleet.StatusFailed:
		return fleet.WakeFailed
	case fleet.StatusRunning:
		return fleet.WakeStuck
	}
	for _, kind := range []fleet.WakeKind{fleet.WakeQuestion, fleet.WakeOverBudget, fleet.WakeNeedsDecision} {
		if fleetHasWake(wakes, kind) {
			return kind
		}
	}
	if strings.HasPrefix(task.StatusReason, "over the ") {
		return fleet.WakeOverBudget
	}
	return fleet.WakeNeedsDecision
}

// deliverPrompt asks the owner to decide about a task.
func (n *fleetNotices) deliverPrompt(ctx context.Context, key chatKey, task fleet.Task, wakes []fleet.Wake) error {
	kind := promptKind(task, wakes)
	reason := promptReason(task, kind)
	// The same task stopping for the same reason in the same run is asked
	// about once.
	if asked, err := n.Store.HasPrompt(0, task.ID, reason); err == nil && asked {
		return nil
	}
	loc := func(k string, vars map[string]string) string { return n.Localize(key.chatID, k, vars) }

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %s · %s\n", fleetWakeEmoji[kind], loc("fleet_wake_"+string(kind), nil),
		n.projectName(ctx, task.ProjectID), task.ID)
	b.WriteString(fleetGoalLine(task))
	detail := firstLineOf(task.StatusReason)
	if kind == fleet.WakeStuck {
		for _, w := range wakes {
			if w.Kind == fleet.WakeStuck && w.Detail != "" {
				detail = w.Detail
			}
		}
	}
	if detail != "" {
		b.WriteString("\n" + fleetClip(detail, 300))
	}
	if kind == fleet.WakeQuestion {
		if question := n.lastAgentText(ctx, task.ID); question != "" {
			b.WriteString("\n\n💬 " + fleetClip(question, 1500))
		}
	}
	b.WriteString("\n\n" + loc("fleet_prompt_hint_"+string(kind), nil))

	messageID, err := n.Messenger.SendCard(key, b.String(), n.promptButtons(loc, task, kind))
	if err != nil {
		return err
	}
	if err := n.Store.SavePrompt(fleetPrompt{Key: key, MessageID: messageID, TaskID: task.ID, Reason: reason}); err != nil {
		log.Printf("[fleet] remember prompt for task %s: %v", task.ID, err)
	}
	return nil
}

var fleetWakeEmoji = map[fleet.WakeKind]string{
	fleet.WakeQuestion:      "💬",
	fleet.WakeNeedsDecision: "❓",
	fleet.WakeFailed:        "❌",
	fleet.WakeStuck:         "🕸",
	fleet.WakeOverBudget:    "💸",
	fleet.WakeReviewReady:   "👀",
	fleet.WakeCompleted:     "✅",
}

// promptButtons offers the usual ways out of each kind of trouble.
func (n *fleetNotices) promptButtons(loc func(string, map[string]string) string, task fleet.Task, kind fleet.WakeKind) [][]fleetButton {
	btn := func(action string) fleetButton {
		return fleetButton{Text: loc("fleet_btn_"+action, nil), Data: fleetCallbackData(action, task.ID)}
	}
	discard := fleetButton{Text: loc("fleet_btn_discard", nil), Data: fleetCallbackData(fleetActPromptDiscard, task.ID)}
	switch kind {
	case fleet.WakeQuestion:
		// The answer is typed as a reply; the only button is the way out.
		return [][]fleetButton{{discard}}
	case fleet.WakeOverBudget:
		return [][]fleetButton{{btn(fleetActGoOn), discard}}
	case fleet.WakeFailed:
		return [][]fleetButton{{btn(fleetActRetry), discard}}
	case fleet.WakeStuck:
		return [][]fleetButton{{btn(fleetActCancel)}}
	default:
		return [][]fleetButton{
			{btn(fleetActRetry), btn(fleetActHint)},
			{btn(fleetActPass), discard},
		}
	}
}

// lastAgentText is the last thing the task's agent said.
func (n *fleetNotices) lastAgentText(ctx context.Context, taskID string) string {
	ev, ok, err := n.Fleet.Store.LastEvent(ctx, taskID, fleet.EventAgentText)
	if err != nil || !ok {
		return ""
	}
	var body struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(ev.Payload, &body)
	return strings.TrimSpace(body.Text)
}

// deliverResult tells the owner what a task produced.
func (n *fleetNotices) deliverResult(ctx context.Context, key chatKey, task fleet.Task) error {
	reason := fmt.Sprintf("result|%s|%d", task.Status, task.PRNumber)
	if sent, err := n.Store.HasPrompt(0, task.ID, reason); err == nil && sent {
		return nil
	}
	loc := func(k string, vars map[string]string) string { return n.Localize(key.chatID, k, vars) }

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %s · %s\n", fleetStatusEmoji[task.Status], loc("fleet_status_"+string(task.Status), nil),
		n.projectName(ctx, task.ProjectID), task.ID)
	b.WriteString(fleetGoalLine(task))

	var reportPath string
	if task.Shape == fleet.ShapeScout {
		reportPath, _ = n.Fleet.Runner.ReportPath(task.ProjectID, task.ID)
		if summary := fleetReportSummary(reportPath, 900); summary != "" {
			b.WriteString("\n\n" + summary)
		}
	} else {
		if diff, err := n.Fleet.Commander.TaskDiff(ctx, task.ID); err == nil {
			if files, ins, del, ok := parseDiffStat(diff.Stat); ok {
				b.WriteString("\n" + loc("fleet_result_changes", map[string]string{
					"{files}": strconv.Itoa(files + len(diff.Untracked)), "{ins}": strconv.Itoa(ins), "{del}": strconv.Itoa(del),
				}))
			}
		}
		b.WriteString("\n" + n.verificationLine(ctx, loc, task.ID))
		if task.PRURL != "" {
			b.WriteString("\n🔗 " + task.PRURL)
		}
		if key := fleetResultNextKey(task); key != "" {
			b.WriteString("\n\n" + loc(key, map[string]string{"{branch}": task.Branch}))
		}
	}
	b.WriteString("\n" + loc("fleet_card_meter", map[string]string{
		"{elapsed}": fleetElapsed(task, time.Now()), "{cost}": fmt.Sprintf("%.2f", task.CostUSD),
	}))

	messageID, err := n.Messenger.SendCard(key, b.String(), n.resultButtons(loc, task))
	if err != nil {
		return err
	}
	if err := n.Store.SavePrompt(fleetPrompt{Key: key, MessageID: messageID, TaskID: task.ID, Reason: reason}); err != nil {
		log.Printf("[fleet] remember result message for task %s: %v", task.ID, err)
	}
	if reportPath != "" && n.Docs != nil {
		if _, err := os.Stat(reportPath); err == nil {
			if err := n.Docs.SendDocument(key, reportPath, task.ID+" report"); err != nil {
				log.Printf("[fleet] send report of task %s: %v", task.ID, err)
			}
		}
	}
	return nil
}

// fleetResultNextKey is the message that says what happens next with a
// finished task's work, or "" when nothing is left to do.
func fleetResultNextKey(task fleet.Task) string {
	switch {
	case task.Status == fleet.StatusReviewReady && task.TrustMode == fleet.TrustLocalOnly:
		return "fleet_result_next_local"
	case task.Status == fleet.StatusReviewReady && task.TrustMode == fleet.TrustNoMistakes:
		return "fleet_result_next_no_mistakes"
	case task.Status == fleet.StatusDone && task.TrustMode == fleet.TrustDirectPR && task.PRNumber > 0:
		return "fleet_result_next_direct_pr"
	}
	return ""
}

// resultButtons offers what is left for the owner to do with finished work.
func (n *fleetNotices) resultButtons(loc func(string, map[string]string) string, task fleet.Task) [][]fleetButton {
	discard := fleetButton{Text: loc("fleet_btn_discard", nil), Data: fleetCallbackData(fleetActPromptDiscard, task.ID)}
	switch {
	case task.Status == fleet.StatusReviewReady && task.TrustMode == fleet.TrustNoMistakes:
		// One action, and only the owner's: approving merges the pull request.
		return [][]fleetButton{{{Text: loc("fleet_btn_approve_merge", nil), Data: fleetCallbackData(fleetActApprove, task.ID)}}}
	case task.Status == fleet.StatusReviewReady:
		return [][]fleetButton{{
			{Text: loc("fleet_btn_approve", nil), Data: fleetCallbackData(fleetActApprove, task.ID)}, discard,
		}}
	case task.Status == fleet.StatusDone && task.TrustMode == fleet.TrustDirectPR && task.PRNumber > 0:
		return [][]fleetButton{{{Text: loc("fleet_btn_merge", nil), Data: fleetCallbackData(fleetActMerge, task.ID)}}}
	}
	return nil
}

// verificationLine sums up the task's latest round of checks.
func (n *fleetNotices) verificationLine(ctx context.Context, loc func(string, map[string]string) string, taskID string) string {
	checks, err := n.Fleet.Commander.Verifications(ctx, taskID)
	if err != nil || len(checks) == 0 {
		return loc("fleet_result_verify_none", nil)
	}
	latest := 0
	for _, c := range checks {
		if c.Attempt > latest {
			latest = c.Attempt
		}
	}
	passed, total := 0, 0
	for _, c := range checks {
		if c.Attempt != latest {
			continue
		}
		total++
		if c.Passed {
			passed++
		}
	}
	key := "fleet_result_verify_passed"
	if passed != total {
		key = "fleet_result_verify_failed"
	}
	return loc(key, map[string]string{"{passed}": strconv.Itoa(passed), "{total}": strconv.Itoa(total)})
}

var diffStatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// parseDiffStat reads the totals from the last line of `git diff --stat`.
func parseDiffStat(stat string) (files, insertions, deletions int, ok bool) {
	m := diffStatRe.FindStringSubmatch(stat)
	if m == nil {
		return 0, 0, 0, false
	}
	files, _ = strconv.Atoi(m[1])
	insertions, _ = strconv.Atoi(m[2])
	deletions, _ = strconv.Atoi(m[3])
	return files, insertions, deletions, true
}

// fleetReportSummary is the opening of a scout report, cut at a paragraph
// or line end where possible.
func fleetReportSummary(path string, max int) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	cut := string(runes[:max])
	if i := strings.LastIndex(cut, "\n"); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "\n…"
}

// renderSummary describes what happened while the owner was quiet or away.
func (n *fleetNotices) renderSummary(chatID int64, s fleet.AwaySummary) string {
	loc := func(k string, vars map[string]string) string { return n.Localize(chatID, k, vars) }
	var b strings.Builder
	b.WriteString(loc("fleet_away_header", map[string]string{
		"{mode}":  loc("fleet_mode_"+string(s.Mode), nil),
		"{since}": s.Since.Local().Format("01-02 15:04"),
		"{until}": s.Until.Local().Format("01-02 15:04"),
	}))
	if s.Empty() {
		b.WriteString("\n" + loc("fleet_away_nothing", nil))
		return b.String()
	}
	line := func(r fleet.NotifyRecord, label string) {
		fmt.Fprintf(&b, "\n• %s", label)
		if r.TaskID != "" {
			b.WriteString(" · " + r.TaskID)
		} else if r.ProjectID != "" {
			b.WriteString(" · " + r.ProjectID)
		}
		if r.Detail != "" {
			b.WriteString(" — " + fleetClip(firstLineOf(r.Detail), 120))
		}
		if r.Note != "" {
			b.WriteString(" (" + fleetClip(r.Note, 120) + ")")
		}
	}
	if len(s.Handled) > 0 {
		b.WriteString("\n\n" + loc("fleet_away_handled", nil))
		for _, r := range s.Handled {
			line(r, loc("fleet_disp_"+string(r.Disposition), nil))
		}
	}
	if len(s.Held) > 0 {
		b.WriteString("\n\n" + loc("fleet_away_held", nil))
		for _, r := range s.Held {
			line(r, loc("fleet_wake_"+string(r.Kind), nil))
		}
		b.WriteString("\n\n" + loc("fleet_away_held_hint", nil))
	}
	return b.String()
}

// confirmDiscard reports whether this press on a prompt's discard button is
// the second within the confirmation window. The first press only arms it.
func (n *fleetNotices) confirmDiscard(taskID string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.discardPressed == nil {
		n.discardPressed = map[string]time.Time{}
	}
	if first, ok := n.discardPressed[taskID]; ok && now.Sub(first) <= fleetDiscardConfirmWindow {
		delete(n.discardPressed, taskID)
		return true
	}
	n.discardPressed[taskID] = now
	return false
}

// ── notification modes ───────────────────────────────────────────────────────

// fleetAFKArgs is a parsed /afk command.
type fleetAFKArgs struct {
	Duration time.Duration
	Actions  []fleet.AFKAction
	// Projects are project IDs or names, as typed.
	Projects []string
}

var (
	errFleetAFKUsage    = errors.New("fleet_afk_usage")
	errFleetAFKDuration = errors.New("fleet_afk_err_duration")
)

// MaxFleetAFK is the longest an /afk grant may run.
const MaxFleetAFK = 7 * 24 * time.Hour

var fleetDurationRe = regexp.MustCompile(`^(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?$`)

// parseFleetDuration reads "8h", "90m", "1d12h", "2h30m".
func parseFleetDuration(s string) (time.Duration, error) {
	m := fleetDurationRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil || m[0] == "" {
		return 0, errFleetAFKDuration
	}
	var d time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i+1])
		if err != nil || n > 100000 {
			return 0, errFleetAFKDuration
		}
		d += time.Duration(n) * unit
	}
	if d <= 0 || d > MaxFleetAFK {
		return 0, errFleetAFKDuration
	}
	return d, nil
}

// parseFleetAFK parses what follows /afk:
//
//	<duration> [approve] [resume] [all] [project ...]
//
// The duration comes first. "approve" and "resume" list what may be done
// without asking, "all" is both; with neither, everything is only held.
// Any other word names a project the grant is limited to. Words may be
// separated by spaces or commas.
func parseFleetAFK(rest string) (fleetAFKArgs, error) {
	words := strings.FieldsFunc(rest, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
	if len(words) == 0 {
		return fleetAFKArgs{}, errFleetAFKUsage
	}
	d, err := parseFleetDuration(words[0])
	if err != nil {
		return fleetAFKArgs{}, err
	}
	args := fleetAFKArgs{Duration: d}
	seen := map[fleet.AFKAction]bool{}
	add := func(a fleet.AFKAction) {
		if !seen[a] {
			seen[a] = true
			args.Actions = append(args.Actions, a)
		}
	}
	for _, w := range words[1:] {
		switch strings.ToLower(w) {
		case "approve":
			add(fleet.AFKApprove)
		case "resume":
			add(fleet.AFKResume)
		case "all":
			add(fleet.AFKApprove)
			add(fleet.AFKResume)
		default:
			args.Projects = append(args.Projects, w)
		}
	}
	return args, nil
}

// resolveProjects maps typed project IDs or names onto project IDs. It
// returns the first word that names no project.
func (f *FleetService) resolveProjects(ctx context.Context, words []string) (ids []string, unknown string, err error) {
	if len(words) == 0 {
		return nil, "", nil
	}
	projects, err := f.Store.ListProjects(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, w := range words {
		found := ""
		for _, p := range projects {
			if strings.EqualFold(p.ID, w) || strings.EqualFold(p.Name, w) {
				found = p.ID
				break
			}
		}
		if found == "" {
			return nil, w, nil
		}
		ids = append(ids, found)
	}
	sort.Strings(ids)
	return ids, "", nil
}

// fleetQuiet handles /quiet.
func (t *TelegramBot) fleetQuiet(ctx context.Context, key chatKey) {
	if _, err := t.fleetNotifier.SetMode(ctx, fleet.NotifyQuiet, nil); err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.fleetNotices.SetHome(key)
	t.send(key, t.fleetMsg(key.chatID, "fleet_quiet_on", nil))
}

// fleetAFK handles /afk.
func (t *TelegramBot) fleetAFK(ctx context.Context, key chatKey, rest string) {
	args, err := parseFleetAFK(rest)
	if err != nil {
		msg := t.fleetMsg(key.chatID, "fleet_afk_usage", nil)
		if errors.Is(err, errFleetAFKDuration) {
			msg = t.fleetMsg(key.chatID, "fleet_afk_err_duration", nil) + "\n\n" + msg
		}
		t.send(key, msg)
		return
	}
	projects, unknown, err := t.fleet.resolveProjects(ctx, args.Projects)
	if err == nil && unknown != "" {
		t.send(key, t.fleetMsg(key.chatID, "fleet_afk_err_project", map[string]string{"{name}": unknown}))
		return
	}
	var prefs fleet.NotifyPrefs
	if err == nil {
		prefs, err = t.fleetNotifier.SetMode(ctx, fleet.NotifyAFK, &fleet.AFKGrant{
			Actions: args.Actions, Projects: projects, ExpiresAt: time.Now().Add(args.Duration),
		})
	}
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.fleetNotices.SetHome(key)

	actions := t.fleetMsg(key.chatID, "fleet_afk_actions_none", nil)
	if len(prefs.Grant.Actions) > 0 {
		var names []string
		for _, a := range prefs.Grant.Actions {
			names = append(names, t.fleetMsg(key.chatID, "fleet_afk_action_"+string(a), nil))
		}
		actions = strings.Join(names, "、")
	}
	scope := t.fleetMsg(key.chatID, "fleet_afk_projects_all", nil)
	if len(projects) > 0 {
		scope = strings.Join(projects, ", ")
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_afk_on", map[string]string{
		"{until}":    prefs.Grant.ExpiresAt.Local().Format("01-02 15:04"),
		"{actions}":  actions,
		"{projects}": scope,
	}))
}

// fleetBack handles /back.
func (t *TelegramBot) fleetBack(ctx context.Context, key chatKey) {
	summary, err := t.fleetNotifier.Back(ctx)
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	if summary.Mode == fleet.NotifyNormal || summary.Mode == "" {
		t.send(key, t.fleetMsg(key.chatID, "fleet_back_already", nil))
		return
	}
	t.send(key, t.fleetNotices.renderSummary(key.chatID, summary))
}

// fleetHandleReply routes a reply to one of the fleet's messages (a prompt,
// a delivery message or a card) into that task's session. It reports
// whether the message was such a reply.
func (t *TelegramBot) fleetHandleReply(key chatKey, userID int64, replyToMessageID int, text string) bool {
	if t.fleet == nil || t.fleetCards == nil || replyToMessageID == 0 || text == "" || strings.HasPrefix(text, "/") {
		return false
	}
	taskID, ok, err := t.fleetCards.Store.PromptByMessage(key.chatID, replyToMessageID)
	if err == nil && !ok {
		var card fleetCard
		if card, ok, err = t.fleetCards.Store.ByMessage(key, replyToMessageID); ok {
			taskID = card.TaskID
		}
	}
	if err != nil || !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetCommandTimeout)
	defer cancel()
	if _, err := t.fleet.Commander.Reply(ctx, fleetActor(userID), fleet.ReplyRequest{TaskID: taskID, Text: text}); err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return true
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_reply_sent", map[string]string{"{id}": taskID}))
	return true
}
