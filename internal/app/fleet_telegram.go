package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// errFleetNoProject is returned when a topic has no project to dispatch to.
var errFleetNoProject = errors.New("topic is not bound to a fleet project")

// fleetCommandTimeout bounds the fleet work done for one Telegram command.
const fleetCommandTimeout = 30 * time.Second

// Leave time for both issue read attempts, the retry delay and task creation.
const fleetDispatchTimeout = 75 * time.Second

// fleetMsg returns a fleet message in the chat's language. Placeholders are
// written with their braces, as they appear in the locale files:
// {"{name}": "alice"}.
func (t *TelegramBot) fleetMsg(chatID int64, key string, vars map[string]string) string {
	if len(vars) == 0 {
		return t.getLocalizedMessage(chatID, key, nil)
	}
	// The i18n manager adds the braces itself.
	bare := make(map[string]string, len(vars))
	for name, value := range vars {
		bare[strings.TrimSuffix(strings.TrimPrefix(name, "{"), "}")] = value
	}
	return t.getLocalizedMessage(chatID, key, bare)
}

// fleetActor names a Telegram user in the fleet's audit log.
func fleetActor(userID int64) fleet.Actor {
	return fleet.Actor{ID: strconv.FormatInt(userID, 10), Via: "telegram"}
}

// repoRoot returns the top level of the git repository containing dir, the
// form in which the fleet stores project paths.
func fleetRepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", fleet.ErrNotGitRepo, dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// ProjectForDir returns the fleet project whose repository contains dir.
func (f *FleetService) ProjectForDir(ctx context.Context, dir string) (fleet.Project, error) {
	root, err := fleetRepoRoot(ctx, dir)
	if err != nil {
		return fleet.Project{}, err
	}
	return f.Store.GetProjectByRepoPath(ctx, root)
}

// EnsureProject returns the fleet project for dir, registering it with the
// default trust mode when it is not registered yet.
func (f *FleetService) EnsureProject(ctx context.Context, actor fleet.Actor, dir string) (project fleet.Project, created bool, err error) {
	project, err = f.ProjectForDir(ctx, dir)
	if err == nil {
		return project, false, nil
	}
	if !errors.Is(err, fleet.ErrProjectNotFound) {
		return fleet.Project{}, false, err
	}
	project, err = f.Commander.RegisterProject(ctx, actor, fleet.Project{RepoPath: dir, TrustMode: f.TrustMode})
	if errors.Is(err, fleet.ErrProjectExists) {
		// Registered by someone else in the meantime.
		project, err = f.ProjectForDir(ctx, dir)
		return project, false, err
	}
	return project, err == nil, err
}

// topicProjectDirLister is the part of storage the backfill reads.
type topicProjectDirLister interface {
	ListTopicProjectDirs() ([]string, error)
}

// BackfillTopicProjects registers every project path already bound to a
// topic. Paths that are registered, missing, or not git repositories are
// skipped, so running it again changes nothing.
func (f *FleetService) BackfillTopicProjects(ctx context.Context, storage topicProjectDirLister) (registered int, err error) {
	dirs, err := storage.ListTopicProjectDirs()
	if err != nil {
		return 0, err
	}
	actor := fleet.Actor{ID: "alice", Via: "startup"}
	for _, dir := range dirs {
		_, created, err := f.EnsureProject(ctx, actor, dir)
		switch {
		case err == nil && created:
			registered++
		case err != nil && !errors.Is(err, fleet.ErrNotGitRepo):
			log.Printf("[fleet] backfill %s: %v", dir, err)
		}
	}
	return registered, nil
}

// fleetTopicDir returns the project path bound to a topic with /project, or
// "" when there is none.
func (t *TelegramBot) fleetTopicDir(key chatKey) string {
	if globalStorage == nil {
		return ""
	}
	dir, err := globalStorage.GetTopicSetting(key.chatID, key.threadID)
	if err != nil {
		log.Printf("[fleet] read topic setting chat=%d thread=%d: %v", key.chatID, key.threadID, err)
		return ""
	}
	return strings.TrimSpace(dir)
}

