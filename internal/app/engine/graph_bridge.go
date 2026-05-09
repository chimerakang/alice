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

	"claude-tg-agent/internal/app/engine/graph"
	"claude-tg-agent/internal/app/hermes"
)

// graph_bridge.go is the production wiring that connects PlanExecuteEngine's
// existing collaborators (DirectEngine, PlannerSession, ReviewPhase) to the
// graph package's Node interfaces (#169 γ6). It provides:
//
//   - thin adapters that satisfy graph.SubTaskRunner / graph.SubTaskReviewer
//     / graph.TaskReviewer using the engine's existing methods, so the graph
//     nodes can drive the same LLM/store stack as the legacy Run() path
//   - BuildGraphRegistry, which wires all five concrete Nodes
//     (planner / executor / strict_review / reviewer / approval) plus the
//     terminal sentinel into a single graph.Registry
//   - RunViaGraph, an alternate entry point on PlanExecuteEngine that creates
//     the seed snapshot from the existing TaskState and dispatches Walker.Run
//     against the registry
//
// γ6 ships RunViaGraph alongside the legacy Run() so the cut-over is a flag
// flip rather than a rewrite. Run() stays canonical for production today;
// RunViaGraph is exercised by the bridge integration test and is ready for
// gradual migration of call sites.

// BuildGraphRegistry wires the five concrete Hermes nodes for use by the
// Walker. The returned Registry covers planner / executor /
// strict_review / reviewer / approval. Terminal is a sentinel — the
// Walker recognises hermes.RuntimeStepTerminal directly without
// needing a registered Node.
//
// cc is the chat context the executor adapter will hand to DirectEngine on
// each sub-task call. May be nil for trace replays / dry runs that do not
// need conversational state.
func (e *PlanExecuteEngine) BuildGraphRegistry(cc *ChatContext) *graph.Registry {
	registry := graph.NewRegistry()
	strictCfg := e.strictMode()
	reviewMode := e.reviewMode()
	reviewIsPerTask := reviewMode == ReviewModePerTask
	replanCoord := newReplanCoordinator(e)

	registry.Register(&graph.PlannerNode{
		Planner:      e.planner,
		PlannerModel: e.cfg.PlannerModel,
		MaxSubTasks:  15,
	})
	registry.Register(&graph.ExecutorNode{
		Runner:              &executorSubTaskRunner{engine: e, cc: cc},
		ReviewModeIsPerTask: reviewIsPerTask,
		FailurePauseEnabled: e.cfg.OnSubTaskFailurePause != nil,
		StrictReviewEnabled: strictCfg.Enabled && reviewMode == ReviewModePerSubTask,
	})
	registry.Register(&graph.StrictReviewNode{
		Reviewer:            &subTaskReviewerAdapter{engine: e, strictCfg: strictCfg},
		MaxRetriesPerSub:    strictCfg.MaxRetriesPerSub,
		ReviewModeIsPerTask: reviewIsPerTask,
	})
	registry.Register(&graph.ReviewerNode{
		Reviewer: &taskReviewerAdapter{engine: e, replan: replanCoord},
	})
	registry.Register(&graph.ReplanSetupNode{
		Decider: &replanDeciderAdapter{engine: e, replan: replanCoord},
	})
	registry.Register(&graph.ApprovalNode{ReviewModeIsPerTask: reviewIsPerTask})
	return registry
}

// replanCoordinator is the in-process bridge between taskReviewerAdapter
// (which has the just-finished review) and replanDeciderAdapter (which
// needs that review to decide partial vs full retry on the next attempt).
//
// The legacy Run() shared this state via stack-local prevReview / prevPlan
// vars on a single goroutine; the graph path crosses Walker hops, so the
// state lives on a small struct that both adapters reference. Process-local
// only — restart loses replan context, matching Run()'s behaviour.
type replanCoordinator struct {
	engine *PlanExecuteEngine

	mu      sync.Mutex
	perTask map[string]*replanState
}

type replanState struct {
	lastReview  ReviewResult
	lastPlan    []hermes.SubTask
	attemptIdx  int // 0 = initial run completed; 1+ = replan attempt index
	maxAttempts int
}

