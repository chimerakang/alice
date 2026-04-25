package hermes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CoordinatorConfig holds the runtime configuration for one Hermes session.
type CoordinatorConfig struct {
	PlannerModel  string
	ExecutorModel string

	// HeavyExecutorModel optionally overrides ExecutorModel for sub-tasks that
	// look like substantive code changes (Edit/Write/file_patch tool hints, or
	// description containing implementation verbs). Empty means "always use
	// ExecutorModel" — the legacy single-model behaviour.
	HeavyExecutorModel string

	ProjectDir    string
	ChatID        int64

	MaxRetriesPerSubtask  int
	MaxPlannerJSONRetries int
	InterruptPolicy       InterruptPolicy
	ProgressVerbosity     Verbosity

	Budget TokenBudget

	AccumulatedCfg AccumulatedConfig

	// PlannerRules is prepended to the plannerSystemPrompt on every Plan() call.
	// When empty, only the embedded plannerSystemPrompt is used.
	PlannerRules string
	// ExecutorRules is passed as coreRules to BuildExecutorPrompt on every Execute() call.
	ExecutorRules string

	// GitHub integration
	GithubIssueNumber int        // 0 = no issue linked
	GithubCfg         GithubCfg  // GitHub notification settings

	// PostCompletionHook is called (in a goroutine) after the task finishes,
	// regardless of success or failure. Nil means no hook.
	PostCompletionHook func(ctx context.Context)

	// ContinueCh is an optional channel the Telegram handler can signal to resume
	// execution after a budget warning. When nil, a budget-exceeded event kills the
	// task immediately (legacy behaviour). The coordinator closes the channel once
	// it has been consumed.
	ContinueCh chan struct{}

	// ContinueTimeout is how long the coordinator waits for a signal on ContinueCh
	// before giving up. Zero defaults to 15 minutes.
	ContinueTimeout time.Duration
}

// Coordinator orchestrates the Planner-Executor lifecycle for a single chat.
// It is safe to call from multiple goroutines.
type Coordinator struct {
	cfg           CoordinatorConfig
	planner       *PlannerSession
	executor      *ExecutorSession // light model (e.g. Haiku); always present
	executorHeavy *ExecutorSession // heavy model (e.g. Sonnet); nil when feature disabled
	store         TaskStateStore
	progress      ProgressReporter
	hooks         *HookRegistry

	mu          sync.Mutex
	taskID      string
	cancelFn    context.CancelFunc
	interrupted bool               // true after InterruptWith is called
	msgQueue    []int64            // queued Telegram message IDs (queue policy)
}

// NewCoordinator creates a ready-to-use Coordinator.
//
// execFnHeavy is optional: pass nil to use a single executor for every
// sub-task. When non-nil, sub-tasks classified as heavy (Edit/Write/file_patch
// hints or implementation verbs in the description) are routed through it.
func NewCoordinator(
	cfg CoordinatorConfig,
	planFn CallPlanFunc,
	execFn CallStreamFunc,
	execFnHeavy CallStreamFunc,
	store TaskStateStore,
	progress ProgressReporter,
	hooks *HookRegistry,
) *Coordinator {
	if hooks == nil {
		hooks = NewHookRegistry()
	}
	if progress == nil {
		progress = &NoopProgressReporter{}
	}
	c := &Coordinator{
		cfg:      cfg,
		planner:  NewPlannerSession(planFn, cfg.MaxPlannerJSONRetries, cfg.PlannerRules),
		executor: NewExecutorSession(execFn, cfg.ExecutorRules, cfg.MaxRetriesPerSubtask),
		store:    store,
		progress: progress,
		hooks:    hooks,
	}
	if execFnHeavy != nil && cfg.HeavyExecutorModel != "" {
		c.executorHeavy = NewExecutorSession(execFnHeavy, cfg.ExecutorRules, cfg.MaxRetriesPerSubtask)
	}
	return c
}

