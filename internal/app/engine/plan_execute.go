package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"claude-tg-agent/internal/app/hermes"
	"claude-tg-agent/internal/app/issueops"
)

// PlanExecuteConfig holds the runtime configuration for a Hermes-style
// plan-execute run backed by DirectEngine for each sub-task.
type ReviewMode string

const (
	ReviewModePerTask    ReviewMode = "per_task"
	ReviewModePerSubTask ReviewMode = "per_subtask"
)

type PlanExecuteConfig struct {
	PlannerModel string
	ProjectDir   string
	ChatID       int64
	ThreadID     int

	MaxPlannerJSONRetries int
	Budget                hermes.TokenBudget
	PlannerRules          string
	ExecutorRules         string
	PlannerSessionID      string

	GithubIssueNumber int
	GithubCfg         hermes.GithubCfg

	PostCompletionHook func(ctx context.Context)

	ReviewPhase       ReviewPhase
	ReviewStore       ReviewResultStore
	DisableReview     bool
	ReviewMinSubTasks int
	ReviewMode        ReviewMode
	StrictMode        StrictModeConfig
	TaskRetry         TaskRetryConfig
	OnReview          func(ctx context.Context, state hermes.TaskState, review ReviewResult, notification ReviewNotification)
	// OnReviewSkipped fires when the reviewer phase produced an unusable
	// result (timeout, parse error, verdict/score contradiction, etc) and
	// the engine elected to skip storage and the OnReview notification.
	// Wire this so the user still sees that review was attempted but
	// could not conclude — silent skips look like the review never ran.
	OnReviewSkipped func(ctx context.Context, state hermes.TaskState, reason error)
	// OnTaskRetry fires after a review on attempt N triggers a re-plan.
	// attempt is the zero-based index of the just-completed attempt; the
	// retry that follows is attempt+1. maxRetries comes from TaskRetry
	// config. Use this to tell the user the engine is restarting with
	// re-plan, since OnReview is suppressed for non-final attempts.
	OnTaskRetry func(ctx context.Context, attempt, maxRetries int, review ReviewResult)

	// OnRuntimeEvent receives normalized runtime events emitted by the engine.
	// The app layer can persist these as trace/span records without making the
	// engine depend on storage packages.
	OnRuntimeEvent func(ctx context.Context, event Event)

	ContinueCh      chan struct{}
	ContinueTimeout time.Duration

	// WalkingAgentEnabled keeps the same Claude session across consecutive
	// Executor sub-tasks of one task (when they share a model). When true:
	//   - hermesExecutorRunner skips ClearSessionForModel between sub-tasks
	//   - executeSubTask emits a slim prompt for round 2+ instead of the full
	//     rules+goal+accumulated+subtask block
	//   - WalkingAgentMaxContextTokens guards against context-window overflow
	// See issue #149 + docs/arch/hermes-walking-agent.md.
	WalkingAgentEnabled bool

	// WalkingAgentMaxContextTokens is the threshold (cumulative input tokens
	// observed for the walking session) above which the engine forces a fresh
	// session to avoid hitting Claude's 200K context window. 0 = 120000.
	WalkingAgentMaxContextTokens int

	// ExecutorModel and HeavyExecutorModel mirror the runner's model selection
	// so the engine can predict which model the next sub-task will run on
	// without consulting the runner. Used by walking-agent mode to decide
	// whether the next sub-task can reuse the prior session (same model) or
	// must start fresh (model boundary). Optional: when both are empty, all
	// walking continuations downgrade to cold prompts. Issue #149.
	ExecutorModel      string
	HeavyExecutorModel string

	// OnSubTaskFailurePause is invoked when a sub-task ends with !success and
	// gives the operator a chance to retry, skip, abort, or retry-with-hint
	// before the engine advances. Return FailurePauseChoice{Decision: FailureSkip}
	// to keep the legacy silent-advance behaviour; nil callback also means
	// silent skip.
	OnSubTaskFailurePause func(ctx context.Context, idx, total int, subTask hermes.SubTask, errText string, kind hermes.FailureKind) FailurePauseChoice

	// OnGraphInterrupt is invoked by the async graph path when the Walker halts
	// on a durable Interrupt. The callback should notify the operator; resume is
	// driven later by ApplyInterruptResolution + ResumeViaGraph.
	OnGraphInterrupt func(ctx context.Context, state hermes.TaskState, interrupt hermes.HermesInterrupt)

	// OnPlanningError is invoked when the graph path's planner returns a
	// typed planner error (ErrPlannerJSONFailed / ErrPlannerEmptyPlan /
	// ErrPlannerChecklistViolation). Receivers should show an actionable
	// menu — generic OnError still fires for fallback logging.
	// When nil, only OnError is called.
	OnPlanningError func(ctx context.Context, taskID string, err error)

	OnDone func(ctx context.Context, state hermes.TaskState)
}

// FailureDecision is the operator's choice when a sub-task fails and the
// pause hook fires.
type FailureDecision int

const (
	// FailureSkip marks the sub-task failed and advances to the next one.
	// This is the legacy behaviour and the default when no callback is wired.
	FailureSkip FailureDecision = iota
	// FailureRetry re-runs the same sub-task. Attempts counter is preserved
	// so the executor still sees this is not its first try.
	FailureRetry
	// FailureAbort stops the entire task with TaskStatusFailed.
	FailureAbort
)

// FailurePauseChoice is the operator's response to a failure pause. Hint is
// non-empty only when the operator picked the "✏️ 修正方向" path; the engine
// prepends it to the next executor prompt as a Markdown "OperatorHint" block.
type FailurePauseChoice struct {
	Decision FailureDecision
	Hint     string
}

// label returns a stable string label for FailureDecision used in runtime
// events. Mirrors the const names so dashboards can filter on them.
func (d FailureDecision) label() string {
	switch d {
	case FailureRetry:
		return "retry"
	case FailureAbort:
		return "abort"
	case FailureSkip:
		return "skip"
	default:
		return "unknown"
	}
}

// buildFailurePauseInterrupt assembles a HermesInterrupt record for a
// sub-task failure pause. The Payload field carries the data a future
// startup-resume path needs to re-issue the Telegram button without
// re-running the failed sub-task.
func buildFailurePauseInterrupt(taskID string, idx, total int, subTask hermes.SubTask, errText string, kind hermes.FailureKind, now time.Time) hermes.HermesInterrupt {
	expires := now.Add(24 * time.Hour)
	return hermes.HermesInterrupt{
		ID:         fmt.Sprintf("%s:subfail:%d:%d", taskID, idx, now.UnixNano()),
		SourceStep: hermes.RuntimeStepExecutor,
		ResumeStep: hermes.RuntimeStepExecutor,
		Reason:     "subtask_failure_pause",
		CreatedAt:  now,
		ExpiresAt:  &expires,
		Payload: map[string]any{
			"sub_task_idx":  idx,
			"sub_task_id":   subTask.ID,
			"sub_task_desc": subTask.Description,
			"total":         total,
			"error_text":    errText,
			"failure_kind":  kind.Label(),
		},
	}
}

// PlanExecuteEngine plans a goal and executes each sub-task through DirectEngine.
type PlanExecuteEngine struct {
	cfg      PlanExecuteConfig
	planner  *hermes.PlannerSession
	direct   *DirectEngine
	store    hermes.TaskStateStore
	runtime  hermes.RuntimeStepStore
	reporter hermes.ProgressReporter
	issueOps issueops.IssueOps

	mu          sync.Mutex
	taskID      string
	cancelFn    context.CancelFunc
	interrupted bool

	// Walking-agent state (issue #149). All scoped to one task; reset on task
	// start. Only used when cfg.WalkingAgentEnabled.
	walkingExecutorModel string // model used by previous executor sub-task this task
	walkingTokensSeen    int    // cumulative input tokens (uncached + cache_read + cache_write) since last fresh session
}

func (e *PlanExecuteEngine) issueOpsService() issueops.IssueOps {
	if e != nil && e.issueOps != nil {
		return e.issueOps
	}
	return issueops.New()
}

// runtimeStepStore returns the snapshot-aware runtime step store for the
// given task store. After #169 slice 3b every built-in store satisfies
// RuntimeStepStore (SQLiteTaskStore, MemoryTaskStore, NoopTaskStore); this
// function only falls back to a wrapper when an exotic test stub is used.
// The wrapper applies reducer updates to a synthetic state so callers can
// keep treating runtime as non-nil without conditional branches.
func runtimeStepStore(store hermes.TaskStateStore) hermes.RuntimeStepStore {
	if runtime, ok := store.(hermes.RuntimeStepStore); ok {
		return runtime
	}
	return syntheticRuntimeStepStore{}
}

// syntheticRuntimeStepStore satisfies RuntimeStepStore for legacy task
// stores that do not implement snapshot persistence. It validates updates
// through the reducer and returns a transient snapshot so plan_execute
// callers do not have to special-case nil. Nothing is persisted.
type syntheticRuntimeStepStore struct{}

func (syntheticRuntimeStepStore) CommitRuntimeStep(commit hermes.RuntimeCommit) (hermes.Snapshot, error) {
	if commit.CreatedAt.IsZero() {
		commit.CreatedAt = time.Now()
	}
	state, err := hermes.ApplyStateUpdates(hermes.HermesState{TaskID: commit.TaskID}, commit.Updates)
	if err != nil {
		return hermes.Snapshot{}, err
	}
	return hermes.Snapshot{
		TaskID:     commit.TaskID,
		State:      state,
		NextStep:   commit.NextStep,
		SourceNode: commit.SourceNode,
		Metadata:   commit.Metadata,
		CreatedAt:  commit.CreatedAt,
	}, nil
}

func NewPlanExecuteEngine(
	cfg PlanExecuteConfig,
	planFn hermes.CallPlanFunc,
	direct *DirectEngine,
	store hermes.TaskStateStore,
	reporter hermes.ProgressReporter,
) *PlanExecuteEngine {
	if store == nil {
		store = &hermes.NoopTaskStore{}
	}
	if reporter == nil {
		reporter = &hermes.NoopProgressReporter{}
	}
	planner := hermes.NewPlannerSession(planFn, cfg.MaxPlannerJSONRetries, cfg.PlannerRules)
	engine := &PlanExecuteEngine{
		cfg:      cfg,
		planner:  planner,
		direct:   direct,
		store:    store,
		runtime:  runtimeStepStore(store),
		reporter: reporter,
		issueOps: issueops.New(),
	}
	planner.SetRecoveryDecider(func(req hermes.PlannerRecoveryRequest) hermes.PlannerRecoveryDecision {
		recoveryReq := RecoveryRequest{
			Mode:        req.Mode,
			Attempt:     req.Attempt,
			MaxAttempts: req.MaxAttempts,
		}
		decision := DecideRecovery(recoveryReq)
		LogRecoveryDecision(recoveryReq, decision)
		engine.emitRuntimeEvent(context.Background(), RecoveryTraceEvent(recoveryReq, decision, time.Now()))
		return hermes.PlannerRecoveryDecision{
			Retry:       decision.Action == RecoveryActionRetry,
			Reason:      decision.Reason,
			NextAttempt: decision.NextAttempt,
		}
	})
	planner.SetPlanQualityGateReporter(func(gate hermes.PlanQualityGateEvent) {
		engine.emitRuntimeEvent(context.Background(), Event{
			Type:      "PlanQualityGate",
			Timestamp: time.Now(),
			Payload: map[string]any{
				"action":       gate.Action,
				"reason":       gate.Reason,
				"attempt":      gate.Attempt,
				"max_attempts": gate.MaxAttempts,
				"task_count":   gate.TaskCount,
				"violation":    gate.Violation,
			},
		})
	})
	return engine
}

