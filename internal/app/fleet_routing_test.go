package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/chimerakang/alice-core/fleet"

	alicev1 "claude-tg-agent/gen/go/alice/v1"
)

// Tier models are test fixtures; production reads them from config.
func routingFixture() ModelRoutingConfig {
	return ModelRoutingConfig{
		FastModel: "m-fast", SmartModel: "m-smart", DeepModel: "m-deep", FlagshipModel: "m-flag",
		CodexSmartModel: "c-smart",
	}
}

func routingSvc(cfg FleetConfig, r ModelRoutingConfig) *FleetService {
	return &FleetService{Config: cfg, Routing: r}
}

func TestChooseModelTiers(t *testing.T) {
	cfg := FleetConfig{TierLabels: map[string]string{"docs": "fast", "Size:XL": "flagship", "big": "deep"}}
	svc := routingSvc(cfg, routingFixture())

	cases := []struct {
		name   string
		in     fleetRouteInput
		tier   string
		reason string
	}{
		{"label fast", fleetRouteInput{Goal: "do the thing", Labels: []string{"docs"}}, "fast", fleetReasonLabel},
		{"label case-insensitive", fleetRouteInput{Labels: []string{"size:xl"}}, "flagship", fleetReasonLabel},
		{"highest label wins", fleetRouteInput{Labels: []string{"docs", "big"}}, "deep", fleetReasonLabel},
		{"unknown label ignored", fleetRouteInput{Goal: "ok", Labels: []string{"bug"}}, "smart", fleetReasonDefault},
		{"empty goal is unsure", fleetRouteInput{}, "smart", fleetReasonDefault},
		{"complex verb", fleetRouteInput{Goal: "refactor the whole storage layer"}, "deep", fleetReasonComplexity},
	}
	for _, c := range cases {
		got, ok := svc.chooseModel(c.in)
		if !ok || got.Tier != c.tier || got.Reason != c.reason || got.Source != fleetModelSourceCommander {
			t.Errorf("%s: got %+v ok=%v, want tier %s reason %s", c.name, got, ok, c.tier, c.reason)
		}
		if want := routingFixture().fleetTierModel(fleetTier(c.tier)); got.Model != want {
			t.Errorf("%s: model %q, want %q", c.name, got.Model, want)
		}
	}
}

func TestChooseModelTrivialGoesFast(t *testing.T) {
	svc := routingSvc(FleetConfig{}, routingFixture())
	// Whatever the classifier calls trivial must map to the fast tier.
	goal := "git status"
	if cl := ClassifyComplexity(goal); cl.Complexity != ComplexityTrivial || cl.MatchedRule == "default" {
		t.Skipf("classifier no longer treats %q as trivial", goal)
	}
	if got, ok := svc.chooseModel(fleetRouteInput{Goal: goal}); !ok || got.Tier != "fast" {
		t.Fatalf("got %+v", got)
	}
}

func TestChooseModelMissingTiers(t *testing.T) {
	r := routingFixture()
	r.FlagshipModel = ""
	svc := routingSvc(FleetConfig{TierLabels: map[string]string{"hard": "flagship"}}, r)
	got, ok := svc.chooseModel(fleetRouteInput{Labels: []string{"hard"}})
	if !ok || got.Tier != "deep" {
		t.Fatalf("unset flagship should fall back to deep, got %+v", got)
	}
	if _, ok := routingSvc(FleetConfig{}, ModelRoutingConfig{FastModel: "m"}).chooseModel(fleetRouteInput{}); ok {
		t.Fatal("without a smart model nothing should be chosen")
	}
}

func TestChooseModelDisabled(t *testing.T) {
	off := false
	svc := routingSvc(FleetConfig{AutoModel: &off}, routingFixture())
	if _, ok := svc.chooseModel(fleetRouteInput{Goal: "refactor everything"}); ok {
		t.Fatal("disabled switch must not choose")
	}
}