// heavyToolHints lists Claude Code tool names that indicate code-modifying work.
var heavyToolHints = map[string]struct{}{
	"Edit": {}, "Write": {}, "MultiEdit": {}, "NotebookEdit": {}, "file_patch": {},
}

// heavyDescriptionTokens triggers the heavy executor when present in the
// sub-task description. Single-character substrings are deliberately avoided
// (cf. classifier.go actionVerbs cleanup).
var heavyDescriptionTokens = []string{
	"refactor", "implement", "rewrite", "redesign", "migrate",
	"重構", "重寫", "實作", "實現", "修正", "修復",
}

// pickExecutor returns the right ExecutorSession for the given sub-task and
// reports the model name so AddModelUsage records the correct tier.
// Falls back to the light executor whenever the heavy one is not configured.
func (c *Coordinator) pickExecutor(t SubTask) (*ExecutorSession, string) {
	if c.executorHeavy == nil {
		return c.executor, c.cfg.ExecutorModel
	}
	for _, hint := range t.ToolHints {
		if _, ok := heavyToolHints[hint]; ok {
			return c.executorHeavy, c.cfg.HeavyExecutorModel
		}
	}
	desc := strings.ToLower(t.Description)
	for _, kw := range heavyDescriptionTokens {
		if strings.Contains(desc, kw) {
			return c.executorHeavy, c.cfg.HeavyExecutorModel
		}
	}
	return c.executor, c.cfg.ExecutorModel
}

// Start launches the Hermes lifecycle asynchronously.
// It returns immediately; progress events arrive via the ProgressReporter.
// The returned taskID can be used to query or interrupt the task.
func (c *Coordinator) Start(ctx context.Context, goal string) (taskID string, err error) {
	budget := c.cfg.Budget
	budget.StartedAt = time.Now()

	task := TaskState{
		ID:              newUUID(),
		ChatID:          c.cfg.ChatID,
		Goal:            goal,
		Status:          TaskStatusPlanning,
		InterruptPolicy: c.cfg.InterruptPolicy,
		TokenBudget:     budget,
	}

	created, err := c.store.CreateTask(task)
	if err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}

	c.mu.Lock()
	c.taskID = created.ID
	runCtx, cancel := context.WithCancel(ctx)
	c.cancelFn = cancel
	c.interrupted = false
	c.mu.Unlock()

	go c.run(runCtx, created.ID, goal)
	return created.ID, nil
}

// InterruptWith signals the coordinator to handle a user interrupt.
// Behaviour depends on InterruptPolicy:
//   - queue: messageID is queued for processing after current sub-task
//   - interrupt: current task is cancelled and marked interrupted
//   - inject: messageID is noted; the next prompt will include the new text
func (c *Coordinator) InterruptWith(messageID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.cfg.InterruptPolicy {
	case InterruptAbort:
		c.interrupted = true
		if c.cancelFn != nil {
			c.cancelFn()
		}
		if c.taskID != "" {
			if err := c.store.MarkInterrupted(c.taskID, messageID); err != nil {
				log.Printf("[hermes] MarkInterrupted: %v", err)
			}
		}
	case InterruptInject:
		// The next executor prompt will be rebuilt from state which may include feedback
		c.msgQueue = append(c.msgQueue, messageID)
	default: // InterruptQueue
		c.msgQueue = append(c.msgQueue, messageID)
	}
}

// TaskID returns the current task ID, or empty if none is running.
func (c *Coordinator) TaskID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.taskID
}

// IsRunning returns true while a task lifecycle goroutine is active.
func (c *Coordinator) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancelFn != nil
}

