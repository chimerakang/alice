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
func matchFleetQuotaError(message string, patterns []*regexp.Regexp, layout string, location *time.Location, now time.Time) (bool, time.Time, error) {
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
		if reset.Year() == 0 {
			// A layout without date fields represents the next occurrence
			// of that local clock time. A partial date is ambiguous.
			probe := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
			if probe.Format(layout) != probe.AddDate(1, 1, 1).Format(layout) {
				return true, time.Time{}, fmt.Errorf("quota reset has no usable calendar year")
			}
			local := now.In(location)
			reset = time.Date(local.Year(), local.Month(), local.Day(),
				reset.Hour(), reset.Minute(), reset.Second(), reset.Nanosecond(), location)
			if !reset.After(now) {
				reset = reset.AddDate(0, 0, 1)
			}
		}
		return true, reset, nil
	}
	return false, time.Time{}, nil
}

// These allowances are Fleet-local estimates over a rolling window.
type fleetScoutQuotaConfig struct {
	MaxTransfers  int      `json:"max_transfers"`
	WindowMinutes int64    `json:"window_minutes"`
	ClaudeTokens  int64    `json:"claude_tokens"`
	CodexTokens   int64    `json:"codex_tokens"`
	ClaudeErrors  []string `json:"claude_error_patterns"`
	CodexErrors   []string `json:"codex_error_patterns"`
	ResetLayout   string   `json:"reset_layout"`
	ResetTimezone string   `json:"reset_timezone"`
}

func (cfg fleetScoutQuotaConfig) errorMatcher() (func(string, string) (bool, time.Time), error) {
	if cfg.MaxTransfers < 0 || cfg.WindowMinutes < 0 || cfg.WindowMinutes > int64(math.MaxInt64)/int64(time.Minute) ||
		cfg.ClaudeTokens < 0 || cfg.CodexTokens < 0 {
		return nil, fmt.Errorf("fleet.scout_quota: invalid transfer limit, window or token allowance")
	}
	layout := cfg.ResetLayout
	if layout == "" {
		layout = time.RFC3339
	}
	location := time.UTC
	if cfg.ResetTimezone != "" {
		var err error
		location, err = time.LoadLocation(cfg.ResetTimezone)
		if err != nil {
			return nil, fmt.Errorf("fleet.scout_quota.reset_timezone: %w", err)
		}
	}
	compiled := make(map[string][]*regexp.Regexp)
	for backend, patterns := range map[string][]string{fleetBackendClaude: cfg.ClaudeErrors, fleetBackendCodex: cfg.CodexErrors} {
		for _, pattern := range patterns {
			if strings.TrimSpace(pattern) == "" {
				return nil, fmt.Errorf("fleet.scout_quota: empty %s error pattern", backend)
			}
			expr, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("fleet.scout_quota: invalid %s error pattern: %w", backend, err)
			}
			compiled[backend] = append(compiled[backend], expr)
		}
	}
	return func(backend, diagnostic string) (bool, time.Time) {
		blocked, reset, _ := matchFleetQuotaError(diagnostic, compiled[backend], layout, location, time.Now())
		return blocked, reset
	}, nil
}

func (s *FleetService) estimatedBackendQuota(ctx context.Context, now time.Time) (map[string]float64, error) {
	cfg := s.Config.ScoutQuota
	remaining := make(map[string]float64, 2)
	if s.Store == nil || cfg.WindowMinutes <= 0 {
		return remaining, nil
	}
	allowance := map[string]int64{fleetBackendClaude: cfg.ClaudeTokens, fleetBackendCodex: cfg.CodexTokens}
	for backend, limit := range allowance {
		if limit > 0 {
			remaining[backend] = 1
		}
	}
	if len(remaining) == 0 {
		return remaining, nil
	}
	start := now.Add(-time.Duration(cfg.WindowMinutes) * time.Minute)
	err := s.Store.VisitBackendUsage(ctx, start, now, func(backend string, input, output int64, _ float64, _ int64) {
		if limit := allowance[backend]; limit > 0 {
			remaining[backend] = math.Max(0, 1-(float64(input)+float64(output))/float64(limit))
		}
	})
	return remaining, err
}

func (s *FleetService) preferScoutByQuota(ctx context.Context, choice fleetScoutBackendChoice) fleetScoutBackendChoice {
	if choice.Backend != fleetBackendCodex || choice.Reason != fleetReasonAuto || s.Store == nil {
		return choice
	}
	now := time.Now()
	var snapshots [2]fleetQuotaSnapshot
	var sources [2]string
	var estimates map[string]float64
	for i, backend := range []string{fleetBackendClaude, fleetBackendCodex} {
		blocked, reset, err := s.Store.BackendQuotaBlocked(ctx, backend)
		if err != nil {
			return choice
		}
		snapshots[i] = fleetQuotaSnapshot{Blocked: blocked, ResetsAt: reset}
		value, measurementReset, known, err := s.Store.BackendQuotaMeasurement(ctx, backend)
		if err == nil && known {
			snapshots[i].Remaining = &value
			sources[i] = "provider"
			if !blocked {
				snapshots[i].ResetsAt = measurementReset
			}
			continue
		}
		if estimates == nil {
			estimates, _ = s.estimatedBackendQuota(ctx, now)
		}
		if estimate, ok := estimates[backend]; ok {
			snapshots[i].Remaining = &estimate
			sources[i] = "estimate"
			if !blocked {
				snapshots[i].ResetsAt = time.Time{}
			}
		}
	}
	decision := routeWithQuota(now, choice.Backend, true, snapshots[0], snapshots[1])
	if decision.Backend == "" {
		return choice // the scheduler holds the task until a backend is available
	}
	if snapshots[0].Blocked || snapshots[1].Blocked {
		return fleetScoutBackendChoice{Backend: decision.Backend, Reason: "quota_available"}
	}
	if a, known := snapshots[0].remaining(now); known {
		if b, known := snapshots[1].remaining(now); known {
			return fleetScoutBackendChoice{Backend: decision.Backend, Reason: fmt.Sprintf(
				"quota_remaining: claude=%.0f%% (%s), codex=%.0f%% (%s)", 100*a, sources[0], 100*b, sources[1])}
		}
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
