package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

type planExecuteRunner struct {
	prompts []string
}

func (r *planExecuteRunner) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	r.prompts = append(r.prompts, userMessage)
	return "result for " + userMessage[strings.LastIndex(userMessage, "\n")+1:], nil
}

type planExecuteReporter struct {
	events []string
}

func (r *planExecuteReporter) OnPlanReady(tasks []hermes.SubTask) {
	r.events = append(r.events, "plan")
}

func (r *planExecuteReporter) OnSubTaskStart(idx, total int, task hermes.SubTask) {
	r.events = append(r.events, "start:"+task.ID)
}

func (r *planExecuteReporter) OnSubTaskDone(idx, total int, task hermes.SubTask, success bool, result string) {
	r.events = append(r.events, "done:"+task.ID)
}

func (r *planExecuteReporter) OnRetry(idx, attempt, maxAttempts int, validationErr string) {}
func (r *planExecuteReporter) OnDone(state hermes.TaskState) {
	r.events = append(r.events, "complete:"+string(state.Status))
}
func (r *planExecuteReporter) OnBudgetWarning(budget hermes.TokenBudget) {}
func (r *planExecuteReporter) OnError(err error) {
	r.events = append(r.events, "error:"+err.Error())
}

type recordingReviewPhase struct {
	calls int
	last  ReviewRequest
}

func (r *recordingReviewPhase) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	r.calls++
	r.last = req
	return ReviewResult{
		ReviewerModel: "gpt-5.5",
		Verdict:       VerdictPass,
		OverallScore:  88,
		Feedback:      "review ok",
		SubTaskResults: []ReviewSubTaskResult{
			{SubTaskID: "s1", Score: 90, Feedback: "good"},
		},
		InputTokens:  12,
		OutputTokens: 8,
		CostUSD:      0.42,
	}, nil
}

type scriptedReviewPhase struct {
	results []ReviewResult
	calls   int
}

func (s *scriptedReviewPhase) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	idx := s.calls
	s.calls++
	if len(s.results) == 0 {
		return ReviewResult{
			Verdict:      VerdictPass,
			OverallScore: 100,
		}, nil
	}
	if idx >= len(s.results) {
		return s.results[len(s.results)-1], nil
	}
	return s.results[idx], nil
}

type recordingReviewStore struct {
	calls  int
	lastID string
	last   ReviewResult
}

func (s *recordingReviewStore) StoreReview(ctx context.Context, taskID string, review ReviewResult) error {
	s.calls++
	s.lastID = taskID
	s.last = review
	return nil
}

type recordingReviewNotifier struct {
	calls       int
	lastTaskID  string
	lastReview  ReviewResult
	lastSummary ReviewNotification
}

func (n *recordingReviewNotifier) Notify(ctx context.Context, state hermes.TaskState, review ReviewResult, notification ReviewNotification) {
	n.calls++
	n.lastTaskID = state.ID
	n.lastReview = review
	n.lastSummary = notification
}