func (e *PlanExecuteEngine) Name() string {
	return "plan_execute"
}

func (e *PlanExecuteEngine) TaskID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.taskID
}

func (e *PlanExecuteEngine) emitRuntimeEvent(ctx context.Context, event Event) {
	if e.cfg.OnRuntimeEvent != nil {
		if event.ChatID == 0 {
			event.ChatID = e.cfg.ChatID
		}
		if event.ThreadID == 0 {
			event.ThreadID = e.cfg.ThreadID
		}
		if event.Issue == 0 {
			event.Issue = e.cfg.GithubIssueNumber
		}
		if event.TaskID == "" {
			event.TaskID = e.TaskID()
		}
		e.cfg.OnRuntimeEvent(ctx, event)
	}
}

func (e *PlanExecuteEngine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cancelFn != nil
}

func (e *PlanExecuteEngine) InterruptWith(messageID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.interrupted = true
	if e.cancelFn != nil {
		e.cancelFn()
	}
	if e.taskID != "" {
		e.commitInterruptedBoundary(e.taskID, messageID)
	}
}

func (e *PlanExecuteEngine) Start(ctx context.Context, goal string, cc *ChatContext) (string, error) {
	budget := e.cfg.Budget
	budget.StartedAt = time.Now()
	task := hermes.TaskState{
		ID:                uuid.New().String(),
		ChatID:            e.cfg.ChatID,
		ThreadID:          e.cfg.ThreadID,
		ProjectDir:        e.cfg.ProjectDir,
		Goal:              goal,
		Status:            hermes.TaskStatusPlanning,
		TokenBudget:       budget,
		PlannerSessionID:  e.cfg.PlannerSessionID,
		GithubIssueNumber: e.cfg.GithubIssueNumber,
	}
	created, err := e.store.CreateTask(task)
	if err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}

	e.mu.Lock()
	e.taskID = created.ID
	runCtx, cancel := context.WithCancel(ctx)
	e.cancelFn = cancel
	e.interrupted = false
	if e.cfg.PlannerSessionID != "" {
		e.planner.SetSessionID(e.cfg.PlannerSessionID)
	}
	e.mu.Unlock()

	go e.run(runCtx, created.ID, goal, cc)
	return created.ID, nil
}

func (e *PlanExecuteEngine) Run(ctx context.Context, goal string, cc *ChatContext, prog ProgressSink) (Result, error) {
	start := time.Now()
	taskID, err := e.Start(ctx, goal, cc)
	if err != nil {
		return Result{Duration: time.Since(start)}, err
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return Result{Duration: time.Since(start)}, ctx.Err()
		}
		if !e.IsRunning() {
			break
		}
		<-ticker.C
	}

	state, err := e.store.GetTask(taskID)
	if err != nil {
		return Result{Duration: time.Since(start)}, err
	}
	result := Result{Text: strings.TrimSpace(state.Accumulated), Duration: time.Since(start)}
	if state.Status == hermes.TaskStatusFailed || state.Status == hermes.TaskStatusInterrupted {
		return result, fmt.Errorf("plan_execute ended with status %s", state.Status)
	}
	if prog != nil {
		prog.OnComplete(result.Text)
	}
	return result, nil
}

func (e *PlanExecuteEngine) RunFromState(ctx context.Context, state hermes.TaskState, cc *ChatContext, prog ProgressSink) (Result, error) {
	start := time.Now()
	decision := DecideTaskResume(state)
	if decision.Terminal {
		return Result{Text: strings.TrimSpace(state.Accumulated), Duration: time.Since(start)}, nil
	}
	if !decision.CanResume {
		return Result{Duration: time.Since(start)}, fmt.Errorf("task %s is not resumable: %s", state.ID, decision.Reason)
	}

	runCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.taskID = state.ID
	e.cancelFn = cancel
	e.interrupted = false
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		e.cancelFn = nil
		e.taskID = ""
		e.mu.Unlock()
	}()

	if decision.Reason == "plan_complete_mark_done" {
		source := hermes.RuntimeStepExecutor
		reason := "resume_plan_complete_mark_done"
		if e.reviewMode() == ReviewModePerTask {
			reviewState, _ := e.store.GetTask(state.ID)
			if reviewState.ID == "" {
				reviewState = state
			}
			if _, reviewErr := e.runReview(runCtx, reviewState, ReviewModePerTask, -1, "", true); reviewErr != nil {
				log.Printf("[plan_execute] resume final review skipped task=%s: %v", state.ID, reviewErr)
			}
			source = hermes.RuntimeStepReviewer
			reason = "resume_plan_complete_after_review"
		}
		_ = e.commitTerminalBoundary(state.ID, source, 0, reason)
		updated, _ := e.store.GetTask(state.ID)
		e.reporter.OnDone(updated)
		if e.cfg.OnDone != nil {
			e.cfg.OnDone(runCtx, updated)
		}
		if prog != nil {
			prog.OnComplete(strings.TrimSpace(updated.Accumulated))
		}
		return Result{Text: strings.TrimSpace(updated.Accumulated), Duration: time.Since(start)}, nil
	}

	tasks := append([]hermes.SubTask(nil), state.Plan...)
	{
		status := hermes.TaskStatusExecuting
		currentIdx := decision.FromIdx
		if _, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
			TaskID:     state.ID,
			Updates:    []hermes.StateUpdate{{Status: &status, CurrentIdx: &currentIdx}},
			NextStep:   hermes.RuntimeStepExecutor,
			SourceNode: hermes.RuntimeStepPlanner,
			Metadata:   hermes.SnapshotMetadata{Source: "plan_execute", Reason: "resume_start"},
		}); err != nil {
			return Result{Duration: time.Since(start)}, err
		}
	}
	e.reporter.OnPlanReady(tasks)
	e.onPlanReady(runCtx, state, tasks)

	completed := len(decision.Preserved)
	reviewMode := e.reviewMode()
	strictCfg := e.strictMode()
	for idx := decision.FromIdx; idx < len(tasks); idx++ {
		if runCtx.Err() != nil {
			return Result{Duration: time.Since(start)}, runCtx.Err()
		}
		e.mu.Lock()
		interrupted := e.interrupted
		e.mu.Unlock()
		if interrupted {
			return Result{Duration: time.Since(start)}, fmt.Errorf("task interrupted")
		}
		latest, err := e.store.GetTask(state.ID)
		if err != nil {
			return Result{Duration: time.Since(start)}, err
		}
		if !e.checkBudget(runCtx, state.ID, latest) {
			current, _ := e.store.GetTask(state.ID)
			return Result{Text: strings.TrimSpace(current.Accumulated), Duration: time.Since(start)}, fmt.Errorf("task budget exceeded")
		}

		subTask := tasks[idx]
		if subTask.Status == hermes.SubTaskDone {
			completed++
			continue
		}
		if subTask.Status == hermes.SubTaskSkipped {
			continue
		}
		finalStatus, finalText, finalTokens, success, _ := e.executeSubTask(runCtx, state.ID, state.Goal, latest, tasks, idx, subTask, cc, reviewMode, strictCfg, "")
		e.applySubTaskOutcome(state.ID, tasks, idx, finalStatus, finalText, finalTokens)
		e.reporter.OnSubTaskDone(idx, len(tasks), subTask, success, finalText)
		e.onSubTaskDone(runCtx, idx, len(tasks), tasks, subTask, finalText, finalTokens, completed)
		var accumulated *string
		if success {
			completed++
			latest, _ = e.store.GetTask(state.ID)
			updated, _ := hermes.AppendResult(latest.Accumulated, finalText, completed)
			accumulated = &updated
		}
		next := hermes.RuntimeStepExecutor
		if idx+1 >= len(tasks) {
			next = hermes.RuntimeStepTerminal
		}
		if err := e.commitExecutorBoundary(state.ID, tasks, idx, accumulated, hermes.TaskStatusExecuting, next, 0, "resume_subtask_done"); err != nil {
			return Result{Duration: time.Since(start)}, err
		}
	}

	if err := e.commitTerminalBoundary(state.ID, hermes.RuntimeStepExecutor, 0, "resume_task_done"); err != nil {
		return Result{Duration: time.Since(start)}, err
	}
	finalState, _ := e.store.GetTask(state.ID)
	e.reporter.OnDone(finalState)
	if e.cfg.OnDone != nil {
		e.cfg.OnDone(runCtx, finalState)
	}
	result := Result{Text: strings.TrimSpace(finalState.Accumulated), Duration: time.Since(start)}
	if prog != nil {
		prog.OnComplete(result.Text)
	}
	return result, nil
}

