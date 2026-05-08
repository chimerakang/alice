// Package graph is the LangGraph-style runtime skeleton for Hermes
// (#169 phase γ).
//
// The existing PlanExecuteEngine.run() is a 1700-line hand-coded for-loop
// that walks RuntimeStep transitions implicitly: planner → executor → ...
// → terminal. This package introduces an explicit, snapshot-driven walker
// where each step is a Node that takes the current HermesState and
// returns a partial StateUpdate plus the next RuntimeStep. The walker
// commits each transition through the existing RuntimeStepStore so the
// snapshot history becomes the canonical execution log.
//
// γ slice 1 lands the skeleton plus a TerminalNode so the dispatch
// machinery can be exercised end-to-end without disturbing plan_execute.
// Subsequent slices migrate planner / executor / reviewer / approval
// logic into Nodes one at a time.
package graph

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"claude-tg-agent/internal/app/hermes"
)

// Node is one logical step in the Hermes execution graph. Implementations
// are expected to be pure with respect to HermesState — read what is
// passed in, produce a NodeOutput. Side effects on durable state must
// flow through the StateUpdate returned in NodeOutput so the snapshot
// remains the source of truth.
//
// External effects (LLM calls, Telegram messages, GitHub writes) are
// allowed but should be idempotent or guarded by snapshot metadata; a
// Node may run again on resume after process restart.
type Node interface {
	Name() hermes.RuntimeStep
	Handle(ctx context.Context, state hermes.HermesState) (NodeOutput, error)
}

// NodeOutput is the return shape from Node.Handle. The walker applies
// Updates atomically and routes to NextStep on the next iteration. When
// NextStep equals hermes.RuntimeStepTerminal the walker stops.
type NodeOutput struct {
	// Updates are applied through the reducer in declaration order. Empty
	// slice is allowed — pure routing nodes do not need to mutate state.
	Updates []hermes.StateUpdate
	// NextStep tells the walker which Node to dispatch on the next
	// iteration. Required.
	NextStep hermes.RuntimeStep
	// Reason is recorded as Snapshot.Metadata.Reason for the commit.
	Reason string
	// Halt asks the walker to stop after committing this step even when
	// NextStep is non-terminal. Used by approval-style nodes that want
	// the engine to exit and wait for an external signal (HumanInterrupt,
	// budget continuation) before proceeding.
	Halt bool
}

// Registry maps RuntimeStep names to Node handlers. Walker.Step looks up
// the snapshot's NextStep here; an unregistered step is a fatal error.
type Registry struct {
	nodes map[hermes.RuntimeStep]Node
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{nodes: make(map[hermes.RuntimeStep]Node)}
}

// Register adds a Node, overwriting any existing handler for the same
// step. Registration is idempotent for the same Node instance; conflicts
// are caller error.
func (r *Registry) Register(node Node) {
	if r == nil || node == nil {
		return
	}
	r.nodes[node.Name()] = node
}

// Lookup returns the registered handler for step, or (nil, false) when
// no Node is registered for that RuntimeStep.
func (r *Registry) Lookup(step hermes.RuntimeStep) (Node, bool) {
	if r == nil {
		return nil, false
	}
	node, ok := r.nodes[step]
	return node, ok
}

// Walker drives the snapshot-driven execution loop: read latest
// snapshot → dispatch the registered Node for snapshot.NextStep → commit
// the returned StateUpdate + NextStep → repeat until terminal or halt.
type Walker struct {
	store    walkerStore
	registry *Registry
	// MaxSteps caps the number of dispatch iterations per Run() call as a
	// defensive guard against an infinite-loop misconfiguration. 0 means
	// unbounded (production default — the natural terminal state stops
	// the walker; tests pass a small cap).
	MaxSteps int
}

// walkerStore is the storage surface Walker needs. Implemented by
// hermes.SQLiteTaskStore (production) and hermes.MemoryTaskStore (tests).
type walkerStore interface {
	hermes.SnapshotStore
	hermes.RuntimeStepStore
}

// NewWalker constructs a Walker bound to the given store + registry.
// Returns an error if either argument is nil.
func NewWalker(store walkerStore, registry *Registry) (*Walker, error) {
	if store == nil {
		return nil, errors.New("graph: walker store is required")
	}
	if registry == nil {
		return nil, errors.New("graph: walker registry is required")
	}
	return &Walker{store: store, registry: registry}, nil
}

// ErrUnregisteredStep means snapshot.NextStep does not have a
// corresponding Node handler in the registry. Either an early-stage
// migration left a step unimplemented or the snapshot was committed by a
// newer engine; in production this should be treated as fatal.
var ErrUnregisteredStep = errors.New("graph: no handler registered for runtime step")

// ErrMaxStepsExceeded means Walker.Run hit its iteration cap.
var ErrMaxStepsExceeded = errors.New("graph: max walker steps exceeded")

// ErrNodePanic means a Node implementation panicked during Handle.
// Walker.Run recovers the panic, commits a terminal snapshot marking the
// task Failed (with the panic message recorded in state.Errors), and
// returns this error wrapped so callers can distinguish "node bug" from
// regular error returns. The committed snapshot's Metadata.Reason is
// "node_panic" so dashboards can filter for it.
var ErrNodePanic = errors.New("graph: node panic")

// NodePanicError carries the panic value + node name so callers can
// inspect what failed without losing the original cause. Wraps
// ErrNodePanic via Unwrap so errors.Is(err, ErrNodePanic) works.
type NodePanicError struct {
	Step  hermes.RuntimeStep
	Value any
}

func (e *NodePanicError) Error() string {
	return fmt.Sprintf("graph: node %q panic: %v", e.Step, e.Value)
}