// run is the main lifecycle goroutine.
func (c *Coordinator) run(ctx context.Context, taskID, goal string) {
	defer func() {
		c.mu.Lock()
		c.cancelFn = nil
		c.taskID = ""
		c.mu.Unlock()
	}()

	// ── Phase 1: Planning ─────────────────────────────────────────────────────
	// Pre-Planner gate: short non-implementation goals skip the Opus call and
	// run as a single Executor turn — saves ~300 tokens of Planner overhead and
	// ~10s of cold-start latency for status checks, single git commands, and
	// other one-shot work.
	var (
		tasks            []SubTask
		planIn, planOut  int
		err              error
		plannerSkipped   bool
	)
	if ClassifyGoal(goal) == GoalSimple {
		tasks = []SubTask{{
			ID:          "s1",
			Description: "Execute the goal directly: " + goal,
			ToolHints:   nil,
			Status:      SubTaskPending,
		}}
		plannerSkipped = true
		log.Printf("[hermes] Planner skipped for simple goal (chat=%d): %.60s", c.cfg.ChatID, goal)
	} else {
		tasks, planIn, planOut, err = c.planner.Plan(ctx, goal, c.cfg.ProjectDir)
	}
	_ = plannerSkipped // reserved for future progress event differentiation
	if err != nil {
		var jfail *ErrPlannerJSONFailed
		if errors.As(err, &jfail) {
			c.progress.OnError(fmt.Errorf("Planner JSON 解析失敗，降級回一般模式：%v", err))
		} else {
			c.progress.OnError(err)
		}
		_ = c.store.MarkStatus(taskID, TaskStatusFailed)

		// GitHub: post failure comment and apply failure label
		issueNum := c.cfg.GithubIssueNumber
		ghCfg := c.cfg.GithubCfg
		if issueNum > 0 && ghCfg.Enabled {
			if ghCfg.ShouldComment("fail") {
				body := CommentFailed("Planning", err.Error(), 0, 0)
				if err := PostComment(ctx, c.cfg.ProjectDir, issueNum, body); err != nil {
					log.Printf("[hermes] GitHub comment planning fail: %v", err)
				}
			}
			if ghCfg.FailureLabel != "" {
				if err := ApplyLabel(ctx, c.cfg.ProjectDir, issueNum, ghCfg.FailureLabel); err != nil {
					log.Printf("[hermes] GitHub apply failure label: %v", err)
				}
			}
		}
		return
	}

	_ = c.store.AddTokenUsage(taskID, planIn+planOut)
	_ = c.store.AddModelUsage(taskID, c.cfg.PlannerModel, planIn, planOut)
	if sid := c.planner.SessionID(); sid != "" {
		_ = c.store.UpdatePlannerSession(taskID, sid)
	}

	// Complexity Gate enforcement: validate sub-task count
	if len(tasks) > 15 {
		c.progress.OnError(fmt.Errorf("complexity violation: plan has %d sub-tasks (max 15); Planner exceeded granularity limits", len(tasks)))
		_ = c.store.MarkStatus(taskID, TaskStatusFailed)
		return
	}

	// Persist the plan so UpdateSubTask can find sub-task records.
	if err := c.store.StorePlan(taskID, tasks); err != nil {
		c.progress.OnError(fmt.Errorf("persist plan: %w", err))
		_ = c.store.MarkStatus(taskID, TaskStatusFailed)
		return
	}

	if err := c.store.MarkStatus(taskID, TaskStatusExecuting); err != nil {
		c.progress.OnError(err)
		return
	}

	// Load state (now includes persisted plan).
	state, err := c.store.GetTask(taskID)
	if err != nil {
		c.progress.OnError(err)
		return
	}

	c.progress.OnPlanReady(tasks)

	// GitHub: post "started" comment + write plan checklist if no checklist existed
	issueNum := c.cfg.GithubIssueNumber
	ghCfg := c.cfg.GithubCfg
	if issueNum > 0 && ghCfg.Enabled {
		if ghCfg.ShouldComment("start") {
			body := CommentStarted(c.cfg.PlannerModel, c.cfg.ExecutorModel)
			if err := PostComment(ctx, c.cfg.ProjectDir, issueNum, body); err != nil {
				log.Printf("[hermes] GitHub comment start: %v", err)
			}
		}
		// Write plan back to Issue if it had no checklist
		if ghCfg.SyncChecklist {
			if err := WritePlanToIssue(ctx, c.cfg.ProjectDir, issueNum, state.Goal, tasks); err != nil {
				log.Printf("[hermes] GitHub write plan: %v", err)
			}
		}
	}

	// ── Phase 2: Execution loop ───────────────────────────────────────────────
	accCfg := c.cfg.AccumulatedCfg
	completedCount := 0

	for idx := 0; idx < len(tasks); {
		// Re-load state for up-to-date budget and accumulated.
		state, err = c.store.GetTask(taskID)
		if err != nil {
			c.progress.OnError(err)
			_ = c.store.MarkStatus(taskID, TaskStatusFailed)
			return
		}

		if state.TokenBudget.Exceeded() {
			c.progress.OnBudgetWarning(state.TokenBudget)

			// If caller supplied a ContinueCh, pause and wait for the user to
			// confirm before proceeding. On confirmation the wallclock budget is
			// reset so remaining sub-tasks get a fresh window.
			if ch := c.cfg.ContinueCh; ch != nil {
				timeout := c.cfg.ContinueTimeout
				if timeout <= 0 {
					timeout = 15 * time.Minute
				}
				select {
				case <-ch:
					// User confirmed — reset wallclock and carry on.
					c.mu.Lock()
					c.cfg.Budget.StartedAt = time.Now()
					c.mu.Unlock()
					// Persist the reset so the next iteration reads updated budget.
					if err := c.store.ResetBudgetStartedAt(taskID, c.cfg.Budget.StartedAt); err != nil {
						log.Printf("[hermes] ResetBudgetStartedAt: %v", err)
					}
					log.Printf("[hermes] budget warning acknowledged by user — resuming task %s at subtask %d", taskID, idx)
				case <-time.After(timeout):
					log.Printf("[hermes] budget warning timed out after %v — stopping task %s", timeout, taskID)
					_ = c.store.MarkStatus(taskID, TaskStatusFailed)
					return
				case <-ctx.Done():
					return
				}
			} else {
				_ = c.store.MarkStatus(taskID, TaskStatusFailed)
				if issueNum > 0 && ghCfg.Enabled {
					if ghCfg.ShouldComment("budget_exceeded") {
						body := CommentBudgetExceeded(state.TokenBudget.UsedTokens, state.TokenBudget.MaxTotalTokens)
						if err := PostComment(ctx, c.cfg.ProjectDir, issueNum, body); err != nil {
							log.Printf("[hermes] GitHub comment budget: %v", err)
						}
					}
					if ghCfg.FailureLabel != "" {
						if err := ApplyLabel(ctx, c.cfg.ProjectDir, issueNum, ghCfg.FailureLabel); err != nil {
							log.Printf("[hermes] GitHub apply failure label on budget: %v", err)
						}
					}
				}
				return
			}
		}

		// Interrupt check
		c.mu.Lock()
		interrupted := c.interrupted
		c.mu.Unlock()
		if interrupted || ctx.Err() != nil {
			return
		}

		// Decide whether the next slice of sub-tasks can run in parallel.
		// GatherReadOnlyBatch returns 1 index for write-capable hints and up to
		// MaxParallelReadOnly indices when consecutive sub-tasks all have hints
		// drawn from the read-only set (Read/Grep/Glob/WebFetch/...).
		batch := GatherReadOnlyBatch(tasks, idx)
		outcomes := c.runBatch(ctx, taskID, batch, len(tasks), tasks, state)

		// Post-process each outcome in original index order so accumulated
		// summary, GitHub comments and progress events stay deterministic.
		for _, oc := range outcomes {
			execTokens := oc.result.InputTokens + oc.result.OutputTokens

			if oc.execErr != nil {
				if err := c.store.UpdateSubTask(taskID, oc.idx, SubTaskFailed, oc.execErr.Error(), execTokens); err != nil {
					log.Printf("[hermes] UpdateSubTask(failed) idx=%d: %v", oc.idx, err)
				}
				c.progress.OnSubTaskDone(oc.idx, len(tasks), oc.subTask, false, oc.execErr.Error())
				continue
			}

			if oc.result.Attempts > 1 && oc.result.ValidationError != "" {
				c.progress.OnRetry(oc.idx, oc.result.Attempts, c.executor.maxRetries, oc.result.ValidationError)
			}

			success := oc.result.ValidationError == ""
			finalStatus := SubTaskDone
			if !success {
				finalStatus = SubTaskFailed
			}
			if err := c.store.UpdateSubTask(taskID, oc.idx, finalStatus, oc.result.ResultText, execTokens); err != nil {
				log.Printf("[hermes] UpdateSubTask(%s) idx=%d: %v", finalStatus, oc.idx, err)
			}
			c.progress.OnSubTaskDone(oc.idx, len(tasks), oc.subTask, success, oc.result.ResultText)

			if success {
				completedCount++
				tasks[oc.idx].Status = SubTaskDone
				tasks[oc.idx].Result = oc.result.ResultText

				if issueNum > 0 && ghCfg.Enabled && ghCfg.SyncChecklist {
					if err := SyncChecklist(ctx, c.cfg.ProjectDir, issueNum, tasks); err != nil {
						log.Printf("[hermes] GitHub sync checklist idx=%d: %v", oc.idx, err)
					}
				}
				if issueNum > 0 && ghCfg.Enabled && ghCfg.ShouldComment("complete") {
					body := CommentSubTaskProgress(oc.idx, len(tasks), oc.subTask, oc.result.ResultText, execTokens, completedCount)
					if err := PostComment(ctx, c.cfg.ProjectDir, issueNum, body); err != nil {
						log.Printf("[hermes] GitHub comment subtask progress idx=%d: %v", oc.idx, err)
					}
				}
			}

			// Append this sub-task's result to the rolling summary in idx order.
			state, _ = c.store.GetTask(taskID)
			updated, needsCompress := AppendResult(state.Accumulated, oc.result.ResultText, completedCount, accCfg)
			_ = c.store.UpdateAccumulated(taskID, updated)

			if needsCompress {
				c.runCompression(ctx, taskID, state, updated)
			}
		}

		idx += len(batch)
	}

	// ── Phase 3: Done ─────────────────────────────────────────────────────────
	_ = c.store.MarkStatus(taskID, TaskStatusDone)
	finalState, _ := c.store.GetTask(taskID)
	c.progress.OnDone(finalState)

	// GitHub: post done comment, auto-close, apply failure label
	if issueNum > 0 && ghCfg.Enabled {
		if ghCfg.ShouldComment("complete") {
			body := CommentDone(finalState)
			if err := PostComment(ctx, c.cfg.ProjectDir, issueNum, body); err != nil {
				log.Printf("[hermes] GitHub comment done: %v", err)
			}
		}
		allDone := completedCount == len(tasks)
		if allDone && ghCfg.AutoCloseLabel != "" {
			// Re-fetch labels to check current state
			if issue, err := FetchIssue(ctx, c.cfg.ProjectDir, issueNum); err == nil {
				if HasLabel(issue, ghCfg.AutoCloseLabel) {
					if err := CloseIssue(ctx, c.cfg.ProjectDir, issueNum); err != nil {
						log.Printf("[hermes] GitHub close issue: %v", err)
					}
				}
			}
		}
	}

	// Post-completion: run optional hook (e.g. /task-sync)
	if c.cfg.PostCompletionHook != nil {
		go c.cfg.PostCompletionHook(ctx)
	}
}

