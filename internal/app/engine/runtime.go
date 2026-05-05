package engine

import (
	"context"
	"time"
)

// RuntimeAgent is the shared lifecycle surface for Alice's agent-like runtime
// components. Each agent may own different business logic, but should expose
// state and consume/emit typed runtime events through this contract.
type RuntimeAgent interface {
	Name() string
	State() StateSnapshot
	Handle(context.Context, Event) ([]Command, error)
}

// StateSnapshot is the shared, lightweight runtime-state shape exposed by
// agent-like components. Future agents should reuse this vocabulary even when
// their internal states differ.
type StateSnapshot struct {
	Agent    string
	State    string
	Since    time.Time
	Terminal bool
	Reason   string
}

// Event is the runtime input shape used between agents. It is intentionally
// small and transport-friendly so it can later be logged as a trace span or
// persisted without knowing every agent's private fields.
type Event struct {
	Type      string
	ChatID    int64
	ThreadID  int
	TaskID    string
	Issue     int
	Payload   any
	Timestamp time.Time
}

// Command is the runtime output shape returned by an agent after handling an
// event. Commands describe requested work; the runtime decides who executes it.
type Command struct {
	Type    string
	Target  string
	Payload any
}
