package engine

import (
	"context"
	"errors"
	"fmt"
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
	registry.Register(&graph.ApprovalNode{})
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

	mu        sync.Mutex
	perTask   map[string]*replanState
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
	if err := e.seedPlannerSnapshot(taskID); err != nil {
		return hermes.Snapshot{}, err
	}
	store, ok := e.store.(walkerSnapshotStore)
	if !ok {
		return hermes.Snapshot{}, errors.New("engine: task store does not satisfy snapshot interfaces required by graph.Walker")
	}
	walker, err := graph.NewWalker(store, e.BuildGraphRegistry(cc))
	if err != nil {
		return hermes.Snapshot{}, err
	}
	return walker.Run(ctx, taskID)
}

// walkerSnapshotStore mirrors graph's internal walkerStore: the union
// of SnapshotStore (read) and RuntimeStepStore (write) needed by the
// Walker. SQLiteTaskStore and MemoryTaskStore both satisfy it.
type walkerSnapshotStore interface {
	hermes.SnapshotStore
	hermes.RuntimeStepStore
}

func (e *PlanExecuteEngine) ensureTaskForGraph(goal string) (string, error) {
	e.mu.Lock()
	taskID := e.taskID
	e.mu.Unlock()
	if taskID != "" {
		return taskID, nil
	}
	task := hermes.TaskState{
		ID:                uuid.NewString(),
		ChatID:            e.cfg.ChatID,
		ThreadID:          e.cfg.ThreadID,
		Goal:              goal,
		ProjectDir:        e.cfg.ProjectDir,
		Status:            hermes.TaskStatusPlanning,
		TokenBudget:       e.cfg.Budget,
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
// What's deliberately NOT here: walking-agent slim-prompt logic, outer
// failure-retry, and per-attempt operator-hint injection. Those concerns
// belong on the graph side (StrictReviewNode for retry, ApprovalNode for
// failure pause) or in a follow-up walking-agent node.
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
	prompt := buildSubTaskGoalVariant(
		r.engine.cfg.ExecutorRules,
		state.Goal,
		state.Accumulated,
		idx,
		len(state.Plan),
		subTask,
		feedback,
		false, // walkingActive — γ6 first cut leaves walking-agent off the graph path
	)
	r.engine.direct.BindSubTask(subTask)
	res, err := r.engine.direct.Run(ctx, prompt, r.cc, subTaskSink{})
	if err != nil {
		return graph.SubTaskRunResult{}, err
	}
	return graph.SubTaskRunResult{
		Text:                     res.Text,
		Model:                    res.Model,
		InputTokens:              res.InputTokens,
		UncachedInputTokens:      res.InputTokens,
		CacheReadInputTokens:     res.CacheReadInputTokens,
		CacheCreationInputTokens: res.CacheCreationInputTokens,
		OutputTokens:             res.OutputTokens,
		CostUSD:                  res.Cost,
	}, nil
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
	decision := DecideRecovery(RecoveryRequest{
		Mode:        "task_review",
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		Review:      review,
		TaskRetry:   retryCfg,
	})
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
