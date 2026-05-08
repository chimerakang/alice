package graph

import (
	"context"
	"errors"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// recordingNode is a test Node that records calls and returns a scripted
// NodeOutput sequence.
type recordingNode struct {
	step    hermes.RuntimeStep
	outputs []NodeOutput
	calls   int
}

func (n *recordingNode) Name() hermes.RuntimeStep { return n.step }
func (n *recordingNode) Handle(_ context.Context, _ hermes.HermesState) (NodeOutput, error) {
	idx := n.calls
	n.calls++
	if idx >= len(n.outputs) {
		return n.outputs[len(n.outputs)-1], nil
	}
	return n.outputs[idx], nil
}

func makeWalkerStore(t *testing.T) *hermes.MemoryTaskStore {
	t.Helper()
	store := hermes.NewMemoryTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{
		ID:     "task-graph",
		ChatID: 42,
		Goal:   "graph test",
		Status: hermes.TaskStatusPlanning,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Seed the initial snapshot so the walker has somewhere to start.
	executing := hermes.TaskStatusExecuting
	if _, err := store.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     "task-graph",
		Updates:    []hermes.StateUpdate{{Status: &executing}},
		NextStep:   hermes.RuntimeStepPlanner,
		SourceNode: hermes.RuntimeStepPlanner,
		Metadata:   hermes.SnapshotMetadata{Source: "test", Reason: "seed"},
	}); err != nil {
		t.Fatalf("seed CommitRuntimeStep: %v", err)
	}
	return store
}

func TestWalker_DispatchesToRegisteredNodeAndStopsOnTerminal(t *testing.T) {
	store := makeWalkerStore(t)
	plannerNode := &recordingNode{
		step: hermes.RuntimeStepPlanner,
		outputs: []NodeOutput{{
			NextStep: hermes.RuntimeStepTerminal,
			Reason:   "planner_done",
		}},
	}
	registry := NewRegistry()
	registry.Register(plannerNode)
	registry.Register(TerminalNode{Reason: "test_terminal"})

	walker, err := NewWalker(store, registry)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	walker.MaxSteps = 10

	final, err := walker.Run(context.Background(), "task-graph")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plannerNode.calls != 1 {
		t.Errorf("planner calls = %d, want 1", plannerNode.calls)
	}
	if final.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("final NextStep = %q, want terminal", final.NextStep)
	}
	if final.Metadata.Reason != "planner_done" {
		t.Errorf("metadata.reason = %q, want planner_done", final.Metadata.Reason)
	}
}

func TestWalker_HaltStopsAfterCommit(t *testing.T) {
	store := makeWalkerStore(t)
	approvalNode := &recordingNode{
		step: hermes.RuntimeStepPlanner,
		outputs: []NodeOutput{{
			NextStep: hermes.RuntimeStepApproval,
			Reason:   "needs_approval",
			Halt:     true,
		}},
	}
	registry := NewRegistry()
	registry.Register(approvalNode)

	walker, err := NewWalker(store, registry)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	walker.MaxSteps = 10

	final, err := walker.Run(context.Background(), "task-graph")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if approvalNode.calls != 1 {
		t.Fatalf("planner calls = %d, want 1 (halt should stop loop)", approvalNode.calls)
	}
	if final.NextStep != hermes.RuntimeStepApproval {
		t.Errorf("final NextStep = %q, want approval", final.NextStep)
	}
}

func TestWalker_ErrorsOnUnregisteredStep(t *testing.T) {
	store := makeWalkerStore(t)
	registry := NewRegistry()
	// Empty registry on purpose.
	walker, err := NewWalker(store, registry)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	walker.MaxSteps = 5

	_, err = walker.Run(context.Background(), "task-graph")
	if !errors.Is(err, ErrUnregisteredStep) {
		t.Errorf("err = %v, want ErrUnregisteredStep", err)
	}
}

func TestWalker_MaxStepsCapEnforced(t *testing.T) {
	// Looping node never terminates — guard must trip before infinite
	// dispatch.
	store := makeWalkerStore(t)
	loopNode := &recordingNode{
		step: hermes.RuntimeStepPlanner,
		outputs: []NodeOutput{{
			NextStep: hermes.RuntimeStepPlanner, // stays on planner
			Reason:   "loop",
		}},
	}
	registry := NewRegistry()
	registry.Register(loopNode)
	walker, err := NewWalker(store, registry)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	walker.MaxSteps = 3

	_, err = walker.Run(context.Background(), "task-graph")
	if !errors.Is(err, ErrMaxStepsExceeded) {
		t.Errorf("err = %v, want ErrMaxStepsExceeded", err)
	}
	if loopNode.calls != 3 {
		t.Errorf("looping node calls = %d, want 3 (cap)", loopNode.calls)
	}
}

func TestWalker_RejectsEmptyNextStep(t *testing.T) {
	store := makeWalkerStore(t)
	badNode := &recordingNode{
		step: hermes.RuntimeStepPlanner,
		outputs: []NodeOutput{{
			NextStep: "",
		}},
	}
	registry := NewRegistry()
	registry.Register(badNode)
	walker, err := NewWalker(store, registry)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	walker.MaxSteps = 5

	_, err = walker.Run(context.Background(), "task-graph")
	if err == nil {
		t.Fatal("expected error for empty NextStep")
	}
}

func TestWalker_AppliesUpdatesViaCommit(t *testing.T) {
	store := makeWalkerStore(t)
	idx := 7
	mutator := &recordingNode{
		step: hermes.RuntimeStepPlanner,
		outputs: []NodeOutput{{
			Updates: []hermes.StateUpdate{
				{CurrentIdx: &idx},
			},
			NextStep: hermes.RuntimeStepTerminal,
			Reason:   "mutated",
		}},
	}
	registry := NewRegistry()
	registry.Register(mutator)
	walker, err := NewWalker(store, registry)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	walker.MaxSteps = 5

	final, err := walker.Run(context.Background(), "task-graph")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if final.State.CurrentIdx != 7 {
		t.Errorf("CurrentIdx = %d, want 7 (StateUpdate not applied)", final.State.CurrentIdx)
	}
}

func TestNewWalker_RejectsNilArgs(t *testing.T) {
	if _, err := NewWalker(nil, NewRegistry()); err == nil {
		t.Error("expected error for nil store")
	}
	store := hermes.NewMemoryTaskStore()
	if _, err := NewWalker(store, nil); err == nil {
		t.Error("expected error for nil registry")
	}
}

func TestRegistry_LookupReturnsFalseForUnknownStep(t *testing.T) {
	r := NewRegistry()
	r.Register(TerminalNode{})
	if _, ok := r.Lookup(hermes.RuntimeStepPlanner); ok {
		t.Error("expected planner step to be unregistered")
	}
	if _, ok := r.Lookup(hermes.RuntimeStepTerminal); !ok {
		t.Error("expected terminal step to be registered")
	}
}
