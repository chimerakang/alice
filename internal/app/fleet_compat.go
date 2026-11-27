package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/chimerakang/alice-core/fleet"
)

// With the fleet on, the commands of the old task system keep working by
// doing the nearest fleet thing, and say what the fleet's own command is.
// A command with nothing in the fleet to act on falls through to its old
// behaviour, which still serves the ordinary chat agent.

// fleetCompat handles an old command in fleet mode. It reports whether the
// command was handled.
func (t *TelegramBot) fleetCompat(ctx context.Context, key chatKey, userID int64, command string, parts []string, rest string) bool {
	switch command {
	case "/hermes", "/ghermes":
		return t.fleetCompatHermes(ctx, key, userID, command, parts, rest)
	case "/abort":
		// A chat agent at work in this topic is what /abort has always
		// stopped; leave that to the old handler.
		if t.fleetChatAgentBusy(key) {
			return false
		}
		return t.fleetCompatAct(ctx, key, userID, command, parts, "/fleet tasks",
			[]fleet.Status{fleet.StatusRunning, fleet.StatusVerifying, fleet.StatusQueued},
			func(id string) error {
				_, err := t.fleet.Commander.Cancel(ctx, fleetActor(userID), id, "")
				return err
			}, "fleet_compat_cancelled")
	case "/retry":
		return t.fleetCompatAct(ctx, key, userID, command, parts, "/fleet tasks",
			[]fleet.Status{fleet.StatusFailed, fleet.StatusNeedsDecision},
			func(id string) error {
				_, err := fleetCommanderRetry(ctx, t.fleet, fleetActor(userID), id)
				return err
			}, "fleet_compat_retried")
	case "/resume":
		// Anything after the task ID is sent to the agent as instructions.
		instruction := ""
		if len(parts) > 2 {
			instruction = strings.TrimSpace(strings.SplitN(rest, parts[1], 2)[1])
		}
		return t.fleetCompatAct(ctx, key, userID, command, parts, "/fleet tasks",
			[]fleet.Status{fleet.StatusNeedsDecision},
			func(id string) error {
				_, err := t.fleet.Commander.Reply(ctx, fleetActor(userID), fleet.ReplyRequest{TaskID: id, Text: instruction})
				return err
			}, "fleet_compat_resumed")
	case "/close":
		// /close #<n> closes a GitHub issue, as before. Only /close with a
		// fleet task's ID discards that task.
		if len(parts) != 2 || !t.fleetTaskExists(ctx, parts[1]) {
			return false
		}
		if _, err := t.fleet.Commander.Discard(ctx, fleetActor(userID), parts[1]); err != nil {
			t.fleetSendError(key, err)
			return true
		}
		t.send(key, t.fleetMsg(key.chatID, "fleet_compat_discarded", map[string]string{"{id}": parts[1]}))
		return true
	case "/parallel":
		return t.fleetCompatParallel(ctx, key, userID, rest)
	case "/strict":
		return t.fleetCompatStrict(ctx, key, userID, parts)
	case "/mr":
		t.send(key, t.fleetMsg(key.chatID, "fleet_compat_mr_unsupported", nil))
		return true
	}
	return false
}

func (t *TelegramBot) fleetSendError(key chatKey, err error) {
	t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
}

// fleetChatAgentBusy reports whether this topic's chat agent is in the
// middle of a turn. It never creates an agent.
func (t *TelegramBot) fleetChatAgentBusy(key chatKey) bool {
	t.agentsMu.RLock()
	agent := t.agents[key]
	t.agentsMu.RUnlock()
	return agent != nil && agent.IsProcessing()
}

func (t *TelegramBot) fleetTaskExists(ctx context.Context, id string) bool {
	_, err := t.fleet.Commander.GetTask(ctx, id)
	return err == nil
}

// fleetCompatHermes maps /hermes and /ghermes onto /ship.
func (t *TelegramBot) fleetCompatHermes(ctx context.Context, key chatKey, userID int64, command string, parts []string, rest string) bool {
	note := func() {
		msgKey := "fleet_compat_hermes"
		if command == "/ghermes" {
			msgKey = "fleet_compat_ghermes"
		}
		t.send(key, t.fleetMsg(key.chatID, msgKey, nil))
	}
	if len(parts) > 1 {
		switch strings.ToLower(parts[1]) {
		case "issues":
			return false // listing GitHub issues is unchanged
		case "status":
			note()
			t.fleetTaskList(ctx, key)
			return true
		case "stop":
			return t.fleetCompat(ctx, key, userID, "/abort", []string{"/abort"}, "")
		case "restart":
			rest = strings.TrimSpace(strings.TrimPrefix(rest, parts[1]))
		}
	}
	note()
	t.fleetDispatch(ctx, key, userID, fleet.ShapeShip, rest)
	return true
}

