package engine

import (
	"fmt"
	"time"
)

type ExecutionState string

const (
	ExecutionStateIdle       ExecutionState = "idle"
	ExecutionStateStarting   ExecutionState = "starting"
	ExecutionStateStreaming  ExecutionState = "streaming"
	ExecutionStateRetrying   ExecutionState = "retrying"
	ExecutionStateCancelling ExecutionState = "cancelling"
	ExecutionStateSucceeded  ExecutionState = "succeeded"
	ExecutionStateFailed     ExecutionState = "failed"
	ExecutionStateCancelled  ExecutionState = "cancelled"
)

type ExecutionSnapshot struct {
	State    ExecutionState
	Since    time.Time
	Terminal bool
	Reason   string
}

type ExecutionLifecycle struct {
	state  ExecutionState
	since  time.Time
	reason string
}

func NewExecutionLifecycle() *ExecutionLifecycle {
	return &ExecutionLifecycle{
		state: ExecutionStateIdle,
		since: time.Now(),
	}
}

func (l *ExecutionLifecycle) Transition(to ExecutionState, reason string) error {
	if l == nil {
		return nil
	}
	from := l.state
	if from == "" {
		from = ExecutionStateIdle
	}
	if !ValidExecutionStateTransition(from, to) {
		return fmt.Errorf("invalid execution state transition: %s -> %s", from, to)
	}
	if l.state != to {
		l.state = to
		l.since = time.Now()
	}
	l.reason = reason
	return nil
}

func (l *ExecutionLifecycle) Snapshot() ExecutionSnapshot {
	if l == nil {
		return ExecutionSnapshot{State: ExecutionStateIdle, Terminal: true, Reason: "nil_lifecycle"}
	}
	state := l.state
	if state == "" {
		state = ExecutionStateIdle
	}
	return ExecutionSnapshot{
		State:    state,
		Since:    l.since,
		Terminal: IsTerminalExecutionState(state),
		Reason:   l.reason,
	}
}

func (l *ExecutionLifecycle) IsProcessing() bool {
	if l == nil {
		return false
	}
	state := l.Snapshot().State
	return !IsTerminalExecutionState(state) && state != ExecutionStateIdle
}

func ValidExecutionStateTransition(from, to ExecutionState) bool {
	if to == "" {
		return false
	}
	if from == "" || from == to {
		return true
	}
	switch from {
	case ExecutionStateIdle:
		return to == ExecutionStateStarting
	case ExecutionStateStarting:
		return to == ExecutionStateStreaming || to == ExecutionStateCancelling || to == ExecutionStateFailed
	case ExecutionStateStreaming:
		return to == ExecutionStateRetrying || to == ExecutionStateCancelling || to == ExecutionStateSucceeded || to == ExecutionStateFailed
	case ExecutionStateRetrying:
		return to == ExecutionStateStreaming || to == ExecutionStateCancelling || to == ExecutionStateFailed
	case ExecutionStateCancelling:
		return to == ExecutionStateCancelled || to == ExecutionStateFailed
	case ExecutionStateSucceeded, ExecutionStateFailed, ExecutionStateCancelled:
		return to == ExecutionStateIdle || to == ExecutionStateStarting
	default:
		return false
	}
}

func IsTerminalExecutionState(state ExecutionState) bool {
	switch state {
	case ExecutionStateSucceeded, ExecutionStateFailed, ExecutionStateCancelled:
		return true
	default:
		return false
	}
}
