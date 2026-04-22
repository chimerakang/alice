package hermes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CoordinatorConfig holds the runtime configuration for one Hermes session.
type CoordinatorConfig struct {
	PlannerModel  string
	ExecutorModel string
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
}

// Coordinator orchestrates the Planner-Executor lifecycle for a single chat.
// It is safe to call from multiple goroutines.
type Coordinator struct {
	cfg      CoordinatorConfig
	planner  *PlannerSession
	executor *ExecutorSession
	store    TaskStateStore
	progress ProgressReporter
	hooks    *HookRegistry

	mu          sync.Mutex
	taskID      string
	cancelFn    context.CancelFunc
	interrupted bool               // true after InterruptWith is called
	msgQueue    []int64            // queued Telegram message IDs (queue policy)
}

// NewCoordinator creates a ready-to-use Coordinator.
func NewCoordinator(
	cfg CoordinatorConfig,
	planFn CallPlanFunc,
	execFn CallStreamFunc,
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
	return &Coordinator{
		cfg:      cfg,
		planner:  NewPlannerSession(planFn, cfg.MaxPlannerJSONRetries, cfg.PlannerRules),
		executor: NewExecutorSession(execFn, cfg.ExecutorRules, cfg.MaxRetriesPerSubtask),
		store:    store,
		progress: progress,
		hooks:    hooks,
	}
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
	tasks, planTokens, err := c.planner.Plan(ctx, goal, c.cfg.ProjectDir)
	if err != nil {
		var jfail *ErrPlannerJSONFailed
		if errors.As(err, &jfail) {
			c.progress.OnError(fmt.Errorf("Planner JSON 解析失敗，降級回一般模式：%v", err))
		} else {
			c.progress.OnError(err)
		}
		_ = c.store.MarkStatus(taskID, TaskStatusFailed)
		return
	}

	_ = c.store.AddTokenUsage(taskID, planTokens)
	if sid := c.planner.SessionID(); sid != "" {
		_ = c.store.UpdatePlannerSession(taskID, sid)
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

	// ── Phase 2: Execution loop ───────────────────────────────────────────────
	accCfg := c.cfg.AccumulatedCfg
	completedCount := 0

	for idx := range tasks {
		// Re-load state for up-to-date budget and accumulated.
		state, err = c.store.GetTask(taskID)
		if err != nil {
			c.progress.OnError(err)
			_ = c.store.MarkStatus(taskID, TaskStatusFailed)
			return
		}

		if state.TokenBudget.Exceeded() {
			c.progress.OnBudgetWarning(state.TokenBudget)
			_ = c.store.MarkStatus(taskID, TaskStatusFailed)
			return
		}

		// Interrupt check
		c.mu.Lock()
		interrupted := c.interrupted
		c.mu.Unlock()
		if interrupted || ctx.Err() != nil {
			return
		}

		subTask := tasks[idx]
		c.progress.OnSubTaskStart(idx, len(tasks), subTask)
		if err := c.store.UpdateSubTask(taskID, idx, SubTaskInProgress, "", 0); err != nil {
			log.Printf("[hermes] UpdateSubTask(in_progress) idx=%d: %v", idx, err)
		}

		// Set current sub-task for BuildExecutorPrompt
		state.CurrentIdx = idx

		// Execute with retries (validator failures handled inside executor.Execute)
		result, execErr := c.executor.Execute(ctx, state, c.cfg.ProjectDir)

		execTokens := result.InputTokens + result.OutputTokens
		_ = c.store.AddTokenUsage(taskID, execTokens)

		if execErr != nil {
			if err := c.store.UpdateSubTask(taskID, idx, SubTaskFailed, execErr.Error(), execTokens); err != nil {
				log.Printf("[hermes] UpdateSubTask(failed) idx=%d: %v", idx, err)
			}
			c.progress.OnSubTaskDone(idx, len(tasks), subTask, false, execErr.Error())
			continue
		}

		// Notify if there were retries
		if result.Attempts > 1 && result.ValidationError != "" {
			c.progress.OnRetry(idx, result.Attempts, c.executor.maxRetries, result.ValidationError)
		}

		success := result.ValidationError == ""
		finalStatus := SubTaskDone
		if !success {
			finalStatus = SubTaskFailed
		}
		if err := c.store.UpdateSubTask(taskID, idx, finalStatus, result.ResultText, execTokens); err != nil {
			log.Printf("[hermes] UpdateSubTask(%s) idx=%d: %v", finalStatus, idx, err)
		}
		c.progress.OnSubTaskDone(idx, len(tasks), subTask, success, result.ResultText)

		if success {
			completedCount++
			tasks[idx].Status = SubTaskDone
			tasks[idx].Result = result.ResultText
		}

		// Update accumulated summary
		state, _ = c.store.GetTask(taskID)
		updated, needsCompress := AppendResult(state.Accumulated, result.ResultText, completedCount, accCfg)
		_ = c.store.UpdateAccumulated(taskID, updated)

		if needsCompress {
			c.runCompression(ctx, taskID, state, updated)
		}
	}

	// ── Phase 3: Done ─────────────────────────────────────────────────────────
	_ = c.store.MarkStatus(taskID, TaskStatusDone)
	finalState, _ := c.store.GetTask(taskID)
	c.progress.OnDone(finalState)
}

// runCompression calls the Planner to compress the accumulated summary.
func (c *Coordinator) runCompression(ctx context.Context, taskID string, state TaskState, accumulated string) {
	req := CompressRequest{
		Goal:        state.Goal,
		Accumulated: accumulated,
		Artifacts:   state.Artifacts,
	}
	compressed, tokens, err := c.planner.Compress(ctx, req, c.cfg.ProjectDir)
	if err != nil {
		log.Printf("[hermes] compression failed for task %s: %v", taskID, err)
		return
	}
	_ = c.store.UpdateAccumulated(taskID, compressed)
	_ = c.store.AddTokenUsage(taskID, tokens)
}

// newUUID returns a random UUID string.
func newUUID() string {
	return uuid.New().String()
}
