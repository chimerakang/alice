package app

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/chimerakang/alice-core/fleet"

	alicev1 "claude-tg-agent/gen/go/alice/v1"
)

// Fleet model routing: the commander picks a model tier for a task that was
// dispatched without a model, records why, and moves an auto-chosen model up
// one tier when the task is retried after a failure. The tiered models come
// from model_routing; nothing here names a model, a label or a threshold.

// fleetTier is one rung of the model ladder. The names are the keys used in
// config (tier_labels, scout_codex_tiers) and in the recorded events.
type fleetTier string

const (
	fleetTierFast     fleetTier = "fast"
	fleetTierSmart    fleetTier = "smart"
	fleetTierDeep     fleetTier = "deep"
	fleetTierFlagship fleetTier = "flagship"
)

// fleetTierLadder runs from the cheapest tier to the most capable.
var fleetTierLadder = []fleetTier{fleetTierFast, fleetTierSmart, fleetTierDeep, fleetTierFlagship}

func (t fleetTier) rank() int {
	for i, c := range fleetTierLadder {
		if c == t {
			return i
		}
	}
	return -1
}

func parseFleetTier(s string) (fleetTier, bool) {
	t := fleetTier(strings.ToLower(strings.TrimSpace(s)))
	return t, t.rank() >= 0
}

// model returns the configured model of a tier, or "" when the tier is not
// configured.
func (c ModelRoutingConfig) fleetTierModel(t fleetTier) string {
	switch t {
	case fleetTierFast:
		return strings.TrimSpace(c.FastModel)
	case fleetTierSmart:
		return strings.TrimSpace(c.SmartModel)
	case fleetTierDeep:
		return strings.TrimSpace(c.DeepModel)
	case fleetTierFlagship:
		return strings.TrimSpace(c.FlagshipModel)
	}
	return ""
}

// fleetCodexTierModel returns the Codex model configured for a tier, if any.
func (c ModelRoutingConfig) fleetCodexTierModel(t fleetTier) string {
	switch t {
	case fleetTierFast:
		return strings.TrimSpace(c.CodexFastModel)
	case fleetTierSmart:
		return strings.TrimSpace(c.CodexSmartModel)
	case fleetTierDeep:
		return strings.TrimSpace(c.CodexDeepModel)
	}
	return ""
}

// resolveTier lowers t to the nearest tier that has a model: an unset
// flagship becomes deep, an unset deep becomes smart, an unset fast becomes
// smart. It reports false when smart is not configured.
func (c ModelRoutingConfig) resolveTier(t fleetTier) (fleetTier, string, bool) {
	// Smart is the default tier; without it there is nothing to choose.
	if c.fleetTierModel(fleetTierSmart) == "" {
		return "", "", false
	}
	for r := t.rank(); r >= 0; r-- {
		if m := c.fleetTierModel(fleetTierLadder[r]); m != "" {
			return fleetTierLadder[r], m, true
		}
	}
	return fleetTierSmart, c.fleetTierModel(fleetTierSmart), true
}

// nextTier returns the next configured tier above t, if there is one.
func (c ModelRoutingConfig) nextTier(t fleetTier) (fleetTier, string, bool) {
	for r := t.rank() + 1; r >= 1 && r < len(fleetTierLadder); r++ {
		if m := c.fleetTierModel(fleetTierLadder[r]); m != "" {
			return fleetTierLadder[r], m, true
		}
	}
	return "", "", false
}

// Event types the commander records on a task.
const (
	// fleetEventModelSelected carries the model choice: who made it, the
	// tier, the model and why. A retry escalation records another one.
	fleetEventModelSelected = "model.selected"
	// fleetEventScoutBackend records which Scout backend was chosen.
	fleetEventScoutBackend = "scout.backend"
)

// Who chose a task's model.
const (
	fleetModelSourceManual    = "manual"
	fleetModelSourceCommander = "commander"
)

// Reason codes. The dashboard and Telegram translate them; Detail carries the
// label, matched rule or status reason.
const (
	fleetReasonLabel      = "label"
	fleetReasonComplexity = "complexity"
	fleetReasonDefault    = "default"
	fleetReasonEscalated  = "escalated"
	fleetReasonUSDBudget  = "usd_budget"
	fleetReasonAuto       = "auto"
	fleetReasonConfigured = "configured"
)

