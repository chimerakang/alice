package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"claude-tg-agent/internal/app/hermes"
	"claude-tg-agent/internal/app/issueops"
)

type planExecuteRunner struct {
	prompts []string
}

func (r *planExecuteRunner) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	r.prompts = append(r.prompts, userMessage)
	return "result for " + userMessage[strings.LastIndex(userMessage, "\n")+1:], nil
}

type tokenMetricsRunner struct {
	model      string
	input      int
	output     int
	cacheRead  int
	cacheWrite int
	cost       float64
}

func (r *tokenMetricsRunner) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	return "ok", nil
}

func (r *tokenMetricsRunner) LastCallMetrics() (string, int, int, float64) {
	return r.model, r.input, r.output, r.cost
}

func (r *tokenMetricsRunner) LastCacheMetrics() (int, int) {
	return r.cacheRead, r.cacheWrite
}

type staticRunner struct {
	result string
}

func (r *staticRunner) Run(userMessage string, onUpdate func(string, bool)) (string, error) {
	return r.result, nil
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

type statusRecordingStore struct {
	hermes.TaskStateStore
	statuses []hermes.TaskStatus
}

func (s *statusRecordingStore) MarkStatus(taskID string, status hermes.TaskStatus) error {
	s.statuses = append(s.statuses, status)
	return s.TaskStateStore.MarkStatus(taskID, status)
}

// CommitRuntimeStep delegates to the embedded store when it satisfies
// RuntimeStepStore. Records the resulting status in s.statuses ONLY when
// the commit's StateUpdate explicitly set Status (i.e. a status transition,
// not telemetry / interrupt-only updates). This matches the semantics of
// the legacy MarkStatus tracking before #169 slice 3b routed all engine
// writes through CommitRuntimeStep.
func (s *statusRecordingStore) CommitRuntimeStep(commit hermes.RuntimeCommit) (hermes.Snapshot, error) {
	rt, ok := s.TaskStateStore.(hermes.RuntimeStepStore)
	if !ok {
		return hermes.Snapshot{}, fmt.Errorf("statusRecordingStore: embedded store does not implement RuntimeStepStore")
	}
	statusWritten := false
	for _, u := range commit.Updates {
		if u.Status != nil {
			statusWritten = true
			break
		}
	}
	snap, err := rt.CommitRuntimeStep(commit)
	if err == nil && statusWritten && snap.State.Status != "" {
		// Each commit that writes Status adds an entry. After #169 slice 3a
		// commitExecutorBoundary skips Status when it would be a no-op, so
		// the recording only fires for genuine phase transitions
		// (planner/replan ready and terminal commits).
		s.statuses = append(s.statuses, snap.State.Status)
	}
	return snap, err
}

type fakeIssueOps struct {
	mapping      issueops.ChecklistMappingResult
	mappingErr   error
	syncResult   issueops.SyncChecklistResult
	syncErr      error
	recorded     []issueops.RecordEvidenceRequest
	syncRequests []issueops.SyncChecklistRequest
	loadCalls    int
	callOrder    []string
}

func (f *fakeIssueOps) LoadIssue(ctx context.Context, projectDir string, issueNumber int) (*hermes.IssueContext, error) {
	return nil, nil
}

func (f *fakeIssueOps) LoadIssueChecklistMapping(ctx context.Context, projectDir string, issueNumber int, subtasks []hermes.SubTask) (issueops.ChecklistMappingResult, error) {
	f.loadCalls++
	f.callOrder = append(f.callOrder, "load_mapping")
	if f.mappingErr != nil {
		return issueops.ChecklistMappingResult{}, f.mappingErr
	}
	return f.mapping, nil
}

func (f *fakeIssueOps) CommentDone(ctx context.Context, projectDir string, issueNumber int, finalState hermes.TaskState, notes string) error {
	return nil
}

func (f *fakeIssueOps) CommentBudgetExceeded(ctx context.Context, projectDir string, issueNumber int, used, max int) error {
	return nil
}

func (f *fakeIssueOps) ApplyLabel(ctx context.Context, projectDir string, issueNumber int, label string) error {
	return nil
}

func (f *fakeIssueOps) PlanIssue(ctx context.Context, req issueops.PlanIssueRequest) error {
	return nil
}

func (f *fakeIssueOps) RecordEvidence(ctx context.Context, req issueops.RecordEvidenceRequest) error {
	f.callOrder = append(f.callOrder, "record_evidence")
	f.recorded = append(f.recorded, req)
	return nil
}

func (f *fakeIssueOps) SyncChecklist(ctx context.Context, req issueops.SyncChecklistRequest) (issueops.SyncChecklistResult, error) {
	f.callOrder = append(f.callOrder, "sync_checklist")
	f.syncRequests = append(f.syncRequests, req)
	return f.syncResult, f.syncErr
}

func (f *fakeIssueOps) AssessCloseReadiness(ctx context.Context, req issueops.AssessCloseReadinessRequest) (issueops.CloseReadinessResult, error) {
	return issueops.CloseReadinessResult{}, nil
}

func (f *fakeIssueOps) CloseIssue(ctx context.Context, req issueops.CloseIssueRequest) (issueops.CloseReadinessResult, error) {
	return issueops.CloseReadinessResult{}, nil
}

func TestPlanExecuteEngineRunsPlannedSubTasksThroughDirectEngine(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reporter := &planExecuteReporter{}
	reviewPhase := &recordingReviewPhase{}
	reviewStore := &recordingReviewStore{}
	reviewNotifier := &recordingReviewNotifier{}
	var runtimeEvents []Event
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"read context","tool_hints":["Read"]},` +
				`{"id":"s2","description":"edit code","tool_hints":["Edit"]}]` +
				"\n```",
			SessionID:    "planner-session",
			InputTokens:  11,
			OutputTokens: 7,
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		ReviewPhase:           reviewPhase,
		ReviewStore:           reviewStore,
		OnReview:              reviewNotifier.Notify,
		OnRuntimeEvent: func(ctx context.Context, event Event) {
			runtimeEvents = append(runtimeEvents, event)
		},
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
	history, err := store.ListSnapshotHistory(taskID)
	if err != nil {
		t.Fatalf("ListSnapshotHistory: %v", err)
	}
	if len(history) < 4 {
		t.Fatalf("snapshot history length = %d, want at least plan, sub-tasks, terminal", len(history))
	}
	if history[0].SourceNode != hermes.RuntimeStepPlanner || history[len(history)-1].NextStep != hermes.RuntimeStepTerminal {
		t.Fatalf("snapshot boundary mismatch: first=%+v last=%+v", history[0], history[len(history)-1])
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
	var planGate *Event
	for i := range runtimeEvents {
		if runtimeEvents[i].Type == "PlanQualityGate" {
			planGate = &runtimeEvents[i]
			break
		}
	}
	if planGate == nil {
		t.Fatalf("runtime events = %#v, want PlanQualityGate", runtimeEvents)
	}
	payload, ok := planGate.Payload.(map[string]any)
	if !ok || payload["action"] != "allow" || payload["reason"] != "gate_passed" || payload["task_count"] != 2 {
		t.Fatalf("PlanQualityGate payload = %#v", planGate.Payload)
	}
	// #148 1E: ModelUsages now also records reviewer's tokens + cost. Assert
	// the planner row by lookup; reviewer row presence is asserted separately.
	var plannerUsage, reviewerUsage *hermes.ModelUsage
	for i := range state.ModelUsages {
		switch state.ModelUsages[i].Model {
		case "planner-model":
			plannerUsage = &state.ModelUsages[i]
		case "gpt-5.5":
			reviewerUsage = &state.ModelUsages[i]
		}
	}
	if plannerUsage == nil || plannerUsage.InputTokens != 11 || plannerUsage.OutputTokens != 7 {
		t.Fatalf("planner usage = %#v (full=%#v)", plannerUsage, state.ModelUsages)
	}
	if reviewerUsage == nil || reviewerUsage.CostUSD != 0.42 {
		t.Fatalf("reviewer usage missing or wrong cost: %#v (full=%#v)", reviewerUsage, state.ModelUsages)
	}
	plannerPhase := findPhaseUsage(state.PhaseUsages, "planner", "planner-model")
	if plannerPhase == nil || plannerPhase.InputTokens != 11 || plannerPhase.OutputTokens != 7 {
		t.Fatalf("planner phase usage = %#v (full=%#v)", plannerPhase, state.PhaseUsages)
	}
	reviewerPhase := findPhaseUsage(state.PhaseUsages, "reviewer", "gpt-5.5")
	if reviewerPhase == nil || reviewerPhase.InputTokens != 12 || reviewerPhase.OutputTokens != 8 || reviewerPhase.CostUSD != 0.42 {
		t.Fatalf("reviewer phase usage = %#v (full=%#v)", reviewerPhase, state.PhaseUsages)
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

func TestMapChecklistSyncEventBlockedIssueState(t *testing.T) {
	res := issueops.SyncChecklistResult{
		State: hermes.IssueStateBlocked,
		Guard: issueops.ChecklistSyncGuard{
			IssueState:       hermes.IssueStateInProgress,
			HasBlockingLabel: true,
		},
	}
	event, to, ok := mapChecklistSyncEvent(issueops.SyncOutcomeIssueState, res)
	if !ok {
		t.Fatal("mapChecklistSyncEvent ok = false, want true")
	}
	if event != hermes.IssueEventHumanDecisionRequired || to != hermes.IssueStateBlocked {
		t.Fatalf("event/to = %q/%q, want %q/%q", event, to, hermes.IssueEventHumanDecisionRequired, hermes.IssueStateBlocked)
	}
}

func TestPlanExecuteEngineSkipsReviewForSingleSubTask(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reviewPhase := &recordingReviewPhase{}
	reviewStore := &recordingReviewStore{}
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"execute directly"}]` +
				"\n```",
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
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

func TestPlanExecuteEngineTokenUsageIncludesCacheTokens(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &tokenMetricsRunner{
		model:      "claude-sonnet-4-5",
		input:      10,
		output:     3,
		cacheRead:  100,
		cacheWrite: 20,
		cost:       0.01,
	}
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text:         "```json\n" + `[{"id":"s1","description":"execute once","tool_hints":["Read"]}]` + "\n```",
			InputTokens:  5,
			OutputTokens: 2,
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		DisableReview:         true,
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	taskID, err := engine.Start(context.Background(), "implement token accounting feature", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	const wantExecutorTokens = 10 + 100 + 20 + 3
	const wantTotalTokens = 5 + 2 + wantExecutorTokens
	if state.TokenBudget.UsedTokens != wantTotalTokens {
		t.Fatalf("used tokens = %d, want %d", state.TokenBudget.UsedTokens, wantTotalTokens)
	}
	if len(state.Plan) != 1 || state.Plan[0].TokensUsed != wantExecutorTokens {
		t.Fatalf("sub-task tokens = %#v, want %d", state.Plan, wantExecutorTokens)
	}
	var executorUsage *hermes.ModelUsage
	for i := range state.ModelUsages {
		if state.ModelUsages[i].Model == "claude-sonnet-4-5" {
			executorUsage = &state.ModelUsages[i]
			break
		}
	}
	if executorUsage == nil || executorUsage.InputTokens != 10+100+20 || executorUsage.OutputTokens != 3 {
		t.Fatalf("executor usage = %#v (all=%#v)", executorUsage, state.ModelUsages)
	}
	if executorUsage.UncachedInputTokens != 10 || executorUsage.CacheReadInputTokens != 100 || executorUsage.CacheCreationInputTokens != 20 {
		t.Fatalf("executor usage cache breakdown = %#v", executorUsage)
	}
	executorPhase := findPhaseUsage(state.PhaseUsages, "executor", "claude-sonnet-4-5")
	if executorPhase == nil || executorPhase.InputTokens != 10+100+20 || executorPhase.OutputTokens != 3 || executorPhase.CostUSD != 0.01 {
		t.Fatalf("executor phase usage = %#v (all=%#v)", executorPhase, state.PhaseUsages)
	}
	if executorPhase.UncachedInputTokens != 10 || executorPhase.CacheReadInputTokens != 100 || executorPhase.CacheCreationInputTokens != 20 {
		t.Fatalf("executor phase cache breakdown = %#v", executorPhase)
	}
}

func TestPlanExecuteEngineRecordsMappingEvidenceBeforeChecklistSync(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	result := "**結論**：完成驗證\n\n**證據**：\n- `go test ./internal/app/engine -run TestPlanExecuteEngineRecordsMappingEvidenceBeforeChecklistSync` PASS\n\n**未驗證**：無\n\n**下一步**：無"
	runner := &staticRunner{result: result}
	ops := &fakeIssueOps{
		mapping: issueops.ChecklistMappingResult{
			State: hermes.IssueStateChecklistSynced,
			Mappings: []issueops.ChecklistMapping{
				{
					SubTaskIndex:       0,
					SubTaskID:          "s1",
					SubTaskDescription: "Run validation",
					ChecklistText:      "Run validation",
					Confidence:         issueops.ChecklistMappingConfidenceHigh,
					Score:              100,
				},
			},
		},
		syncResult: issueops.SyncChecklistResult{
			Outcome:    issueops.SyncOutcomeDryRun,
			DryRun:     true,
			WouldWrite: true,
			Guard: issueops.ChecklistSyncGuard{
				IssueState:        hermes.IssueStateInProgress,
				HasCompletedItems: true,
				HasBodyChange:     true,
			},
		},
	}
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" + `[{"id":"s1","description":"Run validation","tool_hints":["Bash"]}]` + "\n```",
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		DisableReview:         true,
		GithubIssueNumber:     17,
		GithubCfg: hermes.GithubCfg{
			Enabled:       true,
			CommentOnDone: true,
			SyncChecklist: true,
		},
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})
	engine.issueOps = ops

	taskID, err := engine.Start(context.Background(), "run validation", NewChatContext(42, 0, "/repo"))
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
	if !reflect.DeepEqual(ops.callOrder, []string{"load_mapping", "record_evidence", "sync_checklist"}) {
		t.Fatalf("call order = %#v", ops.callOrder)
	}
	if ops.loadCalls != 1 {
		t.Fatalf("load calls = %d, want 1", ops.loadCalls)
	}
	if len(ops.recorded) != 1 {
		t.Fatalf("recorded evidence calls = %d, want 1", len(ops.recorded))
	}
	if len(ops.syncRequests) != 1 {
		t.Fatalf("sync calls = %d, want 1", len(ops.syncRequests))
	}

	evidenceReq := ops.recorded[0]
	if evidenceReq.ChecklistMapping == nil || evidenceReq.ChecklistMapping.SubTaskID != "s1" {
		t.Fatalf("evidence mapping = %#v", evidenceReq.ChecklistMapping)
	}
	if evidenceReq.Validation == nil {
		t.Fatal("validation evidence = nil, want parsed command")
	}
	if evidenceReq.Validation.Command != "go test ./internal/app/engine -run TestPlanExecuteEngineRecordsMappingEvidenceBeforeChecklistSync" {
		t.Fatalf("validation command = %q", evidenceReq.Validation.Command)
	}
	if !evidenceReq.Validation.Passed || evidenceReq.Validation.ExitCode != 0 {
		t.Fatalf("validation evidence = %+v", evidenceReq.Validation)
	}

	syncReq := ops.syncRequests[0]
	if syncReq.ChecklistMapping == nil {
		t.Fatal("sync checklist mapping = nil, want loaded mapping result")
	}
	if syncReq.RequireHumanDecision {
		t.Fatalf("RequireHumanDecision = true, want false: %+v", syncReq)
	}
	if len(syncReq.ChecklistMapping.Mappings) != 1 || syncReq.ChecklistMapping.Mappings[0].SubTaskID != "s1" {
		t.Fatalf("sync mapping = %+v", syncReq.ChecklistMapping)
	}
}

func TestPlanExecuteEngineChecklistSyncRequiresHumanDecisionWhenMappingLoadFails(t *testing.T) {
	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		ProjectDir:        "/repo",
		GithubIssueNumber: 17,
		GithubCfg: hermes.GithubCfg{
			Enabled:       true,
			CommentOnDone: true,
			SyncChecklist: true,
		},
	}, nil, NewDirectEngine(&planExecuteRunner{}), hermes.NewMemoryTaskStore(), &planExecuteReporter{})
	ops := &fakeIssueOps{
		mappingErr: errors.New("gh issue view failed"),
		syncResult: issueops.SyncChecklistResult{
			Outcome: issueops.SyncOutcomeNeedsHuman,
			Guard: issueops.ChecklistSyncGuard{
				NeedsHumanConfirmation: true,
			},
		},
	}
	engine.issueOps = ops

	subTask := hermes.SubTask{ID: "s1", Description: "Run validation"}
	tasks := []hermes.SubTask{subTask}
	result := "**結論**：完成驗證\n\n**證據**：\n- `go test ./internal/app/engine -run TestPlanExecuteEngineChecklistSyncRequiresHumanDecisionWhenMappingLoadFails` PASS\n\n**未驗證**：無\n\n**下一步**：無"
	engine.onSubTaskDone(context.Background(), 0, len(tasks), tasks, subTask, result, 12, 1)

	if !reflect.DeepEqual(ops.callOrder, []string{"load_mapping", "record_evidence", "sync_checklist"}) {
		t.Fatalf("call order = %#v", ops.callOrder)
	}
	if len(ops.syncRequests) != 1 {
		t.Fatalf("sync calls = %d, want 1", len(ops.syncRequests))
	}
	if !ops.syncRequests[0].RequireHumanDecision {
		t.Fatalf("RequireHumanDecision = false, want true: %+v", ops.syncRequests[0])
	}
	if ops.syncRequests[0].ChecklistMapping != nil {
		t.Fatalf("sync checklist mapping = %+v, want nil on load failure", ops.syncRequests[0].ChecklistMapping)
	}
}

func findPhaseUsage(usages []hermes.PhaseUsage, phase, model string) *hermes.PhaseUsage {
	for i := range usages {
		if usages[i].Phase == phase && usages[i].Model == model {
			return &usages[i]
		}
	}
	return nil
}

func TestPlanExecuteEngineDoesNotRetryFailedSubTask(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &failingOnceRunner{}
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"first"}]` +
				"\n```",
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
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

func TestPlanExecuteEngineUsesValidatingDuringTaskReviewRetry(t *testing.T) {
	baseStore := hermes.NewMemoryTaskStore()
	store := &statusRecordingStore{TaskStateStore: baseStore}
	runner := &planExecuteRunner{}
	reviewPhase := &scriptedReviewPhase{
		results: []ReviewResult{
			{Verdict: VerdictFail, OverallScore: 40, Feedback: "missing validation"},
			{Verdict: VerdictPass, OverallScore: 95, Feedback: "ok"},
		},
	}
	planCalls := 0
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		planCalls++
		if planCalls == 1 {
			return hermes.CallPlanResult{
				Text: "```json\n" +
					`[{"id":"s1","description":"first"},{"id":"s2","description":"second"}]` +
					"\n```",
			}, nil
		}
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"replanned first"},{"id":"s2","description":"replanned second"}]` +
				"\n```",
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		ReviewPhase:           reviewPhase,
		ReviewMode:            ReviewModePerTask,
		TaskRetry:             TaskRetryConfig{Enabled: true, ScoreThreshold: 70, MaxTaskRetries: 1},
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	// Goal must contain an implementation verb so hermes.ClassifyGoal returns
	// GoalNeedsPlanner — otherwise the Complexity Gate synthesises a single
	// sub-task and skips the planner + review entirely. We're specifically
	// exercising the per-task review-and-retry loop, which only fires when
	// the plan has >= ReviewMinSubTasks (2) sub-tasks.
	taskID, err := engine.Start(context.Background(), "implement complex retry goal", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := baseStore.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if state.Status != hermes.TaskStatusDone {
		t.Fatalf("status = %s, want done", state.Status)
	}
	wantStatuses := []hermes.TaskStatus{
		hermes.TaskStatusExecuting,
		hermes.TaskStatusExecuting,
		hermes.TaskStatusDone,
	}
	if !reflect.DeepEqual(store.statuses, wantStatuses) {
		t.Fatalf("statuses:\n got %#v\nwant %#v", store.statuses, wantStatuses)
	}
	// With NeedsPlanner-classified goal both attempts call the planner and
	// run all sub-tasks, then review. Pre-fix this test exercised a bogus
	// path where the Complexity Gate skipped the planner on attempt 0 and
	// the runReview-skipped-review-was-treated-as-fail bug triggered the
	// re-plan; that path is gone (see task_retry.go: empty Verdict no longer
	// triggers retry).
	if reviewPhase.calls != 2 || planCalls != 2 || len(runner.prompts) != 4 {
		t.Fatalf("calls: review=%d plan=%d runner=%d (want review=2 plan=2 runner=4)",
			reviewPhase.calls, planCalls, len(runner.prompts))
	}
}

func TestPlanExecuteEngineTaskRetryPreservesHighScoreSubTasks(t *testing.T) {
	baseStore := hermes.NewMemoryTaskStore()
	store := &statusRecordingStore{TaskStateStore: baseStore}
	runner := &planExecuteRunner{}
	reviewPhase := &scriptedReviewPhase{
		results: []ReviewResult{
			{
				Verdict:      VerdictFail,
				OverallScore: 45,
				Feedback:     "second sub-task missed validation",
				SubTaskResults: []ReviewSubTaskResult{
					{SubTaskID: "s1", Score: 92, Feedback: "good"},
					{SubTaskID: "s2", Score: 35, Feedback: "missing validation"},
				},
			},
			{Verdict: VerdictPass, OverallScore: 95, Feedback: "ok"},
		},
	}
	planCalls := 0
	var replanPrompt string
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		planCalls++
		if planCalls == 1 {
			return hermes.CallPlanResult{
				Text: "```json\n" +
					`[{"id":"s1","description":"first"},{"id":"s2","description":"second"}]` +
					"\n```",
			}, nil
		}
		replanPrompt = message
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s2","description":"fix validation for second"}]` +
				"\n```",
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		ReviewPhase:           reviewPhase,
		ReviewMode:            ReviewModePerTask,
		TaskRetry:             TaskRetryConfig{Enabled: true, ScoreThreshold: 70, MaxTaskRetries: 1},
	}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	taskID, err := engine.Start(context.Background(), "implement complex retry goal", NewChatContext(42, 0, "/repo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForPlanExecute(t, engine)

	state, err := baseStore.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if state.Status != hermes.TaskStatusDone {
		t.Fatalf("status = %s, want done", state.Status)
	}
	if planCalls != 2 || reviewPhase.calls != 2 || len(runner.prompts) != 3 {
		t.Fatalf("calls: plan=%d review=%d runner=%d (want 2/2/3)", planCalls, reviewPhase.calls, len(runner.prompts))
	}
	if len(state.Plan) != 2 {
		t.Fatalf("plan length = %d, want preserved+replanned = 2: %#v", len(state.Plan), state.Plan)
	}
	if state.Plan[0].ID != "s1" || state.Plan[0].Status != hermes.SubTaskDone {
		t.Fatalf("first sub-task not preserved: %#v", state.Plan)
	}
	if state.Plan[1].Description != "fix validation for second" || state.Plan[1].Status != hermes.SubTaskDone {
		t.Fatalf("second sub-task should be replanned replacement: %#v", state.Plan)
	}
	if !strings.Contains(replanPrompt, "PARTIAL RETRY PRESERVED WORK") || !strings.Contains(replanPrompt, "MUST NOT be repeated") {
		t.Fatalf("replan prompt missing preserved-work guard:\n%s", replanPrompt)
	}
}

func TestPlanExecuteEngineRunFromStateSkipsPreservedSubTasks(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	task := hermes.TaskState{
		ID:          "resume-task",
		ChatID:      42,
		ThreadID:    7,
		ProjectDir:  "/repo",
		Goal:        "continue work",
		Status:      hermes.TaskStatusExecuting,
		CurrentIdx:  1,
		Accumulated: "Sub-task 1 result:\nfirst already done",
		Plan: []hermes.SubTask{
			{ID: "s1", Description: "first", Status: hermes.SubTaskDone, Result: "first already done"},
			{ID: "s2", Description: "second", Status: hermes.SubTaskPending},
		},
	}
	if _, err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	runner := &planExecuteRunner{}
	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		ProjectDir: "/repo",
		ChatID:     42,
	}, func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		t.Fatal("planner should not be called by RunFromState")
		return hermes.CallPlanResult{}, nil
	}, NewDirectEngine(runner), store, &planExecuteReporter{})

	result, err := engine.RunFromState(context.Background(), task, NewChatContext(42, 7, "/repo"), nil)
	if err != nil {
		t.Fatalf("RunFromState: %v", err)
	}
	if len(runner.prompts) != 1 {
		t.Fatalf("runner prompts = %d, want 1 for remaining sub-task only", len(runner.prompts))
	}
	if !strings.Contains(runner.prompts[0], "second") {
		t.Fatalf("resume prompt should execute second sub-task:\n%s", runner.prompts[0])
	}
	state, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if state.Status != hermes.TaskStatusDone {
		t.Fatalf("status = %s, want done", state.Status)
	}
	if state.Plan[0].Attempts != 0 {
		t.Fatalf("preserved sub-task attempts = %d, want 0", state.Plan[0].Attempts)
	}
	if state.Plan[1].Status != hermes.SubTaskDone || state.Plan[1].Attempts != 1 {
		t.Fatalf("remaining sub-task not executed once: %#v", state.Plan[1])
	}
	if !strings.Contains(result.Text, "first already done") || !strings.Contains(result.Text, "result for") {
		t.Fatalf("result should contain preserved and resumed accumulated text:\n%s", result.Text)
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
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"first"},{"id":"s2","description":"second"}]` +
				"\n```",
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
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
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"first"},{"id":"s2","description":"second"}]` +
				"\n```",
		}, nil
	}

	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
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