func TestEscalation(t *testing.T) {
	svc := routingSvc(FleetConfig{}, routingFixture())
	auto := func(tier, model string) *fleetModelChoice {
		return &fleetModelChoice{Source: fleetModelSourceCommander, Tier: tier, Model: model}
	}
	task := func(status fleet.Status, reason, model string) fleet.Task {
		return fleet.Task{Status: status, StatusReason: reason, Model: model}
	}

	steps := []struct{ from, to string }{{"fast", "smart"}, {"smart", "deep"}, {"deep", "flagship"}}
	for _, s := range steps {
		fromModel := routingFixture().fleetTierModel(fleetTier(s.from))
		got, ok := svc.escalation(task(fleet.StatusFailed, "agent error: x", fromModel), auto(s.from, fromModel))
		if !ok || got.Tier != s.to || got.Reason != fleetReasonEscalated || got.From != fromModel {
			t.Errorf("%s: got %+v ok=%v", s.from, got, ok)
		}
	}
	if _, ok := svc.escalation(task(fleet.StatusNeedsDecision, "reached the turn limit", "m-smart"), auto("smart", "m-smart")); !ok {
		t.Error("needs_decision should escalate")
	}

	// No escalation.
	if _, ok := svc.escalation(task(fleet.StatusFailed, "x", "m-flag"), auto("flagship", "m-flag")); ok {
		t.Error("top tier must stay")
	}
	if _, ok := svc.escalation(task(fleet.StatusFailed, "x", "m-smart"), &fleetModelChoice{Source: fleetModelSourceManual, Tier: "smart", Model: "m-smart"}); ok {
		t.Error("manual choice must not escalate")
	}
	if _, ok := svc.escalation(task(fleet.StatusFailed, "x", "m-other"), auto("smart", "m-smart")); ok {
		t.Error("a model changed by hand since must not escalate")
	}
	if _, ok := svc.escalation(task(fleet.StatusFailed, "x", "m-smart"), nil); ok {
		t.Error("a task with no recorded choice is manual")
	}
	for _, reason := range []string{
		"over the task budget: $5 of $5",
		"over the fleet's daily budget: x",
		"agent process is gone: it died or the host restarted while it was running",
		"agent error: Claude usage limit reached",
		"agent error: You've hit your weekly limit · resets 2pm (Asia/Taipei)",
		"verification was cut off by a host restart",
		"agent was left running without supervision by a host restart and has been stopped",
	} {
		if _, ok := svc.escalation(task(fleet.StatusNeedsDecision, reason, "m-smart"), auto("smart", "m-smart")); ok {
			t.Errorf("%q must not escalate", reason)
		}
	}
	if _, ok := svc.escalation(task(fleet.StatusReviewReady, "x", "m-smart"), auto("smart", "m-smart")); ok {
		t.Error("only failed or needs_decision tasks escalate")
	}

	// Unset flagship: deep is the end.
	r := routingFixture()
	r.FlagshipModel = ""
	noFlag := routingSvc(FleetConfig{}, r)
	if _, ok := noFlag.escalation(task(fleet.StatusFailed, "x", "m-deep"), auto("deep", "m-deep")); ok {
		t.Error("escalation must stop at deep when flagship is unset")
	}
	// Disabled switch.
	off := false
	offSvc := routingSvc(FleetConfig{AutoModel: &off}, routingFixture())
	if _, ok := offSvc.escalation(task(fleet.StatusFailed, "x", "m-smart"), auto("smart", "m-smart")); ok {
		t.Error("disabled switch must not escalate")
	}
}

