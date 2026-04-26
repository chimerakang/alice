package app

// hermes_bridge.go wires the existing CLIClient infrastructure to the hermes package's
// function-type interfaces (CallPlanFunc, CallStreamFunc), keeping the hermes package
// free of any dependency on the parent app package.

import (
	"context"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// makePlanFn returns a hermes.CallPlanFunc backed by the given Client and model.
// projectDir is ignored here — it comes from the call site via the function signature.
func makePlanFn(client Client, model string) hermes.CallPlanFunc {
	return func(ctx context.Context, message, projectDir string) (text, sessionID string, inTokens, outTokens int, err error) {
		var collected strings.Builder
		resp, callErr := client.CallPlan(ctx, message, projectDir, model, func(contentType, t string) {
			if contentType == "text" {
				collected.WriteString(t)
			}
		})
		if callErr != nil {
			return "", "", 0, 0, callErr
		}
		t := collected.String()
		if t == "" {
			t = resp.TextContent
		}
		return t, resp.SessionID, resp.Usage.InputTokens, resp.Usage.OutputTokens, nil
	}
}

// makeExecFn returns a hermes.CallStreamFunc backed by the given Client and model.
// Each invocation is a fresh CLI session (no sessionID reuse — Executor cold-starts).
func makeExecFn(client Client, model string) hermes.CallStreamFunc {
	return func(ctx context.Context, prompt, projectDir string, onContent func(string)) (result string, inTokens, outTokens int, validationErr string, err error) {
		var collected strings.Builder
		resp, callErr := client.CallStream(ctx, prompt, projectDir, "" /*no session*/, model,
			nil, // onToolUse: hooks run inside CLI; we don't intercept here
			func(contentType, t string) {
				if contentType == "text" {
					if onContent != nil {
						onContent(t)
					}
					collected.WriteString(t)
				}
			},
		)
		if callErr != nil {
			return "", 0, 0, "", callErr
		}
		text := collected.String()
		if text == "" {
			text = resp.TextContent
		}
		// validation_error is surfaced via resp.Result when a post-hook fails
		// (hooks attach it to the result; CLI returns it in the final text or error)
		// For now, check if the response marks itself as an error.
		var valErr string
		if resp.IsError && strings.Contains(resp.Result, "validation_error") {
			valErr = resp.Result
		}
		return text, resp.Usage.InputTokens, resp.Usage.OutputTokens, valErr, nil
	}
}