// fleetCompatAct applies an old command to a fleet task: the one named, or
// the only one of the topic's project in a status the command applies to.
// With several candidates it lists them; with none it is not handled, so
// the old command runs.
func (t *TelegramBot) fleetCompatAct(ctx context.Context, key chatKey, userID int64, command string, parts []string,
	newCommand string, statuses []fleet.Status, act func(taskID string) error, doneKey string) bool {

	var taskID string
	switch {
	case len(parts) > 1 && t.fleetTaskExists(ctx, parts[1]):
		taskID = parts[1]
	case len(parts) > 1:
		// Arguments that name no fleet task belong to the old command.
		return false
	default:
		project, err := t.fleetTopicProject(ctx, key)
		if err != nil {
			return false
		}
		tasks, err := t.fleet.Commander.ListTasks(ctx, fleet.TaskFilter{ProjectID: project.ID, Statuses: statuses})
		if err != nil || len(tasks) == 0 {
			return false
		}
		if len(tasks) > 1 {
			rows := make([][]map[string]interface{}, 0, len(tasks))
			for _, task := range tasks {
				rows = append(rows, []map[string]interface{}{{
					"text":          fleetClip(fleetStatusEmoji[task.Status]+" "+fleetGoalLine(task), 48),
					"callback_data": fleetCallbackData(fleetActCard, task.ID),
				}})
			}
			t.sendMenuMessage(key, t.fleetMsg(key.chatID, "fleet_compat_pick", map[string]string{
				"{command}": command, "{count}": strconv.Itoa(len(tasks)),
			}), rows)
			return true
		}
		taskID = tasks[0].ID
	}
	if err := act(taskID); err != nil {
		t.fleetSendError(key, err)
		return true
	}
	t.send(key, t.fleetMsg(key.chatID, doneKey, map[string]string{"{id}": taskID, "{new}": newCommand}))
	return true
}

// fleetCompatParallel maps /parallel onto several /ship tasks.
func (t *TelegramBot) fleetCompatParallel(ctx context.Context, key chatKey, userID int64, rest string) bool {
	goals := ParseParallelTasks(rest)
	switch {
	case len(goals) < 2:
		t.send(key, t.fleetMsg(key.chatID, "parallel_min_tasks", nil))
		return true
	case len(goals) > 5:
		t.send(key, t.fleetMsg(key.chatID, "parallel_max_tasks", nil))
		return true
	}
	project, ok := t.fleetRequireProject(ctx, key)
	if !ok {
		return true
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_compat_parallel", map[string]string{"{count}": strconv.Itoa(len(goals))}))
	for _, goal := range goals {
		task, err := t.fleet.DispatchIssue(ctx, fleetActor(userID), fleet.DispatchRequest{
			ProjectID: project.ID, Goal: goal.Description, Shape: fleet.ShapeShip,
		})
		if err != nil {
			t.fleetSendError(key, err)
			continue
		}
		t.fleetPostCard(ctx, key, task)
	}
	return true
}

// fleetCompatStrict maps /strict onto the no-mistakes trust mode.
func (t *TelegramBot) fleetCompatStrict(ctx context.Context, key chatKey, userID int64, parts []string) bool {
	project, ok := t.fleetRequireProject(ctx, key)
	if !ok {
		return true
	}
	var mode fleet.TrustMode
	switch {
	case len(parts) == 2 && strings.EqualFold(parts[1], "on"):
		mode = fleet.TrustNoMistakes
	case len(parts) == 2 && strings.EqualFold(parts[1], "off"):
		// Back to the configured default, unless that is strict itself.
		if mode = t.fleet.TrustMode; mode == fleet.TrustNoMistakes {
			mode = fleet.TrustLocalOnly
		}
	default:
		t.send(key, t.fleetMsg(key.chatID, "fleet_compat_strict_usage", map[string]string{"{mode}": string(project.TrustMode)}))
		return true
	}
	updated, err := t.fleet.Commander.UpdateProject(ctx, fleetActor(userID), project.ID, "trust mode: "+string(mode),
		func(p *fleet.Project) error {
			p.TrustMode = mode
			return nil
		})
	if err != nil {
		if errors.Is(err, fleet.ErrInvalidProject) {
			t.send(key, t.fleetMsg(key.chatID, "fleet_compat_strict_no_remote", nil))
			return true
		}
		t.fleetSendError(key, err)
		return true
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_compat_strict_set", map[string]string{
		"{name}": updated.Name, "{mode}": string(updated.TrustMode),
	}))
	return true
}
