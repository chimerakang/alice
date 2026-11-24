package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chimerakang/alice-core/fleet"
	"github.com/chimerakang/alice-core/worktree"
)

// FleetConfig controls the fleet: one agent run per task, each in its own
// git worktree, across every registered project. It is off unless enabled.
type FleetConfig struct {
	Enabled bool `json:"enabled"`
	// WorktreeRoot is where task checkouts are created. Defaults to
	// "worktrees" beside the database.
	WorktreeRoot string `json:"worktree_root"`
	// ReportRoot is where scout reports are saved. Defaults to
	// "fleet-reports" beside the database.
	ReportRoot string `json:"report_root"`
	// MaxConcurrent caps active tasks across all projects. Zero uses the
	// fleet's default.
	MaxConcurrent int `json:"max_concurrent"`
	// DefaultTrustMode is given to newly registered projects: "local-only"
	// (default), "direct-pr" or "no-mistakes".
	DefaultTrustMode string `json:"default_trust_mode"`
	// DailyBudgetUSD pauses the whole fleet for the day once reached. Zero
	// means no limit.
	DailyBudgetUSD float64 `json:"daily_budget_usd"`
	// TaskBudgetUSD caps one task's lifetime spend. Zero means no limit.
	TaskBudgetUSD float64 `json:"task_budget_usd"`
	// OwnerUserID is the Telegram user the fleet reports to, and the only
	// one who may approve, merge or wave a task through. Defaults to the
	// first of allowed_user_ids.
	OwnerUserID int64 `json:"owner_user_id"`
	// DashboardURL is the dashboard's address as the owner reaches it, e.g.
	// "http://localhost:3939". When set, task cards link to it.
	DashboardURL string `json:"dashboard_url"`
}

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

	Store     *fleet.Store
	Runner    *fleet.Runner
	Scheduler *fleet.Scheduler
	Watcher   *fleet.Watcher
	Commander *fleet.Commander
	Bus       *fleet.Bus

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
	mode, err := cfg.trustMode()
	if err != nil {
		return nil, fmt.Errorf("fleet.default_trust_mode: %w", err)
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
	issues := &fleet.IssueTracker{Store: store}
	sched := &fleet.Scheduler{
		Store:         store,
		Runner:        runner,
		Deliverer:     deliverer,
		Issues:        issues,
		MaxConcurrent: cfg.MaxConcurrent,
		OnRecovery:    logFleetRecovery,
	}
	return &FleetService{
		Config:    cfg,
		TrustMode: mode,
		Store:     store,
		Runner:    runner,
		Scheduler: sched,
		Watcher:   &fleet.Watcher{Store: store, Runner: runner},
		Commander: &fleet.Commander{Store: store, Scheduler: sched, Deliverer: deliverer, Issues: issues},
		Bus:       &fleet.Bus{Store: store},
	}, nil
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
