package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// newFleetTestBot returns a bot wired to a running fleet on a fresh database.
// Without an i18n manager, every message it sends is its message key.
func newFleetTestBot(t *testing.T) (*TelegramBot, *FleetService, *SQLiteStorage) {
	t.Helper()
	svc, storage := newFleetTestService(t, FleetConfig{Enabled: true})
	bot := &TelegramBot{
		agents:          map[chatKey]*Agent{},
		config:          &Config{DefaultProjectDir: t.TempDir()},
		messageQueue:    make(chan *TelegramMessage, 16),
		langPreferences: map[int64]string{},
		fleet:           svc,
	}
	return bot, svc, storage
}

// nextFleetMessage returns the text of the next message the bot queued.
func nextFleetMessage(t *testing.T, bot *TelegramBot) string {
	t.Helper()
	select {
	case msg := <-bot.messageQueue:
		text, _ := msg.Params["text"].(string)
		return text
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
		return ""
	}
}

func expectNoFleetMessage(t *testing.T, bot *TelegramBot) {
	t.Helper()
	select {
	case msg := <-bot.messageQueue:
		t.Fatalf("unexpected message: %v", msg.Params["text"])
	case <-time.After(100 * time.Millisecond):
	}
}

func bindTopic(t *testing.T, storage *SQLiteStorage, key chatKey, dir string) {
	t.Helper()
	if err := storage.SaveTopicSetting(key.chatID, key.threadID, dir); err != nil {
		t.Fatalf("SaveTopicSetting: %v", err)
	}
}

func TestFleetAfterProjectCommandRegistersOnce(t *testing.T) {
	bot, svc, storage := newFleetTestBot(t)
	key := chatKey{chatID: 7, threadID: 3}
	repo := newFleetTestRepo(t)
	bindTopic(t, storage, key, repo) // what a successful /project <path> saves

	bot.fleetAfterCommand(key, 42, "/project "+repo)
	if got := nextFleetMessage(t, bot); got != "fleet_project_registered" {
		t.Fatalf("message = %q, want fleet_project_registered", got)
	}
	projects, err := svc.Store.ListProjects(context.Background())
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects = %+v, err %v; want exactly one", projects, err)
	}
	if projects[0].TrustMode != fleet.TrustLocalOnly {
		t.Errorf("TrustMode = %q, want the default local-only", projects[0].TrustMode)
	}

	// Setting the same project again registers nothing and says nothing.
	bot.fleetAfterCommand(key, 42, "/project "+repo)
	expectNoFleetMessage(t, bot)
	if projects, _ := svc.Store.ListProjects(context.Background()); len(projects) != 1 {
		t.Fatalf("projects after second /project = %d, want 1", len(projects))
	}

	// The registration is in the audit log under the Telegram user.
	audit, err := svc.Commander.Audit(context.Background(), fleet.AuditFilter{})
	if err != nil || len(audit) == 0 || audit[0].Actor != "42" || audit[0].Via != "telegram" {
		t.Fatalf("audit = %+v, err %v; want a telegram entry by 42", audit, err)
	}
}