func newReplanCoordinator(engine *PlanExecuteEngine) *replanCoordinator {
	return &replanCoordinator{
		engine:  engine,
		perTask: make(map[string]*replanState),
	}
}

func (c *replanCoordinator) recordReview(taskID string, review ReviewResult, plan []hermes.SubTask, maxAttempts int) {
	if c == nil || taskID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.perTask[taskID]
	if !ok {
		st = &replanState{}
		c.perTask[taskID] = st
	}
	st.lastReview = review
	st.lastPlan = append([]hermes.SubTask(nil), plan...)
	st.maxAttempts = maxAttempts
}

func (c *replanCoordinator) currentAttempt(taskID string) int {
	if c == nil || taskID == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.perTask[taskID]
	if !ok {
		return 0
	}
	return st.attemptIdx
}

func (c *replanCoordinator) consumeForReplan(taskID string) (ReviewResult, []hermes.SubTask, int) {
	if c == nil || taskID == "" {
		return ReviewResult{}, nil, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.perTask[taskID]
	if !ok {
		return ReviewResult{}, nil, 0
	}
	st.attemptIdx++
	return st.lastReview, append([]hermes.SubTask(nil), st.lastPlan...), st.attemptIdx
}

// RunViaGraphResult is the Result-returning wrapper used by call
// sites migrating off the legacy Run() (#171). It dispatches the
// Walker, then projects the final snapshot into the same Result shape
// Run() returned (Text, Duration, basic telemetry). Callers that
// previously consumed result.Text only — e.g. the direct-chat
// plan/execute path in agent.go — can swap Run() for this without
// further changes.
//
// On terminal failure (state.Status == failed / interrupted) the
// returned error mirrors Run()'s "plan_execute ended with status X"
// shape so recovery code keeps working unchanged.
func (e *PlanExecuteEngine) RunViaGraphResult(ctx context.Context, goal string, cc *ChatContext, prog ProgressSink) (Result, error) {
	start := time.Now()
	final, err := e.RunViaGraph(ctx, goal, cc)
	if err != nil {
		return Result{Duration: time.Since(start)}, err
	}
	text := strings.TrimSpace(final.State.Accumulated)
	result := Result{Text: text, Duration: time.Since(start)}
	if final.State.Status == hermes.TaskStatusFailed || final.State.Status == hermes.TaskStatusInterrupted {
		return result, fmt.Errorf("plan_execute ended with status %s", final.State.Status)
	}
	if prog != nil {
		prog.OnComplete(text)
	}
	return result, nil
}

// RunViaGraph is the alternate Walker-driven entry point. Today its
// production wiring footprint is intentionally smaller than Run():
// RunViaGraph creates / loads the task, seeds an initial snapshot
// pointing at the planner step, then hands control to the Walker.
// Recovery / replan / outer-retry budget tracking — the parts of Run()
// that wrap multiple Walker passes — remain in the legacy path until
// a follow-up cut-over.
//
// Callers wanting to migrate one task at a time can opt into this path
// from a feature flag without touching the rest of plan_execute.
func (e *PlanExecuteEngine) RunViaGraph(ctx context.Context, goal string, cc *ChatContext) (hermes.Snapshot, error) {
	if e == nil {
		return hermes.Snapshot{}, errors.New("engine: nil PlanExecuteEngine")
	}
	taskID, err := e.ensureTaskForGraph(goal)
	if err != nil {
		return hermes.Snapshot{}, err
	}
	defer e.clearGraphRunState(taskID)
	return e.runViaGraphTask(ctx, taskID, cc)
}

// StartViaGraph is the async Class-B companion to RunViaGraph (#171).
// It mirrors Start's task creation + cancellation contract, but dispatches
// the Walker in a goroutine. The method is intentionally feature-flagged at
// call sites until progress-event fan-out fully matches the legacy run().
func (e *PlanExecuteEngine) StartViaGraph(ctx context.Context, goal string, cc *ChatContext) (string, error) {
	if e == nil {
		return "", errors.New("engine: nil PlanExecuteEngine")
	}
	taskID, err := e.createTaskForGraph(goal)
	if err != nil {
		return "", err
	}
	if err := e.seedPlannerSnapshot(taskID); err != nil {
		e.clearGraphRunState(taskID)
		return "", err
	}
	if err := e.startGraphRun(ctx, taskID, cc); err != nil {
		e.clearGraphRunState(taskID)
		return "", err
	}
	return taskID, nil
}

// ResumeViaGraph restarts the async graph Walker for an existing task after an
// external actor has resolved its durable Interrupt. It is the graph-native
// companion to RunFromState for Telegram retry/skip/abort buttons.
func (e *PlanExecuteEngine) ResumeViaGraph(ctx context.Context, taskID string, cc *ChatContext) error {
	if e == nil {
		return errors.New("engine: nil PlanExecuteEngine")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("engine: resume graph task_id is required")
	}
	if _, err := e.store.GetTask(taskID); err != nil {
		return fmt.Errorf("engine: resume graph task %s: %w", taskID, err)
	}
	e.mu.Lock()
	e.taskID = taskID
	e.mu.Unlock()
	if err := e.startGraphRun(ctx, taskID, cc); err != nil {
		e.clearGraphRunState(taskID)
		return err
	}
	return nil
}

func (e *PlanExecuteEngine) startGraphRun(ctx context.Context, taskID string, cc *ChatContext) error {
	runCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.cancelFn = cancel
	e.interrupted = false
	if e.cfg.PlannerSessionID != "" {
		e.planner.SetSessionID(e.cfg.PlannerSessionID)
	}
	e.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			e.clearGraphRunState(taskID)
		}()
		final, runErr := e.runViaGraphTask(runCtx, taskID, cc)
		if runErr != nil {
			logGraphRunError(taskID, runErr)
			if !errors.Is(runErr, graph.ErrInterrupted) && !errors.Is(runErr, context.Canceled) {
				e.commitFailureBoundary(taskID, hermes.RuntimeStepExecutor, 0, "graph_run_failed")
			}
			if e.reporter != nil {
				e.reporter.OnError(runErr)
			}
			return
		}
		state := hermes.TaskState{}
		if final.State.TaskID != "" {
			state = hermesStateToTaskStateForGraph(final.State)
		}
		interrupt := final.State.Interrupt
		if latest, err := e.store.(hermes.SnapshotStore).GetLatestSnapshot(taskID); err == nil && latest.State.Interrupt != nil {
			interrupt = latest.State.Interrupt
		}
		if loaded, err := e.store.GetTask(taskID); err == nil {
			state = loaded
		}
		if interrupt != nil && e.cfg.OnGraphInterrupt != nil {
			e.cfg.OnGraphInterrupt(runCtx, state, *interrupt)
			return
		}
		if state.Status == hermes.TaskStatusDone {
			e.reporter.OnDone(state)
			if e.cfg.OnDone != nil {
				e.cfg.OnDone(runCtx, state)
			}
			e.onDone(runCtx, state, countDoneSubTasks(state.Plan), len(state.Plan))
			if e.cfg.PostCompletionHook != nil {
				go func() {
					hookCtx, hookCancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer hookCancel()
					e.cfg.PostCompletionHook(hookCtx)
				}()
			}
		}
	}()
	return nil
}

