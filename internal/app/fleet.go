package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/worktree"
)

func fleetCanonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func fleetPathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(fleetCanonicalPath(root), fleetCanonicalPath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// FleetConfig controls the fleet: one agent run per task, each in its own
// git worktree, across every registered project. It is the default task
// backend; set backend to "hermes" to run the old Hermes system instead.
type FleetConfig struct {
	// Backend selects the task system: "fleet" (default when empty) or
	// "hermes".
	Backend string `json:"backend"`
	// Enabled is the older on/off switch. It no longer has any effect:
	// Backend decides, and an empty Backend means fleet.
	//
	// Deprecated: use Backend.
	Enabled bool `json:"enabled"`
	// ScoutBackend selects the agent for read-only tasks. Empty uses Claude.
	ScoutBackend string `json:"scout_backend"`
	// ScoutCodexModel overrides the Codex CLI's default model.
	ScoutCodexModel string `json:"scout_codex_model"`
	// ScoutQuota optionally prefers the eligible backend with more estimated capacity.
	ScoutQuota fleetScoutQuotaConfig `json:"scout_quota"`
	// WorktreeRoot is where task checkouts are created. Defaults to
	// "worktrees" beside the database.
	WorktreeRoot string `json:"worktree_root"`
	// ReportRoot is where scout reports are saved. Defaults to
	// "fleet-reports" beside the database.
	ReportRoot string `json:"report_root"`
	// MaxConcurrent caps active tasks across all projects. Zero uses the
	// fleet's default.
	MaxConcurrent int `json:"max_concurrent"`
	// Zero disables startup recovery. Positive values cap attempts per task.
	StartupResumeLimit int `json:"startup_resume_limit"`
	// DefaultTrustMode is given to newly registered projects: "local-only"
	// (default), "direct-pr" or "no-mistakes".
	DefaultTrustMode string `json:"default_trust_mode"`
	// DailyBudgetUSD pauses the whole fleet for the day once reached. Zero
	// means no limit.
	DailyBudgetUSD float64 `json:"daily_budget_usd"`
	// TaskBudgetUSD caps one task's lifetime spend. Zero means no limit.
	TaskBudgetUSD float64 `json:"task_budget_usd"`
	// VerifyStepTimeoutMinutes caps each verify command of a task. Zero uses
	// defaultFleetVerifyStepTimeout; a busy machine needs more than a quiet
	// one, since a timed-out check counts as a failed one.
	VerifyStepTimeoutMinutes int `json:"verify_step_timeout_minutes"`
	// OwnerUserID is the Telegram user the fleet reports to, and the only
	// one who may approve, merge or wave a task through. Defaults to the
	// first of allowed_user_ids.
	OwnerUserID int64 `json:"owner_user_id"`
	// DashboardURL is the dashboard's address as the owner reaches it, e.g.
	// "http://localhost:3939". When set, task cards link to it.
	DashboardURL string `json:"dashboard_url"`
	// EpicLabels are the GitHub label names that mark an issue as an epic.
	// Defaults to ["epic"] when empty.
	EpicLabels []string `json:"epic_labels"`
	// EpicTitlePattern is a regexp that matches epic issue titles, e.g.
	// `^\[M\d+\] Epic:`. Empty means no title-based detection.
	EpicTitlePattern string `json:"epic_title_pattern"`
	// EpicChildrenHeading titles the sub-issue section appended to epics the
	// dashboard creates. Defaults to "Sub-issues" when empty.
	EpicChildrenHeading string `json:"epic_children_heading"`
	// AutoModel lets the commander choose a model tier for tasks dispatched
	// without one, and escalate auto-chosen models when a task is retried.
	// Nil means on; false restores the single-default-model behaviour. It
	// does nothing unless model_routing.smart_model is set.
	AutoModel *bool `json:"auto_model"`
	// TierLabels maps an issue label (case-insensitive) to a tier: "fast",
	// "smart", "deep" or "flagship". When several labels match, the highest
	// tier wins. Labels not listed here have no effect.
	TierLabels map[string]string `json:"tier_labels"`
	// ScoutBackend is "claude", "codex" or "auto" (the commander decides).
	// Empty means "claude".
	ScoutCodexBinary string `json:"scout_codex_binary"`
	// ScoutCodexTiers lists the tiers whose Scout tasks "auto" may hand to
	// Codex. Empty means auto never picks Codex.
	ScoutCodexTiers []string `json:"scout_codex_tiers"`
	// NoEscalateReasons are lower-case fragments of a task's status reason
	// that mean it stopped for a reason unrelated to model ability (quota,
	// budget, lost process). Custom entries extend the built-in list.
	NoEscalateReasons []string `json:"no_escalate_reasons"`
}

const (
	fleetBackendFleet  = "fleet"
	fleetBackendHermes = "hermes"
)

// defaultFleetVerifyStepTimeout is the cap on one verify command when
// fleet.verify_step_timeout_minutes is not set.
const defaultFleetVerifyStepTimeout = 20 * time.Minute

// verifyStepTimeout returns the cap on one verify command of a task.
func (c FleetConfig) verifyStepTimeout() (time.Duration, error) {
	switch {
	case c.VerifyStepTimeoutMinutes < 0:
		return 0, fmt.Errorf("must not be negative, got %d", c.VerifyStepTimeoutMinutes)
	case c.VerifyStepTimeoutMinutes == 0:
		return defaultFleetVerifyStepTimeout, nil
	}
	return time.Duration(c.VerifyStepTimeoutMinutes) * time.Minute, nil
}

// Active reports whether the fleet is the task backend. Anything but an
// explicit "hermes" selects it.
func (c FleetConfig) Active() bool {
	return !strings.EqualFold(strings.TrimSpace(c.Backend), fleetBackendHermes)
}

// validateBackend rejects a backend value that is neither system.
func (c FleetConfig) validateBackend() error {
	switch strings.ToLower(strings.TrimSpace(c.Backend)) {
	case "", fleetBackendFleet, fleetBackendHermes:
		return nil
	}
	return fmt.Errorf("fleet.backend %q: must be %q or %q", c.Backend, fleetBackendFleet, fleetBackendHermes)
}

// autoModelEnabled reports whether the commander picks models.
func (c FleetConfig) autoModelEnabled() bool { return c.AutoModel == nil || *c.AutoModel }

// trustMode returns the configured default trust mode.
func (c FleetConfig) trustMode() (fleet.TrustMode, error) {
	if strings.TrimSpace(c.DefaultTrustMode) == "" {
		return fleet.TrustLocalOnly, nil
	}
	return fleet.ParseTrustMode(c.DefaultTrustMode)
}

// FleetService is the running fleet and everything needed to command it.
type FleetService struct {
	Config    FleetConfig
	TrustMode fleet.TrustMode
	// Routing supplies the tiered models the commander chooses from.
	Routing ModelRoutingConfig

	Store     *fleet.Store
	Runner    *fleet.Runner
	Scheduler *fleet.Scheduler
	Watcher   *fleet.Watcher
	Commander *fleet.Commander
	Bus       *fleet.Bus
	IssueSync *FleetIssueSync

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// globalFleet is the running fleet, or nil when the fleet is disabled.
var globalFleet *FleetService

// fleetEventType is the WebSocket and runtime-event type prefix for fleet
// events, e.g. "fleet.status".
const fleetEventPrefix = "fleet."

// fleetDatabase returns the database the fleet shares with the rest of
// alice, or nil when persistence is off.
func fleetDatabase() *sql.DB {
	if s, ok := globalStorage.(*SQLiteStorage); ok && s != nil {
		return s.db
	}
	return nil
}

// NewFleetService builds the fleet on db without starting anything.
func NewFleetService(cfg FleetConfig, db *sql.DB, databasePath, defaultModel string) (*FleetService, error) {
	if db == nil {
		return nil, fmt.Errorf("fleet needs persistence: enable_persistence must be on")
	}
	if cfg.StartupResumeLimit < 0 {
		return nil, fmt.Errorf("fleet.startup_resume_limit must be non-negative")
	}
	mode, err := cfg.trustMode()
	if err != nil {
		return nil, fmt.Errorf("fleet.default_trust_mode: %w", err)
	}
	verifyStepTimeout, err := cfg.verifyStepTimeout()
	if err != nil {
		return nil, fmt.Errorf("fleet.verify_step_timeout_minutes: %w", err)
	}
	dataDir := filepath.Dir(databasePath)
	if strings.TrimSpace(cfg.WorktreeRoot) == "" {
		cfg.WorktreeRoot = filepath.Join(dataDir, "worktrees")
	}
	if strings.TrimSpace(cfg.ReportRoot) == "" {
		cfg.ReportRoot = filepath.Join(dataDir, "fleet-reports")
	}
	// Agents run with these as paths in other working directories.
	if cfg.WorktreeRoot, err = filepath.Abs(cfg.WorktreeRoot); err != nil {
		return nil, fmt.Errorf("fleet.worktree_root: %w", err)
	}
	if cfg.ReportRoot, err = filepath.Abs(cfg.ReportRoot); err != nil {
		return nil, fmt.Errorf("fleet.report_root: %w", err)
	}

	store, err := fleet.NewStore(db)
	if err != nil {
		return nil, err
	}
	// Always set, even with no limits configured: a task dispatched with
	// its own budget is only held to it when the runner has a policy.
	budget := &fleet.Budget{
		Daily: fleet.Limit{CostUSD: cfg.DailyBudgetUSD},
		Task:  fleet.Limit{CostUSD: cfg.TaskBudgetUSD},
	}
	runner := &fleet.Runner{
		Store:        store,
		Worktrees:    worktree.Layout{Root: cfg.WorktreeRoot},
		ReportRoot:   cfg.ReportRoot,
		DefaultModel: defaultModel,
		Budget:       budget,
	}
	deliverer := &fleet.Deliverer{Store: store}
	// The tracker still ticks checklist items; what the issue is told, and
	// when it is closed, is FleetIssueSync's.
	issues := &fleet.IssueTracker{Store: store, NoComments: true}
	sched := &fleet.Scheduler{
		Store:              store,
		Runner:             runner,
		Verifier:           &fleet.Verifier{Store: store, ReportRoot: runner.ReportRoot, StepTimeout: verifyStepTimeout},
		Deliverer:          deliverer,
		Issues:             issues,
		MaxConcurrent:      cfg.MaxConcurrent,
		OnRecovery:         logFleetRecovery,
		StartupResumeLimit: cfg.StartupResumeLimit,
	}
	svc := &FleetService{
		Config:    cfg,
		TrustMode: mode,
		Store:     store,
		Runner:    runner,
		Scheduler: sched,
		Watcher:   &fleet.Watcher{Store: store, Runner: runner},
		Commander: &fleet.Commander{Store: store, Scheduler: sched, Deliverer: deliverer, Issues: issues},
		Bus:       &fleet.Bus{Store: store},
		IssueSync: &FleetIssueSync{Store: store, Host: ghFleetIssueHost{}},
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ScoutBackend)) {
	case "", fleetBackendClaude:
	case fleetBackendAuto, fleetBackendCodex:
		runner.CodexScoutBinary = strings.TrimSpace(cfg.ScoutCodexBinary)
		if runner.CodexScoutBinary == "" {
			runner.CodexScoutBinary = "codex"
		}
		runner.CodexScoutModel = cfg.ScoutCodexModel
		if strings.EqualFold(strings.TrimSpace(cfg.ScoutBackend), fleetBackendAuto) {
			runner.ScoutRoute = svc.routeScout
			if configurable, ok := any(runner).(interface {
				ConfigureScoutQuotaFallback(int, func(context.Context, fleet.Task, string) (string, bool))
			}); ok {
				configurable.ConfigureScoutQuotaFallback(cfg.ScoutQuota.MaxTransfers, svc.scoutQuotaFallback)
			}
		}
	default:
		return nil, fmt.Errorf("fleet.scout_backend %q: must be claude, codex or auto", cfg.ScoutBackend)
	}
	return svc, nil
}

// Start runs the scheduler, the watcher and the event forwarder until ctx
// ends or Stop is called.
func (f *FleetService) Start(ctx context.Context) error {
	ctx, f.cancel = context.WithCancel(ctx)

	// Subscribe before the scheduler starts, so the events written while it
	// recovers from the last shutdown are forwarded too.
	events, err := f.Bus.Subscribe(ctx, fleet.EventFilter{Types: fleet.LifecycleEvents})
	if err != nil {
		f.cancel()
		return fmt.Errorf("fleet: subscribe to events: %w", err)
	}
	f.run("fleet.events", func() error {
		for ev := range events {
			forwardFleetEvent(ev)
		}
		return nil
	})
	f.run("fleet.scheduler", func() error { return f.Scheduler.Run(ctx) })
	f.run("fleet.watcher", func() error { return f.Watcher.Run(ctx) })
	f.run("fleet.issue_sync", func() error { return f.IssueSync.Run(ctx, f.Bus) })
	return nil
}

func (f *FleetService) run(name string, fn func() error) {
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		done := globalJobTracker.Start(name)
		err := fn()
		if err != nil && err != context.Canceled {
			log.Printf("[fleet] %s stopped: %v", name, err)
		}
		done(err)
	}()
}

