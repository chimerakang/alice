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

func TestPlanExecuteEngineRunsPlannedSubTasksThroughDirectEngine(t *testing.T) {
	store := hermes.NewMemoryTaskStore()
	runner := &planExecuteRunner{}
	reporter := &planExecuteReporter{}
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