func TestFleetAfterProjectCommandSubdirectoryMapsToRepo(t *testing.T) {
	bot, svc, storage := newFleetTestBot(t)
	key := chatKey{chatID: 7, threadID: 3}
	repo := newFleetTestRepo(t)
	sub := filepath.Join(repo, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	bindTopic(t, storage, key, sub)

	bot.fleetAfterCommand(key, 42, "/project "+sub)
	nextFleetMessage(t, bot)
	project, err := bot.fleetTopicProject(context.Background(), key)
	if err != nil {
		t.Fatalf("fleetTopicProject: %v", err)
	}
	if resolved, _ := filepath.EvalSymlinks(repo); project.RepoPath != resolved && project.RepoPath != repo {
		t.Errorf("RepoPath = %q, want the repository root %q", project.RepoPath, repo)
	}
	if projects, _ := svc.Store.ListProjects(context.Background()); len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
}

func TestFleetAfterProjectCommandNonGitPath(t *testing.T) {
	bot, svc, storage := newFleetTestBot(t)
	key := chatKey{chatID: 7, threadID: 3}
	dir := t.TempDir()
	bindTopic(t, storage, key, dir)

	bot.fleetAfterCommand(key, 42, "/project "+dir)
	if got := nextFleetMessage(t, bot); got != "fleet_project_not_git" {
		t.Fatalf("message = %q, want fleet_project_not_git", got)
	}
	if projects, _ := svc.Store.ListProjects(context.Background()); len(projects) != 0 {
		t.Fatalf("projects = %d, want 0", len(projects))
	}
}

func TestFleetAfterCommandIgnoresOtherCommands(t *testing.T) {
	bot, svc, storage := newFleetTestBot(t)
	key := chatKey{chatID: 7, threadID: 3}
	bindTopic(t, storage, key, newFleetTestRepo(t))

	for _, text := range []string{"/status", "/project", "/project mode direct-pr", "/projects x"} {
		bot.fleetAfterCommand(key, 42, text)
	}
	expectNoFleetMessage(t, bot)
	if projects, _ := svc.Store.ListProjects(context.Background()); len(projects) != 0 {
		t.Fatalf("projects = %d, want 0", len(projects))
	}
}

func TestFleetBackfillTopicProjectsIsIdempotent(t *testing.T) {
	_, svc, storage := newFleetTestBot(t)
	repoA, repoB := newFleetTestRepo(t), newFleetTestRepo(t)
	bindTopic(t, storage, chatKey{chatID: 1, threadID: 1}, repoA)
	bindTopic(t, storage, chatKey{chatID: 1, threadID: 2}, repoA) // two topics, one project
	bindTopic(t, storage, chatKey{chatID: 1, threadID: 3}, repoB)
	bindTopic(t, storage, chatKey{chatID: 1, threadID: 4}, t.TempDir())                     // not a repository
	bindTopic(t, storage, chatKey{chatID: 1, threadID: 5}, filepath.Join(repoA, "no-such")) // gone

	ctx := context.Background()
	n, err := svc.BackfillTopicProjects(ctx, storage)
	if err != nil || n != 2 {
		t.Fatalf("first backfill = %d, %v; want 2, nil", n, err)
	}
	n, err = svc.BackfillTopicProjects(ctx, storage)
	if err != nil || n != 0 {
		t.Fatalf("second backfill = %d, %v; want 0, nil", n, err)
	}
	if projects, _ := svc.Store.ListProjects(ctx); len(projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(projects))
	}
}

func TestFleetBareProjectShowsSettingsOnlyWhenBound(t *testing.T) {
	bot, _, storage := newFleetTestBot(t)
	key := chatKey{chatID: 7, threadID: 3}

	// Unbound: not handled, so the usual usage message is shown instead.
	if bot.handleFleetCommand(key, 42, "/project") {
		t.Fatal("bare /project on an unbound topic should fall through")
	}

	bindTopic(t, storage, key, newFleetTestRepo(t))
	bot.fleetAfterCommand(key, 42, "/project x")
	nextFleetMessage(t, bot)

	if !bot.handleFleetCommand(key, 42, "/project@alice_bot") {
		t.Fatal("bare /project on a bound topic should be handled")
	}
	if got := nextFleetMessage(t, bot); got != "fleet_project_info" {
		t.Fatalf("message = %q, want fleet_project_info", got)
	}
}

func TestFleetProjectMode(t *testing.T) {
	bot, svc, storage := newFleetTestBot(t)
	key := chatKey{chatID: 7, threadID: 3}
	ctx := context.Background()

	// Unbound topic: told to run /project first.
	if !bot.handleFleetCommand(key, 42, "/project mode local-only") {
		t.Fatal("/project mode should be handled")
	}
	if got := nextFleetMessage(t, bot); got != "fleet_no_project" {
		t.Fatalf("message = %q, want fleet_no_project", got)
	}

	bindTopic(t, storage, key, newFleetTestRepo(t))
	bot.fleetAfterCommand(key, 42, "/project x")
	nextFleetMessage(t, bot)

	for _, bad := range []string{"/project mode", "/project mode yolo", "/project mode a b"} {
		bot.handleFleetCommand(key, 42, bad)
		if got := nextFleetMessage(t, bot); got != "fleet_project_mode_usage" {
			t.Fatalf("%q: message = %q, want fleet_project_mode_usage", bad, got)
		}
	}

	// The test repository has no remote, so a mode that pushes is refused
	// and the project keeps its mode.
	bot.handleFleetCommand(key, 42, "/project mode direct-pr")
	if got := nextFleetMessage(t, bot); got != "fleet_error" {
		t.Fatalf("message = %q, want fleet_error", got)
	}
	project, _ := bot.fleetTopicProject(ctx, key)
	if project.TrustMode != fleet.TrustLocalOnly {
		t.Fatalf("TrustMode after refused change = %q, want local-only", project.TrustMode)
	}

	bot.handleFleetCommand(key, 42, "/project mode local-only")
	if got := nextFleetMessage(t, bot); got != "fleet_project_mode_set" {
		t.Fatalf("message = %q, want fleet_project_mode_set", got)
	}
	if projects, _ := svc.Store.ListProjects(ctx); len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
}