func (e *NodePanicError) Unwrap() error { return ErrNodePanic }

// safeHandle wraps node.Handle with a panic recover so a buggy Node
// cannot leak its panic past the Walker. The recovered value is
// returned as a *NodePanicError; output is the zero value in that case.
func safeHandle(ctx context.Context, node Node, state hermes.HermesState) (output NodeOutput, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &NodePanicError{Step: node.Name(), Value: r}
			output = NodeOutput{}
		}
	}()
	return node.Handle(ctx, state)
}

// Run dispatches Nodes for taskID until a terminal NextStep is reached,
// a halt is requested, or MaxSteps is exceeded. Each iteration:
//  1. loads the latest snapshot to read the canonical state
//  2. looks up the Node for snapshot.NextStep
//  3. calls Node.Handle and inspects NodeOutput
//  4. commits Updates + NextStep through CommitRuntimeStep
//  5. if NextStep == RuntimeStepTerminal or Halt is set → return
//
// Returns the final snapshot and any error encountered.
func (w *Walker) Run(ctx context.Context, taskID string) (hermes.Snapshot, error) {
	if w == nil {
		return hermes.Snapshot{}, errors.New("graph: nil walker")
	}
	steps := 0
	var lastSnapshot hermes.Snapshot
	for {
		if ctx.Err() != nil {
			return lastSnapshot, ctx.Err()
		}
		if w.MaxSteps > 0 && steps >= w.MaxSteps {
			return lastSnapshot, fmt.Errorf("%w: ran %d steps", ErrMaxStepsExceeded, steps)
		}
		steps++

		snap, err := w.store.GetLatestSnapshot(taskID)
		if err != nil {
			return lastSnapshot, fmt.Errorf("graph: load latest snapshot: %w", err)
		}
		lastSnapshot = snap
		if snap.NextStep == hermes.RuntimeStepTerminal {
			return snap, nil
		}

		node, ok := w.registry.Lookup(snap.NextStep)
		if !ok {
			return snap, fmt.Errorf("%w: %q", ErrUnregisteredStep, snap.NextStep)
		}

		output, err := safeHandle(ctx, node, snap.State)
		if err != nil {
			var panicErr *NodePanicError
			if errors.As(err, &panicErr) {
				// Persist a terminal snapshot so future Walker passes
				// don't redispatch the panicking node. Commit failure here
				// is reported alongside the panic but cannot itself stop
				// us from returning — the panic is the load-bearing
				// signal.
				if committed, commitErr := w.commitPanicTerminal(taskID, snap, panicErr); commitErr == nil {
					lastSnapshot = committed
				} else {
					log.Printf("[graph] panic terminal commit failed task=%s step=%s: %v", taskID, snap.NextStep, commitErr)
				}
				return lastSnapshot, panicErr
			}
			return snap, fmt.Errorf("graph: node %q failed: %w", snap.NextStep, err)
		}
		reason := output.Reason
		if reason == "" {
			reason = string(snap.NextStep) + "_step"
		}
		next := output.NextStep
		if next == "" {
			return snap, fmt.Errorf("graph: node %q returned empty NextStep", snap.NextStep)
		}
		committed, err := w.store.CommitRuntimeStep(hermes.RuntimeCommit{
			TaskID:     taskID,
			Updates:    output.Updates,
			NextStep:   next,
			SourceNode: snap.NextStep,
			Metadata: hermes.SnapshotMetadata{
				Source: "graph_walker",
				Reason: reason,
			},
			CreatedAt: time.Now(),
		})
		if err != nil {
			return snap, fmt.Errorf("graph: commit after node %q: %w", snap.NextStep, err)
		}
		lastSnapshot = committed
		if next == hermes.RuntimeStepTerminal || output.Halt {
			return committed, nil
		}
	}
}

// commitPanicTerminal commits a terminal snapshot after a node panic.
// The committed StateUpdate marks the task Failed (when not already
// terminal), records the panic message in state.Errors, and tags the
// snapshot Metadata so dashboards can filter for "node_panic" hops.
func (w *Walker) commitPanicTerminal(taskID string, last hermes.Snapshot, panicErr *NodePanicError) (hermes.Snapshot, error) {
	failed := hermes.TaskStatusFailed
	update := hermes.StateUpdate{
		Errors: []hermes.HermesStateError{{
			Step:      panicErr.Step,
			Message:   panicErr.Error(),
			Retryable: false,
			CreatedAt: time.Now(),
		}},
	}
	// Only inject a Status write when the current state can transition
	// to Failed — the reducer rejects illegal transitions, and a task
	// already in a terminal state should keep its status.
	switch last.State.Status {
	case hermes.TaskStatusDone, hermes.TaskStatusFailed, hermes.TaskStatusInterrupted:
		// already terminal — keep status, only record the error
	default:
		update.Status = &failed
	}
	return w.store.CommitRuntimeStep(hermes.RuntimeCommit{
		TaskID:     taskID,
		Updates:    []hermes.StateUpdate{update},
		NextStep:   hermes.RuntimeStepTerminal,
		SourceNode: panicErr.Step,
		Metadata: hermes.SnapshotMetadata{
			Source: "graph_walker",
			Reason: "node_panic",
		},
		CreatedAt: time.Now(),
	})
}

// LogDispatch is a small helper Nodes can use to emit a uniform log line
// when they begin handling. Not required, but helpful when migrating
// existing plan_execute logic so the migrated path's log lines stay
// recognisable.
func LogDispatch(node Node, taskID string) {
	log.Printf("[graph] dispatch node=%s task=%s", node.Name(), taskID)
}
