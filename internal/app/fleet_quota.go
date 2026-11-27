package app

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// fleetQuotaSnapshot summarizes one backend's current limiting window.
// A nil Remaining means its remaining proportion is unknown.
type fleetQuotaSnapshot struct {
	Remaining *float64
	Blocked   bool
	ResetsAt  time.Time
}

func (q fleetQuotaSnapshot) unavailable(now time.Time) bool {
	return q.Blocked && (q.ResetsAt.IsZero() || now.Before(q.ResetsAt))
}

func (q fleetQuotaSnapshot) remaining(now time.Time) (float64, bool) {
	if !q.ResetsAt.IsZero() && !now.Before(q.ResetsAt) {
		return 0, false
	}
	if q.Remaining == nil {
		return 0, false
	}
	v := *q.Remaining
	return v, v >= 0 && v <= 1 && !math.IsNaN(v)
}

type fleetQuotaDecision struct {
	Backend   string
	WaitUntil time.Time
}

// routeWithQuota considers only backends the task can actually use.
// An empty Backend means the task should wait, even if no reset is known.
func routeWithQuota(now time.Time, preferred string, codexEligible bool, claude, codex fleetQuotaSnapshot) fleetQuotaDecision {
	claudeBlocked := claude.unavailable(now)
	codexBlocked := !codexEligible || codex.unavailable(now)
	switch {
	case claudeBlocked && codexBlocked:
		next := claude.ResetsAt
		if codexEligible && !codex.ResetsAt.IsZero() && (next.IsZero() || codex.ResetsAt.Before(next)) {
			next = codex.ResetsAt
		}
		return fleetQuotaDecision{WaitUntil: next}
	case claudeBlocked:
		return fleetQuotaDecision{Backend: fleetBackendCodex}
	case codexBlocked:
		return fleetQuotaDecision{Backend: fleetBackendClaude}
	}
	a, aKnown := claude.remaining(now)
	b, bKnown := codex.remaining(now)
	if aKnown && bKnown && a != b {
		if b > a {
			return fleetQuotaDecision{Backend: fleetBackendCodex}
		}
		return fleetQuotaDecision{Backend: fleetBackendClaude}
	}
	if preferred == fleetBackendCodex {
		return fleetQuotaDecision{Backend: preferred}
	}
	return fleetQuotaDecision{Backend: fleetBackendClaude}
}

// matchFleetQuotaError matches configured CLI error patterns. An optional
// named reset group contains a timestamp in the configured Go time layout.
func matchFleetQuotaError(message string, patterns []*regexp.Regexp, layout string, location *time.Location) (bool, time.Time, error) {
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(message)
		if match == nil {
			continue
		}
		i := pattern.SubexpIndex("reset")
		if i < 0 || strings.TrimSpace(match[i]) == "" {
			return true, time.Time{}, nil
		}
		reset, err := time.ParseInLocation(layout, strings.TrimSpace(match[i]), location)
		if err != nil {
			return true, time.Time{}, fmt.Errorf("parse quota reset: %w", err)
		}
		return true, reset, nil
	}
	return false, time.Time{}, nil
}

// These allowances are Fleet-local estimates over a rolling window.
type fleetScoutQuotaConfig struct {
	MaxTransfers  int   `json:"max_transfers"`
	WindowMinutes int64 `json:"window_minutes"`
	ClaudeTokens  int64 `json:"claude_tokens"`
	CodexTokens   int64 `json:"codex_tokens"`
}

func (s *FleetService) preferScoutByEstimatedQuota(ctx context.Context, choice fleetScoutBackendChoice) fleetScoutBackendChoice {
	cfg := s.Config.ScoutQuota
	if choice.Backend != fleetBackendCodex || choice.Reason != fleetReasonAuto ||
		cfg.WindowMinutes <= 0 || cfg.WindowMinutes > int64((1<<63-1)/time.Minute) ||
		cfg.ClaudeTokens <= 0 || cfg.CodexTokens <= 0 {
		return choice
	}
	store, ok := any(s.Store).(fleetBackendUsageStore)
	if !ok || s.Store == nil {
		return choice
	}

	claudeRemaining, codexRemaining := 1.0, 1.0
	now := time.Now()
	start := now.Add(-time.Duration(cfg.WindowMinutes) * time.Minute)
	err := store.VisitBackendUsage(ctx, start, now, func(backend string, input, output int64, _ float64, _ int64) {
		used := float64(input) + float64(output)
		switch backend {
		case fleetBackendClaude:
			claudeRemaining = 1 - used/float64(cfg.ClaudeTokens)
		case fleetBackendCodex:
			codexRemaining = 1 - used/float64(cfg.CodexTokens)
		}
	})
	if err != nil {
		return choice
	}
	// Estimates only affect preference. Fleet-local observations do not
	// establish provider exhaustion and must not leave a task waiting.
	claudeRemaining = math.Max(0, claudeRemaining)
	codexRemaining = math.Max(0, codexRemaining)
	decision := routeWithQuota(now, choice.Backend, true,
		fleetQuotaSnapshot{Remaining: &claudeRemaining}, fleetQuotaSnapshot{Remaining: &codexRemaining})
	if decision.Backend == fleetBackendClaude {
		return fleetScoutBackendChoice{Backend: fleetBackendClaude, Reason: "estimated_quota"}
	}
	return choice
}

func (s *FleetService) scoutQuotaFallback(_ context.Context, task fleet.Task, backend string) (string, bool) {
	switch backend {
	case fleetBackendClaude:
		return task.Model, true
	case fleetBackendCodex:
		for _, tier := range fleetTierLadder {
			if task.Model == "" || s.Routing.fleetTierModel(tier) != task.Model {
				continue
			}
			choice := s.chooseScoutBackend(string(tier), false)
			return s.Routing.fleetCodexTierModel(tier), choice.Backend == fleetBackendCodex
		}
	}
	return "", false
}