func (e *PlanExecuteEngine) runViaGraphTask(ctx context.Context, taskID string, cc *ChatContext) (hermes.Snapshot, error) {
	if err := e.seedPlannerSnapshot(taskID); err != nil {
		return hermes.Snapshot{}, err
	}
	store, ok := e.store.(walkerSnapshotStore)
	if !ok {
		return hermes.Snapshot{}, errors.New("engine: task store does not satisfy snapshot interfaces required by graph.Walker")
	}
	// Walking-agent process-local toggle on the runner. The snapshot's
	// state.Walking carries the cross-sub-task decision data; this flag
	// just lets the underlying runner know it can skip
	// ClearSessionForModel between calls. Reset on exit so non-Hermes
	// work after the task does not inherit walking semantics.
	if e.cfg.WalkingAgentEnabled {
		e.direct.SetWalkingEnabled(true)
		defer e.direct.SetWalkingEnabled(false)
	}
	walkerStore := walkerSnapshotStore(&graphProgressStore{
		walkerSnapshotStore: store,
		engine:              e,
		ctx:                 ctx,
	})
	walker, err := graph.NewWalker(walkerStore, e.BuildGraphRegistry(cc))
	if err != nil {
		return hermes.Snapshot{}, err
	}
	return walker.Run(ctx, taskID)
}