func TestPlanExecuteEngineRunsPlannedSubTasksThroughDirectEngine(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reporter := &planExecuteReporter{}
	reviewPhase := &recordingReviewPhase{}
	reviewStore := &recordingReviewStore{}
	reviewNotifier := &recordingReviewNotifier{}
	planFn := func(ctx context.Context, message, projectDir string) (string, string, int, int, error) {
		return "```json\n" +
			`[{"id":"s1","description":"read context","tool_hints":["Read"]},` +
			`{"id":"s2","description":"edit code","tool_hints":["Edit"]}]` +
			"\n```", "planner-session", 11, 7, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		InterruptPolicy:       hermes.InterruptQueue,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		AccumulatedCfg:        hermes.AccumulatedConfig{},
		ReviewPhase:           reviewPhase,
		ReviewStore:           reviewStore,
		OnReview:              reviewNotifier.Notify,
	}, planFn, NewDirectEngine(runner), store, reporter)

	taskID, err := engine.Start(context.Background(), "complex implementation goal", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if state.Status != hermes.TaskStatusDone {
		t.Fatalf("status = %s, want done", state.Status)
	}
	if len(state.Plan) != 2 {
		t.Fatalf("plan length = %d, want 2", len(state.Plan))
	}
	if state.Plan[0].Status != hermes.SubTaskDone || state.Plan[1].Status != hermes.SubTaskDone {
		t.Fatalf("sub-task statuses = %#v", state.Plan)
	}
	if len(runner.prompts) != 2 {
		t.Fatalf("direct runner calls = %d, want 2", len(runner.prompts))
	}
	if !strings.Contains(runner.prompts[1], "Completed sub-task results so far") {
		t.Fatalf("second prompt did not include accumulated context:\n%s", runner.prompts[1])
	}

	wantEvents := []string{"plan", "start:s1", "done:s1", "start:s2", "done:s2", "complete:done"}
	if !reflect.DeepEqual(reporter.events, wantEvents) {
		t.Fatalf("events:\n got %#v\nwant %#v", reporter.events, wantEvents)
	}
	if len(state.ModelUsages) != 1 || state.ModelUsages[0].Model != "planner-model" || state.ModelUsages[0].InputTokens != 11 || state.ModelUsages[0].OutputTokens != 7 {
		t.Fatalf("model usage = %#v", state.ModelUsages)
	}
	if reviewPhase.calls != 1 {
		t.Fatalf("review calls = %d, want 1", reviewPhase.calls)
	}
	if reviewStore.calls != 1 || reviewStore.lastID != taskID {
		t.Fatalf("review store = %+v", reviewStore)
	}
	if reviewStore.last.Verdict != VerdictPass || reviewStore.last.ReviewerModel != "gpt-5.5" {
		t.Fatalf("review store payload = %+v", reviewStore.last)
	}
	if reviewPhase.last.TaskID != taskID || reviewPhase.last.Accumulated == "" {
		t.Fatalf("review request missing context: %+v", reviewPhase.last)
	}
	if reviewNotifier.calls != 1 || reviewNotifier.lastSummary.TaskID != taskID || reviewNotifier.lastSummary.AdvisoryRetry {
		t.Fatalf("review notifier = %+v", reviewNotifier)
	}
}