// fleetTopicProject returns the fleet project of a topic. It returns
// errFleetNoProject when the topic is unbound or its path is not a fleet
// project.
func (t *TelegramBot) fleetTopicProject(ctx context.Context, key chatKey) (fleet.Project, error) {
	dir := t.fleetTopicDir(key)
	if dir == "" {
		return fleet.Project{}, errFleetNoProject
	}
	project, err := t.fleet.ProjectForDir(ctx, dir)
	if errors.Is(err, fleet.ErrProjectNotFound) || errors.Is(err, fleet.ErrNotGitRepo) {
		return fleet.Project{}, errFleetNoProject
	}
	return project, err
}

// fleetRequireProject is fleetTopicProject for command handlers: when the
// topic has no project it tells the user to run /project and reports false.
func (t *TelegramBot) fleetRequireProject(ctx context.Context, key chatKey) (fleet.Project, bool) {
	project, err := t.fleetTopicProject(ctx, key)
	if err == nil {
		return project, true
	}
	if errors.Is(err, errFleetNoProject) {
		t.send(key, t.fleetMsg(key.chatID, "fleet_no_project", nil))
	} else {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
	}
	return fleet.Project{}, false
}

// handleFleetCommand handles the commands the fleet adds or takes over. It
// reports whether the command was handled; everything else falls through to
// the existing command handling. With the fleet disabled it handles nothing.
func (t *TelegramBot) handleFleetCommand(key chatKey, userID int64, text string) bool {
	if t.fleet == nil {
		return false
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false
	}
	command := strings.Split(parts[0], "@")[0]
	timeout := fleetCommandTimeout
	switch command {
	case "/ship", "/scout", "/hermes", "/ghermes":
		timeout = fleetDispatchTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// What follows the command word, with its line breaks intact.
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), parts[0]))
	switch command {
	case "/ship":
		t.fleetDispatch(ctx, key, userID, fleet.ShapeShip, rest)
		return true
	case "/scout":
		t.fleetDispatch(ctx, key, userID, fleet.ShapeScout, rest)
		return true
	case "/fleet":
		switch {
		case len(parts) == 1:
			t.fleetOverview(ctx, key)
		case parts[1] == "tasks":
			t.fleetTaskList(ctx, key)
		default:
			t.send(key, t.fleetMsg(key.chatID, "fleet_usage", nil))
		}
		return true
	case "/quiet":
		t.fleetQuiet(ctx, key)
		return true
	case "/afk":
		t.fleetAFK(ctx, key, rest)
		return true
	case "/back":
		t.fleetBack(ctx, key)
		return true
	case "/hermes", "/ghermes", "/abort", "/retry", "/resume", "/close", "/parallel", "/strict", "/mr":
		return t.fleetCompat(ctx, key, userID, command, parts, rest)
	case "/project":
		switch {
		case len(parts) == 1:
			return t.fleetShowProject(ctx, key)
		case parts[1] == "mode":
			t.fleetSetProjectMode(ctx, key, userID, parts[2:])
			return true
		}
	}
	return false
}

// fleetShowProject answers a bare /project with the topic's fleet settings.
// An unbound topic falls through to the usual usage message.
func (t *TelegramBot) fleetShowProject(ctx context.Context, key chatKey) bool {
	project, err := t.fleetTopicProject(ctx, key)
	if err != nil {
		return false
	}
	verify := t.fleetMsg(key.chatID, "fleet_project_verify_none", nil)
	if len(project.VerifyCommands) > 0 {
		verify = strings.Join(project.VerifyCommands, "\n   ")
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_project_info", map[string]string{
		"{name}":       project.Name,
		"{id}":         project.ID,
		"{path}":       project.RepoPath,
		"{branch}":     project.DefaultBranch,
		"{mode}":       string(project.TrustMode),
		"{verify}":     verify,
		"{concurrent}": strconv.Itoa(project.MaxConcurrent),
	}))
	return true
}

