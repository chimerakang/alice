package app

import (
	"testing"
)

// Issue #148 1B + 1D: recordCallCost must reset baseline on session change,
// fall back to a token×rate estimate when the CLI returned $0 (Max sub) or
// when the session-relative delta would be negative.
func TestProjectStateRecordCallCostSessionBoundary(t *testing.T) {
	ps := &projectState{}

	// Session A: cumulative 0.20 — first call so delta = full cumulative.
	respA1 := &CLIResponse{SessionID: "session-A", TotalCostUSD: 0.20}
	respA1.Usage.InputTokens = 100
	respA1.Usage.OutputTokens = 50
	if got := ps.recordCallCost(respA1, "claude-sonnet-4-6"); got != 0.20 {
		t.Fatalf("first call delta = %v, want 0.20", got)
	}

	// Same session, cumulative grows to 0.50 — delta is 0.30.
	respA2 := &CLIResponse{SessionID: "session-A", TotalCostUSD: 0.50}
	respA2.Usage.InputTokens = 200
	respA2.Usage.OutputTokens = 100
	if got := ps.recordCallCost(respA2, "claude-sonnet-4-6"); got != 0.30 {
		t.Fatalf("same-session delta = %v, want 0.30", got)
	}

	// New session B with smaller cumulative (0.10) — without the fix this
	// would produce delta=-0.40. With the fix, it must be 0.10.
	respB1 := &CLIResponse{SessionID: "session-B", TotalCostUSD: 0.10}
	respB1.Usage.InputTokens = 50
	respB1.Usage.OutputTokens = 25
	if got := ps.recordCallCost(respB1, "claude-sonnet-4-6"); got != 0.10 {
		t.Fatalf("new-session delta = %v, want 0.10 (no negative leak)", got)
	}
}

func TestProjectStateRecordCallCostMaxSubFallback(t *testing.T) {
	ps := &projectState{}

	// Max subscription: TotalCostUSD = 0 but token usage is real.
	resp := &CLIResponse{SessionID: "free-session", TotalCostUSD: 0}
	resp.Usage.InputTokens = 1000
	resp.Usage.CacheReadInputTokens = 50000
	resp.Usage.CacheCreationInputTokens = 200
	resp.Usage.OutputTokens = 500

	got := ps.recordCallCost(resp, "claude-sonnet-4-6")
	if got <= 0 {
		t.Fatalf("Max-sub fallback cost = %v, want > 0", got)
	}

	// Sonnet pricing: $3/M input, $15/M output. cache_read 0.1x, cache_create 1.25x.
	// weighted_input = 1000 + 50000*0.1 + 200*1.25 = 1000 + 5000 + 250 = 6250
	// cost = (6250*3 + 500*15) / 1e6 = (18750 + 7500) / 1e6 = 0.02625
	want := 0.02625
	const epsilon = 1e-6
	if got < want-epsilon || got > want+epsilon {
		t.Fatalf("fallback cost = %v, want %v (sonnet w/ cache discount)", got, want)
	}
}

func TestProjectStateRecordCallCostUnknownModelReturnsZero(t *testing.T) {
	ps := &projectState{}
	resp := &CLIResponse{SessionID: "x", TotalCostUSD: 0}
	resp.Usage.InputTokens = 100
	resp.Usage.OutputTokens = 50

	if got := ps.recordCallCost(resp, "unknown-model-xyz"); got != 0 {
		t.Fatalf("unknown model fallback = %v, want 0 (cannot price)", got)
	}
}

func TestEstimateClaudeCostFable52(t *testing.T) {
	for _, model := range []string{"claude-fable-5-2", "global.anthropic.claude-fable-5-2"} {
		// Per million tokens: $8 input + $0.20 cache read + $10 cache write + $40 output.
		if got := EstimateClaudeCost(model, 1_000_000, 1_000_000, 1_000_000, 1_000_000); got != 58.2 {
			t.Errorf("%s: estimated cost = %v, want 58.2", model, got)
		}
	}

	previous := ModelPricing["fable_5_2"]
	t.Cleanup(func() { ModelPricing["fable_5_2"] = previous })
	var config ModelPricingConfig
	config.Fable52.Input, config.Fable52.Output = 4, 20
	InitModelPricing(&config)
	if got := EstimateClaudeCost("claude-fable-5-2", 1_000_000, 0, 0, 1_000_000); got != 24 {
		t.Fatalf("configured estimate = %v, want 24", got)
	}
}