func (e *PlanExecuteEngine) run(ctx context.Context, taskID, goal string, cc *ChatContext) {
	// Walking-agent state lives across all sub-tasks of one task. Reset on
	// task entry and on task exit so subsequent direct (non-Hermes) work
	// doesn't accidentally inherit the flag. See issue #149.
	if e.cfg.WalkingAgentEnabled {
		e.walkingExecutorModel = ""
		e.walkingTokensSeen = 0
		e.direct.SetWalkingEnabled(true)
	}
	defer func() {
		if e.cfg.WalkingAgentEnabled {
			e.direct.SetWalkingEnabled(false)
		}
		if r := recover(); r != nil {
			log.Printf("[plan_execute] task %s panic: %v", taskID, r)
			e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, 0, "panic_recover")
		} else if state, err := e.store.GetTask(taskID); err == nil && !state.IsTerminal() {
			log.Printf("[plan_execute] task %s exited before terminal status; marking failed (status=%s current_idx=%d)", taskID, state.Status, state.CurrentIdx)
			e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, 0, "non_terminal_exit")
		}
		e.mu.Lock()
		// Cancel runCtx so callers and subprocesses tied to this run are
		// released; previously the cancel func was only nil'd, leaving ctx
		// alive indefinitely.
		if e.cancelFn != nil {
			e.cancelFn()
		}
		e.cancelFn = nil
		e.taskID = ""
		e.mu.Unlock()
	}()

	retryCfg := e.cfg.TaskRetry
	if retryCfg.Enabled {
		retryCfg = retryCfg.WithDefaults()
	}
	maxRetries := 0
	if retryCfg.Enabled {
		maxRetries = retryCfg.MaxTaskRetries
	}

	var (
		prevReview     ReviewResult
		prevPlan       []hermes.SubTask
		lastTasks      []hermes.SubTask
		lastCompleted  int
		hadFinalReview bool
	)

	for attempt := 0; ; attempt++ {
		currentGoal := goal
		var partialRetry partialRetryPlan
		if attempt > 0 {
			partialRetry = buildPartialRetryPlan(prevReview, prevPlan, retryCfg.ScoreThreshold)
			if len(partialRetry.Preserved) > 0 {
				currentGoal = buildPartialReplanGoal(goal, prevReview, partialRetry)
				if err := e.commitAccumulatedBoundary(taskID, partialRetry.Accumulated, attempt, "partial_replan_context"); err != nil {
					e.reporter.OnError(fmt.Errorf("persist partial replan context: %w", err))
					e.commitFailureBoundary(taskID, hermes.RuntimeStepPlanner, attempt, "persist_partial_replan_context_failed")
					return
				}
			} else {
				currentGoal = buildReplanGoal(goal, prevReview, prevPlan)
				// Reset accumulated state so the new plan executes from scratch.
				if err := e.commitAccumulatedBoundary(taskID, "", attempt, "full_replan_reset"); err != nil {
					e.reporter.OnError(fmt.Errorf("persist replan reset: %w", err))
					e.commitFailureBoundary(taskID, hermes.RuntimeStepPlanner, attempt, "persist_replan_reset_failed")
					return
				}
			}
		}

		tasks, planIn, planOut, planCost, plannerSkipped, err := e.plan(ctx, currentGoal)
		if err != nil {
			e.handlePlanningError(ctx, taskID, err)
			return
		}
		if len(partialRetry.Preserved) > 0 {
			tasks = mergePartialRetryPlan(partialRetry.Preserved, tasks, attempt)
		}
		if !plannerSkipped {
			plannerPhase := "planner"
			if attempt > 0 {
				plannerPhase = "retry_planner"
			}
			e.commitTelemetryBoundary(taskID, hermes.RuntimeStepPlanner, attempt,
				hermes.ModelUsage{
					Model:               e.cfg.PlannerModel,
					InputTokens:         planIn,
					UncachedInputTokens: planIn,
					OutputTokens:        planOut,
					CostUSD:             planCost,
				},
				hermes.PhaseUsage{
					Phase:               plannerPhase,
					Model:               e.cfg.PlannerModel,
					InputTokens:         planIn,
					UncachedInputTokens: planIn,
					OutputTokens:        planOut,
					CostUSD:             planCost,
				},
				planIn+planOut,
				"planner_telemetry")
			if sid := e.planner.SessionID(); sid != "" {
				e.commitPlannerSessionBoundary(taskID, sid, attempt)
			}
		}
		if len(tasks) > 15 {
			e.reporter.OnError(fmt.Errorf("complexity violation: plan has %d sub-tasks (max 15)", len(tasks)))
			e.commitFailureBoundary(taskID, hermes.RuntimeStepPlanner, attempt, "complexity_violation_max_subtasks")
			return
		}
		if err := e.commitPlanBoundary(taskID, tasks, attempt); err != nil {
			e.reporter.OnError(fmt.Errorf("persist plan: %w", err))
			e.commitFailureBoundary(taskID, hermes.RuntimeStepPlanner, attempt, "persist_plan_failed")
			return
		}
		e.reporter.OnPlanReady(tasks)
		state, _ := e.store.GetTask(taskID)
		e.onPlanReady(ctx, state, tasks)

		completed := 0
		// pendingOperatorHint carries the "✏️ 修正方向" text the operator
		// supplied at the previous failure pause; it is consumed by the next
		// executeSubTask call and cleared so it never spills into a second
		// sub-task.
		var pendingOperatorHint string
		blockCount := 0
		autoFixedCount := 0
		reviewMode := e.reviewMode()
		strictCfg := e.strictMode()
		for idx := 0; idx < len(tasks); idx++ {
			state, err := e.store.GetTask(taskID)
			if err != nil {
				e.reporter.OnError(err)
				e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "fetch_task_failed")
				return
			}
			if !e.checkBudget(ctx, taskID, state) {
				return
			}
			e.mu.Lock()
			interrupted := e.interrupted
			e.mu.Unlock()
			if interrupted || ctx.Err() != nil {
				return
			}

			subTask := tasks[idx]
			if subTask.Status == hermes.SubTaskDone {
				completed++
				continue
			}
			operatorHint := pendingOperatorHint
			pendingOperatorHint = ""
			finalStatus, finalText, finalTokens, success, subMetrics := e.executeSubTask(ctx, taskID, goal, state, tasks, idx, subTask, cc, reviewMode, strictCfg, operatorHint)
			if subMetrics.blockedOnce {
				blockCount++
			}
			if subMetrics.autoFixed {
				autoFixedCount++
			}
			if finalStatus == hermes.SubTaskDone {
				completed++
				e.applySubTaskOutcome(taskID, tasks, idx, hermes.SubTaskDone, finalText, finalTokens)
				e.reporter.OnSubTaskDone(idx, len(tasks), subTask, true, finalText)
				e.onSubTaskDone(ctx, idx, len(tasks), tasks, subTask, finalText, finalTokens, completed)

				state, _ = e.store.GetTask(taskID)
				updated, _ := hermes.AppendResult(state.Accumulated, finalText, completed)
				next := hermes.RuntimeStepExecutor
				if idx+1 >= len(tasks) {
					if reviewMode == ReviewModePerTask {
						next = hermes.RuntimeStepReviewer
					} else {
						next = hermes.RuntimeStepTerminal
					}
				}
				if err := e.commitExecutorBoundary(taskID, tasks, idx, &updated, hermes.TaskStatusExecuting, next, attempt, "subtask_done"); err != nil {
					e.reporter.OnError(fmt.Errorf("persist subtask result: %w", err))
					e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "persist_subtask_done_failed")
					return
				}
			} else if finalStatus == hermes.SubTaskSkipped {
				if strings.HasPrefix(strings.TrimSpace(finalText), "PARTIAL") {
					if cb := e.cfg.OnSubTaskFailurePause; cb != nil {
						kind := hermes.ClassifyFailure(finalText)
						interrupt := buildFailurePauseInterrupt(taskID, idx, len(tasks), subTask, finalText, kind, time.Now())
						if err := e.commitInterruptBoundary(taskID, interrupt, attempt, "subtask_partial_pause"); err != nil {
							log.Printf("[plan_execute] persist partial pause interrupt: %v", err)
						}
						e.emitRuntimeEvent(ctx, Event{
							Type:      "HumanInterruptCreated",
							Timestamp: time.Now(),
							Payload: map[string]any{
								"interrupt_id": interrupt.ID,
								"reason":       interrupt.Reason,
								"source_step":  string(interrupt.SourceStep),
								"resume_step":  string(interrupt.ResumeStep),
								"sub_task_idx": idx,
								"sub_task_id":  subTask.ID,
								"failure_kind": kind.Label(),
							},
						})

						choice := cb(ctx, idx, len(tasks), subTask, finalText, kind)

						if err := e.commitInterruptCleared(taskID, attempt, "subtask_partial_resumed"); err != nil {
							log.Printf("[plan_execute] persist partial pause clear: %v", err)
						}
						e.emitRuntimeEvent(ctx, Event{
							Type:      "HumanInterruptResumed",
							Timestamp: time.Now(),
							Payload: map[string]any{
								"interrupt_id": interrupt.ID,
								"sub_task_idx": idx,
								"sub_task_id":  subTask.ID,
								"decision":     choice.Decision.label(),
								"has_hint":     strings.TrimSpace(choice.Hint) != "",
							},
						})

						switch choice.Decision {
						case FailureRetry:
							if hint := strings.TrimSpace(choice.Hint); hint != "" {
								log.Printf("[plan_execute] partial pause: retry-with-hint idx=%d task=%s hint=%.60q", idx, taskID, hint)
								pendingOperatorHint = hint
							} else {
								log.Printf("[plan_execute] partial pause: retry idx=%d task=%s", idx, taskID)
							}
							idx-- // re-run same idx; for-loop will idx++
							continue
						case FailureAbort:
							log.Printf("[plan_execute] partial pause: abort task=%s at idx=%d", taskID, idx)
							e.applySubTaskOutcome(taskID, tasks, idx, hermes.SubTaskFailed, finalText, finalTokens)
							e.reporter.OnSubTaskDone(idx, len(tasks), subTask, false, finalText)
							e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "partial_pause_abort")
							return
						}
					}
				}
				e.applySubTaskOutcome(taskID, tasks, idx, hermes.SubTaskSkipped, finalText, finalTokens)
				e.reporter.OnSubTaskDone(idx, len(tasks), subTask, false, finalText)
				e.onSubTaskDone(ctx, idx, len(tasks), tasks, subTask, finalText, finalTokens, completed)

				state, _ = e.store.GetTask(taskID)
				updated, _ := hermes.AppendResult(state.Accumulated, finalText, completed)
				next := hermes.RuntimeStepExecutor
				if idx+1 >= len(tasks) {
					if reviewMode == ReviewModePerTask {
						next = hermes.RuntimeStepReviewer
					} else {
						next = hermes.RuntimeStepTerminal
					}
				}
				if err := e.commitExecutorBoundary(taskID, tasks, idx, &updated, hermes.TaskStatusExecuting, next, attempt, "subtask_skipped"); err != nil {
					e.reporter.OnError(fmt.Errorf("persist skipped subtask: %w", err))
					e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "persist_subtask_skipped_failed")
					return
				}
			} else if !success {
				// Failure pause: ask the operator whether to retry, skip, or
				// abort. Without a callback wired, fall through to the legacy
				// silent-skip behaviour.
				choice := FailurePauseChoice{Decision: FailureSkip}
				if cb := e.cfg.OnSubTaskFailurePause; cb != nil {
					kind := hermes.ClassifyFailure(finalText)
					// Persist the interrupt to the snapshot BEFORE blocking on
					// the operator. This makes paused tasks observable in the
					// dashboard and lets the orphan-cleanup pass identify
					// tasks whose engine goroutine died (e.g. alice restart)
					// while waiting for a click.
					interrupt := buildFailurePauseInterrupt(taskID, idx, len(tasks), subTask, finalText, kind, time.Now())
					if err := e.commitInterruptBoundary(taskID, interrupt, attempt, "subtask_failure_pause"); err != nil {
						log.Printf("[plan_execute] persist failure pause interrupt: %v", err)
					}
					e.emitRuntimeEvent(ctx, Event{
						Type:      "HumanInterruptCreated",
						Timestamp: time.Now(),
						Payload: map[string]any{
							"interrupt_id": interrupt.ID,
							"reason":       interrupt.Reason,
							"source_step":  string(interrupt.SourceStep),
							"resume_step":  string(interrupt.ResumeStep),
							"sub_task_idx": idx,
							"sub_task_id":  subTask.ID,
							"failure_kind": kind.Label(),
						},
					})

					choice = cb(ctx, idx, len(tasks), subTask, finalText, kind)

					if err := e.commitInterruptCleared(taskID, attempt, "subtask_failure_resumed"); err != nil {
						log.Printf("[plan_execute] persist failure pause clear: %v", err)
					}
					e.emitRuntimeEvent(ctx, Event{
						Type:      "HumanInterruptResumed",
						Timestamp: time.Now(),
						Payload: map[string]any{
							"interrupt_id": interrupt.ID,
							"sub_task_idx": idx,
							"sub_task_id":  subTask.ID,
							"decision":     choice.Decision.label(),
							"has_hint":     strings.TrimSpace(choice.Hint) != "",
						},
					})
				}
				switch choice.Decision {
				case FailureRetry:
					if hint := strings.TrimSpace(choice.Hint); hint != "" {
						log.Printf("[plan_execute] failure pause: retry-with-hint idx=%d task=%s hint=%.60q", idx, taskID, hint)
						pendingOperatorHint = hint
					} else {
						log.Printf("[plan_execute] failure pause: retry idx=%d task=%s", idx, taskID)
					}
					idx-- // re-run same idx; for-loop will idx++
					continue
				case FailureAbort:
					log.Printf("[plan_execute] failure pause: abort task=%s at idx=%d", taskID, idx)
					e.applySubTaskOutcome(taskID, tasks, idx, hermes.SubTaskFailed, finalText, finalTokens)
					e.reporter.OnSubTaskDone(idx, len(tasks), subTask, false, finalText)
					e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "failure_pause_abort")
					return
				default: // FailureSkip
					e.applySubTaskOutcome(taskID, tasks, idx, hermes.SubTaskFailed, finalText, finalTokens)
					e.reporter.OnSubTaskDone(idx, len(tasks), subTask, false, finalText)
					e.onSubTaskDone(ctx, idx, len(tasks), tasks, subTask, finalText, finalTokens, completed)
					next := hermes.RuntimeStepExecutor
					if idx+1 >= len(tasks) {
						if reviewMode == ReviewModePerTask {
							next = hermes.RuntimeStepReviewer
						} else {
							next = hermes.RuntimeStepTerminal
						}
					}
					if err := e.commitExecutorBoundary(taskID, tasks, idx, nil, hermes.TaskStatusExecuting, next, attempt, "subtask_failed_skip"); err != nil {
						e.reporter.OnError(fmt.Errorf("persist failed subtask: %w", err))
						e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "persist_subtask_failed_skip_failed")
						return
					}
				}
			}
		}

		lastTasks = tasks
		lastCompleted = completed

		// Per-subtask strict mode handles its own reviews — task-level
		// retry only applies to per-task review mode.
		if reviewMode != ReviewModePerTask {
			if err := e.commitTerminalBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "task_done_without_final_review"); err != nil {
				e.reporter.OnError(fmt.Errorf("persist terminal status: %w", err))
				e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, attempt, "persist_terminal_no_review_failed")
				return
			}
			break
		}

		// Keep the task active while the final review decides whether this
		// attempt is actually complete or must be re-planned. The task stays
		// in TaskStatusExecuting through the validate-and-retry loop — no
		// separate validating status to surface to the user.
		finalState, _ := e.store.GetTask(taskID)

		// Run review with notify=false: the engine itself decides whether
		// to surface the review or trigger a re-plan based on the result.
		// Both OnReview and OnReviewSkipped are fired manually below so
		// that retry attempts stay quiet on the user side.
		review, reviewErr := e.runReview(ctx, finalState, ReviewModePerTask, -1, "", false, blockCount, autoFixedCount)
		recovery := RecoveryDecision{Action: RecoveryActionNone, Reason: "review_error"}
		if reviewErr == nil {
			recoveryReq := RecoveryRequest{
				Mode:      "task_review",
				Attempt:   attempt,
				Review:    review,
				TaskRetry: retryCfg,
			}
			recovery = DecideRecovery(recoveryReq)
			LogRecoveryDecision(recoveryReq, recovery)
			e.emitRuntimeEvent(ctx, RecoveryTraceEvent(recoveryReq, recovery, time.Now()))
		}
		if reviewErr == nil && recovery.Action == RecoveryActionRetry {
			if e.cfg.OnTaskRetry != nil {
				e.cfg.OnTaskRetry(ctx, attempt, maxRetries, review)
			}
			prevReview = review
			prevPlan = tasks
			continue
		}

		if err := e.commitTerminalBoundary(taskID, hermes.RuntimeStepReviewer, attempt, "task_done_after_review"); err != nil {
			e.reporter.OnError(fmt.Errorf("persist terminal status: %w", err))
			e.commitFailureBoundary(taskID, hermes.RuntimeStepReviewer, attempt, "persist_terminal_after_review_failed")
			return
		}
		finalState, _ = e.store.GetTask(taskID)

		// Final attempt — surface whichever outcome the reviewer produced.
		switch {
		case reviewErr != nil:
			if e.cfg.OnReviewSkipped != nil {
				e.cfg.OnReviewSkipped(ctx, finalState, reviewErr)
			}
		case review.Verdict != "" && e.cfg.OnReview != nil:
			e.cfg.OnReview(ctx, finalState, review, BuildReviewNotification(finalState.ID, review))
		}
		hadFinalReview = true
		break
	}

	_ = hadFinalReview // currently informational; reserved for future telemetry

	finalState, _ := e.store.GetTask(taskID)
	e.reporter.OnDone(finalState)
	if e.cfg.OnDone != nil {
		e.cfg.OnDone(ctx, finalState)
	}
	e.onDone(ctx, finalState, lastCompleted, len(lastTasks))
	if e.cfg.PostCompletionHook != nil {
		go func() {
			hookCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			e.cfg.PostCompletionHook(hookCtx)
		}()
	}
}

