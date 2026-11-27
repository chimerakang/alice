package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"claude-tg-agent/internal/app/hermes"
	"github.com/chimerakang/alice-core/fleet"
)

// fleetDispatchArgs is a parsed /ship or /scout command.
type fleetDispatchArgs struct {
	// Goal is what to do. Empty when Issue is set.
	Goal string
	// Issue is the GitHub issue to dispatch from, or 0.
	Issue int
	// Model overrides the model for this task, or "".
	Model string
	// BudgetUSD caps this task's total cost, or 0 for the default.
	BudgetUSD float64
}

// Dispatch parse errors. Each is also the i18n key of its message.
var (
	errFleetDispatchEmpty      = errors.New("fleet_dispatch_err_empty")
	errFleetDispatchFlagValue  = errors.New("fleet_dispatch_err_flag_value")
	errFleetDispatchBudget     = errors.New("fleet_dispatch_err_budget")
	errFleetDispatchDuplicated = errors.New("fleet_dispatch_err_duplicate")
)

var (
	fleetTokenRe = regexp.MustCompile(`\S+`)
	// fleetIssueRe matches a goal that is nothing but an issue reference.
	fleetIssueRe = regexp.MustCompile(`^#(\d+)$`)
)

// parseFleetDispatch parses what follows /ship or /scout:
//
//	<goal>                 a task described in words
//	#<number>              a task from a GitHub issue
//	--model <name>         run this task on another model
//	--budget <usd>         cap this task's total cost, e.g. 5, $5, 2.50
//
// Flags may appear anywhere, as "--flag value" or "--flag=value". Everything
// else, line breaks included, is the goal; other words starting with "--"
// are part of it.
func parseFleetDispatch(text string) (fleetDispatchArgs, error) {
	var (
		args fleetDispatchArgs
		seen = map[string]bool{}
		goal strings.Builder
		kept = 0 // text before this offset has been copied or dropped
	)
	tokens := fleetTokenRe.FindAllStringIndex(text, -1)
	for i := 0; i < len(tokens); i++ {
		token := text[tokens[i][0]:tokens[i][1]]
		name, value, hasValue := strings.Cut(strings.TrimPrefix(token, "--"), "=")
		if !strings.HasPrefix(token, "--") || (name != "model" && name != "budget") {
			continue
		}
		end := tokens[i][1]
		if !hasValue {
			if i+1 >= len(tokens) {
				return fleetDispatchArgs{}, errFleetDispatchFlagValue
			}
			i++
			value, end = text[tokens[i][0]:tokens[i][1]], tokens[i][1]
		}
		if value == "" || strings.HasPrefix(value, "--") {
			return fleetDispatchArgs{}, errFleetDispatchFlagValue
		}
		if seen[name] {
			return fleetDispatchArgs{}, errFleetDispatchDuplicated
		}
		seen[name] = true
		switch name {
		case "model":
			args.Model = value
		case "budget":
			usd, err := parseFleetBudget(value)
			if err != nil {
				return fleetDispatchArgs{}, errFleetDispatchBudget
			}
			args.BudgetUSD = usd
		}
		// Drop the flag and its value from the goal.
		start := tokens[i][0]
		if !hasValue {
			start = tokens[i-1][0]
		}
		goal.WriteString(text[kept:start])
		kept = end
	}
	goal.WriteString(text[kept:])

	rest := strings.TrimSpace(goal.String())
	if rest == "" {
		return fleetDispatchArgs{}, errFleetDispatchEmpty
	}
	if m := fleetIssueRe.FindStringSubmatch(rest); m != nil {
		number, err := strconv.Atoi(m[1])
		if err != nil || number <= 0 {
			return fleetDispatchArgs{}, errFleetDispatchEmpty
		}
		args.Issue = number
		return args, nil
	}
	args.Goal = rest
	return args, nil
}

// parseFleetBudget reads a cost in US dollars: "5", "$5", "2.50", "5usd".
func parseFleetBudget(value string) (float64, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.TrimPrefix(v, "$")
	v = strings.TrimSuffix(v, "usd")
	usd, err := strconv.ParseFloat(v, 64)
	if err != nil || usd <= 0 || usd > 1e6 {
		return 0, fmt.Errorf("invalid budget %q", value)
	}
	return usd, nil
}

// fleetDispatch handles /ship and /scout: it queues a task for the topic's
// project and answers with the task's card.
func (t *TelegramBot) fleetDispatch(ctx context.Context, key chatKey, userID int64, shape fleet.Shape, rest string) {
	usageKey := "fleet_" + string(shape) + "_usage"
	args, err := parseFleetDispatch(rest)
	if err != nil {
		msg := t.fleetMsg(key.chatID, usageKey, nil)
		if !errors.Is(err, errFleetDispatchEmpty) {
			msg = t.fleetMsg(key.chatID, err.Error(), nil) + "\n\n" + msg
		}
		t.send(key, msg)
		return
	}
	project, ok := t.fleetRequireProject(ctx, key)
	if !ok {
		return
	}
	if args.Issue > 0 {
		t.send(key, t.fleetMsg(key.chatID, "fleet_issue_reading", map[string]string{
			"{project}": project.Name,
			"{number}":  strconv.Itoa(args.Issue),
		}))
		if !t.fleetIssuePassesGate(ctx, key, project, args.Issue) {
			return
		}
	}

	task, err := fleetCommanderDispatch(ctx, t.fleet, fleetActor(userID), fleet.DispatchRequest{
		ProjectID: project.ID,
		Goal:      args.Goal,
		Issue:     args.Issue,
		Shape:     shape,
		Model:     args.Model,
		Budget:    fleet.Limit{CostUSD: args.BudgetUSD},
	})
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.fleetPostCard(ctx, key, task)
}

// fleetPostCard answers with the task's card, or with a plain line if the
// card cannot be sent: the task is dispatched either way.
func (t *TelegramBot) fleetPostCard(ctx context.Context, key chatKey, task fleet.Task) {
	if t.fleetCards != nil {
		err := t.fleetCards.Post(ctx, key, task)
		if err == nil {
			return
		}
		log.Printf("[fleet] post card for task %s: %v", task.ID, err)
	}
	t.send(key, t.fleetMsg(key.chatID, "fleet_dispatched", map[string]string{
		"{id}":   task.ID,
		"{goal}": fleetGoalLine(task),
	}))
}

var fleetFetchIssue = hermes.FetchIssueForDispatch

// fleetIssuePassesGate applies the existing issue quality check before an
// issue is dispatched. It reports whether to go ahead; when not, the reason
// has been sent.
func (t *TelegramBot) fleetIssuePassesGate(ctx context.Context, key chatKey, project fleet.Project, number int) bool {
	issue, err := fleetFetchIssue(ctx, project.RepoPath, number)
	if err != nil {
		t.send(key, t.fleetMsg(key.chatID, "fleet_issue_read_failed", map[string]string{
			"{number}": strconv.Itoa(number),
			"{error}":  err.Error(),
		}))
		return false
	}
	decision := decideIssueQualityGate(issue, false)
	recordIssueQualityGateDecision(ctx, key, issue, decision, false)
	switch decision.Action {
	case issueQualityGateSkip, issueQualityGateNeedsClarification:
		if decision.Message != "" {
			t.send(key, decision.Message)
		}
		return false
	}
	return true
}
