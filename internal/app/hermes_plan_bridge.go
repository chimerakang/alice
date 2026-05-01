package app

import (
	"context"
	"strings"

	"claude-tg-agent/internal/app/hermes"
)

// makePlanFn adapts the app Client planner call to Hermes' planner interface.
// CostUSD comes from the CLI's total_cost_usd; under Claude Max subscription
// the CLI may report 0, in which case we fall back to a token×rate estimate
// using EstimateClaudeCost so per-call cost still flows into ModelUsage.
// See #148 1D + 1E.
func makePlanFn(client Client, model string) hermes.CallPlanFunc {
	return func(ctx context.Context, message, projectDir, sessionID string) (hermes.CallPlanResult, error) {
		var collected strings.Builder
		resp, callErr := client.CallPlan(ctx, message, projectDir, sessionID, model, func(contentType, t string) {
			if contentType == "text" {
				collected.WriteString(t)
			}
		})
		if callErr != nil {
			return hermes.CallPlanResult{}, callErr
		}
		text := collected.String()
		if text == "" {
			text = resp.TextContent
		}
		cost := resp.TotalCostUSD
		if cost <= 0 {
			cost = EstimateClaudeCost(
				model,
				resp.Usage.InputTokens,
				resp.Usage.CacheReadInputTokens,
				resp.Usage.CacheCreationInputTokens,
				resp.Usage.OutputTokens,
			)
		}
		return hermes.CallPlanResult{
			Text:         text,
			SessionID:    resp.SessionID,
			InputTokens:  resp.TotalInputTokensWithCache(),
			OutputTokens: resp.Usage.OutputTokens,
			CostUSD:      cost,
		}, nil
	}
}
