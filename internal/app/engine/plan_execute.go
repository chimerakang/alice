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
)

// PlanExecuteConfig holds the runtime configuration for a Hermes-style
// plan-execute run backed by DirectEngine for each sub-task.
type PlanExecuteConfig struct {
	PlannerModel string
	ProjectDir   string
	ChatID       int64

	MaxPlannerJSONRetries int
	InterruptPolicy       hermes.InterruptPolicy
	Budget                hermes.TokenBudget
	AccumulatedCfg        hermes.AccumulatedConfig
	PlannerRules          string
	PlannerSessionID      string

	GithubIssueNumber int
	GithubCfg         hermes.GithubCfg

	PostCompletionHook func(ctx context.Context)

	ReviewPhase       ReviewPhase
	ReviewStore       ReviewResultStore
	DisableReview     bool
	ReviewMinSubTasks int

	ContinueCh      chan struct{}
	ContinueTimeout time.Duration

	OnDone func(ctx context.Context, state hermes.TaskState)
}

// PlanExecuteEngine plans a goal and executes each sub-task through DirectEngine.
type PlanExecuteEngine struct {
	cfg      PlanExecuteConfig
	planner  *hermes.PlannerSession
	direct   *DirectEngine
	store    hermes.TaskStateStore
	reporter hermes.ProgressReporter

	mu          sync.Mutex
	taskID      string
	cancelFn    context.CancelFunc
	interrupted bool
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
	return &PlanExecuteEngine{
		cfg:      cfg,
		planner:  hermes.NewPlannerSession(planFn, cfg.MaxPlannerJSONRetries, cfg.PlannerRules),
		direct:   direct,
		store:    store,
		reporter: reporter,
	}
}

func (e *PlanExecuteEngine) Name() string {
	return "plan_execute"
}

func (e *PlanExecuteEngine) TaskID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.taskID
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
		Goal:              goal,
		Status:            hermes.TaskStatusPlanning,
		InterruptPolicy:   e.cfg.InterruptPolicy,
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

func (e *PlanExecuteEngine) run(ctx context.Context, taskID, goal string, cc *ChatContext) {
	defer func() {
		e.mu.Lock()
		e.cancelFn = nil
		e.taskID = ""
		e.mu.Unlock()
	}()

	tasks, planIn, planOut, plannerSkipped, err := e.plan(ctx, goal)
	if err != nil {
		e.handlePlanningError(ctx, taskID, err)
		return
	}
	if !plannerSkipped {
		_ = e.store.AddTokenUsage(taskID, planIn+planOut)
		_ = e.store.AddModelUsage(taskID, e.cfg.PlannerModel, planIn, planOut)
		if sid := e.planner.SessionID(); sid != "" {
			_ = e.store.UpdatePlannerSession(taskID, sid)
		}
	}
	if len(tasks) > 15 {
		e.reporter.OnError(fmt.Errorf("complexity violation: plan has %d sub-tasks (max 15)", len(tasks)))
		_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
		return
	}
	if err := e.store.StorePlan(taskID, tasks); err != nil {
		e.reporter.OnError(fmt.Errorf("persist plan: %w", err))
		_ = e.store.MarkStatus(taskID, hermes.TaskStatusFailed)
		return
	}
	if err := e.store.MarkStatus(taskID, hermes.TaskStatusExecuting); err != nil {
		e.reporter.OnError(err)
		return
	}
	e.reporter.OnPlanReady(tasks)
	state, _ := e.store.GetTask(taskID)
	e.onPlanReady(ctx, state, tasks)

	completed := 0
	for idx := range tasks {
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
		e.reporter.OnSubTaskStart(idx, len(tasks), subTask)
		if err := e.store.UpdateSubTask(taskID, idx, hermes.SubTaskInProgress, "", 0); err != nil {
			log.Printf("[plan_execute] UpdateSubTask(in_progress) idx=%d: %v", idx, err)
		}

		result, execErr := e.direct.Run(ctx, buildSubTaskGoal(goal, state.Accumulated, idx, len(tasks), subTask), cc, subTaskSink{})
		if execErr != nil {
			if err := e.store.UpdateSubTask(taskID, idx, hermes.SubTaskFailed, execErr.Error(), 0); err != nil {
				log.Printf("[plan_execute] UpdateSubTask(failed) idx=%d: %v", idx, err)
			}
			e.reporter.OnSubTaskDone(idx, len(tasks), subTask, false, execErr.Error())
			continue
		}

		text := strings.TrimSpace(result.Text)
		if err := e.store.UpdateSubTask(taskID, idx, hermes.SubTaskDone, text, result.InputTokens+result.OutputTokens); err != nil {
			log.Printf("[plan_execute] UpdateSubTask(done) idx=%d: %v", idx, err)
		}
		e.reporter.OnSubTaskDone(idx, len(tasks), subTask, true, text)
		completed++
		tasks[idx].Status = hermes.SubTaskDone
		tasks[idx].Result = text
		e.onSubTaskDone(ctx, idx, len(tasks), tasks, subTask, text, result.InputTokens+result.OutputTokens, completed)

		state, _ = e.store.GetTask(taskID)
		updated, _ := hermes.AppendResult(state.Accumulated, text, completed, e.cfg.AccumulatedCfg)
		_ = e.store.UpdateAccumulated(taskID, updated)
	}

	_ = e.store.MarkStatus(taskID, hermes.TaskStatusDone)
	finalState, _ := e.store.GetTask(taskID)
	e.runReview(ctx, finalState)
	e.reporter.OnDone(finalState)
	if e.cfg.OnDone != nil {
		e.cfg.OnDone(ctx, finalState)
	}
	e.onDone(ctx, finalState, completed, len(tasks))
	if e.cfg.PostCompletionHook != nil {
		go e.cfg.PostCompletionHook(ctx)
	}
}

