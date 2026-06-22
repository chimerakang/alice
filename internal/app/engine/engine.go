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
	Text                     string
	Artifacts                []Artifact
	Review                   *ReviewResult
	InputTokens              int
	OutputTokens             int
	Cost                     float64
	Duration                 time.Duration
	CacheReadInputTokens     int // session-cached prefix read on this call (#149)
	CacheCreationInputTokens int // new content written to cache on this call (#149)
	// Model is the actual model that produced Text (e.g. "claude-sonnet-4-5",
	// "gpt-5.5"). Empty when the runner can't report it. PlanExecuteEngine
	// uses this to attribute Executor tokens to the right model in the
	// per-model breakdown summary.
	Model string
}

// InputTokenVolume returns the full input volume seen by the model for this
// call: uncached input + cached prefix reads + newly-created cache tokens.
func (r Result) InputTokenVolume() int {
	return r.InputTokens + r.CacheReadInputTokens + r.CacheCreationInputTokens
}

// TokenVolume returns the total context volume for budget/reporting purposes.
func (r Result) TokenVolume() int {
	return r.InputTokenVolume() + r.OutputTokens
}

// DirectRunnerMetrics is implemented by DirectRunners that can report the
// model, token, and cost figures of their most recent Run() call. DirectEngine
// type-asserts to this interface and populates Result.Model + tokens + Cost
// when available — falling back to zero values for runners that don't
// implement it. Cost is the per-call USD figure (#148 1B/1D), not a session
// cumulative — Hermes summary sums it directly.
type DirectRunnerMetrics interface {
	LastCallMetrics() (model string, inTokens, outTokens int, cost float64)
}

// DirectRunnerCacheMetrics extends DirectRunnerMetrics with cache-token
// breakdown. Implemented by Agent.LastCacheMetrics so the engine can compute
// transcript size from cache_read + cache_write directly (#149 watermark
// fix). Optional — runners that don't implement it leave the cache fields
// at zero and walkingTokensSeen falls back to the legacy InputTokens-based
// estimate.
type DirectRunnerCacheMetrics interface {
	LastCacheMetrics() (cacheRead, cacheWrite int)
}
