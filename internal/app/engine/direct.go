package engine

import (
	"context"
	"time"
)

// DirectRunner is the legacy Agent.Run shape wrapped by DirectEngine.
type DirectRunner interface {
	Run(userMessage string, onUpdate func(string, bool)) (string, error)
}

// DirectEngine delegates execution to the existing direct Agent.Run path.
type DirectEngine struct {
	runner DirectRunner
}

func NewDirectEngine(runner DirectRunner) *DirectEngine {
	return &DirectEngine{runner: runner}
}

func (e *DirectEngine) Name() string {
	return "direct"
}

func (e *DirectEngine) Run(ctx context.Context, goal string, cc *ChatContext, prog ProgressSink) (Result, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return Result{Duration: time.Since(start)}, err
	}

	if prog != nil {
		prog.OnSubTaskStart(1, 1, goal)
	}

	text, err := e.runner.Run(goal, func(update string, silent bool) {
		if prog == nil || update == "" {
			return
		}
		if silent {
			prog.OnToolUse(update, nil)
			return
		}
		prog.OnContent("status", update)
	})

	result := Result{
		Text:     text,
		Duration: time.Since(start),
	}
	if err != nil {
		return result, err
	}

	if prog != nil {
		prog.OnSubTaskDone(1, text)
		prog.OnComplete(text)
	}
	return result, nil
}
