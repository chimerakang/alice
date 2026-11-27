package app

import (
	"context"
	"time"

	"github.com/chimerakang/alice-core/ghissue"
)

// removedCommandKeys names, for each command of the removed task system, the
// message that points at what replaced it.
var removedCommandKeys = map[string]string{
	"/hermes":       "removed_cmd_hermes",
	"/ghermes":      "removed_cmd_hermes",
	"/resume":       "removed_cmd_resume",
	"/retry":        "removed_cmd_retry",
	"/hermes-stats": "removed_cmd_stats",
	"/strict":       "removed_cmd_strict",
	"/mr":           "removed_cmd_mr",
}

func (t *TelegramBot) sendRemovedCommand(key chatKey, command string) {
	t.send(key, t.getLocalizedMessage(key.chatID, removedCommandKeys[command], nil))
}

// answerRemovedButton answers a button left under a message the removed task
// system sent.
func (t *TelegramBot) answerRemovedButton(key chatKey, queryID string) {
	t.answerCallbackQuery(queryID, t.getLocalizedMessage(key.chatID, "removed_button", nil))
}

// handleListIssues answers /hermes issues, the one subcommand that only read
// GitHub and so still works.
func (t *TelegramBot) handleListIssues(key chatKey) {
	projectDir := t.getAgent(key).ProjectDir()
	go func() {
		done := globalJobTracker.Start("telegram.list_issues")
		var jobErr error
		defer func() { done(jobErr) }()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := ghissue.ListIssues(ctx, projectDir, 15)
		if err != nil {
			jobErr = err
			t.send(key, t.getLocalizedMessage(key.chatID, "issue_list_failed", map[string]string{"error": err.Error()}))
			return
		}
		t.send(key, ghissue.FormatIssueList(items))
	}()
}