func (e *PlanExecuteEngine) plan(ctx context.Context, goal string) ([]hermes.SubTask, int, int, float64, bool, error) {
	if hermes.ClassifyGoal(goal) == hermes.GoalSimple {
		return []hermes.SubTask{{
			ID:          "s1",
			Description: "Execute the goal directly: " + goal,
			Status:      hermes.SubTaskPending,
		}}, 0, 0, 0, true, nil
	}
	tasks, inT, outT, costUSD, err := e.planner.Plan(ctx, goal, e.cfg.ProjectDir)
	return tasks, inT, outT, costUSD, false, err
}

func (e *PlanExecuteEngine) commitPlanBoundary(taskID string, tasks []hermes.SubTask, attempt int) error {
	status := hermes.TaskStatusExecuting
	currentIdx := 0
	reason := "plan_ready"
	if attempt > 0 {
		reason = "replan_ready"
	}
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID: taskID,
		Updates: []hermes.StateUpdate{
			{Plan: tasks, Status: &status, CurrentIdx: &currentIdx},
		},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepPlanner,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	return err
}

func (e *PlanExecuteEngine) commitAccumulatedBoundary(taskID string, accumulated string, attempt int, reason string) error {
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID: taskID,
		Updates: []hermes.StateUpdate{
			{Accumulated: &accumulated},
		},
		NextStep:   hermes.RuntimeStepPlanner,
		SourceNode: hermes.RuntimeStepReviewer,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	return err
}

func (e *PlanExecuteEngine) commitExecutorBoundary(taskID string, tasks []hermes.SubTask, idx int, accumulated *string, status hermes.TaskStatus, next hermes.RuntimeStep, attempt int, reason string) error {
	nextIdx := idx + 1
	// Only write Status when it represents an actual transition. All
	// intermediate sub-task commits pass TaskStatusExecuting which is the
	// steady state — emitting it on every commit would inflate the
	// dashboard's status-transition timeline. Terminal transitions use
	// commitTerminalBoundary / commitFailureBoundary instead.
	updates := []hermes.StateUpdate{{Plan: tasks, CurrentIdx: &nextIdx}}
	if status != hermes.TaskStatusExecuting {
		s := status
		updates[0].Status = &s
	}
	if idx >= 0 && idx < len(tasks) {
		updates = append(updates, hermes.StateUpdateForSubTaskResult(tasks[idx], idx))
	}
	if accumulated != nil {
		updates = append(updates, hermes.StateUpdate{Accumulated: accumulated})
	}
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    updates,
		NextStep:   next,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	return err
}

// commitTelemetryBoundary persists token / cost telemetry through the
// reducer so the snapshot history records who paid for what at each
// phase. modelUsage and phaseUsage are passed by value (zero-valued
// fields are skipped). tokenDelta is added to TokenBudget.UsedTokens.
//
// When no snapshot store is wired, falls back to the legacy direct-write
// path (AddModelUsageBreakdown / AddPhaseUsageBreakdown / AddTokenUsage)
// so test stubs and NoopTaskStore continue to work.
func (e *PlanExecuteEngine) commitTelemetryBoundary(taskID string, source hermes.RuntimeStep, attempt int, modelUsage hermes.ModelUsage, phaseUsage hermes.PhaseUsage, tokenDelta int, reason string) {
	hasModel := modelUsage.Model != ""
	hasPhase := phaseUsage.Phase != "" && phaseUsage.Model != ""
	if !hasModel && !hasPhase && tokenDelta == 0 {
		return
	}
	update := hermes.StateUpdate{TokenUsageDelta: tokenDelta}
	if hasModel {
		update.ModelUsages = []hermes.ModelUsage{modelUsage}
	}
	if hasPhase {
		update.PhaseUsages = []hermes.PhaseUsage{phaseUsage}
	}
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{update},
		NextStep:   source, // telemetry doesn't advance the workflow
		SourceNode: source,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	if err != nil {
		log.Printf("[plan_execute] commitTelemetryBoundary task=%s reason=%s: %v", taskID, reason, err)
	}
}

// commitInterruptedBoundary records a user-initiated interrupt at a snapshot
// boundary. Status moves to TaskStatusInterrupted with the originating
// Telegram message ID captured in the Interrupt payload so dashboards can
// trace who pulled the brake.
func (e *PlanExecuteEngine) commitInterruptedBoundary(taskID string, messageID int64) {
	now := time.Now()
	status := hermes.TaskStatusInterrupted
	interrupt := hermes.HermesInterrupt{
		ID:         fmt.Sprintf("%s:user-interrupt:%d", taskID, now.UnixNano()),
		MessageID:  messageID,
		SourceStep: hermes.RuntimeStepExecutor,
		Reason:     "user_interrupt",
		CreatedAt:  now,
	}
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{Status: &status, Interrupt: &interrupt}},
		NextStep:   hermes.RuntimeStepTerminal,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata: hermes.SnapshotMetadata{
			Source: "plan_execute",
			Reason: "user_interrupt",
		},
	})
	if err != nil {
		log.Printf("[plan_execute] commitInterruptedBoundary task=%s: %v — falling back to legacy MarkInterrupted", taskID, err)
		_ = e.store.MarkInterrupted(taskID, messageID)
	}
}

// commitPlannerSessionBoundary persists a Planner backend session ID so the
// next Plan call after restart can use --resume. Snapshot only; no status
// change. Falls back to legacy when no runtime store is wired.
func (e *PlanExecuteEngine) commitPlannerSessionBoundary(taskID, sessionID string, attempt int) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	sid := sessionID
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{PlannerSessionID: &sid}},
		NextStep:   hermes.RuntimeStepPlanner,
		SourceNode: hermes.RuntimeStepPlanner,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  "planner_session_persisted",
			Attempt: attempt,
		},
	})
	if err != nil {
		log.Printf("[plan_execute] commitPlannerSessionBoundary task=%s: %v", taskID, err)
		_ = e.store.UpdatePlannerSession(taskID, sessionID)
	}
}

// commitBudgetResetBoundary writes a fresh TokenBudget.StartedAt at a
// snapshot boundary; used when the operator clicks "continue" on a budget
// warning and the engine restarts the measurement window.
func (e *PlanExecuteEngine) commitBudgetResetBoundary(taskID string, startedAt time.Time) {
	ts := startedAt
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{BudgetStartedAt: &ts}},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata: hermes.SnapshotMetadata{
			Source: "plan_execute",
			Reason: "budget_continue_reset",
		},
	})
	if err != nil {
		log.Printf("[plan_execute] commitBudgetResetBoundary task=%s: %v", taskID, err)
		_ = e.store.ResetBudgetStartedAt(taskID, startedAt)
	}
}

// commitSubTaskStartBoundary marks a sub-task as in-progress through a Plan
// update. The reducer treats Plan as a full replacement so we copy the
// existing plan with only the chosen index mutated. Falls back to legacy
// MarkSubTaskStarted when no runtime store is wired.
func (e *PlanExecuteEngine) commitSubTaskStartBoundary(taskID string, idx int) {
	current, err := e.store.GetTask(taskID)
	if err != nil {
		log.Printf("[plan_execute] commitSubTaskStartBoundary task=%s GetTask: %v", taskID, err)
		_ = e.store.MarkSubTaskStarted(taskID, idx)
		return
	}
	if idx < 0 || idx >= len(current.Plan) {
		return
	}
	plan := append([]hermes.SubTask(nil), current.Plan...)
	plan[idx].Status = hermes.SubTaskInProgress
	currentIdx := idx
	_, err = e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{Plan: plan, CurrentIdx: &currentIdx}},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata: hermes.SnapshotMetadata{
			Source: "plan_execute",
			Reason: "subtask_started",
		},
	})
	if err != nil {
		log.Printf("[plan_execute] commitSubTaskStartBoundary task=%s idx=%d: %v", taskID, idx, err)
		_ = e.store.MarkSubTaskStarted(taskID, idx)
	}
}

