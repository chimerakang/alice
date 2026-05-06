package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
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

func runtimeStepStore(store hermes.TaskStateStore) hermes.RuntimeStepStore {
	runtime, _ := store.(hermes.RuntimeStepStore)
	return runtime
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
		if err := e.store.MarkInterrupted(e.taskID, messageID); err != nil {
			log.Printf("[plan_execute] MarkInterrupted: %v", err)
		}
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
		_ = e.commitTerminalBoundary(state.ID, hermes.RuntimeStepExecutor, 0, "resume_plan_complete_mark_done")
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
	if e.runtime == nil {
		if err := e.store.MarkStatus(state.ID, hermes.TaskStatusExecuting); err != nil {
			return Result{Duration: time.Since(start)}, err
		}
	} else {
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
			_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
		} else if state, err := e.store.GetTask(taskID); err == nil && !state.IsTerminal() {
			log.Printf("[plan_execute] task %s exited before terminal status; marking failed (status=%s current_idx=%d)", taskID, state.Status, state.CurrentIdx)
			_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
					_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
					return
				}
			} else {
				currentGoal = buildReplanGoal(goal, prevReview, prevPlan)
				// Reset accumulated state so the new plan executes from scratch.
				if err := e.commitAccumulatedBoundary(taskID, "", attempt, "full_replan_reset"); err != nil {
					e.reporter.OnError(fmt.Errorf("persist replan reset: %w", err))
					_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
			_ = e.store.AddTokenUsage(taskID, planIn+planOut)
			_ = e.store.AddModelUsageBreakdown(taskID, e.cfg.PlannerModel, hermes.TokenUsageBreakdown{
				UncachedInputTokens: planIn,
				OutputTokens:        planOut,
				CostUSD:             planCost,
			})
			plannerPhase := "planner"
			if attempt > 0 {
				plannerPhase = "retry_planner"
			}
			e.recordPhaseUsage(taskID, plannerPhase, e.cfg.PlannerModel, planIn, planOut, planCost)
			if sid := e.planner.SessionID(); sid != "" {
				_ = e.store.UpdatePlannerSession(taskID, sid)
			}
		}
		if len(tasks) > 15 {
			e.reporter.OnError(fmt.Errorf("complexity violation: plan has %d sub-tasks (max 15)", len(tasks)))
			_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
			return
		}
		if err := e.commitPlanBoundary(taskID, tasks, attempt); err != nil {
			e.reporter.OnError(fmt.Errorf("persist plan: %w", err))
			_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
				_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
					_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
					return
				}
			} else if finalStatus == hermes.SubTaskSkipped {
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
					_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
					return
				}
			} else if !success {
				// Failure pause: ask the operator whether to retry, skip, or
				// abort. Without a callback wired, fall through to the legacy
				// silent-skip behaviour.
				choice := FailurePauseChoice{Decision: FailureSkip}
				if cb := e.cfg.OnSubTaskFailurePause; cb != nil {
					kind := hermes.ClassifyFailure(finalText)
					choice = cb(ctx, idx, len(tasks), subTask, finalText, kind)
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
					_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
						_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
				_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
			_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
	if e.runtime == nil {
		if err := e.store.StorePlan(taskID, tasks); err != nil {
			return err
		}
		return e.store.MarkStatus(taskID, status)
	}
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
	if e.runtime == nil {
		return e.store.UpdateAccumulated(taskID, accumulated)
	}
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
	if e.runtime == nil {
		if idx >= 0 && idx < len(tasks) {
			st := tasks[idx]
			if err := e.store.UpdateSubTask(taskID, idx, st.Status, st.Result, st.TokensUsed); err != nil {
				return err
			}
		}
		if accumulated != nil {
			if err := e.store.UpdateAccumulated(taskID, *accumulated); err != nil {
				return err
			}
		}
		return e.store.AdvanceTask(taskID, nextIdx, status)
	}
	updates := []hermes.StateUpdate{{Plan: tasks, CurrentIdx: &nextIdx, Status: &status}}
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

func (e *PlanExecuteEngine) commitTerminalBoundary(taskID string, source hermes.RuntimeStep, attempt int, reason string) error {
	status := hermes.TaskStatusDone
	if e.runtime == nil {
		return e.store.MarkStatus(taskID, status)
	}
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
	_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
}

func (e *PlanExecuteEngine) checkBudget(ctx context.Context, taskID string, state hermes.TaskState) bool {
	if !state.TokenBudget.Exceeded() {
		return true
	}
	e.reporter.OnBudgetWarning(state.TokenBudget)
	ch := e.cfg.ContinueCh
	if ch == nil {
		_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
		if err := e.store.ResetBudgetStartedAt(taskID, startedAt); err != nil {
			log.Printf("[plan_execute] ResetBudgetStartedAt: %v", err)
		}
		return true
	case <-time.After(timeout):
		_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
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
	if err := ops.RecordEvidence(ctx, issueops.RecordEvidenceRequest{
		ProjectDir:  e.cfg.ProjectDir,
		IssueNumber: issueNum,
		Index:       idx,
		Total:       total,
		SubTask:     subTask,
		Result:      result,
		Tokens:      tokens,
		Completed:   completed,
		Comment:     ghCfg.ShouldComment("complete"),
	}); err != nil {
		log.Printf("[plan_execute] GitHub record evidence idx=%d: %v", idx, err)
	}
	if ghCfg.SyncChecklist {
		syncResult, err := ops.SyncChecklist(ctx, issueops.SyncChecklistRequest{
			ProjectDir:  e.cfg.ProjectDir,
			IssueNumber: issueNum,
			SubTasks:    tasks,
		})
		if err != nil || (syncResult.Recovery != nil && syncResult.Recovery.State == hermes.IssueStateBlocked) {
			if syncResult.Recovery != nil {
				log.Printf("[plan_execute] GitHub sync checklist blocked issue #%d: %s (retry=%s, err=%s)", issueNum, syncResult.Recovery.Message, syncResult.Recovery.RetryAction, syncResult.Recovery.Error)
				e.emitRuntimeEvent(ctx, IssueFSMTransitionEvent(issueNum, time.Now(), IssueFSMTransitionPayload{
					From:                   syncResult.Guard.IssueState,
					Event:                  hermes.IssueEventSyncFailed,
					To:                     hermes.IssueStateBlocked,
					Reason:                 syncResult.Recovery.Error,
					Source:                 "engine.sync_checklist",
					ChecklistTotal:         len(tasks),
					RetryAction:            syncResult.Recovery.RetryAction,
					ChecklistSynced:        false,
					WouldWrite:             syncResult.WouldWrite,
					NeedsHumanConfirmation: syncResult.Guard.NeedsHumanConfirmation,
				}))
			} else {
				log.Printf("[plan_execute] GitHub sync checklist idx=%d: %v", idx, err)
			}
		}
	}
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
		if err := e.store.AddModelUsageBreakdown(state.ID, result.ReviewerModel, hermes.TokenUsageBreakdown{
			UncachedInputTokens: result.InputTokens,
			OutputTokens:        result.OutputTokens,
			CostUSD:             result.CostUSD,
		}); err != nil {
			log.Printf("[plan_execute] AddModelUsage(reviewer) model=%s: %v", result.ReviewerModel, err)
		}
		e.recordPhaseUsage(state.ID, "reviewer", result.ReviewerModel, result.InputTokens, result.OutputTokens, result.CostUSD)
		if err := e.store.AddTokenUsage(state.ID, reviewerTokens); err != nil {
			log.Printf("[plan_execute] AddTokenUsage(reviewer): %v", err)
		}
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

func (e *PlanExecuteEngine) recordPhaseUsage(taskID, phase, model string, inputTokens, outputTokens int, costUSD float64) {
	e.recordPhaseUsageBreakdown(taskID, phase, model, hermes.TokenUsageBreakdown{
		UncachedInputTokens: inputTokens,
		OutputTokens:        outputTokens,
		CostUSD:             costUSD,
	})
}

func (e *PlanExecuteEngine) recordPhaseUsageBreakdown(taskID, phase, model string, usage hermes.TokenUsageBreakdown) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(phase) == "" || strings.TrimSpace(model) == "" {
		return
	}
	if usage.InputVolume()+usage.OutputTokens <= 0 {
		return
	}
	if err := e.store.AddPhaseUsageBreakdown(taskID, phase, model, usage); err != nil {
		log.Printf("[plan_execute] AddPhaseUsage(%s) model=%s: %v", phase, model, err)
	}
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
			if err := e.store.MarkSubTaskStarted(taskID, idx); err != nil {
				log.Printf("[plan_execute] MarkSubTaskStarted idx=%d: %v", idx, err)
			}
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
				if err := e.store.AddModelUsageBreakdown(taskID, result.Model, usage); err != nil {
					log.Printf("[plan_execute] AddModelUsage(executor) idx=%d model=%s: %v", idx, result.Model, err)
				}
				e.recordPhaseUsageBreakdown(taskID, "executor", result.Model, usage)
				if err := e.store.AddTokenUsage(taskID, tokensUsed); err != nil {
					log.Printf("[plan_execute] AddTokenUsage(executor) idx=%d: %v", idx, err)
				}
			}
			return hermes.SubTaskFailed, execErr.Error(), tokensUsed, false, metrics
		}

		text := strings.TrimSpace(result.Text)
		tokensUsed := result.TokenVolume()
		if result.Model != "" && tokensUsed > 0 {
			usage := tokenUsageBreakdownFromResult(result)
			if err := e.store.AddModelUsageBreakdown(taskID, result.Model, usage); err != nil {
				log.Printf("[plan_execute] AddModelUsage(executor) idx=%d model=%s: %v", idx, result.Model, err)
			}
			e.recordPhaseUsageBreakdown(taskID, "executor", result.Model, usage)
			if err := e.store.AddTokenUsage(taskID, tokensUsed); err != nil {
				log.Printf("[plan_execute] AddTokenUsage(executor) idx=%d: %v", idx, err)
			}
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
							if err := e.store.UpdateSubTask(taskID, idx, hermes.SubTaskInProgress, text, tokensUsed); err != nil {
								log.Printf("[plan_execute] UpdateSubTask(retry) idx=%d: %v", idx, err)
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