func (e *PlanExecuteEngine) plan(ctx context.Context, goal string) ([]hermes.SubTask, int, int, bool, error) {
	if hermes.ClassifyGoal(goal) == hermes.GoalSimple {
		return []hermes.SubTask{{
			ID:          "s1",
			Description: "Execute the goal directly: " + goal,
			Status:      hermes.SubTaskPending,
		}}, 0, 0, true, nil
	}
	tasks, inT, outT, err := e.planner.Plan(ctx, goal, e.cfg.ProjectDir)
	return tasks, inT, outT, false, err
}

func (e *PlanExecuteEngine) handlePlanningError(ctx context.Context, taskID string, err error) {
	var jfail *hermes.ErrPlannerJSONFailed
	if errors.As(err, &jfail) {
		e.reporter.OnError(fmt.Errorf("Planner JSON 解析失敗，降級回一般模式：%v", err))
	} else {
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
	if ghCfg.ShouldComment("start") {
		body := hermes.CommentStarted(e.cfg.PlannerModel, "direct")
		if err := hermes.PostComment(ctx, e.cfg.ProjectDir, issueNum, body); err != nil {
			log.Printf("[plan_execute] GitHub comment start: %v", err)
		}
	}
	if ghCfg.SyncChecklist {
		if err := hermes.WritePlanToIssue(ctx, e.cfg.ProjectDir, issueNum, state.Goal, tasks); err != nil {
			log.Printf("[plan_execute] GitHub write plan: %v", err)
		}
	}
}

func (e *PlanExecuteEngine) onSubTaskDone(ctx context.Context, idx, total int, tasks []hermes.SubTask, subTask hermes.SubTask, result string, tokens, completed int) {
	issueNum := e.cfg.GithubIssueNumber
	ghCfg := e.cfg.GithubCfg
	if issueNum <= 0 || !ghCfg.Enabled {
		return
	}
	if ghCfg.SyncChecklist {
		if err := hermes.SyncChecklist(ctx, e.cfg.ProjectDir, issueNum, tasks); err != nil {
			log.Printf("[plan_execute] GitHub sync checklist idx=%d: %v", idx, err)
		}
	}
	if ghCfg.ShouldComment("complete") {
		body := hermes.CommentSubTaskProgress(idx, total, subTask, result, tokens, completed)
		if err := hermes.PostComment(ctx, e.cfg.ProjectDir, issueNum, body); err != nil {
			log.Printf("[plan_execute] GitHub comment subtask progress idx=%d: %v", idx, err)
		}
	}
}

func (e *PlanExecuteEngine) onDone(ctx context.Context, finalState hermes.TaskState, completed, total int) {
	issueNum := e.cfg.GithubIssueNumber
	ghCfg := e.cfg.GithubCfg
	if issueNum <= 0 || !ghCfg.Enabled {
		return
	}
	if ghCfg.ShouldComment("complete") {
		body := hermes.CommentDone(finalState)
		if err := hermes.PostComment(ctx, e.cfg.ProjectDir, issueNum, body); err != nil {
			log.Printf("[plan_execute] GitHub comment done: %v", err)
		}
	}
	if completed == total && ghCfg.AutoCloseLabel != "" {
		if issue, err := hermes.FetchIssue(ctx, e.cfg.ProjectDir, issueNum); err == nil && hermes.HasLabel(issue, ghCfg.AutoCloseLabel) {
			if err := hermes.CloseIssue(ctx, e.cfg.ProjectDir, issueNum); err != nil {
				log.Printf("[plan_execute] GitHub close issue: %v", err)
			}
		}
	}
}

func (e *PlanExecuteEngine) onBudgetExceeded(ctx context.Context, state hermes.TaskState) {
	issueNum := e.cfg.GithubIssueNumber
	ghCfg := e.cfg.GithubCfg
	if issueNum <= 0 || !ghCfg.Enabled {
		return
	}
	if ghCfg.ShouldComment("budget_exceeded") {
		body := hermes.CommentBudgetExceeded(state.TokenBudget.UsedTokens, state.TokenBudget.MaxTotalTokens)
		if err := hermes.PostComment(ctx, e.cfg.ProjectDir, issueNum, body); err != nil {
			log.Printf("[plan_execute] GitHub comment budget: %v", err)
		}
	}
	if ghCfg.FailureLabel != "" {
		if err := hermes.ApplyLabel(ctx, e.cfg.ProjectDir, issueNum, ghCfg.FailureLabel); err != nil {
			log.Printf("[plan_execute] GitHub apply failure label on budget: %v", err)
		}
	}
}

func (e *PlanExecuteEngine) runReview(ctx context.Context, state hermes.TaskState) {
	if !e.shouldRunReview(state.Plan) {
		return
	}
	if e.cfg.ReviewPhase == nil {
		return
	}

	reviewReq := ReviewRequest{
		TaskID:         state.ID,
		ProjectDir:     e.cfg.ProjectDir,
		Goal:           state.Goal,
		Accumulated:    state.Accumulated,
		Plan:           append([]hermes.SubTask(nil), state.Plan...),
		SubTaskResults: ReviewInputsFromPlan(state.Plan),
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

	result, err := e.cfg.ReviewPhase.Review(ctx, reviewReq)
	if err != nil {
		log.Printf("[plan_execute] review failed: %v", err)
		return
	}
	if err := result.Validate(); err != nil {
		log.Printf("[plan_execute] review invalid: %v", err)
		return
	}
	if e.cfg.ReviewStore != nil {
		if err := e.cfg.ReviewStore.StoreReview(ctx, state.ID, result); err != nil {
			log.Printf("[plan_execute] store review: %v", err)
		}
	}
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

func buildSubTaskGoal(goal, accumulated string, idx, total int, subTask hermes.SubTask) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Original goal:\n%s\n\n", goal)
	if strings.TrimSpace(accumulated) != "" {
		fmt.Fprintf(&b, "Completed sub-task results so far:\n%s\n\n", accumulated)
	}
	fmt.Fprintf(&b, "Current sub-task (%d/%d):\n%s", idx+1, total, subTask.Description)
	return b.String()
}

type subTaskSink struct{}

func (subTaskSink) OnSubTaskStart(idx, total int, desc string)  {}
func (subTaskSink) OnToolUse(tool string, input map[string]any) {}
func (subTaskSink) OnContent(kind, text string)                 {}
func (subTaskSink) OnSubTaskDone(idx int, result string)        {}
func (subTaskSink) OnComplete(summary string)                   {}