// fleetSetProjectMode handles /project mode <trust mode>.
func (t *TelegramBot) fleetSetProjectMode(ctx context.Context, key chatKey, userID int64, args []string) {
	if len(args) != 1 {
		t.send(key, t.fleetMsg(key.chatID, "fleet_project_mode_usage", nil))
		return
	}
	mode, err := fleet.ParseTrustMode(args[0])
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_project_mode_usage", nil))
		return
	}
	project, ok := t.fleetRequireProject(ctx, key)
	if !ok {
		return
	}
	project, err = t.fleet.Commander.UpdateProject(ctx, fleetActor(userID), project.ID, "trust mode: "+string(mode),
		func(p *fleet.Project) error {
			p.TrustMode = mode
			return nil
		})
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_project_mode_set", map[string]string{
		"{name}": project.Name,
		"{mode}": string(project.TrustMode),
	}))
}

// fleetAfterCommand runs after the existing command handling. When the
// command was a successful /project <path>, the path is registered as a
// fleet project if it is not one yet.
func (t *TelegramBot) fleetAfterCommand(key chatKey, userID int64, text string) {
	if t.fleet == nil {
		return
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	switch strings.Split(parts[0], "@")[0] {
	case "/status":
		ctx, cancel := context.WithTimeout(context.Background(), fleetCommandTimeout)
		defer cancel()
		if line := t.fleetStatusLine(ctx, key.chatID); line != "" {
			t.send(key, line)
		}
		return
	case "/help", "/start":
		t.send(key, t.fleetMsg(key.chatID, "fleet_help", nil))
		return
	case "/project":
	default:
		return
	}
	if len(parts) < 2 || parts[1] == "mode" {
		return
	}
	// /project saved the path only if it was valid; read back what it saved.
	dir := t.fleetTopicDir(key)
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetCommandTimeout)
	defer cancel()
	project, created, err := t.fleet.EnsureProject(ctx, fleetActor(userID), dir)
	switch {
	case errors.Is(err, fleet.ErrNotGitRepo):
		t.send(key, t.fleetMsg(key.chatID, "fleet_project_not_git", nil))
	case err != nil:
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
	case created:
		t.send(key, t.fleetMsg(key.chatID, "fleet_project_registered", map[string]string{
			"{name}": project.Name,
			"{mode}": string(project.TrustMode),
		}))
	}
}

// attachFleet connects the bot to a running fleet: commands reach it, and
// task cards are posted and kept current through the bot.
func (t *TelegramBot) attachFleet(ctx context.Context, svc *FleetService, db *sql.DB) error {
	cards, err := newFleetCardStore(db)
	if err != nil {
		return err
	}
	t.fleet = svc
	t.fleetCards = &fleetCardManager{
		Fleet:        svc,
		Store:        cards,
		Messenger:    t,
		Localize:     t.fleetMsg,
		DashboardURL: svc.Config.DashboardURL,
	}
	svc.run("fleet.cards", func() error { return t.fleetCards.Run(ctx) })

	if err := cards.preparePrompts(); err != nil {
		return err
	}
	owner := svc.Config.OwnerUserID
	if owner == 0 && len(t.config.AllowedUserIDs) > 0 {
		owner = t.config.AllowedUserIDs[0]
	}
	t.fleetNotices = &fleetNotices{
		Fleet:        svc,
		Store:        cards,
		Messenger:    t,
		Docs:         t,
		Localize:     t.fleetMsg,
		Owner:        owner,
		DashboardURL: svc.Config.DashboardURL,
	}
	if topics, ok := globalStorage.(topicBindingLister); ok {
		t.fleetNotices.Topics = topics
	}
	t.fleetNotifier = &fleet.Notifier{
		Store:     svc.Store,
		Commander: svc.Commander,
		User:      strconv.FormatInt(owner, 10),
		Deliver:   t.fleetNotices.Deliver,
	}
	svc.run("fleet.notifier", func() error { return t.fleetNotifier.Run(ctx) })
	return nil
}

