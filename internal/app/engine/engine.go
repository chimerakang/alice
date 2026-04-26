package engine

import (
	"context"
	"time"
)

// ExecutionEngine runs a user goal through a concrete execution strategy.
type ExecutionEngine interface {
	Run(ctx context.Context, goal string, cc *ChatContext, prog ProgressSink) (Result, error)
	Name() string
}

// ProgressSink receives normalized execution progress events.
type ProgressSink interface {
	OnSubTaskStart(idx, total int, desc string)
	OnToolUse(tool string, input map[string]any)
	OnContent(kind, text string)
	OnSubTaskDone(idx int, result string)
	OnComplete(summary string)
}

// Artifact records a generated or modified file surfaced by an engine.
type Artifact struct {
	Path      string
	Hash      string
	SubTaskID string
}

// Result is the normalized output returned by an execution engine.
type Result struct {
	Text         string
	Artifacts    []Artifact
	Review       *ReviewResult
	InputTokens  int
	OutputTokens int
	Cost         float64
	Duration     time.Duration
}