// fleetModelChoice is the payload of a fleetEventModelSelected event.
type fleetModelChoice struct {
	Source string `json:"source"`
	Tier   string `json:"tier,omitempty"`
	Model  string `json:"model"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	// From is the model an escalation replaced.
	From string `json:"from,omitempty"`
}

// fleetRouteInput is what the commander knows about a task before it runs.
type fleetRouteInput struct {
	Shape  fleet.Shape
	Goal   string
	Labels []string
}

// chooseModel picks a tier and model for a task that named none. It returns
// false when auto-selection is off or not configured, in which case the task
// runs on the fleet default as before.
func (s *FleetService) chooseModel(in fleetRouteInput) (fleetModelChoice, bool) {
	if s == nil || !s.Config.autoModelEnabled() {
		return fleetModelChoice{}, false
	}
	tier, reason, detail := fleetTierSmart, fleetReasonDefault, ""

	// A label is an explicit statement by the owner and beats the text.
	if t, label, ok := s.tierFromLabels(in.Labels); ok {
		tier, reason, detail = t, fleetReasonLabel, label
	} else if goal := strings.TrimSpace(in.Goal); goal != "" {
		cl := ClassifyComplexity(goal)
		// Unsure stays on smart: only a positive signal moves off it.
		if cl.MatchedRule != "default" && cl.MatchedRule != "empty" {
			switch cl.Complexity {
			case ComplexityTrivial:
				tier = fleetTierFast
			case ComplexityComplex:
				tier = fleetTierDeep
			}
			if tier != fleetTierSmart {
				reason, detail = fleetReasonComplexity, cl.MatchedRule
			}
		}
	}
	resolved, model, ok := s.Routing.resolveTier(tier)
	if !ok {
		return fleetModelChoice{}, false
	}
	return fleetModelChoice{Source: fleetModelSourceCommander, Tier: string(resolved), Model: model, Reason: reason, Detail: detail}, true
}

// tierFromLabels returns the highest tier any label maps to.
func (s *FleetService) tierFromLabels(labels []string) (fleetTier, string, bool) {
	if len(s.Config.TierLabels) == 0 {
		return "", "", false
	}
	mapped := make(map[string]fleetTier, len(s.Config.TierLabels))
	for label, tier := range s.Config.TierLabels {
		if t, ok := parseFleetTier(tier); ok {
			mapped[strings.ToLower(strings.TrimSpace(label))] = t
		}
	}
	var (
		best  fleetTier
		found string
	)
	for _, label := range labels {
		if t, ok := mapped[strings.ToLower(strings.TrimSpace(label))]; ok && t.rank() > best.rank() {
			best, found = t, label
		}
	}
	return best, found, found != ""
}

// defaultNoEscalateReasons are fragments of fleet status reasons that mean a
// task stopped for something a bigger model would not fix.
var defaultNoEscalateReasons = []string{
	"over the task budget",
	"over the project's daily budget",
	"over the fleet's daily budget",
	"agent process is gone",
	"runner stopped",
	"verification interrupted",
	"usage limit",
	"rate limit",
	"quota",
}

func (s *FleetService) noEscalateReasons() []string {
	return append(append([]string(nil), defaultNoEscalateReasons...), s.Config.NoEscalateReasons...)
}

// escalation decides the model for retrying task. current is the last
// recorded model choice. It returns false when the model should stay: not
// auto-chosen, not failed, stopped for an unrelated reason, or already at the
// top.
func (s *FleetService) escalation(task fleet.Task, current *fleetModelChoice) (fleetModelChoice, bool) {
	if s == nil || !s.Config.autoModelEnabled() || current == nil {
		return fleetModelChoice{}, false
	}
	if current.Source != fleetModelSourceCommander || current.Model != task.Model {
		return fleetModelChoice{}, false
	}
	if task.Status != fleet.StatusNeedsDecision && task.Status != fleet.StatusFailed {
		return fleetModelChoice{}, false
	}
	reason := strings.ToLower(task.StatusReason)
	for _, frag := range s.noEscalateReasons() {
		if frag = strings.ToLower(strings.TrimSpace(frag)); frag != "" && strings.Contains(reason, frag) {
			return fleetModelChoice{}, false
		}
	}
	tier, ok := parseFleetTier(current.Tier)
	if !ok {
		return fleetModelChoice{}, false
	}
	next, model, ok := s.Routing.nextTier(tier)
	if !ok || model == task.Model {
		return fleetModelChoice{}, false
	}
	return fleetModelChoice{
		Source: fleetModelSourceCommander, Tier: string(next), Model: model,
		Reason: fleetReasonEscalated, Detail: string(tier) + "→" + string(next), From: task.Model,
	}, true
}

// lastModelChoice returns the most recent model choice recorded on a task.
func fleetLastModelChoice(ctx context.Context, store *fleet.Store, taskID string) *fleetModelChoice {
	ev, ok, err := store.LastEvent(ctx, taskID, fleetEventModelSelected)
	if err != nil || !ok {
		return nil
	}
	var c fleetModelChoice
	if json.Unmarshal(ev.Payload, &c) != nil {
		return nil
	}
	return &c
}

// fleetModelSetter is what the store needs for a retry to change a task's
// model. alice-core does not provide it yet, so escalation is decided and
// reported but not applied until it does.
type fleetModelSetter interface {
	SetModel(ctx context.Context, taskID, model string) error
}

// Scout backends.
const (
	fleetBackendClaude = "claude"
	fleetBackendCodex  = "codex"
	fleetBackendAuto   = "auto"
)

// fleetScoutBackendChoice is the payload of a fleetEventScoutBackend event.
type fleetScoutBackendChoice struct {
	Backend          string `json:"backend"`
	Reason           string `json:"reason"`
	EffectiveBackend string `json:"effective_backend,omitempty"`
}

// chooseScoutBackend decides between Claude and Codex for a Scout task.
// usdLimited says the task runs under a USD budget (its own, its project's
// or the fleet's): Codex does not report USD usage and cannot run under one,
// so auto mode never hands such a task to Codex.
func (s *FleetService) chooseScoutBackend(tier string, usdLimited bool) fleetScoutBackendChoice {
	switch strings.ToLower(strings.TrimSpace(s.Config.ScoutBackend)) {
	case fleetBackendCodex:
		return fleetScoutBackendChoice{Backend: fleetBackendCodex, Reason: fleetReasonConfigured}
	case fleetBackendAuto:
	default:
		return fleetScoutBackendChoice{Backend: fleetBackendClaude, Reason: fleetReasonConfigured}
	}
	if usdLimited {
		return fleetScoutBackendChoice{Backend: fleetBackendClaude, Reason: fleetReasonUSDBudget}
	}
	if t, ok := parseFleetTier(tier); ok && s.Routing.fleetCodexTierModel(t) != "" {
		for _, allowed := range s.Config.ScoutCodexTiers {
			if a, ok := parseFleetTier(allowed); ok && a == t {
				return fleetScoutBackendChoice{Backend: fleetBackendCodex, Reason: fleetReasonAuto}
			}
		}
	}
	return fleetScoutBackendChoice{Backend: fleetBackendClaude, Reason: fleetReasonAuto}
}

// usdLimited reports whether a task dispatched with the given budget would
// run under any USD limit.
func (s *FleetService) usdLimited(ctx context.Context, projectID string, taskBudgetUSD float64) bool {
	if taskBudgetUSD > 0 || s.Config.TaskBudgetUSD > 0 || s.Config.DailyBudgetUSD > 0 {
		return true
	}
	if s.Store != nil && projectID != "" {
		if p, err := s.Store.GetProject(ctx, projectID); err == nil {
			return p.Budget.Task.CostUSD > 0 || p.Budget.Daily.CostUSD > 0
		}
	}
	return false
}

// fleetIssueLabels reads the labels and text of a source issue, for routing.
// A failure returns nothing: routing then falls back to the goal and smart.
func fleetIssueText(ctx context.Context, svc *FleetService, projectID string, number int) (text string, labels []string) {
	if number <= 0 || svc.Store == nil {
		return "", nil
	}
	p, err := svc.Store.GetProject(ctx, projectID)
	if err != nil {
		return "", nil
	}
	issue, err := hermesFetchIssue(ctx, p.RepoPath, number)
	if err != nil || issue == nil {
		return "", nil
	}
	return issue.Title + "\n" + issue.Body, issue.Labels
}

// fleetCommanderDispatch queues a task like Commander.Dispatch, first
// letting the commander choose the model when none was given, and records the
// choice on the task. A model named in the request is used as is.
func fleetCommanderDispatch(ctx context.Context, svc *FleetService, actor fleet.Actor, req fleet.DispatchRequest) (fleet.Task, error) {
	req.Model = strings.TrimSpace(req.Model)
	var choice *fleetModelChoice
	if req.Model == "" && svc.Config.autoModelEnabled() {
		in := fleetRouteInput{Shape: req.Shape, Goal: req.Goal}
		if req.Issue > 0 {
			text, labels := fleetIssueText(ctx, svc, req.ProjectID, req.Issue)
			in.Goal, in.Labels = strings.TrimSpace(req.Goal+"\n"+text), labels
		}
		if c, ok := svc.chooseModel(in); ok {
			choice, req.Model = &c, c.Model
		}
	}
	task, err := svc.Commander.Dispatch(ctx, actor, req)
	if err != nil {
		return task, err
	}
	if choice != nil {
		svc.recordEvent(ctx, task.ID, fleetEventModelSelected, choice)
	}
	if task.Shape == fleet.ShapeScout && strings.TrimSpace(svc.Config.ScoutBackend) != "" {
		tier := ""
		if choice != nil {
			tier = choice.Tier
		}
		backend := svc.chooseScoutBackend(tier, svc.usdLimited(ctx, task.ProjectID, req.Budget.CostUSD))
		// The pinned runner still uses Claude for every Scout.
		backend.EffectiveBackend = fleetBackendClaude
		svc.recordEvent(ctx, task.ID, fleetEventScoutBackend, backend)
	}
	return task, nil
}

func (s *FleetService) recordEvent(ctx context.Context, taskID, eventType string, payload any) {
	if s.Store == nil {
		return
	}
	if _, err := s.Store.AppendEvent(ctx, taskID, eventType, payload); err != nil {
		log.Printf("[fleet] record %s for task %s: %v", eventType, taskID, err)
	}
}

// fleetCommanderRetry retries a task, first escalating its model one tier
// when the commander chose it and it stopped for a reason a stronger model
// could fix. The escalation is recorded only when it was applied.
func fleetCommanderRetry(ctx context.Context, svc *FleetService, actor fleet.Actor, taskID string) (fleet.Task, error) {
	if task, err := svc.Commander.GetTask(ctx, taskID); err == nil {
		if next, ok := svc.escalation(task, fleetLastModelChoice(ctx, svc.Store, taskID)); ok {
			if setter, can := any(svc.Store).(fleetModelSetter); can {
				if err := setter.SetModel(ctx, taskID, next.Model); err != nil {
					log.Printf("[fleet] escalate task %s to %s: %v", taskID, next.Model, err)
				} else {
					svc.recordEvent(ctx, taskID, fleetEventModelSelected, next)
				}
			} else {
				log.Printf("[fleet] task %s would escalate to %s (%s), but alice-core cannot change a task's model yet", taskID, next.Model, next.Tier)
			}
		}
	}
	return svc.Commander.Retry(ctx, actor, taskID)
}

// fleetAnnotateModel says on the task's wire form who chose its model. A model
// that is set but was not chosen by the commander (or was changed since) is
// the owner's.
func fleetAnnotateModel(ctx context.Context, svc *FleetService, out *alicev1.FleetTask, task fleet.Task) {
	if task.Model == "" {
		return
	}
	out.ModelSource = fleetModelSourceManual
	if svc == nil || svc.Store == nil {
		return
	}
	if c := fleetLastModelChoice(ctx, svc.Store, task.ID); c != nil && c.Source == fleetModelSourceCommander && c.Model == task.Model {
		out.ModelSource, out.ModelTier, out.ModelReason, out.ModelReasonDetail = c.Source, c.Tier, c.Reason, c.Detail
	}
}