func TestChooseScoutBackend(t *testing.T) {
	r := routingFixture()
	auto := routingSvc(FleetConfig{ScoutBackend: "auto", ScoutCodexTiers: []string{"smart"}}, r)

	if got := auto.chooseScoutBackend("smart", false); got.Backend != fleetBackendCodex {
		t.Errorf("auto smart without budget: %+v", got)
	}
	if got := auto.chooseScoutBackend("smart", true); got.Backend != fleetBackendClaude || got.Reason != fleetReasonUSDBudget {
		t.Errorf("a USD budget must keep Codex away: %+v", got)
	}
	if got := auto.chooseScoutBackend("deep", false); got.Backend != fleetBackendClaude {
		t.Errorf("tier not allowed for Codex: %+v", got)
	}
	noCodexModel := routingSvc(FleetConfig{ScoutBackend: "auto", ScoutCodexTiers: []string{"fast"}}, r)
	if got := noCodexModel.chooseScoutBackend("fast", false); got.Backend != fleetBackendClaude {
		t.Errorf("no Codex model for the tier: %+v", got)
	}
	if got := routingSvc(FleetConfig{ScoutBackend: "codex"}, r).chooseScoutBackend("smart", true); got.Backend != fleetBackendCodex {
		t.Errorf("explicit codex is left alone: %+v", got)
	}
	if got := routingSvc(FleetConfig{}, r).chooseScoutBackend("smart", false); got.Backend != fleetBackendClaude {
		t.Errorf("default is claude: %+v", got)
	}
}

func newRoutingEnv(t *testing.T, cfg FleetConfig) (*FleetService, fleet.Project) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "alice.db")
	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	prev := globalStorage
	globalStorage = storage
	t.Cleanup(func() { globalStorage = prev; storage.Close() })
	svc, err := NewFleetService(cfg, storage.db, dbPath, "default-model")
	if err != nil {
		t.Fatal(err)
	}
	svc.Runner.Binary = filepath.Join(t.TempDir(), "no-such-claude")
	svc.Routing = routingFixture()
	t.Cleanup(svc.Scheduler.Wait)
	project, _, err := svc.EnsureProject(context.Background(), fleetActor(42), newFleetTestRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	return svc, project
}