func TestPlanExecuteEngineSkipsReviewForSingleSubTask(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reviewPhase := &recordingReviewPhase{}
	reviewStore := &recordingReviewStore{}
	planFn := func(ctx context.Context, message, projectDir string) (string, string, int, int, error) {
		return "```json\n" +
			`[{"id":"s1","description":"execute directly"}]` +
			"\n```", "", 0, 0, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		InterruptPolicy:       hermes.InterruptQueue,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		AccumulatedCfg:        hermes.AccumulatedConfig{},
		ReviewPhase:           reviewPhase,
		ReviewStore:           reviewStore,
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	taskID, err := engine.Start(context.Background(), "short goal", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if state.Status != hermes.TaskStatusDone {
		t.Fatalf("status = %s, want done", state.Status)
	}
	if reviewPhase.calls != 0 || reviewStore.calls != 0 {
		t.Fatalf("review should be skipped for single sub-task: phase=%d store=%d", reviewPhase.calls, reviewStore.calls)
	}
}

func TestPlanExecuteEngineDoesNotRetryFailedSubTask(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &failingOnceRunner{}
	planFn := func(ctx context.Context, message, projectDir string) (string, string, int, int, error) {
		return "```json\n" +
			`[{"id":"s1","description":"first"}]` +
			"\n```", "", 0, 0, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		InterruptPolicy:       hermes.InterruptQueue,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		AccumulatedCfg:        hermes.AccumulatedConfig{},
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	taskID, err := engine.Start(context.Background(), "goal", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(runner.prompts) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.prompts))
	}
	if state.Plan[0].Status != hermes.SubTaskFailed {
		t.Fatalf("first sub-task status = %s, want failed", state.Plan[0].Status)
	}
}

func TestPlanExecuteEngineRetriesBlockedSubTaskAndContinues(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reviewPhase := &scriptedReviewPhase{
		results: []ReviewResult{
			{
				Verdict:      VerdictPass,
				OverallScore: 75,
				Feedback:     "補上 runtime validation",
				IssueTags:    []ReviewTag{ReviewTagScopeCreep},
			},
			{
				Verdict:      VerdictPass,
				OverallScore: 92,
				Feedback:     "ok",
			},
			{
				Verdict:      VerdictPass,
				OverallScore: 95,
				Feedback:     "ok",
			},
		},
	}
	reviewStore := &recordingReviewStore{}
	planFn := func(ctx context.Context, message, projectDir string) (string, string, int, int, error) {
		return "```json\n" +
			`[{"id":"s1","description":"first"},{"id":"s2","description":"second"}]` +
			"\n```", "", 0, 0, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		InterruptPolicy:       hermes.InterruptQueue,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		AccumulatedCfg:        hermes.AccumulatedConfig{},
		ReviewPhase:           reviewPhase,
		ReviewStore:           reviewStore,
		ReviewMode:            ReviewModePerSubTask,
		StrictMode:            StrictModeConfig{Enabled: true, MaxRetriesPerSub: 2},
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	taskID, err := engine.Start(context.Background(), "請實作 strict goal 流程", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(runner.prompts) != 3 {
		t.Fatalf("runner calls = %d, review calls = %d, prompts = %#v, state = %#v", len(runner.prompts), reviewPhase.calls, runner.prompts, state.Plan)
	}
	if !strings.Contains(runner.prompts[1], "Reviewer feedback to address before retrying") || !strings.Contains(runner.prompts[1], "scope_creep") {
		t.Fatalf("retry prompt missing reviewer feedback:\n%s", runner.prompts[1])
	}
	if state.Plan[0].Status != hermes.SubTaskDone || state.Plan[1].Status != hermes.SubTaskDone {
		t.Fatalf("sub-task statuses = %#v", state.Plan)
	}
	if state.Plan[0].Attempts != 2 || state.Plan[1].Attempts != 1 {
		t.Fatalf("unexpected attempts: %#v", state.Plan)
	}
	if reviewPhase.calls != 3 || reviewStore.calls != 3 {
		t.Fatalf("review calls = %d store calls = %d, want 3/3", reviewPhase.calls, reviewStore.calls)
	}
}

func TestPlanExecuteEngineMarksPartialAfterStrictRetryExhaustion(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reviewPhase := &scriptedReviewPhase{
		results: []ReviewResult{
			{
				Verdict:      VerdictPass,
				OverallScore: 75,
				Feedback:     "加上 runtime validation",
				IssueTags:    []ReviewTag{ReviewTagScopeCreep},
			},
			{
				Verdict:      VerdictPass,
				OverallScore: 75,
				Feedback:     "仍然缺少 runtime validation",
				IssueTags:    []ReviewTag{ReviewTagScopeCreep},
			},
			{
				Verdict:      VerdictPass,
				OverallScore: 93,
				Feedback:     "ok",
			},
		},
	}
	planFn := func(ctx context.Context, message, projectDir string) (string, string, int, int, error) {
		return "```json\n" +
			`[{"id":"s1","description":"first"},{"id":"s2","description":"second"}]` +
			"\n```", "", 0, 0, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		InterruptPolicy:       hermes.InterruptQueue,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		AccumulatedCfg:        hermes.AccumulatedConfig{},
		ReviewPhase:           reviewPhase,
		ReviewMode:            ReviewModePerSubTask,
		StrictMode:            StrictModeConfig{Enabled: true, MaxRetriesPerSub: 1},
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	taskID, err := engine.Start(context.Background(), "請實作 strict goal 流程", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(runner.prompts) != 3 {
		t.Fatalf("runner calls = %d, review calls = %d, prompts = %#v, state = %#v", len(runner.prompts), reviewPhase.calls, runner.prompts, state.Plan)
	}
	if state.Plan[0].Status != hermes.SubTaskSkipped {
		t.Fatalf("first sub-task status = %s, want skipped/partial", state.Plan[0].Status)
	}
	if !strings.Contains(state.Plan[0].Result, "PARTIAL") || !strings.Contains(state.Accumulated, "PARTIAL") {
		t.Fatalf("partial result not annotated:\nstate=%#v", state.Plan[0])
	}
	if state.Plan[1].Status != hermes.SubTaskDone {
		t.Fatalf("second sub-task status = %s, want done", state.Plan[1].Status)
	}
	if reviewPhase.calls != 3 {
		t.Fatalf("review calls = %d, want 3", reviewPhase.calls)
	}
}

type failingOnceRunner struct {
	prompts []string
}

func (r *failingOnceRunner) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	r.prompts = append(r.prompts, userMessage)
	if len(r.prompts) == 1 {
		return "", context.DeadlineExceeded
	}
	return "ok", nil
}

func waitForPlanExecute(t *testing.T, engine *PlanExecuteEngine) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !engine.IsRunning() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("plan execute engine still running after timeout")
}