// batchOutcome carries one sub-task's execution result through the post-batch
// processing loop. Fields mirror what the legacy single-subtask code path
// computed inline before being refactored for parallel dispatch.
type batchOutcome struct {
	idx           int
	subTask       SubTask
	result        ExecuteResult
	execErr       error
	executorModel string
}

// runBatch executes batchIdxs sub-tasks. A single-element batch runs inline;
// larger batches dispatch goroutines so independent read-only sub-tasks
// (Read/Grep/Glob/WebFetch — see GatherReadOnlyBatch) can overlap their CLI
// cold-starts. Outcomes are returned in original idx order so the caller's
// post-processing (UpdateSubTask, accumulated append, GitHub sync) stays
// deterministic.
//
// State is cloned per goroutine and CurrentIdx overridden so each Executor
// renders its own prompt. Token usage and per-subtask status updates go
// through the store, whose execWithRetry already serialises SQLite writes.
func (c *Coordinator) runBatch(
	ctx context.Context,
	taskID string,
	batchIdxs []int,
	total int,
	tasks []SubTask,
	baseState TaskState,
) []batchOutcome {
	outcomes := make([]batchOutcome, len(batchIdxs))
	if len(batchIdxs) == 1 {
		outcomes[0] = c.runOneSubTask(ctx, taskID, batchIdxs[0], total, tasks, baseState)
		return outcomes
	}
	var wg sync.WaitGroup
	for slot, ridx := range batchIdxs {
		wg.Add(1)
		go func(slot, ridx int) {
			defer wg.Done()
			outcomes[slot] = c.runOneSubTask(ctx, taskID, ridx, total, tasks, baseState)
		}(slot, ridx)
	}
	wg.Wait()
	return outcomes
}

