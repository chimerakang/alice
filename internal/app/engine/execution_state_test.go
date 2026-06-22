package engine

import "testing"

func TestExecutionLifecycleTransitions(t *testing.T) {
	lifecycle := NewExecutionLifecycle()
	for _, state := range []ExecutionState{
		ExecutionStateStarting,
		ExecutionStateStreaming,
		ExecutionStateRetrying,
		ExecutionStateStreaming,
		ExecutionStateSucceeded,
		ExecutionStateIdle,
	} {
		if err := lifecycle.Transition(state, "test"); err != nil {
			t.Fatalf("Transition(%q): %v", state, err)
		}
	}
	snapshot := lifecycle.Snapshot()
	if snapshot.State != ExecutionStateIdle {
		t.Fatalf("State = %q, want %q", snapshot.State, ExecutionStateIdle)
	}
	if snapshot.Reason != "test" {
		t.Fatalf("Reason = %q, want test", snapshot.Reason)
	}
}

func TestExecutionLifecycleRejectsInvalidTransition(t *testing.T) {
	lifecycle := NewExecutionLifecycle()
	if err := lifecycle.Transition(ExecutionStateSucceeded, "test"); err == nil {
		t.Fatal("Transition(idle -> succeeded) returned nil, want error")
	}
}

func TestExecutionLifecycleIsProcessing(t *testing.T) {
	lifecycle := NewExecutionLifecycle()
	if lifecycle.IsProcessing() {
		t.Fatal("IsProcessing idle = true, want false")
	}
	if err := lifecycle.Transition(ExecutionStateStarting, "start"); err != nil {
		t.Fatalf("Transition(starting): %v", err)
	}
	if !lifecycle.IsProcessing() {
		t.Fatal("IsProcessing starting = false, want true")
	}
	if err := lifecycle.Transition(ExecutionStateStreaming, "stream"); err != nil {
		t.Fatalf("Transition(streaming): %v", err)
	}
	if err := lifecycle.Transition(ExecutionStateCancelling, "abort"); err != nil {
		t.Fatalf("Transition(cancelling): %v", err)
	}
	if err := lifecycle.Transition(ExecutionStateCancelled, "aborted"); err != nil {
		t.Fatalf("Transition(cancelled): %v", err)
	}
	if lifecycle.IsProcessing() {
		t.Fatal("IsProcessing cancelled = true, want false")
	}
	if !lifecycle.Snapshot().Terminal {
		t.Fatal("Terminal cancelled = false, want true")
	}
}