// commitFailureBoundary marks the task as failed at a snapshot boundary so
// the failure carries provenance (source step + reason) into the runtime
// log instead of being a silent legacy-table write. Falls back to the
// legacy MarkStatus path when no snapshot store is wired (NoopTaskStore /
// in-memory test harnesses); this keeps existing callsites correct while
// production runs benefit from full snapshot coverage. Errors during
// commit are logged and a legacy MarkStatus is attempted as last resort
// so the task at least leaves the executing state.
func (e *PlanExecuteEngine) commitFailureBoundary(taskID string, source hermes.RuntimeStep, attempt int, reason string) {
	status := hermes.TaskStatusFailed
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{Status: &status}},
		NextStep:   hermes.RuntimeStepTerminal,
		SourceNode: source,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	if err != nil {
		log.Printf("[plan_execute] commitFailureBoundary task=%s reason=%s: %v — falling back to legacy MarkStatus", taskID, reason, err)
		_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
	}
}

// commitInterruptBoundary persists a HermesInterrupt to the snapshot when a
// human-in-the-loop pause fires. The task status stays in Executing — the
// interrupt is observable state, not a status change. Callers are expected
// to emit a corresponding HumanInterruptCreated runtime event.
func (e *PlanExecuteEngine) commitInterruptBoundary(taskID string, interrupt hermes.HermesInterrupt, attempt int, reason string) error {
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{Interrupt: &interrupt}},
		NextStep:   hermes.RuntimeStepApproval,
		SourceNode: hermes.RuntimeStepExecutor,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	return err
}

// commitInterruptCleared persists the resolution of a human-in-the-loop pause.
// Mirrors commitInterruptBoundary; emit HumanInterruptResumed at the call site.
func (e *PlanExecuteEngine) commitInterruptCleared(taskID string, attempt int, reason string) error {
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{ClearInterrupt: true}},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepApproval,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	return err
}

func (e *PlanExecuteEngine) commitTerminalBoundary(taskID string, source hermes.RuntimeStep, attempt int, reason string) error {
	status := hermes.TaskStatusDone
	_, err := e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{{Status: &status}},
		NextStep:   hermes.RuntimeStepTerminal,
		SourceNode: source,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  reason,
			Attempt: attempt,
		},
	})
	return err
}

func (e *PlanExecuteEngine) applySubTaskOutcome(taskID string, tasks []hermes.SubTask, idx int, status hermes.SubTaskStatus, result string, tokens int) {
	if idx < 0 || idx >= len(tasks) {
		return
	}
	attempts := tasks[idx].Attempts
	tokensUsed := tasks[idx].TokensUsed
	if latest, err := e.store.GetTask(taskID); err == nil && idx < len(latest.Plan) {
		attempts = latest.Plan[idx].Attempts
		tokensUsed = latest.Plan[idx].TokensUsed
	}
	tasks[idx].Status = status
	tasks[idx].Result = result
	tasks[idx].TokensUsed = tokensUsed + tokens
	tasks[idx].Attempts = attempts + 1
}

func (e *PlanExecuteEngine) handlePlanningError(ctx context.Context, taskID string, err error) {
	var jfail *hermes.ErrPlannerJSONFailed
	var empty *hermes.ErrPlannerEmptyPlan
	switch {
	case errors.As(err, &empty):
		e.reporter.OnError(fmt.Errorf("Planner 判定無事可做（連續回空計畫）— 目標可能已完成或無法拆解。請檢查目前程式碼/Issue 狀態，必要時手動關閉 Issue 或調整目標描述。"))
	case errors.As(err, &jfail):
		e.reporter.OnError(fmt.Errorf("Planner JSON 解析失敗，降級回一般模式：%v", err))
	default:
		e.reporter.OnError(err)
	}
	e.commitFailureBoundary(taskID, hermes.RuntimeStepPlanner, 0, "planning_error")
}

func (e *PlanExecuteEngine) checkBudget(ctx context.Context, taskID string, state hermes.TaskState) bool {
	if !state.TokenBudget.Exceeded() {
		return true
	}
	e.reporter.OnBudgetWarning(state.TokenBudget)
	ch := e.cfg.ContinueCh
	if ch == nil {
		e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, 0, "budget_exceeded_no_resume")
		e.onBudgetExceeded(ctx, state)
		return false
	}
	timeout := e.cfg.ContinueTimeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	select {
	case <-ch:
		startedAt := time.Now()
		e.mu.Lock()
		e.cfg.Budget.StartedAt = startedAt
		e.mu.Unlock()
		e.commitBudgetResetBoundary(taskID, startedAt)
		return true
	case <-time.After(timeout):
		e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, 0, "budget_exceeded_continue_timeout")
		e.onBudgetExceeded(ctx, state)
		return false
	case <-ctx.Done():
		return false
	}
}

func (e *PlanExecuteEngine) onPlanReady(ctx context.Context, state hermes.TaskState, tasks []hermes.SubTask) {
	issueNum := e.cfg.GithubIssueNumber
	ghCfg := e.cfg.GithubCfg
	if issueNum <= 0 || !ghCfg.Enabled {
		return
	}
	ops := e.issueOpsService()
	if err := ops.PlanIssue(ctx, issueops.PlanIssueRequest{
		ProjectDir:    e.cfg.ProjectDir,
		IssueNumber:   issueNum,
		PlannerModel:  e.cfg.PlannerModel,
		ExecutorModel: "direct",
		Goal:          state.Goal,
		Tasks:         tasks,
		CommentStart:  ghCfg.ShouldComment("start"),
		WritePlan:     ghCfg.SyncChecklist,
	}); err != nil {
		log.Printf("[plan_execute] GitHub plan issue: %v", err)
	}
}

func (e *PlanExecuteEngine) onSubTaskDone(ctx context.Context, idx, total int, tasks []hermes.SubTask, subTask hermes.SubTask, result string, tokens, completed int) {
	issueNum := e.cfg.GithubIssueNumber
	ghCfg := e.cfg.GithubCfg
	if issueNum <= 0 || !ghCfg.Enabled {
		return
	}
	ops := e.issueOpsService()
	var (
		mappingResult        *issueops.ChecklistMappingResult
		currentMapping       *issueops.ChecklistMapping
		requireHumanDecision bool
	)
	if ghCfg.ShouldComment("complete") || ghCfg.SyncChecklist {
		loadedMapping, err := ops.LoadIssueChecklistMapping(ctx, e.cfg.ProjectDir, issueNum, tasks)
		if err != nil {
			requireHumanDecision = ghCfg.SyncChecklist
			log.Printf("[plan_execute] GitHub load checklist mapping idx=%d: %v", idx, err)
		} else {
			mappingResult = &loadedMapping
			currentMapping = findChecklistMappingForSubTask(loadedMapping, idx, subTask)
		}
	}
	if err := ops.RecordEvidence(ctx, issueops.RecordEvidenceRequest{
		ProjectDir:       e.cfg.ProjectDir,
		IssueNumber:      issueNum,
		Index:            idx,
		Total:            total,
		SubTask:          subTask,
		Result:           result,
		Tokens:           tokens,
		Completed:        completed,
		ChecklistMapping: currentMapping,
		Validation:       buildValidationEvidence(subTask, result),
		Comment:          ghCfg.ShouldComment("complete"),
	}); err != nil {
		log.Printf("[plan_execute] GitHub record evidence idx=%d: %v", idx, err)
	}
	if ghCfg.SyncChecklist {
		syncResult, err := ops.SyncChecklist(ctx, issueops.SyncChecklistRequest{
			ProjectDir:           e.cfg.ProjectDir,
			IssueNumber:          issueNum,
			SubTasks:             tasks,
			ChecklistMapping:     mappingResult,
			RequireHumanDecision: requireHumanDecision,
		})
		e.recordChecklistSyncOutcome(ctx, issueNum, idx, len(tasks), syncResult, err)
	}
}

func findChecklistMappingForSubTask(result issueops.ChecklistMappingResult, idx int, subTask hermes.SubTask) *issueops.ChecklistMapping {
	for i := range result.Mappings {
		mapping := &result.Mappings[i]
		switch {
		case strings.TrimSpace(subTask.ID) != "" && mapping.SubTaskID == strings.TrimSpace(subTask.ID):
			return mapping
		case mapping.SubTaskIndex == idx:
			return mapping
		case strings.TrimSpace(mapping.SubTaskDescription) != "" && strings.TrimSpace(mapping.SubTaskDescription) == strings.TrimSpace(subTask.Description):
			return mapping
		}
	}
	return nil
}

func buildValidationEvidence(subTask hermes.SubTask, result string) *issueops.ValidationEvidence {
	command, output, passed, ok := extractValidationCommand(result)
	if !ok {
		return nil
	}
	exitCode := 1
	if passed {
		exitCode = 0
	}
	reference := "validation:result"
	if id := strings.TrimSpace(subTask.ID); id != "" {
		reference = "subtask:" + id + "#validation"
	}
	return &issueops.ValidationEvidence{
		Command:   command,
		Passed:    passed,
		ExitCode:  exitCode,
		Output:    output,
		Reference: reference,
		SubTaskID: strings.TrimSpace(subTask.ID),
	}
}

func extractValidationCommand(result string) (command, output string, passed, ok bool) {
	for _, line := range strings.Split(result, "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "-* \t"))
		if trimmed == "" {
			continue
		}
		candidate := extractCommandCandidate(trimmed)
		if candidate == "" || !looksLikeValidationCommand(candidate) {
			continue
		}
		return candidate, trimmed, inferValidationPassed(trimmed), true
	}
	return "", "", false, false
}

func extractCommandCandidate(line string) string {
	if first := strings.Index(line, "`"); first >= 0 {
		if second := strings.Index(line[first+1:], "`"); second >= 0 {
			return strings.TrimSpace(line[first+1 : first+1+second])
		}
	}
	return strings.TrimSpace(line)
}

