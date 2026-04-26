package app

import (
	"context"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// makePlanFn adapts the app Client planner call to Hermes' planner interface.
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
