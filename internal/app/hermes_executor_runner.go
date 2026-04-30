package app

import (
	appengine "claude-tg-agent/internal/app/engine"
	"claude-tg-agent/internal/app/hermes"
)

// hermesExecutorRunner is the DirectRunner used for Hermes sub-task execution.
// It picks between a light executor model (e.g. Haiku) and a heavy executor
// model (e.g. Sonnet) per sub-task based on engine.IsHeavySubTask, then
// applies the chosen model to the underlying Agent for that one Run call.
//
// Implements appengine.DirectRunner, appengine.DirectRunnerMetrics, and
// appengine.SubTaskBindable. PlanExecuteEngine calls BindSubTask before
// every Run so the runner knows which classification to apply.
type hermesExecutorRunner struct {
	agent              *Agent
	executorModel      string // light tier, used by default
	heavyExecutorModel string // heavy tier, used for Edit/Write/refactor work
	current            hermes.SubTask
}

func newHermesExecutorRunner(agent *Agent, executorModel, heavyExecutorModel string) *hermesExecutorRunner {
	return &hermesExecutorRunner{
		agent:              agent,
		executorModel:      executorModel,
		heavyExecutorModel: heavyExecutorModel,
	}
}

// BindSubTask records the sub-task that the next Run call will execute.
// Called by PlanExecuteEngine via DirectEngine.BindSubTask.
func (r *hermesExecutorRunner) BindSubTask(st hermes.SubTask) {
	r.current = st
}

// pickModel chooses the executor model for the bound sub-task. Heavy
// executor model wins for sub-tasks classified as substantive code work;
// otherwise the light executor runs.
func (r *hermesExecutorRunner) pickModel() string {
	if r.heavyExecutorModel != "" && r.heavyExecutorModel != r.executorModel && appengine.IsHeavySubTask(r.current) {
		return r.heavyExecutorModel
	}
	return r.executorModel
}

func (r *hermesExecutorRunner) Run(msg string, onUpdate func(string, bool)) (string, error) {
	model := r.pickModel()

	prevOverride := r.agent.currentModelOverride
	if model != "" {
		r.agent.currentModelOverride = model
	}
	// Suppress the general-memory + recent-message bridge for the duration of
	// this Hermes Executor call. The Hermes prompt already carries goal +
	// accumulated + current sub-task; the bridge cards re-inject prior runs'
	// Hermes prompts as "Persisted general work memory", causing prompt bloat
	// to grow exponentially across sub-tasks until codex hits its prompt
	// length limit.
	r.agent.SetSuppressMemoryBridge(true)
	defer func() {
		r.agent.currentModelOverride = prevOverride
		r.agent.SetSuppressMemoryBridge(false)
	}()

	// Reset CLI session before each sub-task. The Hermes engine rebuilds full
	// context (goal + accumulated + current subtask) into the prompt each
	// call, so cross-subtask CLI session continuity is redundant and is the
	// main driver of "Prompt is too long" as the transcript grows.
	if model != "" {
		r.agent.ClearSessionForModel(model)
	} else {
		r.agent.ClearSession()
	}
	return r.agent.Run(msg, onUpdate)
}

// LastCallMetrics forwards Agent metrics so DirectEngine can attribute
// Executor tokens to the actual model that ran each sub-task.
func (r *hermesExecutorRunner) LastCallMetrics() (string, int, int) {
	return r.agent.LastCallMetrics()
}