func TestFleetCommandsDoNothingWhenDisabled(t *testing.T) {
	bot := &TelegramBot{messageQueue: make(chan *TelegramMessage, 4), langPreferences: map[int64]string{}}
	key := chatKey{chatID: 7, threadID: 3}
	for _, text := range []string{"/project", "/project mode direct-pr", "/project /tmp"} {
		if bot.handleFleetCommand(key, 42, text) {
			t.Fatalf("%q handled with the fleet disabled", text)
		}
		bot.fleetAfterCommand(key, 42, text)
	}
	expectNoFleetMessage(t, bot)
}

// Every fleet message key used in code exists in both locales, with the same
// placeholders.
func TestFleetLocaleKeys(t *testing.T) {
	load := func(lang string) map[string]string {
		raw, err := os.ReadFile(filepath.Join("..", "..", "locales", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Messages map[string]string `json:"messages"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Messages
	}
	zh, en := load("zh-TW"), load("en")

	keyRe := regexp.MustCompile(`getLocalizedMessage\([^,]+, "(fleet_[a-z0-9_]+)"`)
	varRe := regexp.MustCompile(`\{[a-z_]+\}`)
	files, _ := filepath.Glob("fleet*.go")
	used := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range keyRe.FindAllStringSubmatch(string(src), -1) {
			// A key ending in "_" is the fixed part of one completed at
			// run time; those are listed below.
			if !strings.HasSuffix(m[1], "_") {
				used[m[1]] = true
			}
		}
	}
	if len(used) == 0 {
		t.Fatal("found no fleet message keys in the source")
	}
	// Keys that are built at run time rather than written out in a call.
	for _, status := range fleet.AllStatuses {
		used["fleet_status_"+string(status)] = true
	}
	for _, key := range []string{
		"fleet_shape_ship", "fleet_shape_scout", "fleet_ship_usage", "fleet_scout_usage",
		"fleet_card_meter", "fleet_card_action_queued", "fleet_card_action_running", "fleet_card_action_verifying",
		"fleet_card_confirm_discard", "fleet_btn_cancel", "fleet_btn_retry", "fleet_btn_approve", "fleet_btn_merge",
		"fleet_btn_discard", "fleet_btn_discard_confirm", "fleet_btn_back", "fleet_btn_details",
		"fleet_cb_unknown", "fleet_cb_confirm", "fleet_cb_cancel", "fleet_cb_retry", "fleet_cb_approve",
		"fleet_cb_merge", "fleet_cb_discard", "fleet_disabled",
		errFleetDispatchFlagValue.Error(), errFleetDispatchBudget.Error(), errFleetDispatchDuplicated.Error(),
		"fleet_wake_review_ready", "fleet_wake_completed", "fleet_wake_failed", "fleet_wake_question",
		"fleet_wake_needs_decision", "fleet_wake_stuck", "fleet_wake_over_budget",
		"fleet_prompt_hint_question", "fleet_prompt_hint_needs_decision", "fleet_prompt_hint_failed",
		"fleet_prompt_hint_stuck", "fleet_prompt_hint_over_budget",
		"fleet_btn_pass", "fleet_btn_hint", "fleet_btn_goon", "fleet_btn_approve_merge",
		"fleet_cb_owner_only", "fleet_cb_hint", "fleet_cb_pass", "fleet_cb_goon",
		"fleet_result_changes", "fleet_result_verify_none", "fleet_result_verify_passed", "fleet_result_verify_failed",
		"fleet_result_next_local", "fleet_result_next_no_mistakes", "fleet_result_next_direct_pr",
		"fleet_mode_normal", "fleet_mode_quiet", "fleet_mode_afk", "fleet_disp_auto_approved", "fleet_disp_auto_resumed",
		"fleet_away_header", "fleet_away_nothing", "fleet_away_handled", "fleet_away_held", "fleet_away_held_hint",
		"fleet_afk_usage", "fleet_afk_err_duration", "fleet_afk_action_approve", "fleet_afk_action_resume",
	} {
		used[key] = true
	}
	for key := range used {
		z, okZ := zh[key]
		e, okE := en[key]
		if !okZ || !okE {
			t.Errorf("%s: zh-TW=%v en=%v; want it in both locales", key, okZ, okE)
			continue
		}
		zv, ev := varRe.FindAllString(z, -1), varRe.FindAllString(e, -1)
		set := func(xs []string) string {
			seen := map[string]bool{}
			var out []string
			for _, x := range xs {
				if !seen[x] {
					seen[x] = true
					out = append(out, x)
				}
			}
			sort.Strings(out)
			return strings.Join(out, ",")
		}
		if set(zv) != set(ev) {
			t.Errorf("%s: placeholders differ: zh-TW %v, en %v", key, zv, ev)
		}
	}
}