func (e *PlanExecuteEngine) clearGraphRunState(taskID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.taskID == taskID {
		e.taskID = ""
		e.cancelFn = nil
	}
}

func logGraphRunError(taskID string, err error) {
	log.Printf("[plan_execute.graph] task %s failed: %v", taskID, err)
}

// walkerSnapshotStore mirrors graph's internal walkerStore: the union
// of SnapshotStore (read) and RuntimeStepStore (write) needed by the
// Walker. SQLiteTaskStore and MemoryTaskStore both satisfy it.
type walkerSnapshotStore interface {
	hermes.SnapshotStore
	hermes.RuntimeStepStore
}

type graphProgressStore struct {
	walkerSnapshotStore
	engine *PlanExecuteEngine
	ctx    context.Context
}

func (s *graphProgressStore) CommitRuntimeStep(commit hermes.RuntimeCommit) (hermes.Snapshot, error) {
	var prev hermes.Snapshot
	if s != nil && s.walkerSnapshotStore != nil {
		prev, _ = s.walkerSnapshotStore.GetLatestSnapshot(commit.TaskID)
	}
	committed, err := s.walkerSnapshotStore.CommitRuntimeStep(commit)
	if err != nil {
		return committed, err
	}
	s.emitProgress(prev, committed)
	return committed, nil
}