func looksLikeValidationCommand(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"go test", "npm test", "pnpm test", "yarn test", "pytest", "cargo test",
		"bundle exec rspec", "make test", "make lint", "golangci-lint", "typecheck",
		"lint", "build", "validate", "verification", "verify",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func inferValidationPassed(line string) bool {
	lower := strings.ToLower(line)
	for _, fail := range []string{" fail", "failed", "失敗", "error", "exit 1", "exit=1"} {
		if strings.Contains(lower, fail) {
			return false
		}
	}
	for _, pass := range []string{" pass", "passed", "ok", "通過", "綠燈", "success"} {
		if strings.Contains(lower, pass) {
			return true
		}
	}
	return false
}

// recordChecklistSyncOutcome always logs and emits a runtime FSM event for
// every SyncChecklist attempt — including silent guard rejections — so that
// missed checklist updates surface in observability instead of being swallowed.
func (e *PlanExecuteEngine) recordChecklistSyncOutcome(ctx context.Context, issueNum, idx, total int, res issueops.SyncChecklistResult, callErr error) {
	outcome := res.Outcome
	if outcome == issueops.SyncOutcomeUnknown && callErr != nil {
		outcome = issueops.SyncOutcomeFailed
	}
	reason := strings.TrimSpace(res.Reason)
	if reason == "" && callErr != nil {
		reason = callErr.Error()
	}
	log.Printf("[plan_execute] checklist sync issue #%d idx=%d outcome=%s wrote=%t would_write=%t needs_human=%t reason=%s",
		issueNum, idx, outcome, res.Wrote, res.WouldWrite, res.Guard.NeedsHumanConfirmation, reason)

	event, to, ok := mapChecklistSyncEvent(outcome, res)
	if !ok {
		return
	}
	from := res.Guard.IssueState
	if res.Issue != nil && from == "" {
		from = hermes.IssueStateFromGitHub(res.Issue.State)
	}
	retryAction := ""
	if res.Recovery != nil {
		retryAction = res.Recovery.RetryAction
	}
	e.emitRuntimeEvent(ctx, IssueFSMTransitionEvent(issueNum, time.Now(), IssueFSMTransitionPayload{
		From:                   from,
		Event:                  event,
		To:                     to,
		Reason:                 reason,
		Source:                 "engine.sync_checklist",
		ChecklistTotal:         total,
		RetryAction:            retryAction,
		ChecklistSynced:        res.Wrote,
		HasBodyChange:          res.Guard.HasBodyChange,
		HasBlockingLabel:       res.Guard.HasBlockingLabel,
		WouldWrite:             res.WouldWrite,
		Wrote:                  res.Wrote,
		DryRun:                 res.DryRun,
		NeedsHumanConfirmation: res.Guard.NeedsHumanConfirmation,
	}))
}

// mapChecklistSyncEvent translates a sync outcome into the FSM event/target
// pair to emit. Returns ok=false when the outcome is purely informational and
// no transition should be recorded (e.g. issue already closed/blocked, or
// body already in sync).
func mapChecklistSyncEvent(outcome issueops.SyncOutcome, res issueops.SyncChecklistResult) (hermes.IssueEvent, hermes.IssueState, bool) {
	switch outcome {
	case issueops.SyncOutcomeWrote:
		return hermes.IssueEventChecklistSynced, hermes.IssueStateChecklistSynced, true
	case issueops.SyncOutcomeDryRun:
		return hermes.IssueEventChecklistSyncRequested, hermes.IssueStateChecklistUnsynced, true
	case issueops.SyncOutcomeNoMatch:
		return hermes.IssueEventChecklistMismatchDetected, hermes.IssueStateChecklistUnsynced, true
	case issueops.SyncOutcomeNeedsHuman:
		return hermes.IssueEventHumanDecisionRequired, hermes.IssueStateBlocked, true
	case issueops.SyncOutcomeFailed, issueops.SyncOutcomeLoadFailed:
		return hermes.IssueEventSyncFailed, hermes.IssueStateBlocked, true
	case issueops.SyncOutcomeIssueState:
		if res.State == hermes.IssueStateBlocked || res.Guard.HasBlockingLabel {
			return hermes.IssueEventHumanDecisionRequired, hermes.IssueStateBlocked, true
		}
		return "", "", false
	case issueops.SyncOutcomeNoChange, issueops.SyncOutcomeUnknown:
		return "", "", false
	default:
		return "", "", false
	}
}

// computeChecklistDeclarationDrift returns the IDs of checklist items that
// were declared by at least one done sub-task yet remain unchecked in the
// current issue body. A non-empty result indicates the SyncChecklist patch
// failed to land for some declared items (issue #168 validation guard).
func computeChecklistDeclarationDrift(plan []hermes.SubTask, items []hermes.ChecklistItem) []string {
	declared := make(map[string]bool)
	for _, st := range plan {
		if st.Status != hermes.SubTaskDone {
			continue
		}
		for _, id := range st.ChecklistItemIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				declared[id] = true
			}
		}
	}
	if len(declared) == 0 {
		return nil
	}
	var drifted []string
	for _, item := range items {
		if !declared[item.ID] {
			continue
		}
		if !item.Checked {
			drifted = append(drifted, item.ID)
		}
	}
	if len(drifted) == 0 {
		return nil
	}
	sort.Strings(drifted)
	return drifted
}

func (e *PlanExecuteEngine) onDone(ctx context.Context, finalState hermes.TaskState, completed, total int) {
	issueNum := e.cfg.GithubIssueNumber
	ghCfg := e.cfg.GithubCfg
	if issueNum <= 0 || !ghCfg.Enabled {
		return
	}
	var notes []string
	ops := e.issueOpsService()
	readiness, err := ops.AssessCloseReadiness(ctx, issueops.AssessCloseReadinessRequest{
		ProjectDir:         e.cfg.ProjectDir,
		IssueNumber:        issueNum,
		RequiredCloseLabel: ghCfg.AutoCloseLabel,
		ReviewAccepted:     true,
		ValidationPassed:   true,
	})
	if err != nil {
		log.Printf("[plan_execute] GitHub post-run reconciliation skipped issue #%d: assess readiness: %v", issueNum, err)
		if ghCfg.AutoCloseLabel != "" {
			notes = append(notes, fmt.Sprintf("Issue was not auto-closed because Alice could not re-fetch the issue to verify the `%s` label.", ghCfg.AutoCloseLabel))
		}
	} else {
		notes = append(notes, readiness.Notes...)
		if readiness.Issue != nil {
			e.emitRuntimeEvent(ctx, IssueFSMTransitionEvent(issueNum, time.Now(), IssueFSMTransitionPayload{
				From:                   hermes.IssueStateFromGitHub(readiness.Issue.State),
				Event:                  hermes.IssueEventForState(readiness.State),
				To:                     readiness.State,
				Reason:                 strings.TrimSpace(strings.Join(readiness.Notes, "\n")),
				Source:                 "engine.on_done",
				ChecklistTotal:         readiness.Reconciliation.ChecklistTotal,
				CheckedCount:           readiness.Reconciliation.CheckedCount,
				UncheckedCount:         len(readiness.Reconciliation.Unchecked),
				HasBlockingLabel:       readiness.Guard.HasBlockingLabel,
				HasRequiredLabel:       readiness.HasRequiredLabel,
				ReviewAccepted:         readiness.Guard.ReviewAccepted,
				ValidationPassed:       readiness.Guard.ValidationPassed,
				ChecklistSynced:        readiness.Guard.ChecklistSynced,
				CanAutoClose:           readiness.CanAutoClose,
				NeedsHumanConfirmation: readiness.State == hermes.IssueStateBlocked,
			}))
		}
		if readiness.Reconciliation.HasUnchecked() {
			log.Printf("[plan_execute] GitHub issue #%d still has %d unchecked checklist items after Hermes done", issueNum, len(readiness.Reconciliation.Unchecked))
		}
		if readiness.Issue != nil {
			if drifted := computeChecklistDeclarationDrift(finalState.Plan, readiness.Issue.Checklist); len(drifted) > 0 {
				log.Printf("[plan_execute] checklist declaration drift issue #%d: %d declared item(s) still unchecked: %v", issueNum, len(drifted), drifted)
				notes = append(notes, fmt.Sprintf("Checklist declaration drift: %d sub-task-declared item(s) remain unchecked: %s. Hermes will not auto-tick — please verify and tick manually.", len(drifted), strings.Join(drifted, ", ")))
				e.emitRuntimeEvent(ctx, Event{
					Type:      "ChecklistDeclarationDrift",
					Timestamp: time.Now(),
					Issue:     issueNum,
					Payload: map[string]any{
						"drifted_item_ids": drifted,
						"source":           "engine.on_done",
					},
				})
			}
		}
		if completed == total && ghCfg.AutoCloseLabel != "" {
			if readiness.CanAutoClose {
				closeResult, closeErr := ops.CloseIssue(ctx, issueops.CloseIssueRequest{
					AssessCloseReadinessRequest: issueops.AssessCloseReadinessRequest{
						ProjectDir:         e.cfg.ProjectDir,
						IssueNumber:        issueNum,
						RequiredCloseLabel: ghCfg.AutoCloseLabel,
						ReviewAccepted:     true,
						ValidationPassed:   true,
					},
				})
				if closeErr != nil {
					log.Printf("[plan_execute] GitHub close issue: %v", closeErr)
					if closeResult.Recovery != nil {
						notes = append(notes, fmt.Sprintf("Issue close blocked: %s", closeResult.Recovery.Error))
					}
					if closeResult.Recovery != nil {
						e.emitRuntimeEvent(ctx, IssueFSMTransitionEvent(issueNum, time.Now(), IssueFSMTransitionPayload{
							From:             readiness.State,
							Event:            hermes.IssueEventSyncFailed,
							To:               hermes.IssueStateBlocked,
							Reason:           closeResult.Recovery.Error,
							Source:           "engine.close_issue",
							CanAutoClose:     readiness.CanAutoClose,
							HasRequiredLabel: readiness.HasRequiredLabel,
							ChecklistSynced:  readiness.Guard.ChecklistSynced,
							ReviewAccepted:   readiness.Guard.ReviewAccepted,
							ValidationPassed: readiness.Guard.ValidationPassed,
						}))
					}
				}
				if closeResult.Closed {
					notes = append(notes, fmt.Sprintf("Issue #%d auto-closed by IssueOps.", issueNum))
					if closeResult.Issue != nil {
						e.emitRuntimeEvent(ctx, IssueFSMTransitionEvent(issueNum, time.Now(), IssueFSMTransitionPayload{
							From:             readiness.State,
							Event:            hermes.IssueEventIssueClosed,
							To:               hermes.IssueStateClosed,
							Reason:           "auto_close",
							Source:           "engine.on_done.close",
							CanAutoClose:     readiness.CanAutoClose,
							HasRequiredLabel: readiness.HasRequiredLabel,
							ChecklistSynced:  readiness.Guard.ChecklistSynced,
							ReviewAccepted:   readiness.Guard.ReviewAccepted,
							ValidationPassed: readiness.Guard.ValidationPassed,
						}))
					}
				}
			} else if readiness.Reconciliation.HasUnchecked() {
				notes = append(notes, fmt.Sprintf("Issue was not auto-closed because %d checklist item(s) remain unchecked.", len(readiness.Reconciliation.Unchecked)))
				log.Printf("[plan_execute] GitHub auto-close skipped issue #%d: unchecked checklist remains", issueNum)
			} else if !readiness.HasRequiredLabel && ghCfg.AutoCloseLabel != "" {
				notes = append(notes, fmt.Sprintf("Issue was not auto-closed because it does not have the `%s` label.", ghCfg.AutoCloseLabel))
				log.Printf("[plan_execute] GitHub auto-close skipped issue #%d: missing label %q", issueNum, ghCfg.AutoCloseLabel)
			} else {
				notes = append(notes, "Issue was not auto-closed because close readiness guard did not pass.")
			}
		}
	}
	if ghCfg.ShouldComment("complete") {
		if err := ops.CommentDone(ctx, e.cfg.ProjectDir, issueNum, finalState, strings.Join(notes, "\n\n")); err != nil {
			log.Printf("[plan_execute] GitHub comment done: %v", err)
		}
	}
}

func (e *PlanExecuteEngine) onBudgetExceeded(ctx context.Context, state hermes.TaskState) {
	issueNum := e.cfg.GithubIssueNumber
	ghCfg := e.cfg.GithubCfg
	if issueNum <= 0 || !ghCfg.Enabled {
		return
	}
	ops := e.issueOpsService()
	if ghCfg.ShouldComment("budget_exceeded") {
		if err := ops.CommentBudgetExceeded(ctx, e.cfg.ProjectDir, issueNum, state.TokenBudget.UsedTokens, state.TokenBudget.MaxTotalTokens); err != nil {
			log.Printf("[plan_execute] GitHub comment budget: %v", err)
		}
	}
	if ghCfg.FailureLabel != "" {
		if err := ops.ApplyLabel(ctx, e.cfg.ProjectDir, issueNum, ghCfg.FailureLabel); err != nil {
			log.Printf("[plan_execute] GitHub apply failure label on budget: %v", err)
		}
	}
}

