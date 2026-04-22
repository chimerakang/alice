package hermes

import (
	"context"
	"fmt"
	"strings"
)

// CallStreamFunc is the calling convention the Executor uses to invoke the CLI.
// Each call is a cold-start (no session reuse). Returns: result text, input tokens,
// output tokens, validation error (from hooks), error.
type CallStreamFunc func(
	ctx context.Context,
	prompt, projectDir string,
	onContent func(text string),
) (result string, inTokens, outTokens int, validationErr string, err error)

// ExecutorSession manages cold-start Executor CLI sessions.
// A new implicit CLI session is created for every sub-task.
type ExecutorSession struct {
	callFn     CallStreamFunc
	coreRules  string // prepended to every executor prompt (from #99)
	maxRetries int
}

// NewExecutorSession creates an Executor backed by callFn.
func NewExecutorSession(callFn CallStreamFunc, coreRules string, maxRetries int) *ExecutorSession {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &ExecutorSession{
		callFn:     callFn,
		coreRules:  coreRules,
		maxRetries: maxRetries,
	}
}

// ExecuteResult holds the outcome of one sub-task execution attempt.
type ExecuteResult struct {
	ResultText      string
	InputTokens     int
	OutputTokens    int
	ValidationError string // non-empty if a post-hook validator failed
	Attempts        int
}

// Execute runs the current sub-task from state, retrying on validation failure.
// Each attempt is a fresh cold-start Executor invocation.
func (e *ExecutorSession) Execute(ctx context.Context, state TaskState, projectDir string) (ExecuteResult, error) {
	var res ExecuteResult

	for attempt := 1; attempt <= e.maxRetries; attempt++ {
		res.Attempts = attempt

		// Build executor prompt from current state
		prompt := BuildExecutorPrompt(state, e.coreRules)
		if attempt > 1 {
			// Inject validation error feedback for retry
			prompt += fmt.Sprintf(
				"\n\nError from previous attempt %d:\n%s\n\nFix the issue and re-run.",
				attempt-1, res.ValidationError,
			)
		}

		var contentBuf strings.Builder
		resultText, inT, outT, valErr, err := e.callFn(ctx, prompt, projectDir, func(text string) {
			contentBuf.WriteString(text)
		})
		if err != nil {
			return res, fmt.Errorf("executor attempt %d: %w", attempt, err)
		}

		res.InputTokens += inT
		res.OutputTokens += outT
		if resultText == "" {
			resultText = strings.TrimSpace(contentBuf.String())
		}
		res.ResultText = resultText
		res.ValidationError = valErr

		if valErr == "" {
			// Success path — no validator failure
			return res, nil
		}
		// Validation failed — loop with feedback unless at last attempt
	}

	// All attempts exhausted; return last result with validation error set
	return res, nil
}