func (s *graphProgressStore) emitProgress(prev, committed hermes.Snapshot) {
	if s == nil || s.engine == nil {
		return
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Budget warning fires once on the transition to exceeded — the
	// commit that installed the budget interrupt is keyed by Walker
	// metadata, so we only need to react to it here. legacy run()
	// emitted via reporter.OnBudgetWarning + onBudgetExceeded; the
	// graph path mirrors both.
	if committed.Metadata.Reason == "budget_exceeded" && (prev.State.Interrupt == nil || prev.Metadata.Reason != "budget_exceeded") {
		s.engine.reporter.OnBudgetWarning(committed.State.TokenBudget)
		s.engine.onBudgetExceeded(ctx, hermesStateToTaskStateForGraph(committed.State))
	}
	plan := committed.State.Plan
	if len(plan) == 0 {
		return
	}
	if committed.Metadata.Reason == "plan_ready" || committed.Metadata.Reason == "replan_ready" {
		s.engine.reporter.OnPlanReady(plan)
		s.engine.onPlanReady(ctx, hermesStateToTaskStateForGraph(committed.State), plan)
	}
	for idx, task := range plan {
		if !isResolvedSubTaskStatus(task.Status) {
			continue
		}
		if idx < len(prev.State.Plan) && prev.State.Plan[idx].Status == task.Status && prev.State.Plan[idx].Result == task.Result {
			continue
		}
		success := task.Status == hermes.SubTaskDone
		completed := countDoneSubTasks(plan)
		s.engine.reporter.OnSubTaskDone(idx, len(plan), task, success, task.Result)
		s.engine.onSubTaskDone(ctx, idx, len(plan), plan, task, task.Result, task.TokensUsed, completed)
	}
}

func isResolvedSubTaskStatus(status hermes.SubTaskStatus) bool {
	switch status {
	case hermes.SubTaskDone, hermes.SubTaskSkipped, hermes.SubTaskFailed:
		return true
	default:
		return false
	}
}

func (e *PlanExecuteEngine) ensureTaskForGraph(goal string) (string, error) {
	e.mu.Lock()
	taskID := e.taskID
	e.mu.Unlock()
	if taskID != "" {
		return taskID, nil
	}
	return e.createTaskForGraph(goal)
}

func (e *PlanExecuteEngine) createTaskForGraph(goal string) (string, error) {
	budget := e.cfg.Budget
	if budget.StartedAt.IsZero() {
		budget.StartedAt = time.Now()
	}
	task := hermes.TaskState{
		ID:                uuid.NewString(),
		ChatID:            e.cfg.ChatID,
		ThreadID:          e.cfg.ThreadID,
		Goal:              goal,
		ProjectDir:        e.cfg.ProjectDir,
		Status:            hermes.TaskStatusPlanning,
		TokenBudget:       budget,
		PlannerSessionID:  e.cfg.PlannerSessionID,
		GithubIssueNumber: e.cfg.GithubIssueNumber,
	}
	created, err := e.store.CreateTask(task)
	if err != nil {
		return "", fmt.Errorf("engine: create task for graph run: %w", err)
	}
	e.mu.Lock()
	e.taskID = created.ID
	e.mu.Unlock()
	return created.ID, nil
}

func hermesStateToTaskStateForGraph(state hermes.HermesState) hermes.TaskState {
	return hermes.TaskState{
		ID:                state.TaskID,
		ChatID:            state.ChatID,
		ThreadID:          state.ThreadID,
		PlannerSessionID:  state.PlannerSessionID,
		ExecutorSessionID: state.ExecutorSessionID,
		ProjectDir:        state.ProjectDir,
		Goal:              state.Goal,
		Status:            state.Status,
		CurrentIdx:        state.CurrentIdx,
		Plan:              append([]hermes.SubTask(nil), state.Plan...),
		Accumulated:       state.Accumulated,
		Artifacts:         append([]hermes.Artifact(nil), state.Artifacts...),
		TokenBudget:       state.TokenBudget,
		GithubIssueNumber: state.GithubIssueNumber,
		ModelUsages:       append([]hermes.ModelUsage(nil), state.ModelUsages...),
		PhaseUsages:       append([]hermes.PhaseUsage(nil), state.PhaseUsages...),
	}
}

func countDoneSubTasks(plan []hermes.SubTask) int {
	done := 0
	for _, st := range plan {
		if st.Status == hermes.SubTaskDone {
			done++
		}
	}
	return done
}

// seedPlannerSnapshot writes the initial snapshot that points the Walker at
// the planner step. Idempotent: if a latest snapshot already exists this
// returns nil without overwriting.
func (e *PlanExecuteEngine) seedPlannerSnapshot(taskID string) error {
	store, ok := e.store.(hermes.SnapshotStore)
	if !ok {
		return errors.New("engine: task store does not satisfy SnapshotStore")
	}
	if existing, err := store.GetLatestSnapshot(taskID); err == nil && existing.NextStep != "" {
		return nil
	}
	task, err := e.store.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("engine: load task for snapshot seed: %w", err)
	}
	state := hermes.HermesStateFromTaskState(task)
	if state.Goal == "" {
		state.Goal = task.Goal
	}
	if e.cfg.WalkingAgentEnabled {
		state.Walking = &hermes.WalkingAgentState{
			Enabled:          true,
			MaxContextTokens: e.walkingMaxContextTokens(),
		}
	}
	_, err = store.CreateSnapshot(hermes.Snapshot{
		TaskID:    taskID,
		ChatID:    task.ChatID,
		ThreadID:  task.ThreadID,
		State:     state,
		NextStep:  hermes.RuntimeStepPlanner,
		Metadata:  hermes.SnapshotMetadata{Source: "graph_walker", Reason: "seed"},
		CreatedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("engine: seed planner snapshot: %w", err)
	}
	return nil
}

// executorSubTaskRunner adapts DirectEngine into graph.SubTaskRunner.
// It owns prompt building (including strict-retry feedback prepend) and
// translates DirectEngine.Result back into graph.SubTaskRunResult.
//
// Walking-agent decision logic is now also here — the executor adapter
// reads state.Walking before each call to decide between a slim
// (session-reuse) prompt and a cold (fresh-session) prompt, then
// returns the new walking state on the result so ExecutorNode commits
// it via StateUpdate.Walking. See #169 #1, #7. Outer failure-retry and
// operator-hint injection still live on the graph side (ApprovalNode +
// future hint node).
type executorSubTaskRunner struct {
	engine *PlanExecuteEngine
	cc     *ChatContext
}

func (r *executorSubTaskRunner) RunSubTask(ctx context.Context, state hermes.HermesState, idx int) (graph.SubTaskRunResult, error) {
	if r == nil || r.engine == nil {
		return graph.SubTaskRunResult{}, errors.New("engine: executorSubTaskRunner missing engine")
	}
	if idx < 0 || idx >= len(state.Plan) {
		return graph.SubTaskRunResult{}, fmt.Errorf("engine: sub-task idx %d out of range (plan size %d)", idx, len(state.Plan))
	}
	subTask := state.Plan[idx]
	feedback := subTask.StrictRetryFeedback

	walkingActive, forceFresh, prevWalking := r.decideWalking(state, subTask, feedback)
	prompt := buildSubTaskGoalVariant(
		r.engine.cfg.ExecutorRules,
		state.Goal,
		state.Accumulated,
		idx,
		len(state.Plan),
		subTask,
		feedback,
		walkingActive,
	)
	if forceFresh {
		r.engine.direct.ForceFreshSession()
	}
	r.engine.reporter.OnSubTaskStart(idx, len(state.Plan), subTask)
	r.engine.direct.BindSubTask(subTask)
	res, err := r.engine.direct.Run(ctx, prompt, r.cc, subTaskSink{})

	out := graph.SubTaskRunResult{
		Text:                     res.Text,
		Model:                    res.Model,
		InputTokens:              res.InputTokens,
		UncachedInputTokens:      res.InputTokens,
		CacheReadInputTokens:     res.CacheReadInputTokens,
		CacheCreationInputTokens: res.CacheCreationInputTokens,
		OutputTokens:             res.OutputTokens,
		CostUSD:                  res.Cost,
	}
	if prevWalking != nil && prevWalking.Enabled {
		out.WalkingState = nextWalkingState(prevWalking, res, walkingActive, err)
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

// decideWalking ports the case-by-case decision tree from the legacy
// PlanExecuteEngine.executeSubTask: when can the next call reuse the
// prior session (walkingActive) vs must start fresh (forceFresh).
//
// Mirrors the original ladder so behaviour matches Run():
//
//	walking disabled                              → cold, no walking
//	first sub-task (PrevExecutorModel == "")      → cold, force fresh
//	predicted model unknown                       → cold, force fresh (safe)
//	predicted model != PrevExecutorModel          → model boundary, fresh
//	strict-retry feedback present                 → fresh seat for retry
//	TokensSeen >= MaxContextTokens                → watermark trip
//	default                                       → walking active
func (r *executorSubTaskRunner) decideWalking(state hermes.HermesState, subTask hermes.SubTask, feedback string) (walkingActive, forceFresh bool, prev *hermes.WalkingAgentState) {
	prev = state.Walking
	if prev == nil || !prev.Enabled {
		return false, false, prev
	}
	predicted := r.engine.predictExecutorModel(subTask)
	max := prev.MaxContextTokens
	if max <= 0 {
		max = defaultWalkingMaxContextTokens
	}
	switch {
	case prev.PrevExecutorModel == "":
		return false, true, prev
	case predicted == "":
		return false, true, prev
	case predicted != prev.PrevExecutorModel:
		return false, true, prev
	case strings.TrimSpace(feedback) != "":
		return false, true, prev
	case prev.TokensSeen >= max:
		return false, true, prev
	default:
		return true, false, prev
	}
}

// nextWalkingState computes the post-call walking state from the prior
// state + the runner's metrics. Mirrors the legacy plan_execute.go
// post-run update block: track the actual model that ran, advance the
// transcript watermark when walkingActive, reset on cold or error.
func nextWalkingState(prev *hermes.WalkingAgentState, res Result, walkingActive bool, runErr error) *hermes.WalkingAgentState {
	if prev == nil {
		return nil
	}
	out := *prev
	if runErr != nil {
		// Drop session state on error so the next sub-task starts fresh.
		out.PrevExecutorModel = ""
		out.TokensSeen = 0
		return &out
	}
	if res.Model != "" {
		out.PrevExecutorModel = res.Model
	}
	if walkingActive {
		transcriptSize := res.CacheReadInputTokens + res.CacheCreationInputTokens
		if transcriptSize == 0 {
			transcriptSize = prev.TokensSeen + res.InputTokens + res.OutputTokens
		}
		if transcriptSize > out.TokensSeen {
			out.TokensSeen = transcriptSize
		}
	} else {
		out.TokensSeen = 0
	}
	return &out
}

// subTaskReviewerAdapter wraps PlanExecuteEngine.runReview for the
// strict-mode per-sub-task path. Verdict is mapped through
// ReviewDecisionFromStrictTags so the graph receives the same hard-gate
// decision the legacy strict loop used.
type subTaskReviewerAdapter struct {
	engine    *PlanExecuteEngine
	strictCfg StrictModeConfig
}

func (a *subTaskReviewerAdapter) ReviewSubTask(ctx context.Context, state hermes.HermesState, idx int) (graph.SubTaskReviewResult, error) {
	if a == nil || a.engine == nil {
		return graph.SubTaskReviewResult{}, errors.New("engine: subTaskReviewerAdapter missing engine")
	}
	if idx < 0 || idx >= len(state.Plan) {
		return graph.SubTaskReviewResult{}, fmt.Errorf("engine: review idx %d out of range", idx)
	}
	taskState, err := a.engine.store.GetTask(state.TaskID)
	if err != nil {
		return graph.SubTaskReviewResult{}, fmt.Errorf("engine: load task for sub-task review: %w", err)
	}
	feedback := state.Plan[idx].StrictRetryFeedback
	review, err := a.engine.runReview(ctx, taskState, ReviewModePerSubTask, idx, feedback, false)
	if err != nil {
		return graph.SubTaskReviewResult{}, err
	}
	decision := ReviewDecisionFromStrictTags(review, a.strictCfg)
	verdict := string(decision.Verdict)
	if verdict == "" {
		verdict = string(review.Verdict)
	}
	tags := make([]string, 0, len(decision.MatchedTags))
	for _, t := range decision.MatchedTags {
		tags = append(tags, string(t))
	}
	return graph.SubTaskReviewResult{
		Verdict:      verdict,
		Feedback:     review.Feedback,
		Score:        review.OverallScore,
		BlockTags:    tags,
		Model:        review.ReviewerModel,
		InputTokens:  review.InputTokens,
		OutputTokens: review.OutputTokens,
		CostUSD:      review.CostUSD,
	}, nil
}

// taskReviewerAdapter wraps the per-task review call. After running
// the reviewer it consults DecideRecovery to decide whether to ask the
// graph for a replan — when DecideRecovery says retry within budget,
// the adapter sets Replan=true so ReviewerNode routes to ReplanSetup;
// otherwise it falls through to terminal as before. The just-finished
// review + plan are stashed on the shared replanCoordinator so the
// downstream replanDeciderAdapter can pick the partial-vs-full branch
// without re-running the reviewer.
type taskReviewerAdapter struct {
	engine *PlanExecuteEngine
	replan *replanCoordinator
}

func (a *taskReviewerAdapter) ReviewTask(ctx context.Context, state hermes.HermesState) (graph.TaskReviewResult, error) {
	if a == nil || a.engine == nil {
		return graph.TaskReviewResult{}, errors.New("engine: taskReviewerAdapter missing engine")
	}
	taskState, err := a.engine.store.GetTask(state.TaskID)
	if err != nil {
		return graph.TaskReviewResult{}, fmt.Errorf("engine: load task for task review: %w", err)
	}
	review, err := a.engine.runReview(ctx, taskState, ReviewModePerTask, -1, "", false)
	if err != nil {
		return graph.TaskReviewResult{}, err
	}

	retryCfg := a.engine.cfg.TaskRetry
	if retryCfg.Enabled {
		retryCfg = retryCfg.WithDefaults()
	}
	maxAttempts := 0
	if retryCfg.Enabled {
		maxAttempts = retryCfg.MaxTaskRetries
	}
	attempt := 0
	if a.replan != nil {
		attempt = a.replan.currentAttempt(state.TaskID)
	}
	recoveryReq := RecoveryRequest{
		Mode:        "task_review",
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		Review:      review,
		TaskRetry:   retryCfg,
	}
	decision := DecideRecovery(recoveryReq)
	LogRecoveryDecision(recoveryReq, decision)
	a.engine.emitRuntimeEvent(ctx, RecoveryTraceEvent(recoveryReq, decision, time.Now()))
	wantReplan := decision.Action == RecoveryActionRetry && retryCfg.Enabled && attempt < maxAttempts
	if wantReplan && a.replan != nil {
		a.replan.recordReview(state.TaskID, review, append([]hermes.SubTask(nil), state.Plan...), maxAttempts)
	}

	return graph.TaskReviewResult{
		Verdict:      string(review.Verdict),
		Replan:       wantReplan,
		Feedback:     review.Feedback,
		OverallScore: review.OverallScore,
		Model:        review.ReviewerModel,
		InputTokens:  review.InputTokens,
		OutputTokens: review.OutputTokens,
		CostUSD:      review.CostUSD,
	}, nil
}

// replanDeciderAdapter is the engine-side ReplanDecider. It pulls the
// last review + plan from the shared replanCoordinator (stashed by
// taskReviewerAdapter at the end of the prior attempt), then runs the
// existing buildPartialRetryPlan / buildPartialReplanGoal /
// buildReplanGoal helpers to produce a ReplanDecision the graph can
// install on the next state. Score threshold and goal text generation
// are unchanged from the legacy Run() — this adapter is just relocating
// the call sites.
type replanDeciderAdapter struct {
	engine *PlanExecuteEngine
	replan *replanCoordinator
}

func (a *replanDeciderAdapter) DecideReplan(_ context.Context, state hermes.HermesState) (graph.ReplanDecision, error) {
	if a == nil || a.engine == nil {
		return graph.ReplanDecision{}, errors.New("engine: replanDeciderAdapter missing engine")
	}
	if a.replan == nil {
		return graph.ReplanDecision{}, nil
	}
	prevReview, prevPlan, attemptIdx := a.replan.consumeForReplan(state.TaskID)
	if attemptIdx == 0 {
		// Coordinator had nothing to consume — nothing to replan.
		return graph.ReplanDecision{}, nil
	}

	retryCfg := a.engine.cfg.TaskRetry.WithDefaults()
	originalGoal := state.Goal
	if state.Replan != nil && state.Replan.Goal != "" {
		// Already replanning; never seen in practice (this node runs
		// before PlannerNode clears Replan), but keep the original goal
		// stable across attempts by preferring the unaugmented one.
		originalGoal = state.Goal
	}

	// Notify the operator that a replan attempt is starting before we
	// build the new goal. Mirrors legacy Run()'s OnTaskRetry call: the
	// Telegram side typically prints "🔄 Re-planning attempt N/M" so the
	// user is not left wondering why a fresh plan is being generated.
	if cb := a.engine.cfg.OnTaskRetry; cb != nil {
		maxRetries := 0
		if a.engine.cfg.TaskRetry.Enabled {
			maxRetries = a.engine.cfg.TaskRetry.WithDefaults().MaxTaskRetries
		}
		// attemptIdx is 1-based for the new attempt; legacy convention
		// passes the just-completed attempt index (zero-based), so subtract
		// one to keep notification text consistent with Run().
		cb(context.Background(), attemptIdx-1, maxRetries, prevReview)
	}

	partial := buildPartialRetryPlan(prevReview, prevPlan, retryCfg.ScoreThreshold)
	if len(partial.Preserved) > 0 {
		return graph.ReplanDecision{
			Goal:              buildPartialReplanGoal(originalGoal, prevReview, partial),
			Accumulated:       partial.Accumulated,
			PreservedSubTasks: append([]hermes.SubTask(nil), partial.Preserved...),
			AttemptIdx:        attemptIdx,
			Trigger:           "partial",
		}, nil
	}
	return graph.ReplanDecision{
		Goal:        buildReplanGoal(originalGoal, prevReview, prevPlan),
		Accumulated: "",
		AttemptIdx:  attemptIdx,
		Trigger:     "full",
	}, nil
}