func (e *PlanExecuteEngine) runReview(ctx context.Context, state hermes.TaskState, mode ReviewMode, subTaskIdx int, retryFeedback string, notify bool, blockMetrics ...int) (ReviewResult, error) {
	mode = mode.normalized()
	if mode == "" {
		mode = ReviewModePerTask
	}
	strictCfg := e.strictMode()
	if mode == ReviewModePerSubTask && strictCfg.Enabled {
		// strict per-subtask review can run on small plans as well.
	} else {
		if !e.shouldRunReview(state.Plan) {
			return ReviewResult{}, nil
		}
	}
	if e.cfg.ReviewPhase == nil {
		return ReviewResult{}, nil
	}

	if mode == ReviewModePerSubTask {
		if subTaskIdx < 0 || subTaskIdx >= len(state.Plan) {
			return ReviewResult{}, fmt.Errorf("sub-task index %d out of range for review", subTaskIdx)
		}
	}

	reviewReq := e.buildReviewRequest(state, mode, subTaskIdx, retryFeedback)
	result, err := ReviewPhaseWithTimeout(ctx, e.cfg.ReviewPhase, reviewReq, strictCfg.ReviewTimeout)
	if err != nil {
		// Reviewer failed (timeout, parse error, etc). Do NOT synthesise a fake
		// "pass" verdict on top of the empty/zero-score result — that produced
		// misleading "pass (0/100)" notifications where the verdict claimed
		// success but the score said the opposite. Skip storage but still
		// notify the user via OnReviewSkipped so they know review was
		// attempted but could not conclude.
		log.Printf("[plan_execute] review failed (skipping store): %v", err)
		if notify && e.cfg.OnReviewSkipped != nil {
			e.cfg.OnReviewSkipped(ctx, state, err)
		}
		return ReviewResult{}, err
	}
	if validateErr := result.Validate(); validateErr != nil {
		log.Printf("[plan_execute] review invalid (skipping store): %v", validateErr)
		if notify && e.cfg.OnReviewSkipped != nil {
			e.cfg.OnReviewSkipped(ctx, state, validateErr)
		}
		return ReviewResult{}, validateErr
	}
	if len(blockMetrics) >= 2 {
		result.BlockCount = blockMetrics[0]
		result.AutoFixedCount = blockMetrics[1]
	}
	// Record reviewer's own token + cost usage so the per-model breakdown and
	// total cost include the review pass — previously this was uncounted, so
	// dashboards under-reported by exactly the reviewer's share. See #148 1E.
	if reviewerTokens := result.InputTokens + result.OutputTokens; result.ReviewerModel != "" && reviewerTokens > 0 {
		e.commitTelemetryBoundary(state.ID, hermes.RuntimeStepReviewer, 0,
			hermes.ModelUsage{
				Model:               result.ReviewerModel,
				InputTokens:         result.InputTokens,
				UncachedInputTokens: result.InputTokens,
				OutputTokens:        result.OutputTokens,
				CostUSD:             result.CostUSD,
			},
			hermes.PhaseUsage{
				Phase:               "reviewer",
				Model:               result.ReviewerModel,
				InputTokens:         result.InputTokens,
				UncachedInputTokens: result.InputTokens,
				OutputTokens:        result.OutputTokens,
				CostUSD:             result.CostUSD,
			},
			reviewerTokens,
			"reviewer_telemetry")
	}
	if e.cfg.ReviewStore != nil {
		if err := e.cfg.ReviewStore.StoreReview(ctx, state.ID, result); err != nil {
			log.Printf("[plan_execute] store review: %v", err)
		}
	}
	if notify && e.cfg.OnReview != nil {
		e.cfg.OnReview(ctx, state, result, BuildReviewNotification(state.ID, result))
	}
	return result, nil
}

func (e *PlanExecuteEngine) shouldRunReview(tasks []hermes.SubTask) bool {
	if e.cfg.DisableReview {
		return false
	}
	if e.cfg.ReviewPhase == nil {
		return false
	}
	minTasks := e.cfg.ReviewMinSubTasks
	if minTasks <= 0 {
		minTasks = 2
	}
	return len(tasks) >= minTasks
}

func tokenUsageBreakdownFromResult(result Result) hermes.TokenUsageBreakdown {
	return hermes.TokenUsageBreakdown{
		UncachedInputTokens:      result.InputTokens,
		CacheReadInputTokens:     result.CacheReadInputTokens,
		CacheCreationInputTokens: result.CacheCreationInputTokens,
		OutputTokens:             result.OutputTokens,
		CostUSD:                  result.Cost,
	}
}

func (e *PlanExecuteEngine) reviewMode() ReviewMode {
	if e.cfg.ReviewMode == "" {
		return ReviewModePerTask
	}
	return e.cfg.ReviewMode.normalized()
}

func (e *PlanExecuteEngine) strictMode() StrictModeConfig {
	return e.cfg.StrictMode.WithDefaults()
}

type subTaskExecMetrics struct {
	blockedOnce bool
	autoFixed   bool
}

func (e *PlanExecuteEngine) executeSubTask(ctx context.Context, taskID, goal string, state hermes.TaskState, tasks []hermes.SubTask, idx int, subTask hermes.SubTask, cc *ChatContext, reviewMode ReviewMode, strictCfg StrictModeConfig, operatorHint string) (hermes.SubTaskStatus, string, int, bool, subTaskExecMetrics) {
	attempts := 0
	reviewFeedback := ""
	totalAttempts := strictCfg.MaxRetriesPerSub + 1
	metrics := subTaskExecMetrics{}
	for {
		if attempts == 0 {
			e.commitSubTaskStartBoundary(taskID, idx)
			e.reporter.OnSubTaskStart(idx, len(tasks), subTask)
		} else {
			e.reporter.OnRetry(idx, attempts, totalAttempts, reviewFeedback)
		}

		// Walking-agent: decide whether this sub-task can reuse the prior
		// session (slim prompt) or must start fresh (cold prompt). See issue
		// #149 + docs/arch/hermes-walking-agent.md.
		walkingActive := false
		walkingForceFresh := false
		if e.cfg.WalkingAgentEnabled {
			predictedModel := e.predictExecutorModel(subTask)
			switch {
			case e.walkingExecutorModel == "":
				// First sub-task this task — must seed the session via a cold prompt.
				walkingForceFresh = true
			case predictedModel == "":
				// Engine wasn't given enough info to predict. Downgrade safely.
				walkingForceFresh = true
			case predictedModel != e.walkingExecutorModel:
				// Model boundary; runner will clear the prior session inside Run.
				log.Printf("[hermes.walking] model change predicted prev=%s next=%s — fresh prompt", e.walkingExecutorModel, predictedModel)
				e.walkingExecutorModel = ""
				e.walkingTokensSeen = 0
				walkingForceFresh = true
			case strings.TrimSpace(reviewFeedback) != "":
				// Strict-mode retry — the reviewer's feedback needs to land on a
				// fresh seat for the model to take it seriously, and we want the
				// re-attempt to see goal/accumulated explicitly.
				walkingForceFresh = true
			case e.walkingTokensSeen >= e.walkingMaxContextTokens():
				log.Printf("[hermes.walking] watermark exceeded tokens_seen=%d limit=%d — forcing fresh session", e.walkingTokensSeen, e.walkingMaxContextTokens())
				e.walkingExecutorModel = ""
				e.walkingTokensSeen = 0
				walkingForceFresh = true
			case attempts > 0:
				// Inner retry of the strict loop already handled by reviewFeedback case.
				// Outer retry attempts likewise want a fresh seat.
				walkingForceFresh = true
			default:
				walkingActive = true
			}
		}

		prompt := buildSubTaskGoalVariant(e.cfg.ExecutorRules, goal, state.Accumulated, idx, len(tasks), subTask, reviewFeedback, walkingActive)
		if walkingActive {
			log.Printf("[hermes.walking] reusing session model=%s sub_task=%d/%d tokens_so_far=%d", e.walkingExecutorModel, idx+1, len(tasks), e.walkingTokensSeen)
		}
		if walkingForceFresh {
			e.direct.ForceFreshSession()
		}
		// On the very first attempt of this executeSubTask call, fold in any
		// operator hint the failure-pause flow handed us. The hint is only
		// honoured for the outer retry's first attempt — strict review's
		// inner retry loop has its own reviewFeedback path.
		if attempts == 0 && strings.TrimSpace(operatorHint) != "" {
			prompt = "[Operator hint — apply before continuing]\n" + strings.TrimSpace(operatorHint) + "\n\n" + prompt
		}
		e.direct.BindSubTask(subTask)
		result, execErr := e.direct.Run(ctx, prompt, cc, subTaskSink{})

		// Walking-agent state update.
		//
		// Track the model that actually ran (in case the runner picked one
		// different from our prediction) so the next sub-task's switch can
		// detect a model boundary.
		//
		// Track transcript size only when the prior call was walkingActive.
		// A cold (force-fresh) call's cache_creation reflects this call's
		// internal tool-use chain — that work is NOT carried into the next
		// sub-task because the runner cleared the model session. Counting
		// it would immediately trip the watermark on every sub-task that
		// follows a cold call (see issue #149 follow-up). Reset to 0 after
		// any cold call so the next walking iteration starts measuring
		// fresh.
		//
		// Transcript size is cache_read + cache_creation on this call:
		// cache_read covers the prefix that was already cached (= the
		// transcript at the start of this turn), cache_creation covers what
		// was just added to cache (= this turn's new content). Their sum is
		// approximately what the next walking call's prompt cache will read.
		// Use max() so the watermark only ever reflects the high-water mark.
		if e.cfg.WalkingAgentEnabled && execErr == nil && result.Model != "" {
			e.walkingExecutorModel = result.Model
			if walkingActive {
				transcriptSize := result.CacheReadInputTokens + result.CacheCreationInputTokens
				if transcriptSize == 0 {
					// Runner didn't report cache fields — legacy heuristic.
					transcriptSize = e.walkingTokensSeen + result.InputTokens + result.OutputTokens
				}
				if transcriptSize > e.walkingTokensSeen {
					e.walkingTokensSeen = transcriptSize
				}
			} else {
				// Cold call (first sub-task, model change, retry, watermark
				// reset, etc). Restart the watermark so the upcoming walking
				// iterations get to accumulate from zero.
				e.walkingTokensSeen = 0
			}
		} else if execErr != nil && e.cfg.WalkingAgentEnabled {
			// On error, drop walking state — the next sub-task should start
			// fresh rather than inherit a possibly-broken session.
			e.walkingExecutorModel = ""
			e.walkingTokensSeen = 0
		}
		if execErr != nil {
			if kind := hermes.ClassifyFailure(execErr.Error()); kind == hermes.FailureEnv {
				log.Printf("[plan_execute] env-class failure idx=%d task=%s: %v", idx, taskID, execErr)
			}
			tokensUsed := result.TokenVolume()
			if result.Model != "" && tokensUsed > 0 {
				usage := tokenUsageBreakdownFromResult(result)
				e.commitTelemetryBoundary(taskID, hermes.RuntimeStepExecutor, 0,
					hermes.ModelUsage{
						Model:                    result.Model,
						InputTokens:              usage.InputVolume(),
						UncachedInputTokens:      usage.UncachedInputTokens,
						CacheReadInputTokens:     usage.CacheReadInputTokens,
						CacheCreationInputTokens: usage.CacheCreationInputTokens,
						OutputTokens:             usage.OutputTokens,
						CostUSD:                  usage.CostUSD,
					},
					hermes.PhaseUsage{
						Phase:                    "executor",
						Model:                    result.Model,
						InputTokens:              usage.InputVolume(),
						UncachedInputTokens:      usage.UncachedInputTokens,
						CacheReadInputTokens:     usage.CacheReadInputTokens,
						CacheCreationInputTokens: usage.CacheCreationInputTokens,
						OutputTokens:             usage.OutputTokens,
						CostUSD:                  usage.CostUSD,
					},
					tokensUsed,
					"executor_telemetry_failure")
			}
			return hermes.SubTaskFailed, execErr.Error(), tokensUsed, false, metrics
		}

		text := strings.TrimSpace(result.Text)
		tokensUsed := result.TokenVolume()
		if result.Model != "" && tokensUsed > 0 {
			usage := tokenUsageBreakdownFromResult(result)
			e.commitTelemetryBoundary(taskID, hermes.RuntimeStepExecutor, 0,
				hermes.ModelUsage{
					Model:                    result.Model,
					InputTokens:              usage.InputVolume(),
					UncachedInputTokens:      usage.UncachedInputTokens,
					CacheReadInputTokens:     usage.CacheReadInputTokens,
					CacheCreationInputTokens: usage.CacheCreationInputTokens,
					OutputTokens:             usage.OutputTokens,
					CostUSD:                  usage.CostUSD,
				},
				hermes.PhaseUsage{
					Phase:                    "executor",
					Model:                    result.Model,
					InputTokens:              usage.InputVolume(),
					UncachedInputTokens:      usage.UncachedInputTokens,
					CacheReadInputTokens:     usage.CacheReadInputTokens,
					CacheCreationInputTokens: usage.CacheCreationInputTokens,
					OutputTokens:             usage.OutputTokens,
					CostUSD:                  usage.CostUSD,
				},
				tokensUsed,
				"executor_telemetry_success")
		}

		finalStatus := hermes.SubTaskDone
		finalText := text
		if reviewMode == ReviewModePerSubTask && strictCfg.Enabled {
			latestState, err := e.store.GetTask(taskID)
			if err != nil {
				log.Printf("[plan_execute] GetTask before strict review idx=%d: %v", idx, err)
			} else {
				latestState.Plan = append([]hermes.SubTask(nil), tasks...)
				latestState.Plan[idx].Status = hermes.SubTaskDone
				latestState.Plan[idx].Result = text
				if reviewResult, reviewErr := e.runReview(ctx, latestState, ReviewModePerSubTask, idx, reviewFeedback, false); reviewErr == nil {
					decision := ReviewDecisionFromStrictTags(reviewResult, strictCfg)
					if decision.Verdict == VerdictBlock {
						metrics.blockedOnce = true
						reviewFeedback = buildStrictRetryFeedback(reviewResult, decision)
						recoveryReq := RecoveryRequest{
							Mode:        "strict_review",
							Attempt:     attempts,
							MaxAttempts: strictCfg.MaxRetriesPerSub,
							Strict:      decision,
						}
						recovery := DecideRecovery(recoveryReq)
						LogRecoveryDecision(recoveryReq, recovery)
						e.emitRuntimeEvent(ctx, RecoveryTraceEvent(recoveryReq, recovery, time.Now()))
						if recovery.Action == RecoveryActionRetry {
							attempts = recovery.NextAttempt
							if err := e.commitSubTaskRetryBoundary(taskID, idx, text, tokensUsed, attempts); err != nil {
								log.Printf("[plan_execute] commitSubTaskRetryBoundary idx=%d: %v", idx, err)
							}
							continue
						}
						finalStatus = hermes.SubTaskSkipped
						finalText = annotatePartialResult(text, reviewFeedback)
					}
				}
			}
		}

		if metrics.blockedOnce && finalStatus == hermes.SubTaskDone {
			metrics.autoFixed = true
		}
		return finalStatus, finalText, tokensUsed, finalStatus == hermes.SubTaskDone, metrics
	}
}