// Stop stops the fleet's agents and waits for its goroutines, so the
// database can be closed afterwards. Tasks keep their sessions and are
// picked up again by recovery at the next start.
func (f *FleetService) Stop() {
	if f == nil || f.cancel == nil {
		return
	}
	f.cancel()
	f.wg.Wait()
}

// forwardFleetEvent hands one fleet event to the WebSocket hub, as a
// "fleet.<type>" message carrying the same JSON the REST endpoints return,
// and to the runtime event log.
func forwardFleetEvent(ev fleet.BusEvent) {
	if globalWebSocketHub != nil {
		globalWebSocketHub.BroadcastEvent(fleetEventPrefix+ev.Type, fleetEventWSData(ev.Event, ev.ProjectID))
	}
	if globalStorage == nil {
		return
	}
	payload := map[string]interface{}{}
	if len(ev.Payload) > 0 {
		_ = json.Unmarshal(ev.Payload, &payload)
	}
	payload["fleet_event_id"] = ev.ID
	if ev.ProjectID != "" {
		payload["project_id"] = ev.ProjectID
	}
	record := RuntimeEventRecord{
		Timestamp: ev.CreatedAt,
		Type:      fleetEventPrefix + ev.Type,
		TaskID:    ev.TaskID,
		Payload:   payload,
	}
	if err := globalStorage.InsertRuntimeEvent(record); err != nil {
		log.Printf("[fleet] record runtime event %s for task %s: %v", ev.Type, ev.TaskID, err)
	}
}

func logFleetRecovery(report fleet.RecoveryReport) {
	if len(report.Recovered)+len(report.Orphans)+len(report.MissingWorktrees)+len(report.Problems) == 0 {
		return
	}
	log.Printf("[fleet] recovery: %d task(s) recovered, %d orphan checkout(s), %d missing checkout(s), %d problem(s)",
		len(report.Recovered), len(report.Orphans), len(report.MissingWorktrees), len(report.Problems))
	for _, p := range report.Problems {
		log.Printf("[fleet] recovery problem: %s", p)
	}
}