// SendDocument implements fleetDocSender.
func (t *TelegramBot) SendDocument(key chatKey, path, caption string) error {
	return t.sendMediaFile(key, path, "document", caption)
}

// fleetIsOwner reports whether a user may land work: approve, merge, or wave
// a task through. With no owner configured, every allowed user may.
func (t *TelegramBot) fleetIsOwner(userID int64) bool {
	return t.fleetNotices == nil || t.fleetNotices.Owner == 0 || t.fleetNotices.Owner == userID
}

// fleetKeyboard converts card buttons to a Telegram inline keyboard. An
// empty keyboard removes the buttons a message had.
func fleetKeyboard(buttons [][]fleetButton) map[string]interface{} {
	rows := make([][]map[string]interface{}, 0, len(buttons))
	for _, row := range buttons {
		out := make([]map[string]interface{}, 0, len(row))
		for _, b := range row {
			btn := map[string]interface{}{"text": b.Text}
			if b.URL != "" {
				btn["url"] = b.URL
			} else {
				btn["callback_data"] = b.Data
			}
			out = append(out, btn)
		}
		rows = append(rows, out)
	}
	return map[string]interface{}{"inline_keyboard": rows}
}

// SendCard implements fleetMessenger on the synchronous send path, which
// waits for the rate limiter and retries when Telegram asks it to.
func (t *TelegramBot) SendCard(key chatKey, text string, buttons [][]fleetButton) (int, error) {
	params := map[string]interface{}{
		"chat_id":                  key.chatID,
		"text":                     sanitizeUTF8(text),
		"disable_web_page_preview": true,
	}
	if key.threadID != 0 {
		params["message_thread_id"] = key.threadID
	}
	if len(buttons) > 0 {
		params["reply_markup"] = fleetKeyboard(buttons)
	}
	resp, err := t.sendMessageSync("sendMessage", params)
	if err != nil {
		return 0, err
	}
	result, _ := resp["result"].(map[string]interface{})
	id, ok := result["message_id"].(float64)
	if !ok {
		return 0, errors.New("telegram: sendMessage returned no message_id")
	}
	return int(id), nil
}

// EditCard implements fleetMessenger.
func (t *TelegramBot) EditCard(key chatKey, messageID int, text string, buttons [][]fleetButton) error {
	_, err := t.sendMessageSync("editMessageText", map[string]interface{}{
		"chat_id":                  key.chatID,
		"message_id":               messageID,
		"text":                     sanitizeUTF8(text),
		"disable_web_page_preview": true,
		"reply_markup":             fleetKeyboard(buttons),
	})
	return err
}