func TestDispatchRecordsChoice(t *testing.T) {
	ctx := context.Background()
	svc, project := newRoutingEnv(t, FleetConfig{Enabled: true})

	task, err := fleetCommanderDispatch(ctx, svc, fleetActor(42), fleet.DispatchRequest{ProjectID: project.ID, Goal: "refactor the storage layer"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Model != "m-deep" {
		t.Fatalf("model = %q", task.Model)
	}
	c := fleetLastModelChoice(ctx, svc.Store, task.ID)
	if c == nil || c.Tier != "deep" || c.Model != "m-deep" || c.Reason != fleetReasonComplexity || c.Detail == "" {
		t.Fatalf("recorded choice = %+v", c)
	}
	pt := fleetTaskProtoAnnotated(ctx, svc, task)
	if pt.ModelSource != fleetModelSourceCommander || pt.ModelTier != "deep" || pt.ModelReason != fleetReasonComplexity {
		t.Fatalf("wire form = %+v", pt)
	}
}

func TestDispatchManualModelWins(t *testing.T) {
	ctx := context.Background()
	svc, project := newRoutingEnv(t, FleetConfig{Enabled: true})
	task, err := fleetCommanderDispatch(ctx, svc, fleetActor(42), fleet.DispatchRequest{ProjectID: project.ID, Goal: "refactor the storage layer", Model: "hand-picked"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Model != "hand-picked" {
		t.Fatalf("model = %q", task.Model)
	}
	if c := fleetLastModelChoice(ctx, svc.Store, task.ID); c != nil {
		t.Fatalf("nothing should be recorded for a manual model, got %+v", c)
	}
	if pt := fleetTaskProtoAnnotated(ctx, svc, task); pt.ModelSource != fleetModelSourceManual {
		t.Fatalf("source = %q", pt.ModelSource)
	}
}

func TestDispatchDisabledKeepsDefault(t *testing.T) {
	ctx := context.Background()
	off := false
	svc, project := newRoutingEnv(t, FleetConfig{Enabled: true, AutoModel: &off})
	task, err := fleetCommanderDispatch(ctx, svc, fleetActor(42), fleet.DispatchRequest{ProjectID: project.ID, Goal: "refactor the storage layer"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Model != "" {
		t.Fatalf("model = %q, want empty (fleet default)", task.Model)
	}
	if c := fleetLastModelChoice(ctx, svc.Store, task.ID); c != nil {
		t.Fatalf("unexpected choice %+v", c)
	}
}

func TestScoutExecutionRouteRecordsBackend(t *testing.T) {
	ctx := context.Background()
	svc, project := newRoutingEnv(t, FleetConfig{Enabled: true, ScoutBackend: "auto", ScoutCodexTiers: []string{"smart"}})
	scout := func(budget float64) fleetScoutBackendChoice {
		task, err := fleetCommanderDispatch(ctx, svc, fleetActor(42), fleet.DispatchRequest{
			ProjectID: project.ID, Goal: "where is the token validated?", Shape: fleet.ShapeScout, Budget: fleet.Limit{CostUSD: budget},
		})
		if err != nil {
			t.Fatal(err)
		}
		if svc.Runner.ScoutRoute == nil {
			t.Fatal("runtime Scout router missing")
		}
		codex, model := svc.Runner.ScoutRoute(ctx, task, budget > 0)
		if codex != (budget == 0) || model == "" {
			t.Fatalf("runtime Scout route = %v %q, budget = %g", codex, model, budget)
		}
		ev, ok, err := svc.Store.LastEvent(ctx, task.ID, fleetEventScoutBackend)
		if err != nil || !ok {
			t.Fatalf("no backend event: %v", err)
		}
		var c fleetScoutBackendChoice
		if err := jsonUnmarshal(ev.Payload, &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := scout(0); c.Backend != fleetBackendCodex {
		t.Errorf("no budget: %+v", c)
	}
	if c := scout(3); c.Backend != fleetBackendClaude || c.Reason != fleetReasonUSDBudget {
		t.Errorf("with a USD budget: %+v", c)
	}
}

func TestRetryPersistsEscalatedModelAndEvent(t *testing.T) {
	ctx := context.Background()
	svc, project := newRoutingEnv(t, FleetConfig{Enabled: true})
	task, err := fleetCommanderDispatch(ctx, svc, fleetActor(42), fleet.DispatchRequest{ProjectID: project.ID, Goal: "add a feature to the parser please"})
	if err != nil {
		t.Fatal(err)
	}
	svc.Scheduler.Wait()
	if _, err := svc.Store.Transition(ctx, task.ID, fleet.StatusRunning, "test"); err == nil {
		if _, err := svc.Store.Transition(ctx, task.ID, fleet.StatusFailed, "agent error: x"); err != nil {
			t.Fatal(err)
		}
	}
	if cur, _ := svc.Store.GetTask(ctx, task.ID); cur.Status != fleet.StatusFailed && cur.Status != fleet.StatusNeedsDecision {
		t.Fatalf("task is %s", cur.Status)
	}
	choice := fleetLastModelChoice(ctx, svc.Store, task.ID)
	_, want, ok := svc.Routing.nextTier(fleetTier(choice.Tier))
	if !ok {
		t.Fatal("fixture must support escalation")
	}
	retried, err := fleetCommanderRetry(ctx, svc, fleetActor(42), task.ID)
	if err != nil || retried.Status != fleet.StatusQueued || retried.Model != want {
		t.Fatalf("retry = %+v, err = %v, want queued with %q", retried, err, want)
	}
	if got := fleetLastModelChoice(ctx, svc.Store, task.ID); got == nil || got.Model != want || got.Reason != fleetReasonEscalated {
		t.Fatalf("escalation event = %+v", got)
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func fleetTaskProtoAnnotated(ctx context.Context, svc *FleetService, task fleet.Task) *alicev1.FleetTask {
	out := fleetTaskProto(task, "p")
	fleetAnnotateModel(ctx, svc, out, task)
	return out
}

func TestFleetCustomNoEscalateReasonsPreserveDefaults(t *testing.T) {
	svc := FleetService{Config: FleetConfig{NoEscalateReasons: []string{"maintenance"}}}
	got := svc.noEscalateReasons()
	for _, want := range append([]string{"maintenance"}, defaultNoEscalateReasons...) {
		found := false
		for _, reason := range got {
			if reason == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing exclusion %q", want)
		}
	}
}