// runOneSubTask executes a single sub-task end-to-end excluding the post-
// processing (status update, accumulated append, GitHub side effects). Those
// are deferred to the caller so a parallel batch can update them in idx order.
func (c *Coordinator) runOneSubTask(
	ctx context.Context,
	taskID string,
	idx, total int,
	tasks []SubTask,
	baseState TaskState,
) batchOutcome {
	subTask := tasks[idx]
	c.progress.OnSubTaskStart(idx, total, subTask)
	if err := c.store.UpdateSubTask(taskID, idx, SubTaskInProgress, "", 0); err != nil {
		log.Printf("[hermes] UpdateSubTask(in_progress) idx=%d: %v", idx, err)
	}

	// Clone state so concurrent goroutines do not race on CurrentIdx.
	s := baseState
	s.CurrentIdx = idx

	executor, executorModel := c.pickExecutor(subTask)
	result, execErr := executor.Execute(ctx, s, c.cfg.ProjectDir)

	execTokens := result.InputTokens + result.OutputTokens
	_ = c.store.AddTokenUsage(taskID, execTokens)
	_ = c.store.AddModelUsage(taskID, executorModel, result.InputTokens, result.OutputTokens)

	return batchOutcome{
		idx:           idx,
		subTask:       subTask,
		result:        result,
		execErr:       execErr,
		executorModel: executorModel,
	}
}

// runCompression calls the Planner to compress the accumulated summary.
func (c *Coordinator) runCompression(ctx context.Context, taskID string, state TaskState, accumulated string) {
	req := CompressRequest{
		Goal:        state.Goal,
		Accumulated: accumulated,
		Artifacts:   state.Artifacts,
	}
	compressed, compressedIn, compressedOut, err := c.planner.Compress(ctx, req, c.cfg.ProjectDir)
	if err != nil {
		log.Printf("[hermes] compression failed for task %s: %v", taskID, err)
		return
	}
	_ = c.store.UpdateAccumulated(taskID, compressed)
	_ = c.store.AddTokenUsage(taskID, compressedIn+compressedOut)
	_ = c.store.AddModelUsage(taskID, c.cfg.PlannerModel, compressedIn, compressedOut)
}

// newUUID returns a random UUID string.
func newUUID() string {
	return uuid.New().String()
}