func (e *PlanExecuteEngine) commitSubTaskRetryBoundary(taskID string, idx int, result string, tokensUsed int, attempt int) error {
	current, err := e.store.GetTask(taskID)
	if err != nil {
		return err
	}
	if idx < 0 || idx >= len(current.Plan) {
		return fmt.Errorf("sub-task index %d out of range", idx)
	}
	plan := append([]hermes.SubTask(nil), current.Plan...)
	plan[idx].Status = hermes.SubTaskInProgress
	plan[idx].Result = result
	plan[idx].TokensUsed += tokensUsed
	plan[idx].Attempts++
	currentIdx := idx
	_, err = e.runtime.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID: taskID,
		Updates: []hermes.StateUpdate{
			{Plan: plan, CurrentIdx: &currentIdx},
			hermes.StateUpdateForSubTaskResult(plan[idx], idx),
		},
		NextStep:   hermes.RuntimeStepExecutor,
		SourceNode: hermes.RuntimeStepReviewer,
		Metadata: hermes.SnapshotMetadata{
			Source:  "plan_execute",
			Reason:  "strict_review_retry",
			Attempt: attempt,
		},
	})
	return err
}

func (e *PlanExecuteEngine) buildReviewRequest(state hermes.TaskState, mode ReviewMode, subTaskIdx int, retryFeedback string) ReviewRequest {
	reviewReq := ReviewRequest{
		TaskID:      state.ID,
		ProjectDir:  e.cfg.ProjectDir,
		Goal:        state.Goal,
		Accumulated: state.Accumulated,
		Plan:        append([]hermes.SubTask(nil), state.Plan...),
	}

	switch mode.normalized() {
	case ReviewModePerSubTask:
		reviewReq.ReviewScope = "subtask"
		if subTaskIdx >= 0 && subTaskIdx < len(reviewReq.Plan) {
			reviewReq.Plan = []hermes.SubTask{reviewReq.Plan[subTaskIdx]}
			reviewReq.SubTaskResults = []ReviewSubTaskInput{{
				ID:          reviewReq.Plan[0].ID,
				Index:       subTaskIdx,
				Description: reviewReq.Plan[0].Description,
				Status:      string(reviewReq.Plan[0].Status),
				Result:      strings.TrimSpace(reviewReq.Plan[0].Result),
				ToolHints:   append([]string(nil), reviewReq.Plan[0].ToolHints...),
			}}
		}
	default:
		reviewReq.ReviewScope = "task"
		reviewReq.SubTaskResults = ReviewInputsFromPlan(state.Plan)
	}

	if retryFeedback != "" {
		reviewReq.Accumulated = strings.TrimSpace(reviewReq.Accumulated + "\n\nReviewer feedback requiring retry:\n" + retryFeedback)
	}

	if len(state.Artifacts) > 0 {
		reviewReq.Artifacts = make([]Artifact, 0, len(state.Artifacts))
		for _, artifact := range state.Artifacts {
			reviewReq.Artifacts = append(reviewReq.Artifacts, Artifact{
				Path:      artifact.Path,
				Hash:      artifact.Hash,
				SubTaskID: artifact.SubTaskID,
			})
		}
	}
	return reviewReq
}

// defaultWalkingMaxContextTokens is the watermark above which the walking
// session is force-cleared. 120K leaves comfortable headroom inside Claude
// Sonnet 4.5's 200K context window. Operators can override via
// HermesConfig.WalkingAgentMaxContextTokens. Issue #149.
const defaultWalkingMaxContextTokens = 120_000

// predictExecutorModel mirrors hermesExecutorRunner.pickModel so the engine
// can decide ahead of Run() whether the next sub-task will share a model
// (and therefore session) with the previous one. When both ExecutorModel and
// HeavyExecutorModel are empty (walking mode unwired), returns "" — the caller
// must treat this as "cannot predict, downgrade to cold prompt".
func (e *PlanExecuteEngine) predictExecutorModel(st hermes.SubTask) string {
	if e.cfg.ExecutorModel == "" {
		return ""
	}
	if e.cfg.HeavyExecutorModel != "" && e.cfg.HeavyExecutorModel != e.cfg.ExecutorModel && IsHeavySubTask(st) {
		return e.cfg.HeavyExecutorModel
	}
	return e.cfg.ExecutorModel
}

func (e *PlanExecuteEngine) walkingMaxContextTokens() int {
	if e.cfg.WalkingAgentMaxContextTokens > 0 {
		return e.cfg.WalkingAgentMaxContextTokens
	}
	return defaultWalkingMaxContextTokens
}

// buildSubTaskGoal assembles the executor prompt for one sub-task. The full
// (cold) form rebuilds the entire context block; the slim form is used in
// walking-agent mode for round 2+ when the session transcript already carries
// rules/goal/accumulated and only the new sub-task description is needed.
//
// See issue #149 + docs/arch/hermes-walking-agent.md for the slim-prompt
// rationale and the prompt-bloat regression that the cold form was put in to
// prevent.
func buildSubTaskGoal(executorRules, goal, accumulated string, idx, total int, subTask hermes.SubTask, retryFeedback string) string {
	return buildSubTaskGoalVariant(executorRules, goal, accumulated, idx, total, subTask, retryFeedback, false)
}

func buildSubTaskGoalVariant(executorRules, goal, accumulated string, idx, total int, subTask hermes.SubTask, retryFeedback string, walkingContinuation bool) string {
	if walkingContinuation {
		// Slim form: same Claude session is already primed with rules + goal +
		// prior assistant outputs. Only re-inject reviewer feedback (it's the
		// retry-specific instruction the model wouldn't otherwise see).
		var b strings.Builder
		if strings.TrimSpace(retryFeedback) != "" {
			fmt.Fprintf(&b, "Reviewer feedback to address before retrying:\n%s\n\n", strings.TrimSpace(retryFeedback))
		}
		fmt.Fprintf(&b, "Now do sub-task (%d/%d):\n%s", idx+1, total, subTask.Description)
		return b.String()
	}

	var b strings.Builder
	if rules := strings.TrimSpace(executorRules); rules != "" {
		b.WriteString(rules)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Original goal:\n%s\n\n", goal)
	if strings.TrimSpace(accumulated) != "" {
		fmt.Fprintf(&b, "Completed sub-task results so far:\n%s\n\n", accumulated)
	}
	if strings.TrimSpace(retryFeedback) != "" {
		fmt.Fprintf(&b, "Reviewer feedback to address before retrying:\n%s\n\n", strings.TrimSpace(retryFeedback))
	}
	fmt.Fprintf(&b, "Current sub-task (%d/%d):\n%s", idx+1, total, subTask.Description)
	return b.String()
}

func buildStrictRetryFeedback(review ReviewResult, decision StrictReviewDecision) string {
	var b strings.Builder
	b.WriteString("verdict: ")
	b.WriteString(string(decision.Verdict))
	if len(decision.BlockTags) > 0 {
		b.WriteString("\nblock_tags: ")
		parts := make([]string, 0, len(decision.BlockTags))
		for _, tag := range decision.BlockTags {
			parts = append(parts, string(tag))
		}
		b.WriteString(strings.Join(parts, ", "))
	}
	if text := strings.TrimSpace(review.Feedback); text != "" {
		b.WriteString("\nfeedback: ")
		b.WriteString(text)
	}
	return b.String()
}

func annotatePartialResult(result, retryFeedback string) string {
	result = strings.TrimSpace(result)
	retryFeedback = strings.TrimSpace(retryFeedback)
	if retryFeedback == "" {
		return result
	}
	if result == "" {
		return "PARTIAL\n" + retryFeedback
	}
	return "PARTIAL\n" + result + "\n\nReviewer feedback:\n" + retryFeedback
}

func (m ReviewMode) normalized() ReviewMode {
	switch strings.ToLower(strings.TrimSpace(string(m))) {
	case string(ReviewModePerSubTask):
		return ReviewModePerSubTask
	default:
		return ReviewModePerTask
	}
}

type subTaskSink struct{}

func (subTaskSink) OnSubTaskStart(idx, total int, desc string)  {}
func (subTaskSink) OnToolUse(tool string, input map[string]any) {}
func (subTaskSink) OnContent(kind, text string)                 {}
func (subTaskSink) OnSubTaskDone(idx int, result string)        {}
func (subTaskSink) OnComplete(summary string)                   {}