// handleFleetCallback handles a press on a task card's button.
func (t *TelegramBot) handleFleetCallback(key chatKey, userID int64, queryID, data string) {
	answer := func(msgKey string, vars map[string]string) {
		t.answerCallbackQuery(queryID, fleetClip(t.fleetMsg(key.chatID, msgKey, vars), 190))
	}
	action, taskID, ok := parseFleetCallback(data)
	if !ok {
		answer("fleet_cb_unknown", nil)
		return
	}
	if t.fleet == nil {
		answer("fleet_disabled", nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetCommandTimeout)
	defer cancel()

	// Buttons that open something rather than act on a task.
	switch action {
	case fleetActOverview:
		t.fleetOverview(ctx, key)
		t.answerCallbackQuery(queryID, "")
		return
	case fleetActTasks:
		t.fleetTaskList(ctx, key)
		t.answerCallbackQuery(queryID, "")
		return
	case fleetActCard:
		if err := t.fleetOpenCard(ctx, key, taskID); err != nil {
			answer("fleet_error", map[string]string{"{error}": err.Error()})
			return
		}
		t.answerCallbackQuery(queryID, "")
		return
	}

	// A button only acts on a task that has a card or a prompt in this chat.
	if t.fleetCards != nil {
		card, found, err := t.fleetCards.Store.Get(taskID)
		here := err == nil && found && card.Key.chatID == key.chatID
		if !here {
			here, _ = t.fleetCards.Store.HasPrompt(key.chatID, taskID, "")
		}
		if !here {
			answer("fleet_cb_unknown", nil)
			return
		}
	}
	// Landing work is the owner's decision alone.
	switch action {
	case fleetActApprove, fleetActMerge, fleetActPass, fleetOpFinishEvidence:
		if !t.fleetIsOwner(userID) {
			answer("fleet_cb_owner_only", nil)
			return
		}
	}

	// An old card or prompt may show a button the task no longer allows:
	// say why, and redraw the card with what it allows now.
	if op := fleetCallbackOp(action); op != "" {
		task, _, err := t.fleet.CheckActorAction(ctx, fleetActor(userID), taskID, op)
		var refused *fleetActionError
		if errors.As(err, &refused) {
			loc := func(k string, v map[string]string) string { return t.fleetMsg(key.chatID, k, v) }
			answer("fleet_cb_unavailable", fleetBlockVars(loc, refused.Block, task.Status))
			if t.fleetCards != nil {
				// Leaving any confirmation redraws the card right away.
				t.fleetCards.SetConfirm(ctx, taskID, "")
			}
			return
		}
		if err != nil {
			answer("fleet_error", map[string]string{"{error}": err.Error()})
			return
		}
	}

	actor := fleetActor(userID)
	cmd := t.fleet.Commander
	var task fleet.Task
	var err error
	switch action {
	case fleetActCancel:
		_, err = cmd.Cancel(ctx, actor, taskID, "")
	case fleetActRetry:
		_, err = fleetCommanderRetry(ctx, t.fleet, actor, taskID)
	case fleetOpFinishEvidence:
		task, err = t.fleet.FinishEvidence(ctx, actor, taskID)
	case fleetActApprove:
		task, err = t.fleet.Approve(ctx, actor, taskID, false)
	case fleetActMerge:
		task, err = t.fleet.Approve(ctx, actor, taskID, true)
	case fleetActDiscard:
		// Discarding removes the checkout, uncommitted work included, so
		// it takes a second press.
		if t.fleetCards != nil {
			t.fleetCards.SetConfirm(ctx, taskID, fleetActDiscard)
		}
		answer("fleet_cb_confirm", nil)
		return
	case fleetActBack:
		if t.fleetCards != nil {
			t.fleetCards.SetConfirm(ctx, taskID, "")
		}
		t.answerCallbackQuery(queryID, "")
		return
	case fleetActDiscardConfirm:
		_, err = cmd.Discard(ctx, actor, taskID)
		if t.fleetCards != nil {
			t.fleetCards.SetConfirm(ctx, taskID, "")
		}
	case fleetActPass:
		_, err = t.fleet.Pass(ctx, actor, taskID)
	case fleetActGoOn:
		_, err = cmd.Reply(ctx, actor, fleet.ReplyRequest{TaskID: taskID, IgnoreBudget: true})
	case fleetActHint:
		// The hint itself is typed as a reply to the message.
		answer("fleet_cb_hint", nil)
		return
	case fleetActPromptDiscard:
		// A prompt cannot redraw itself to ask again, so the same button is
		// pressed twice.
		if t.fleetNotices == nil || !t.fleetNotices.confirmDiscard(taskID, time.Now()) {
			answer("fleet_cb_confirm", nil)
			return
		}
		_, err = cmd.Discard(ctx, actor, taskID)
		action = fleetActDiscard
	default:
		answer("fleet_cb_unknown", nil)
		return
	}
	if err != nil {
		answer("fleet_error", map[string]string{"{error}": err.Error()})
	} else {
		answer("fleet_cb_"+strings.TrimSuffix(action, "!"), nil)
		if action == fleetActApprove || action == fleetActMerge {
			t.fleetNotices.notifyLanding(ctx, task)
		}
	}
	// The status change redraws the card by itself; this covers an action
	// that failed after changing what the card should show.
	if t.fleetCards != nil {
		t.fleetCards.Refresh(ctx, taskID)
	}
}