func TestPlanExecuteEngineShowsFailurePauseForStrictPartial(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reviewPhase := &scriptedReviewPhase{
		results: []ReviewResult{
			{
				Verdict:      VerdictPass,
				OverallScore: 75,
				Feedback:     "補 runtime validation",
				IssueTags:    []ReviewTag{ReviewTagScopeCreep},
			},
			{
				Verdict:      VerdictPass,
				OverallScore: 75,
				Feedback:     "仍缺 runtime validation",
				IssueTags:    []ReviewTag{ReviewTagScopeCreep},
			},
			{
				Verdict:      VerdictPass,
				OverallScore: 75,
				Feedback:     "還是缺 runtime validation",
				IssueTags:    []ReviewTag{ReviewTagScopeCreep},
			},
			{
				Verdict:      VerdictPass,
				OverallScore: 93,
				Feedback:     "ok",
			},
		},
	}
	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{
			Text: "```json\n" +
				`[{"id":"s1","description":"first"}]` +
				"\n```",
		}, nil
	}

	pauseCalls := 0
	engine := NewPlanExecuteEngine(PlanExecuteConfig{
		PlannerModel:          "planner-model",
		ProjectDir:            "/repo",
		ChatID:                42,
		MaxPlannerJSONRetries: 1,
		Budget:                hermes.TokenBudget{MaxTotalTokens: 1000},
		ReviewPhase:           reviewPhase,
		ReviewMode:            ReviewModePerSubTask,
		StrictMode:            StrictModeConfig{Enabled: true, MaxRetriesPerSub: 2},
		OnSubTaskFailurePause: func(ctx context.Context, idx, total int, subTask hermes.SubTask, errText string, kind hermes.FailureKind) FailurePauseChoice {
			pauseCalls++
			if !strings.Contains(errText, "PARTIAL") {
				t.Fatalf("pause errText missing PARTIAL: %q", errText)
			}
			return FailurePauseChoice{Decision: FailureRetry}
		},
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
	if pauseCalls != 1 {
		t.Fatalf("pause calls = %d, want 1", pauseCalls)
	}
	if len(runner.prompts) != 4 {
		t.Fatalf("runner calls = %d, want 4", len(runner.prompts))
	}
	if state.Plan[0].Status != hermes.SubTaskDone {
		t.Fatalf("sub-task status = %s, want done after retry", state.Plan[0].Status)
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

// Regression tests for issue #168 — ensure the patterns observed in
// production (issues #29, #167, #80, #92, #74) are correctly handled by the
// new declarative checklist sync.

func TestRegression_Issue29_AllSubTasksWroteButItemsRemain(t *testing.T) {
	// Pattern: 5 sub-tasks all wrote, but issue had >5 acceptance items so
	// items remained unchecked. With declarations, the drift guard surfaces
	// any declared-but-unchecked items at task end.
	plan := []hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-1"}},
		{ID: "s2", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-2"}},
		{ID: "s3", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-3"}},
		{ID: "s4", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-4"}},
		{ID: "s5", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-5"}},
	}
	items := []hermes.ChecklistItem{
		{ID: "item-1", Checked: true},
		{ID: "item-2", Checked: true},
		{ID: "item-3", Checked: true},
		{ID: "item-4", Checked: true},
		{ID: "item-5", Checked: true},
		{ID: "item-6", Checked: false}, // not declared by any sub-task → not drift
	}
	drift := computeChecklistDeclarationDrift(plan, items)
	if len(drift) != 0 {
		t.Errorf("declared items all checked → drift should be empty, got %v", drift)
	}
}

func TestRegression_Issue253_FuzzyOverMappingPrevented(t *testing.T) {
	// Pattern: idx=0 wrote, idx=1..4 returned no_change because fuzzy mapped
	// every sub-task to the same already-checked item. With declarations,
	// each sub-task carries its own item ID and ticks the right item.
	body := "## Acceptance\n" +
		"- [ ] Refactor parser\n" +
		"- [ ] Update README\n" +
		"- [ ] Add unit tests\n" +
		"- [ ] Add integration test\n"
	subtasks := []hermes.SubTask{
		{ID: "s1", Description: "refactor parser", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-1"}},
		{ID: "s2", Description: "update README", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-2"}},
		{ID: "s3", Description: "add unit tests", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-3"}},
		{ID: "s4", Description: "add integration test", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-4"}},
	}
	preview := hermes.BuildChecklistSyncPreview(body, subtasks)
	if !preview.Changed {
		t.Fatal("expected changes")
	}
	for _, want := range []string{"- [x] Refactor parser", "- [x] Update README", "- [x] Add unit tests", "- [x] Add integration test"} {
		if !strings.Contains(preview.BodyAfter, want) {
			t.Errorf("expected %q in body:\n%s", want, preview.BodyAfter)
		}
	}
}

func TestRegression_Issue92_PartialFailureLeavesUnsatisfiedItemsUnchecked(t *testing.T) {
	// Pattern: 1 of N sub-tasks done, others blocked. Declared items for
	// blocked sub-tasks must NOT be ticked.
	body := "## Acceptance\n" +
		"- [ ] Build passes\n" +
		"- [ ] Integration test passes\n" +
		"- [ ] Docs updated\n"
	subtasks := []hermes.SubTask{
		{ID: "s1", Description: "build", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-1"}},
		{ID: "s2", Description: "integration", Status: hermes.SubTaskFailed, ChecklistItemIDs: []string{"item-2"}},
		{ID: "s3", Description: "docs", Status: hermes.SubTaskFailed, ChecklistItemIDs: []string{"item-3"}},
	}
	preview := hermes.BuildChecklistSyncPreview(body, subtasks)
	if !strings.Contains(preview.BodyAfter, "- [x] Build passes") {
		t.Errorf("done sub-task should tick item-1: %s", preview.BodyAfter)
	}
	if strings.Contains(preview.BodyAfter, "- [x] Integration test passes") {
		t.Errorf("failed sub-task must not tick item-2: %s", preview.BodyAfter)
	}
	if strings.Contains(preview.BodyAfter, "- [x] Docs updated") {
		t.Errorf("failed sub-task must not tick item-3: %s", preview.BodyAfter)
	}
}

func TestRegression_Issue74_DeclarationDriftCaughtAtTaskEnd(t *testing.T) {
	// Pattern: sub-task done with declared items but body sync never
	// completed (e.g. gh CLI auth blip mid-run). Drift guard surfaces it.
	plan := []hermes.SubTask{
		{ID: "s1", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-2", "item-5"}},
		{ID: "s2", Status: hermes.SubTaskDone, ChecklistItemIDs: []string{"item-7"}},
	}
	items := []hermes.ChecklistItem{
		{ID: "item-2", Checked: true},  // landed
		{ID: "item-5", Checked: false}, // drift
		{ID: "item-7", Checked: false}, // drift
		{ID: "item-9", Checked: false}, // not declared, ignored
	}
	drift := computeChecklistDeclarationDrift(plan, items)
	wantDrift := []string{"item-5", "item-7"}
	if len(drift) != len(wantDrift) {
		t.Fatalf("drift = %v, want %v", drift, wantDrift)
	}
	for i, id := range wantDrift {
		if drift[i] != id {
			t.Errorf("drift[%d] = %q, want %q", i, drift[i], id)
		}
	}
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

// ── Failure pause interrupt helpers (#169 slice 2) ─────────────────────────

func TestBuildFailurePauseInterrupt_PopulatesPayloadAndExpiry(t *testing.T) {
	now := time.Date(2026, 5, 8, 23, 0, 0, 0, time.UTC)
	subTask := hermes.SubTask{ID: "s3", Description: "run go test ./..."}
	got := buildFailurePauseInterrupt("task-abc", 2, 5, subTask, "deadline exceeded", hermes.FailureEnv, now)

	if got.SourceStep != hermes.RuntimeStepExecutor || got.ResumeStep != hermes.RuntimeStepExecutor {
		t.Errorf("step fields wrong: source=%q resume=%q", got.SourceStep, got.ResumeStep)
	}
	if got.Reason != "subtask_failure_pause" {
		t.Errorf("reason = %q, want subtask_failure_pause", got.Reason)
	}
	if got.CreatedAt != now {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, now.Add(24*time.Hour))
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", got.Payload)
	}
	if payload["sub_task_idx"] != 2 || payload["sub_task_id"] != "s3" {
		t.Errorf("payload mismatch: %+v", payload)
	}
	if payload["error_text"] != "deadline exceeded" {
		t.Errorf("payload error_text = %v", payload["error_text"])
	}
	if payload["failure_kind"] != hermes.FailureEnv.Label() {
		t.Errorf("payload failure_kind = %v, want %v", payload["failure_kind"], hermes.FailureEnv.Label())
	}
}

func TestFailureDecision_Label(t *testing.T) {
	cases := map[FailureDecision]string{
		FailureSkip:         "skip",
		FailureRetry:        "retry",
		FailureAbort:        "abort",
		FailureDecision(99): "unknown",
	}
	for d, want := range cases {
		if got := d.label(); got != want {
			t.Errorf("FailureDecision(%d).label() = %q, want %q", d, got, want)
		}
	}
}

// ── commitFailureBoundary (#169 slice 3a-1) ────────────────────────────────

func TestCommitFailureBoundary_WritesSnapshotWithReason(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	task, err := store.CreateTask(hermes.TaskState{
		ID:     "task-fail",
		ChatID: 42,
		Goal:   "x",
		Status: hermes.TaskStatusExecuting,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	planFn := func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		return hermes.CallPlanResult{}, nil
	}
	runner := &planExecuteRunner{}
	engine := NewPlanExecuteEngine(PlanExecuteConfig{ProjectDir: "/repo", ChatID: 42}, planFn, NewDirectEngine(runner), store, &planExecuteReporter{})

	engine.commitFailureBoundary(task.ID, hermes.RuntimeStepPlanner, 2, "complexity_violation_max_subtasks")

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != hermes.TaskStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, hermes.TaskStatusFailed)
	}

	snap, err := store.GetLatestSnapshot(task.ID)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if snap.SourceNode != hermes.RuntimeStepPlanner {
		t.Errorf("snapshot.SourceNode = %q, want planner", snap.SourceNode)
	}
	if snap.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("snapshot.NextStep = %q, want terminal", snap.NextStep)
	}
	if snap.Metadata.Reason != "complexity_violation_max_subtasks" {
		t.Errorf("snapshot.Metadata.Reason = %q, want complexity_violation_max_subtasks", snap.Metadata.Reason)
	}
	if snap.Metadata.Attempt != 2 {
		t.Errorf("snapshot.Metadata.Attempt = %d, want 2", snap.Metadata.Attempt)
	}
	if snap.State.Status != hermes.TaskStatusFailed {
		t.Errorf("snapshot state status = %q, want failed", snap.State.Status)
	}
}

// noRuntimeStore is a minimal hermes.TaskStateStore that does NOT implement
// hermes.RuntimeStepStore. Kept after #169 slice 3b only as documentation
// that legacy stores still satisfy TaskStateStore — runtime now wraps them
// via syntheticRuntimeStepStore so callers do not gate on nil.
type noRuntimeStore struct {
	statuses map[string]hermes.TaskStatus
}

func (s *noRuntimeStore) CreateTask(t hermes.TaskState) (hermes.TaskState, error)              { return t, nil }
func (s *noRuntimeStore) GetTask(id string) (hermes.TaskState, error)                          { return hermes.TaskState{}, hermes.ErrNoTask }
func (s *noRuntimeStore) GetActiveTaskForChat(chatID int64) (hermes.TaskState, error)          { return hermes.TaskState{}, hermes.ErrNoTask }
func (s *noRuntimeStore) StorePlan(taskID string, plan []hermes.SubTask) error                 { return nil }
func (s *noRuntimeStore) UpdateSubTask(taskID string, idx int, status hermes.SubTaskStatus, result string, tokensUsed int) error {
	return nil
}
func (s *noRuntimeStore) MarkSubTaskStarted(taskID string, idx int) error                      { return nil }
func (s *noRuntimeStore) AdvanceTask(taskID string, nextIdx int, status hermes.TaskStatus) error {
	return nil
}
func (s *noRuntimeStore) AppendArtifact(taskID string, artifact hermes.Artifact) error         { return nil }
func (s *noRuntimeStore) UpdateAccumulated(taskID string, accumulated string) error            { return nil }
func (s *noRuntimeStore) UpdatePlannerSession(taskID, sessionID string) error                  { return nil }
func (s *noRuntimeStore) MarkInterrupted(taskID string, messageID int64) error                 { return nil }
func (s *noRuntimeStore) MarkStatus(taskID string, status hermes.TaskStatus) error {
	s.statuses[taskID] = status
	return nil
}
func (s *noRuntimeStore) ResetBudgetStartedAt(taskID string, t time.Time) error                { return nil }
func (s *noRuntimeStore) AddTokenUsage(taskID string, delta int) error                         { return nil }
func (s *noRuntimeStore) AddModelUsage(taskID, model string, in, out int, cost float64) error  { return nil }
func (s *noRuntimeStore) AddModelUsageBreakdown(taskID, model string, u hermes.TokenUsageBreakdown) error {
	return nil
}
func (s *noRuntimeStore) AddPhaseUsage(taskID, phase, model string, in, out int, cost float64) error {
	return nil
}
func (s *noRuntimeStore) AddPhaseUsageBreakdown(taskID, phase, model string, u hermes.TokenUsageBreakdown) error {
	return nil
}
func (s *noRuntimeStore) ListTasksForChat(chatID int64, limit int) ([]hermes.TaskState, error) {
	return nil, nil
}
