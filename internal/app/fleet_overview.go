package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/chimerakang/alice-core/fleet"
)

// Overview actions. They concern no single task, so their callback data
// carries fleetAllTasks where a task ID would be.
const (
	fleetActOverview = "overview"
	fleetActTasks    = "tasks"
	fleetAllTasks    = "all"
)

// fleetTaskListLimit is how many tasks /fleet tasks lists.
const fleetTaskListLimit = 12

// fleetOverview answers /fleet: every project's counts and today's cost.
func (t *TelegramBot) fleetOverview(ctx context.Context, key chatKey) {
	digest, err := t.fleet.Commander.Digest(ctx)
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	loc := func(k string, vars map[string]string) string { return t.fleetMsg(key.chatID, k, vars) }
	var b strings.Builder
	b.WriteString(loc("fleet_overview_title", nil))
	if len(digest.Projects) == 0 {
		b.WriteString("\n\n" + loc("fleet_overview_empty", nil))
	}
	for _, p := range digest.Projects {
		name := p.Name
		if name == "" {
			name = p.ProjectID
		}
		b.WriteString("\n\n" + loc("fleet_overview_project", fleetCountVars(p.Counts, p.Today, map[string]string{"{name}": name})))
	}
	if len(digest.Projects) > 1 {
		b.WriteString("\n\n" + loc("fleet_overview_total", fleetCountVars(digest.Totals, digest.Today, nil)))
	}
	b.WriteString("\n\n" + loc("fleet_overview_mode", map[string]string{"{mode}": loc("fleet_mode_"+string(t.fleetMode(ctx)), nil)}))

	rows := [][]map[string]interface{}{{
		{"text": loc("fleet_btn_tasks", nil), "callback_data": fleetCallbackData(fleetActTasks, fleetAllTasks)},
	}}
	t.sendMenuMessage(key, b.String(), rows)
}

// fleetCountVars fills the placeholders shared by the overview's lines.
func fleetCountVars(c fleet.DigestCounts, today fleet.Usage, extra map[string]string) map[string]string {
	vars := map[string]string{
		"{running}":  fmt.Sprint(c.Running),
		"{queued}":   fmt.Sprint(c.Queued),
		"{review}":   fmt.Sprint(c.ReviewReady),
		"{decision}": fmt.Sprint(c.NeedsDecision),
		"{failed}":   fmt.Sprint(c.FailedToday),
		"{done}":     fmt.Sprint(c.DoneToday),
		"{cost}":     fmt.Sprintf("%.2f", today.CostUSD),
	}
	for k, v := range extra {
		vars[k] = v
	}
	return vars
}

// fleetMode is the owner's current notification mode.
func (t *TelegramBot) fleetMode(ctx context.Context) fleet.NotifyMode {
	if t.fleetNotifier != nil {
		if prefs, err := t.fleetNotifier.Prefs(ctx); err == nil && prefs.Mode != "" {
			return prefs.Mode
		}
	}
	return fleet.NotifyNormal
}

// fleetTaskList answers /fleet tasks: the topic's project's latest tasks,
// each a button that opens its card here.
func (t *TelegramBot) fleetTaskList(ctx context.Context, key chatKey) {
	project, ok := t.fleetRequireProject(ctx, key)
	if !ok {
		return
	}
	tasks, err := t.fleet.Commander.ListTasks(ctx, fleet.TaskFilter{ProjectID: project.ID, Limit: fleetTaskListLimit})
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	if len(tasks) == 0 {
		t.send(key, t.fleetMsg(key.chatID, "fleet_tasks_empty", map[string]string{"{name}": project.Name}))
		return
	}
	rows := make([][]map[string]interface{}, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, []map[string]interface{}{{
			"text":          fleetClip(fleetStatusEmoji[task.Status]+" "+fleetGoalLine(task), 48),
			"callback_data": fleetCallbackData(fleetActCard, task.ID),
		}})
	}
	t.sendMenuMessage(key, t.fleetMsg(key.chatID, "fleet_tasks_title", map[string]string{"{name}": project.Name}), rows)
}

// fleetOpenCard posts a task's card in this topic. From then on this is the
// card that is kept current; an earlier one stops changing.
func (t *TelegramBot) fleetOpenCard(ctx context.Context, key chatKey, taskID string) error {
	task, err := t.fleet.Commander.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if t.fleetCards == nil {
		return fmt.Errorf("task cards are unavailable")
	}
	return t.fleetCards.Post(ctx, key, task)
}

// fleetStatusLine is the fleet's part of /status.
func (t *TelegramBot) fleetStatusLine(ctx context.Context, chatID int64) string {
	digest, err := t.fleet.Commander.Digest(ctx)
	if err != nil {
		return ""
	}
	vars := fleetCountVars(digest.Totals, digest.Today, map[string]string{
		"{mode}": t.fleetMsg(chatID, "fleet_mode_"+string(t.fleetMode(ctx)), nil),
	})
	return t.fleetMsg(chatID, "fleet_status_line", vars)
}
