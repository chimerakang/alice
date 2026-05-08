package graph

import (
	"context"
	"errors"
	"testing"

	"claude-tg-agent/internal/app/hermes"
)

// panickingNode panics on Handle so panic-recovery tests can assert
// the Walker's defer/recover landing pad commits a terminal snapshot.
type panickingNode struct {
	step  hermes.RuntimeStep
	value any
	calls int
}

func (n *panickingNode) Name() hermes.RuntimeStep { return n.step }
func (n *panickingNode) Handle(_ context.Context, _ hermes.HermesState) (NodeOutput, error) {
	n.calls++
	if n.value != nil {
		panic(n.value)
	}
	panic("boom")
}

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

func TestWalker_RecoversNodePanicAndCommitsTerminal(t *testing.T) {
	store := makeWalkerStore(t)
	bad := &panickingNode{step: hermes.RuntimeStepPlanner, value: "deliberate test panic"}
	registry := NewRegistry()
	registry.Register(bad)

	walker, err := NewWalker(store, registry)
	if err != nil {
		t.Fatalf("NewWalker: %v", err)
	}
	final, runErr := walker.Run(context.Background(), "task-graph")
	if runErr == nil {
		t.Fatalf("expected error from panicking node")
	}
	if !errors.Is(runErr, ErrNodePanic) {
		t.Errorf("error = %v, want errors.Is(ErrNodePanic)", runErr)
	}
	var panicErr *NodePanicError
	if !errors.As(runErr, &panicErr) {
		t.Fatalf("expected *NodePanicError, got %T", runErr)
	}
	if panicErr.Step != hermes.RuntimeStepPlanner {
		t.Errorf("panic step = %q, want planner", panicErr.Step)
	}
	if bad.calls != 1 {
		t.Errorf("panicking node calls = %d, want 1", bad.calls)
	}
	if final.NextStep != hermes.RuntimeStepTerminal {
		t.Errorf("final NextStep = %q, want terminal (panic recovery)", final.NextStep)
	}
	if final.SourceNode != hermes.RuntimeStepPlanner {
		t.Errorf("SourceNode = %q, want planner", final.SourceNode)
	}
	if final.Metadata.Reason != "node_panic" {
		t.Errorf("Reason = %q, want node_panic", final.Metadata.Reason)
	}
	if final.State.Status != hermes.TaskStatusFailed {
		t.Errorf("Status = %q, want failed", final.State.Status)
	}
	if len(final.State.Errors) == 0 {
		t.Fatalf("expected an HermesStateError recorded for the panic")
	}
	got := final.State.Errors[len(final.State.Errors)-1]
	if got.Step != hermes.RuntimeStepPlanner {
		t.Errorf("error step = %q, want planner", got.Step)
	}
	if got.Message == "" {
		t.Errorf("expected non-empty error message")
	}
}

func TestWalker_PanicCommitDoesNotResurrectTerminalStatus(t *testing.T) {
	// If a node somehow panics after the state is already terminal, the
	// commit must not transition the status back to Failed (the reducer
	// would reject the illegal transition and we'd lose the panic record).
	store := hermes.NewMemoryTaskStore()
	if _, err := store.CreateTask(hermes.TaskState{ID: "t-done", Status: hermes.TaskStatusPlanning}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	done := hermes.TaskStatusDone
	if _, err := store.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     "t-done",
		Updates:    []hermes.StateUpdate{{Status: &done}},
		NextStep:   hermes.RuntimeStepPlanner,
		SourceNode: hermes.RuntimeStepPlanner,
		Metadata:   hermes.SnapshotMetadata{Source: "test"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	bad := &panickingNode{step: hermes.RuntimeStepPlanner, value: "after-terminal panic"}
	registry := NewRegistry()
	registry.Register(bad)
	walker, _ := NewWalker(store, registry)

	final, err := walker.Run(context.Background(), "t-done")
	if !errors.Is(err, ErrNodePanic) {
		t.Fatalf("err = %v, want ErrNodePanic", err)
	}
	if final.State.Status != hermes.TaskStatusDone {
		t.Errorf("Status = %q, want done preserved (no illegal transition)", final.State.Status)
	}
	if len(final.State.Errors) == 0 {
		t.Errorf("expected error recorded even when status preserved")
	}
}
