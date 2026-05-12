package engine

import (
	"context"
	"time"

	"claude-tg-agent/internal/app/hermes"
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

// GraphNodeEventPayload captures one LangGraph-style node lifecycle event.
// It is intentionally compact so runtime_events can power a task graph view
// without coupling dashboards to raw snapshot internals.
type GraphNodeEventPayload struct {
	Node       hermes.RuntimeStep `json:"node"`
	Status     string             `json:"status"`
	NextStep   hermes.RuntimeStep `json:"next_step,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	DurationMS int64              `json:"duration_ms,omitempty"`
	StepIndex  int                `json:"step_index,omitempty"`
	Halt       bool               `json:"halt,omitempty"`
	Error      string             `json:"error,omitempty"`
}

// GraphNodeEvent builds a normalized runtime event for graph node telemetry.
func GraphNodeEvent(taskID string, at time.Time, payload GraphNodeEventPayload) Event {
	if at.IsZero() {
		at = time.Now()
	}
	return Event{
		Type:      "GraphNodeEvent",
		Timestamp: at,
		TaskID:    taskID,
		Payload:   payload,
	}
}

// Command is the runtime output shape returned by an agent after handling an
// event. Commands describe requested work; the runtime decides who executes it.
type Command struct {
	Type    string
	Target  string
	Payload any
}

// IssueFSMTransitionPayload captures an observability trace for one IssueOps
// state transition.
type IssueFSMTransitionPayload struct {
	From                   hermes.IssueState `json:"from"`
	Event                  hermes.IssueEvent `json:"event"`
	To                     hermes.IssueState `json:"to"`
	Reason                 string            `json:"reason,omitempty"`
	Source                 string            `json:"source,omitempty"`
	ChecklistTotal         int               `json:"checklist_total,omitempty"`
	CheckedCount           int               `json:"checked_count,omitempty"`
	UncheckedCount         int               `json:"unchecked_count,omitempty"`
	HasBlockingLabel       bool              `json:"has_blocking_label,omitempty"`
	HasRequiredLabel       bool              `json:"has_required_label,omitempty"`
	ReviewAccepted         bool              `json:"review_accepted,omitempty"`
	ValidationPassed       bool              `json:"validation_passed,omitempty"`
	ChecklistSynced        bool              `json:"checklist_synced,omitempty"`
	HasBodyChange          bool              `json:"has_body_change,omitempty"`
	NeedsHumanConfirmation bool              `json:"needs_human_confirmation,omitempty"`
	WouldWrite             bool              `json:"would_write,omitempty"`
	Wrote                  bool              `json:"wrote,omitempty"`
	DryRun                 bool              `json:"dry_run,omitempty"`
	CanAutoClose           bool              `json:"can_auto_close,omitempty"`
	RetryAction            string            `json:"retry_action,omitempty"`
}

// IssueFSMTransitionEvent builds a normalized runtime event for an IssueOps
// FSM transition trace.
func IssueFSMTransitionEvent(issue int, at time.Time, payload IssueFSMTransitionPayload) Event {
	if at.IsZero() {
		at = time.Now()
	}
	return Event{
		Type:      "IssueFSMTransition",
		Timestamp: at,
		Issue:     issue,
		Payload:   payload,
	}
}
